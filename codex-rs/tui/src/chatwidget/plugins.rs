use std::path::PathBuf;
use std::time::Duration;
use std::time::Instant;

use super::ChatWidget;
use crate::app_event::AppEvent;
use crate::bottom_pane::ColumnWidthMode;
use crate::bottom_pane::SelectionItem;
use crate::bottom_pane::SelectionViewParams;
use crate::history_cell;
use crate::onboarding::mark_url_hyperlink;
use crate::render::renderable::ColumnRenderable;
use crate::render::renderable::Renderable;
use crate::shimmer::shimmer_spans;
use crate::tui::FrameRequester;
use codex_app_server_protocol::PluginDetail;
use codex_app_server_protocol::PluginInstallPolicy;
use codex_app_server_protocol::PluginInstallResponse;
use codex_app_server_protocol::PluginListResponse;
use codex_app_server_protocol::PluginMarketplaceEntry;
use codex_app_server_protocol::PluginReadResponse;
use codex_app_server_protocol::PluginSummary;
use codex_app_server_protocol::PluginUninstallResponse;
use codex_features::Feature;
use codex_utils_absolute_path::AbsolutePathBuf;
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::prelude::Widget;
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::widgets::Paragraph;
use ratatui::widgets::WidgetRef;
use ratatui::widgets::Wrap;

const PLUGINS_SELECTION_VIEW_ID: &str = "plugins-selection";
const LOADING_ANIMATION_DELAY: Duration = Duration::from_secs(1);
const LOADING_ANIMATION_INTERVAL: Duration = Duration::from_millis(100);

struct DelayedLoadingHeader {
    started_at: Instant,
    frame_requester: FrameRequester,
    animations_enabled: bool,
    loading_text: String,
    note: Option<String>,
}

impl DelayedLoadingHeader {
    fn new(
        frame_requester: FrameRequester,
        animations_enabled: bool,
        loading_text: String,
        note: Option<String>,
    ) -> Self {
        Self {
            started_at: Instant::now(),
            frame_requester,
            animations_enabled,
            loading_text,
            note,
        }
    }
}

impl Renderable for DelayedLoadingHeader {
    fn render(&self, area: Rect, buf: &mut Buffer) {
        if area.is_empty() {
            return;
        }

        let mut lines = Vec::with_capacity(3);
        lines.push(Line::from("Плагины".bold()));

        let now = Instant::now();
        let elapsed = now.saturating_duration_since(self.started_at);
        if elapsed < LOADING_ANIMATION_DELAY {
            self.frame_requester
                .schedule_frame_in(LOADING_ANIMATION_DELAY - elapsed);
            lines.push(Line::from(self.loading_text.as_str().dim()));
        } else if self.animations_enabled {
            self.frame_requester
                .schedule_frame_in(LOADING_ANIMATION_INTERVAL);
            lines.push(Line::from(shimmer_spans(self.loading_text.as_str())));
        } else {
            lines.push(Line::from(self.loading_text.as_str().dim()));
        }

        if let Some(note) = &self.note {
            lines.push(Line::from(note.as_str().dim()));
        }

        Paragraph::new(lines).render_ref(area, buf);
    }

    fn desired_height(&self, _width: u16) -> u16 {
        2 + u16::from(self.note.is_some())
    }
}

const APPS_HELP_ARTICLE_URL: &str = "https://help.openai.com/en/articles/11487775-apps-in-chatgpt";

struct PluginDisclosureLine {
    line: Line<'static>,
}

impl Renderable for PluginDisclosureLine {
    fn render(&self, area: Rect, buf: &mut Buffer) {
        Paragraph::new(self.line.clone())
            .wrap(Wrap { trim: false })
            .render(area, buf);
        mark_url_hyperlink(buf, area, APPS_HELP_ARTICLE_URL);
    }

    fn desired_height(&self, width: u16) -> u16 {
        Paragraph::new(self.line.clone())
            .wrap(Wrap { trim: false })
            .line_count(width)
            .try_into()
            .unwrap_or(u16::MAX)
    }
}

#[derive(Debug, Clone, Default)]
pub(super) enum PluginsCacheState {
    #[default]
    Uninitialized,
    Loading,
    Ready(PluginListResponse),
    Failed(String),
}

impl ChatWidget {
    pub(crate) fn add_plugins_output(&mut self) {
        if !self.config.features.enabled(Feature::Plugins) {
            self.add_info_message(
                "Плагины отключены.".to_string(),
                Some("Включите функцию плагинов, чтобы пользоваться /plugins.".to_string()),
            );
            return;
        }

        self.prefetch_plugins();

        match self.plugins_cache_for_current_cwd() {
            PluginsCacheState::Ready(response) => {
                self.open_plugins_popup(&response);
            }
            PluginsCacheState::Failed(err) => {
                self.add_to_history(history_cell::new_error_event(err));
            }
            PluginsCacheState::Loading | PluginsCacheState::Uninitialized => {
                self.open_plugins_loading_popup();
            }
        }
        self.request_redraw();
    }

    pub(crate) fn on_plugins_loaded(
        &mut self,
        cwd: PathBuf,
        result: Result<PluginListResponse, String>,
    ) {
        if self.plugins_fetch_state.in_flight_cwd.as_deref() == Some(cwd.as_path()) {
            self.plugins_fetch_state.in_flight_cwd = None;
        }

        if self.config.cwd.as_path() != cwd.as_path() {
            return;
        }

        let auth_flow_active = self.plugin_install_auth_flow.is_some();

        match result {
            Ok(response) => {
                self.plugins_fetch_state.cache_cwd = Some(cwd);
                self.plugins_cache = PluginsCacheState::Ready(response.clone());
                if !auth_flow_active {
                    self.refresh_plugins_popup_if_open(&response);
                }
            }
            Err(err) => {
                if !auth_flow_active {
                    self.plugins_fetch_state.cache_cwd = None;
                    self.plugins_cache = PluginsCacheState::Failed(err.clone());
                    let _ = self.bottom_pane.replace_selection_view_if_active(
                        PLUGINS_SELECTION_VIEW_ID,
                        self.plugins_error_popup_params(&err),
                    );
                }
            }
        }
    }

    fn prefetch_plugins(&mut self) {
        let cwd = self.config.cwd.to_path_buf();
        if self.plugins_fetch_state.in_flight_cwd.as_deref() == Some(cwd.as_path()) {
            return;
        }

        self.plugins_fetch_state.in_flight_cwd = Some(cwd.clone());
        if self.plugins_fetch_state.cache_cwd.as_deref() != Some(cwd.as_path()) {
            self.plugins_cache = PluginsCacheState::Loading;
        }

        self.app_event_tx.send(AppEvent::FetchPluginsList { cwd });
    }

    fn plugins_cache_for_current_cwd(&self) -> PluginsCacheState {
        if self.plugins_fetch_state.cache_cwd.as_deref() == Some(self.config.cwd.as_path()) {
            self.plugins_cache.clone()
        } else {
            PluginsCacheState::Uninitialized
        }
    }

    fn open_plugins_loading_popup(&mut self) {
        if !self.bottom_pane.replace_selection_view_if_active(
            PLUGINS_SELECTION_VIEW_ID,
            self.plugins_loading_popup_params(),
        ) {
            self.bottom_pane
                .show_selection_view(self.plugins_loading_popup_params());
        }
    }

    fn open_plugins_popup(&mut self, response: &PluginListResponse) {
        self.bottom_pane
            .show_selection_view(self.plugins_popup_params(response));
    }

    pub(crate) fn open_plugin_detail_loading_popup(&mut self, plugin_display_name: &str) {
        let params = self.plugin_detail_loading_popup_params(plugin_display_name);
        let _ = self
            .bottom_pane
            .replace_selection_view_if_active(PLUGINS_SELECTION_VIEW_ID, params);
    }

    pub(crate) fn open_plugin_install_loading_popup(&mut self, plugin_display_name: &str) {
        let params = self.plugin_install_loading_popup_params(plugin_display_name);
        let _ = self
            .bottom_pane
            .replace_selection_view_if_active(PLUGINS_SELECTION_VIEW_ID, params);
    }

    pub(crate) fn open_plugin_uninstall_loading_popup(&mut self, plugin_display_name: &str) {
        let params = self.plugin_uninstall_loading_popup_params(plugin_display_name);
        let _ = self
            .bottom_pane
            .replace_selection_view_if_active(PLUGINS_SELECTION_VIEW_ID, params);
    }

    pub(crate) fn on_plugin_detail_loaded(
        &mut self,
        cwd: PathBuf,
        result: Result<PluginReadResponse, String>,
    ) {
        if self.config.cwd.as_path() != cwd.as_path() {
            return;
        }

        let plugins_response = match self.plugins_cache_for_current_cwd() {
            PluginsCacheState::Ready(response) => Some(response),
            _ => None,
        };

        match result {
            Ok(response) => {
                if let Some(plugins_response) = plugins_response {
                    let _ = self.bottom_pane.replace_selection_view_if_active(
                        PLUGINS_SELECTION_VIEW_ID,
                        self.plugin_detail_popup_params(&plugins_response, &response.plugin),
                    );
                }
            }
            Err(err) => {
                let _ = self.bottom_pane.replace_selection_view_if_active(
                    PLUGINS_SELECTION_VIEW_ID,
                    self.plugin_detail_error_popup_params(&err, plugins_response.as_ref()),
                );
            }
        }
    }

    pub(crate) fn on_plugin_install_loaded(
        &mut self,
        cwd: PathBuf,
        _marketplace_path: AbsolutePathBuf,
        _plugin_name: String,
        plugin_display_name: String,
        result: Result<PluginInstallResponse, String>,
    ) -> bool {
        if self.config.cwd.as_path() != cwd.as_path() {
            return true;
        }

        match result {
            Ok(response) => {
                self.plugin_install_apps_needing_auth = response.apps_needing_auth;
                self.plugin_install_auth_flow = None;
                if self.plugin_install_apps_needing_auth.is_empty() {
                    self.add_info_message(
                        format!("Плагин {plugin_display_name} установлен."),
                        Some("Дополнительная авторизация приложений не нужна.".to_string()),
                    );
                    true
                } else {
                    let app_names = self
                        .plugin_install_apps_needing_auth
                        .iter()
                        .map(|app| app.name.as_str())
                        .collect::<Vec<_>>()
                        .join(", ");
                    self.add_info_message(
                        format!("Плагин {plugin_display_name} установлен."),
                        Some(format!(
                            "Ещё нужна авторизация для приложений ({}) : {app_names}",
                            self.plugin_install_apps_needing_auth.len()
                        )),
                    );
                    self.plugin_install_auth_flow = Some(super::PluginInstallAuthFlowState {
                        plugin_display_name,
                        next_app_index: 0,
                    });
                    self.open_plugin_install_auth_popup();
                    false
                }
            }
            Err(err) => {
                self.plugin_install_apps_needing_auth.clear();
                self.plugin_install_auth_flow = None;
                let plugins_response = match self.plugins_cache_for_current_cwd() {
                    PluginsCacheState::Ready(response) => Some(response),
                    _ => None,
                };
                let _ = self.bottom_pane.replace_selection_view_if_active(
                    PLUGINS_SELECTION_VIEW_ID,
                    self.plugin_detail_error_popup_params(&err, plugins_response.as_ref()),
                );
                true
            }
        }
    }

    pub(crate) fn on_plugin_uninstall_loaded(
        &mut self,
        cwd: PathBuf,
        plugin_display_name: String,
        result: Result<PluginUninstallResponse, String>,
    ) {
        if self.config.cwd.as_path() != cwd.as_path() {
            return;
        }

        match result {
            Ok(_response) => {
                self.plugin_install_apps_needing_auth.clear();
                self.plugin_install_auth_flow = None;
                self.add_info_message(
                    format!("Плагин {plugin_display_name} удалён."),
                    Some("Связанные приложения останутся установленными.".to_string()),
                );
            }
            Err(err) => {
                let plugins_response = match self.plugins_cache_for_current_cwd() {
                    PluginsCacheState::Ready(response) => Some(response),
                    _ => None,
                };
                let _ = self.bottom_pane.replace_selection_view_if_active(
                    PLUGINS_SELECTION_VIEW_ID,
                    self.plugin_detail_error_popup_params(&err, plugins_response.as_ref()),
                );
            }
        }
    }

    pub(crate) fn advance_plugin_install_auth_flow(&mut self) {
        let should_finish = {
            let Some(flow) = self.plugin_install_auth_flow.as_mut() else {
                return;
            };
            flow.next_app_index += 1;
            flow.next_app_index >= self.plugin_install_apps_needing_auth.len()
        };

        if should_finish {
            self.finish_plugin_install_auth_flow(/*abandoned*/ false);
            return;
        }

        self.open_plugin_install_auth_popup();
    }

    pub(crate) fn abandon_plugin_install_auth_flow(&mut self) {
        self.finish_plugin_install_auth_flow(/*abandoned*/ true);
    }

    fn open_plugin_install_auth_popup(&mut self) {
        let Some(params) = self.plugin_install_auth_popup_params() else {
            self.finish_plugin_install_auth_flow(/*abandoned*/ false);
            return;
        };
        if !self
            .bottom_pane
            .replace_selection_view_if_active(PLUGINS_SELECTION_VIEW_ID, params)
            && let Some(params) = self.plugin_install_auth_popup_params()
        {
            self.bottom_pane.show_selection_view(params);
        }
    }

    fn plugin_install_auth_popup_params(&self) -> Option<SelectionViewParams> {
        let flow = self.plugin_install_auth_flow.as_ref()?;
        let app = self
            .plugin_install_apps_needing_auth
            .get(flow.next_app_index)?;
        let total = self.plugin_install_apps_needing_auth.len();
        let current = flow.next_app_index + 1;
        let is_installed = self.plugin_install_auth_app_is_installed(app.id.as_str());
        let status_label = if is_installed {
            "В этой сессии уже установлено."
        } else {
            "Чтобы продолжить, установите нужные приложения в ChatGPT:"
        };
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from(
            format!("Плагин {} установлен.", flow.plugin_display_name).bold(),
        ));
        header.push(Line::from(
            format!("Настройка приложения {current}/{total}: {}", app.name).dim(),
        ));
        header.push(Line::from(status_label.dim()));

        let mut items = Vec::new();

        if let Some(install_url) = app.install_url.clone() {
            let install_label = if is_installed {
                "Открыть в ChatGPT"
            } else {
                "Установить в ChatGPT"
            };
            items.push(SelectionItem {
                name: install_label.to_string(),
                description: Some("Открыть страницу приложения в ChatGPT.".to_string()),
                selected_description: Some("Откроет страницу приложения в браузере.".to_string()),
                actions: vec![Box::new(move |tx| {
                    tx.send(AppEvent::OpenUrlInBrowser {
                        url: install_url.clone(),
                    });
                })],
                ..Default::default()
            });
        } else {
            items.push(SelectionItem {
                name: "Ссылка на приложение недоступна".to_string(),
                description: Some("У приложения нет ссылки на установку или управление.".to_string()),
                is_disabled: true,
                ..Default::default()
            });
        }

        if is_installed {
            items.push(SelectionItem {
                name: "Продолжить".to_string(),
                description: Some("Это приложение уже установлено.".to_string()),
                selected_description: Some("Перейти к следующему приложению.".to_string()),
                actions: vec![Box::new(|tx| {
                    tx.send(AppEvent::PluginInstallAuthAdvance {
                        refresh_connectors: false,
                    });
                })],
                ..Default::default()
            });
        } else {
            items.push(SelectionItem {
                name: "Уже установил".to_string(),
                description: Some("Подтвердить установку и перейти дальше.".to_string()),
                selected_description: Some("Продолжить без ожидания обновления списка.".to_string()),
                actions: vec![Box::new(|tx| {
                    tx.send(AppEvent::PluginInstallAuthAdvance {
                        refresh_connectors: true,
                    });
                })],
                ..Default::default()
            });
        }

        items.push(SelectionItem {
            name: "Пропустить остальную настройку".to_string(),
            description: Some("Остановить этот этап для текущего плагина.".to_string()),
            selected_description: Some("Прервать оставшуюся обязательную настройку.".to_string()),
            actions: vec![Box::new(|tx| {
                tx.send(AppEvent::PluginInstallAuthAbandon);
            })],
            ..Default::default()
        });

        Some(SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            footer_hint: Some(plugins_popup_hint_line()),
            items,
            col_width_mode: ColumnWidthMode::AutoAllRows,
            ..Default::default()
        })
    }

    fn plugin_install_auth_app_is_installed(&self, app_id: &str) -> bool {
        self.connectors_for_mentions().is_some_and(|connectors| {
            connectors
                .iter()
                .any(|connector| connector.id == app_id && connector.is_accessible)
        })
    }

    fn finish_plugin_install_auth_flow(&mut self, abandoned: bool) {
        let Some(flow) = self.plugin_install_auth_flow.take() else {
            return;
        };
        self.plugin_install_apps_needing_auth.clear();
        if abandoned {
            self.add_info_message(
                format!(
                    "Оставшаяся настройка плагина {} пропущена.",
                    flow.plugin_display_name
                ),
                Some("Плагин может не заработать, пока не будут установлены нужные приложения.".to_string()),
            );
        } else {
            self.add_info_message(
                format!(
                    "Настройка приложений для плагина {} завершена.",
                    flow.plugin_display_name
                ),
                Some("Дальше плагином можно управлять через /plugins.".to_string()),
            );
        }

        let plugins_response = match self.plugins_cache_for_current_cwd() {
            PluginsCacheState::Ready(response) => Some(response),
            _ => None,
        };
        if let Some(plugins_response) = plugins_response {
            let _ = self.bottom_pane.replace_selection_view_if_active(
                PLUGINS_SELECTION_VIEW_ID,
                self.plugins_popup_params(&plugins_response),
            );
        }
    }

    fn refresh_plugins_popup_if_open(&mut self, response: &PluginListResponse) {
        let _ = self.bottom_pane.replace_selection_view_if_active(
            PLUGINS_SELECTION_VIEW_ID,
            self.plugins_popup_params(response),
        );
    }

    fn plugins_loading_popup_params(&self) -> SelectionViewParams {
        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(DelayedLoadingHeader::new(
                self.frame_requester.clone(),
                self.config.animations,
                "Загружаю список плагинов...".to_string(),
                Some("Пока показывается только маркетплейс ChatGPT.".to_string()),
            )),
            items: vec![SelectionItem {
                name: "Загрузка плагинов...".to_string(),
                description: Some("Список обновится, когда маркетплейс ответит.".to_string()),
                is_disabled: true,
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn plugin_detail_loading_popup_params(&self, plugin_display_name: &str) -> SelectionViewParams {
        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(DelayedLoadingHeader::new(
                self.frame_requester.clone(),
                self.config.animations,
                format!("Загружаю детали плагина {plugin_display_name}..."),
                /*note*/ None,
            )),
            items: vec![SelectionItem {
                name: "Загрузка деталей плагина...".to_string(),
                description: Some("Карточка обновится, когда детали загрузятся.".to_string()),
                is_disabled: true,
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn plugin_install_loading_popup_params(
        &self,
        plugin_display_name: &str,
    ) -> SelectionViewParams {
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from(
            format!("Устанавливаю {plugin_display_name}...").dim(),
        ));

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            items: vec![SelectionItem {
                name: "Установка плагина...".to_string(),
                description: Some("Экран обновится после завершения установки.".to_string()),
                is_disabled: true,
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn plugin_uninstall_loading_popup_params(
        &self,
        plugin_display_name: &str,
    ) -> SelectionViewParams {
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from(
            format!("Удаляю {plugin_display_name}...").dim(),
        ));

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            items: vec![SelectionItem {
                name: "Удаление плагина...".to_string(),
                description: Some("Экран обновится после завершения удаления.".to_string()),
                is_disabled: true,
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn plugins_error_popup_params(&self, err: &str) -> SelectionViewParams {
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from("Не удалось загрузить плагины.".dim()));

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            items: vec![SelectionItem {
                name: "Маркетплейс плагинов недоступен".to_string(),
                description: Some(err.to_string()),
                is_disabled: true,
                ..Default::default()
            }],
            ..Default::default()
        }
    }

    fn plugin_detail_error_popup_params(
        &self,
        err: &str,
        plugins_response: Option<&PluginListResponse>,
    ) -> SelectionViewParams {
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from("Не удалось загрузить детали плагина.".dim()));

        let mut items = vec![SelectionItem {
            name: "Детали плагина недоступны".to_string(),
            description: Some(err.to_string()),
            is_disabled: true,
            ..Default::default()
        }];
        if let Some(plugins_response) = plugins_response.cloned() {
            let cwd = self.config.cwd.to_path_buf();
            items.push(SelectionItem {
                name: "Назад к плагинам".to_string(),
                description: Some("Вернуться к списку плагинов.".to_string()),
                selected_description: Some("Вернуться к списку плагинов.".to_string()),
                actions: vec![Box::new(move |tx| {
                    tx.send(AppEvent::PluginsLoaded {
                        cwd: cwd.clone(),
                        result: Ok(plugins_response.clone()),
                    });
                })],
                ..Default::default()
            });
        }

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            footer_hint: Some(plugins_popup_hint_line()),
            items,
            ..Default::default()
        }
    }

    fn plugins_popup_params(&self, response: &PluginListResponse) -> SelectionViewParams {
        let marketplaces: Vec<&PluginMarketplaceEntry> = response.marketplaces.iter().collect();

        let total: usize = marketplaces
            .iter()
            .map(|marketplace| marketplace.plugins.len())
            .sum();
        let installed = marketplaces
            .iter()
            .flat_map(|marketplace| marketplace.plugins.iter())
            .filter(|plugin| plugin.installed)
            .count();

        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from(
            "Просматривайте плагины из доступных маркетплейсов.".dim(),
        ));
        header.push(Line::from(
            format!("Установлено {installed} из {total} доступных плагинов.").dim(),
        ));
        if let Some(remote_sync_error) = response.remote_sync_error.as_deref() {
            header.push(Line::from(
                format!("Показываю кэшированные данные маркетплейса: {remote_sync_error}").dim(),
            ));
        }

        let mut plugin_entries: Vec<(&PluginMarketplaceEntry, &PluginSummary, String)> =
            marketplaces
                .iter()
                .flat_map(|marketplace| {
                    marketplace
                        .plugins
                        .iter()
                        .map(move |plugin| (*marketplace, plugin, plugin_display_name(plugin)))
                })
                .collect();
        plugin_entries.sort_by(|left, right| {
            right
                .1
                .installed
                .cmp(&left.1.installed)
                .then_with(|| {
                    left.2
                        .to_ascii_lowercase()
                        .cmp(&right.2.to_ascii_lowercase())
                })
                .then_with(|| left.2.cmp(&right.2))
                .then_with(|| left.1.name.cmp(&right.1.name))
                .then_with(|| left.1.id.cmp(&right.1.id))
        });
        let status_label_width = plugin_entries
            .iter()
            .map(|(_, plugin, _)| plugin_status_label(plugin).chars().count())
            .max()
            .unwrap_or(0);

        let mut items: Vec<SelectionItem> = Vec::new();
        for (marketplace, plugin, display_name) in plugin_entries {
            let marketplace_label = marketplace_display_name(marketplace);
            let status_label = plugin_status_label(plugin);
            let description =
                plugin_brief_description(plugin, &marketplace_label, status_label_width);
            let selected_status_label = format!("{status_label:<status_label_width$}");
            let selected_description =
                format!("{selected_status_label}   Enter открывает карточку плагина.");
            let search_value = format!(
                "{display_name} {} {} {}",
                plugin.id, plugin.name, marketplace_label
            );
            let cwd = self.config.cwd.to_path_buf();
            let plugin_display_name = display_name.clone();
            let marketplace_path = marketplace.path.clone();
            let plugin_name = plugin.name.clone();

            items.push(SelectionItem {
                name: display_name,
                description: Some(description),
                selected_description: Some(selected_description),
                search_value: Some(search_value),
                actions: vec![Box::new(move |tx| {
                    tx.send(AppEvent::OpenPluginDetailLoading {
                        plugin_display_name: plugin_display_name.clone(),
                    });
                    tx.send(AppEvent::FetchPluginDetail {
                        cwd: cwd.clone(),
                        params: codex_app_server_protocol::PluginReadParams {
                            marketplace_path: marketplace_path.clone(),
                            plugin_name: plugin_name.clone(),
                        },
                    });
                })],
                ..Default::default()
            });
        }

        if items.is_empty() {
            items.push(SelectionItem {
                name: "Плагины не найдены".to_string(),
                description: Some(
                    "В найденных маркетплейсах пока нет доступных плагинов.".to_string(),
                ),
                is_disabled: true,
                ..Default::default()
            });
        }

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            footer_hint: Some(plugins_popup_hint_line()),
            items,
            is_searchable: true,
            search_placeholder: Some("Начните вводить для поиска по плагинам".to_string()),
            col_width_mode: ColumnWidthMode::AutoAllRows,
            ..Default::default()
        }
    }

    fn plugin_detail_popup_params(
        &self,
        plugins_response: &PluginListResponse,
        plugin: &PluginDetail,
    ) -> SelectionViewParams {
        let marketplace_label = plugin.marketplace_name.clone();
        let display_name = plugin_display_name(&plugin.summary);
        let detail_status_label = if plugin.summary.installed {
            if plugin.summary.enabled {
                "Установлен"
            } else {
                "Установлен · Отключен"
            }
        } else {
            match plugin.summary.install_policy {
                PluginInstallPolicy::NotAvailable => "Недоступен для установки",
                PluginInstallPolicy::Available => "Можно установить",
                PluginInstallPolicy::InstalledByDefault => "Доступен по умолчанию",
            }
        };
        let mut header = ColumnRenderable::new();
        header.push(Line::from("Плагины".bold()));
        header.push(Line::from(
            format!("{display_name} · {detail_status_label} · {marketplace_label}").bold(),
        ));
        if !plugin.summary.installed {
            header.push(PluginDisclosureLine {
                line: Line::from(vec![
                    "Данные, переданные этому приложению, регулируются его ".into(),
                    "условиями использования".bold(),
                    " и ".into(),
                    "политикой конфиденциальности".bold(),
                    ". ".into(),
                    "Подробнее".cyan().underlined(),
                    ".".into(),
                ]),
            });
        }
        if let Some(description) = plugin_detail_description(plugin) {
            header.push(Line::from(description.dim()));
        }

        let cwd = self.config.cwd.to_path_buf();
        let plugins_response = plugins_response.clone();
        let mut items = vec![SelectionItem {
            name: "Назад к плагинам".to_string(),
            description: Some("Вернуться к списку плагинов.".to_string()),
            selected_description: Some("Вернуться к списку плагинов.".to_string()),
            actions: vec![Box::new(move |tx| {
                tx.send(AppEvent::PluginsLoaded {
                    cwd: cwd.clone(),
                    result: Ok(plugins_response.clone()),
                });
            })],
            ..Default::default()
        }];

        if plugin.summary.installed {
            let uninstall_cwd = self.config.cwd.to_path_buf();
            let plugin_id = plugin.summary.id.clone();
            let plugin_display_name = display_name;
            items.push(SelectionItem {
                name: "Удалить плагин".to_string(),
                description: Some("Удалить этот плагин сейчас.".to_string()),
                selected_description: Some("Удалить этот плагин сейчас.".to_string()),
                actions: vec![Box::new(move |tx| {
                    tx.send(AppEvent::OpenPluginUninstallLoading {
                        plugin_display_name: plugin_display_name.clone(),
                    });
                    tx.send(AppEvent::FetchPluginUninstall {
                        cwd: uninstall_cwd.clone(),
                        plugin_id: plugin_id.clone(),
                        plugin_display_name: plugin_display_name.clone(),
                    });
                })],
                ..Default::default()
            });
        } else if plugin.summary.install_policy == PluginInstallPolicy::NotAvailable {
            items.push(SelectionItem {
                name: "Установить плагин".to_string(),
                description: Some("Этот плагин нельзя установить из текущего маркетплейса.".to_string()),
                is_disabled: true,
                ..Default::default()
            });
        } else {
            let install_cwd = self.config.cwd.to_path_buf();
            let marketplace_path = plugin.marketplace_path.clone();
            let plugin_name = plugin.summary.name.clone();
            let plugin_display_name = display_name;
            items.push(SelectionItem {
                name: "Установить плагин".to_string(),
                description: Some("Установить этот плагин сейчас.".to_string()),
                selected_description: Some("Установить этот плагин сейчас.".to_string()),
                actions: vec![Box::new(move |tx| {
                    tx.send(AppEvent::OpenPluginInstallLoading {
                        plugin_display_name: plugin_display_name.clone(),
                    });
                    tx.send(AppEvent::FetchPluginInstall {
                        cwd: install_cwd.clone(),
                        marketplace_path: marketplace_path.clone(),
                        plugin_name: plugin_name.clone(),
                        plugin_display_name: plugin_display_name.clone(),
                    });
                })],
                ..Default::default()
            });
        }

        items.push(SelectionItem {
            name: "Навыки".to_string(),
            description: Some(plugin_skill_summary(plugin)),
            is_disabled: true,
            ..Default::default()
        });
        items.push(SelectionItem {
            name: "Приложения".to_string(),
            description: Some(plugin_app_summary(plugin)),
            is_disabled: true,
            ..Default::default()
        });
        items.push(SelectionItem {
            name: "MCP-серверы".to_string(),
            description: Some(plugin_mcp_summary(plugin)),
            is_disabled: true,
            ..Default::default()
        });

        SelectionViewParams {
            view_id: Some(PLUGINS_SELECTION_VIEW_ID),
            header: Box::new(header),
            footer_hint: Some(plugins_popup_hint_line()),
            items,
            col_width_mode: ColumnWidthMode::AutoAllRows,
            ..Default::default()
        }
    }
}

fn plugins_popup_hint_line() -> Line<'static> {
    Line::from("Esc закрывает окно.")
}

fn marketplace_display_name(marketplace: &PluginMarketplaceEntry) -> String {
    marketplace
        .interface
        .as_ref()
        .and_then(|interface| interface.display_name.as_deref())
        .map(str::trim)
        .filter(|display_name| !display_name.is_empty())
        .map(str::to_string)
        .unwrap_or_else(|| marketplace.name.clone())
}

fn plugin_display_name(plugin: &PluginSummary) -> String {
    plugin
        .interface
        .as_ref()
        .and_then(|interface| interface.display_name.as_deref())
        .map(str::trim)
        .filter(|display_name| !display_name.is_empty())
        .map(str::to_string)
        .unwrap_or_else(|| plugin.name.clone())
}

fn plugin_brief_description(
    plugin: &PluginSummary,
    marketplace_label: &str,
    status_label_width: usize,
) -> String {
    let status_label = plugin_status_label(plugin);
    let status_label = format!("{status_label:<status_label_width$}");
    match plugin_description(plugin) {
        Some(description) => format!("{status_label} · {marketplace_label} · {description}"),
        None => format!("{status_label} · {marketplace_label}"),
    }
}

fn plugin_status_label(plugin: &PluginSummary) -> &'static str {
    if plugin.installed {
        if plugin.enabled {
            "Установлен"
        } else {
            "Установлен · Отключен"
        }
    } else {
        match plugin.install_policy {
            PluginInstallPolicy::NotAvailable => "Недоступен для установки",
            PluginInstallPolicy::Available => "Доступен",
            PluginInstallPolicy::InstalledByDefault => "Доступен по умолчанию",
        }
    }
}

fn plugin_description(plugin: &PluginSummary) -> Option<String> {
    plugin
        .interface
        .as_ref()
        .and_then(|interface| {
            interface
                .short_description
                .as_deref()
                .or(interface.long_description.as_deref())
        })
        .map(str::trim)
        .filter(|description| !description.is_empty())
        .map(str::to_string)
}

fn plugin_detail_description(plugin: &PluginDetail) -> Option<String> {
    plugin
        .description
        .as_deref()
        .or_else(|| {
            plugin
                .summary
                .interface
                .as_ref()
                .and_then(|interface| interface.long_description.as_deref())
        })
        .or_else(|| {
            plugin
                .summary
                .interface
                .as_ref()
                .and_then(|interface| interface.short_description.as_deref())
        })
        .map(str::trim)
        .filter(|description| !description.is_empty())
        .map(str::to_string)
}

fn plugin_skill_summary(plugin: &PluginDetail) -> String {
    if plugin.skills.is_empty() {
        "У плагина нет навыков.".to_string()
    } else {
        plugin
            .skills
            .iter()
            .map(|skill| skill.name.as_str())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

fn plugin_app_summary(plugin: &PluginDetail) -> String {
    if plugin.apps.is_empty() {
        "У плагина нет приложений.".to_string()
    } else {
        plugin
            .apps
            .iter()
            .map(|app| app.name.as_str())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

fn plugin_mcp_summary(plugin: &PluginDetail) -> String {
    if plugin.mcp_servers.is_empty() {
        "У плагина нет MCP-серверов.".to_string()
    } else {
        plugin.mcp_servers.join(", ")
    }
}
