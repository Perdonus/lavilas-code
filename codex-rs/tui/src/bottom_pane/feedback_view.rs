use codex_feedback::feedback_diagnostics::FEEDBACK_DIAGNOSTICS_ATTACHMENT_FILENAME;
use codex_feedback::feedback_diagnostics::FeedbackDiagnostics;
use crossterm::event::KeyCode;
use crossterm::event::KeyEvent;
use crossterm::event::KeyModifiers;
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::text::Span;
use ratatui::widgets::Clear;
use ratatui::widgets::Paragraph;
use ratatui::widgets::StatefulWidgetRef;
use ratatui::widgets::Widget;
use std::cell::RefCell;

use crate::app_event::AppEvent;
use crate::app_event::FeedbackCategory;
use crate::app_event_sender::AppEventSender;
use crate::history_cell;
use crate::render::renderable::Renderable;

use super::CancellationEvent;
use super::bottom_pane_view::BottomPaneView;
use super::popup_consts::standard_popup_hint_line;
use super::textarea::TextArea;
use super::textarea::TextAreaState;

const BASE_CLI_BUG_ISSUE_URL: &str =
    "https://github.com/Perdonus/lavilas-code/issues/new?template=3-cli.yml";
/// Internal routing link for employee feedback follow-ups. This must not be shown to external users.
const CODEX_FEEDBACK_INTERNAL_URL: &str = "http://go/codex-feedback-internal";

/// The target audience for feedback follow-up instructions.
///
/// This is used strictly for messaging/links after feedback upload completes. It
/// must not change feedback upload behavior itself.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum FeedbackAudience {
    OpenAiEmployee,
    External,
}

/// Minimal input overlay to collect an optional feedback note, then submit it
/// through the app-server-managed feedback flow.
pub(crate) struct FeedbackNoteView {
    category: FeedbackCategory,
    app_event_tx: AppEventSender,
    include_logs: bool,

    // UI state
    textarea: TextArea,
    textarea_state: RefCell<TextAreaState>,
    complete: bool,
}

impl FeedbackNoteView {
    pub(crate) fn new(
        category: FeedbackCategory,
        app_event_tx: AppEventSender,
        include_logs: bool,
    ) -> Self {
        Self {
            category,
            app_event_tx,
            include_logs,
            textarea: TextArea::new(),
            textarea_state: RefCell::new(TextAreaState::default()),
            complete: false,
        }
    }

    fn submit(&mut self) {
        let note = self.textarea.text().trim().to_string();
        let reason = if note.is_empty() { None } else { Some(note) };
        self.app_event_tx.send(AppEvent::SubmitFeedback {
            category: self.category,
            reason,
            include_logs: self.include_logs,
        });
        self.complete = true;
    }
}

impl BottomPaneView for FeedbackNoteView {
    fn handle_key_event(&mut self, key_event: KeyEvent) {
        match key_event {
            KeyEvent {
                code: KeyCode::Esc, ..
            } => {
                self.on_ctrl_c();
            }
            KeyEvent {
                code: KeyCode::Enter,
                modifiers: KeyModifiers::NONE,
                ..
            } => {
                self.submit();
            }
            KeyEvent {
                code: KeyCode::Enter,
                ..
            } => {
                self.textarea.input(key_event);
            }
            other => {
                self.textarea.input(other);
            }
        }
    }

    fn on_ctrl_c(&mut self) -> CancellationEvent {
        self.complete = true;
        CancellationEvent::Handled
    }

    fn is_complete(&self) -> bool {
        self.complete
    }

    fn handle_paste(&mut self, pasted: String) -> bool {
        if pasted.is_empty() {
            return false;
        }
        self.textarea.insert_str(&pasted);
        true
    }
}

impl Renderable for FeedbackNoteView {
    fn desired_height(&self, width: u16) -> u16 {
        self.intro_lines(width).len() as u16 + self.input_height(width) + 2u16
    }

    fn cursor_pos(&self, area: Rect) -> Option<(u16, u16)> {
        if area.height < 2 || area.width <= 2 {
            return None;
        }
        let intro_height = self.intro_lines(area.width).len() as u16;
        let text_area_height = self.input_height(area.width).saturating_sub(1);
        if text_area_height == 0 {
            return None;
        }
        let textarea_rect = Rect {
            x: area.x.saturating_add(2),
            y: area.y.saturating_add(intro_height).saturating_add(1),
            width: area.width.saturating_sub(2),
            height: text_area_height,
        };
        let state = *self.textarea_state.borrow();
        self.textarea.cursor_pos_with_state(textarea_rect, state)
    }

    fn render(&self, area: Rect, buf: &mut Buffer) {
        if area.height == 0 || area.width == 0 {
            return;
        }

        let intro_lines = self.intro_lines(area.width);
        let (_, placeholder) = feedback_title_and_placeholder(self.category);
        let input_height = self.input_height(area.width);

        for (offset, line) in intro_lines.iter().enumerate() {
            Paragraph::new(line.clone()).render(
                Rect {
                    x: area.x,
                    y: area.y.saturating_add(offset as u16),
                    width: area.width,
                    height: 1,
                },
                buf,
            );
        }

        // Input line
        let input_area = Rect {
            x: area.x,
            y: area.y.saturating_add(intro_lines.len() as u16),
            width: area.width,
            height: input_height,
        };
        if input_area.width >= 2 {
            for row in 0..input_area.height {
                Paragraph::new(Line::from(vec![gutter()])).render(
                    Rect {
                        x: input_area.x,
                        y: input_area.y.saturating_add(row),
                        width: 2,
                        height: 1,
                    },
                    buf,
                );
            }

            let text_area_height = input_area.height.saturating_sub(1);
            if text_area_height > 0 {
                if input_area.width > 2 {
                    let blank_rect = Rect {
                        x: input_area.x.saturating_add(2),
                        y: input_area.y,
                        width: input_area.width.saturating_sub(2),
                        height: 1,
                    };
                    Clear.render(blank_rect, buf);
                }
                let textarea_rect = Rect {
                    x: input_area.x.saturating_add(2),
                    y: input_area.y.saturating_add(1),
                    width: input_area.width.saturating_sub(2),
                    height: text_area_height,
                };
                let mut state = self.textarea_state.borrow_mut();
                StatefulWidgetRef::render_ref(&(&self.textarea), textarea_rect, buf, &mut state);
                if self.textarea.text().is_empty() {
                    Paragraph::new(Line::from(placeholder.dim())).render(textarea_rect, buf);
                }
            }
        }

        let hint_blank_y = input_area.y.saturating_add(input_height);
        if hint_blank_y < area.y.saturating_add(area.height) {
            let blank_area = Rect {
                x: area.x,
                y: hint_blank_y,
                width: area.width,
                height: 1,
            };
            Clear.render(blank_area, buf);
        }

        let hint_y = hint_blank_y.saturating_add(1);
        if hint_y < area.y.saturating_add(area.height) {
            Paragraph::new(standard_popup_hint_line()).render(
                Rect {
                    x: area.x,
                    y: hint_y,
                    width: area.width,
                    height: 1,
                },
                buf,
            );
        }
    }
}

impl FeedbackNoteView {
    fn input_height(&self, width: u16) -> u16 {
        let usable_width = width.saturating_sub(2);
        let text_height = self.textarea.desired_height(usable_width).clamp(1, 8);
        text_height.saturating_add(1).min(9)
    }

    fn intro_lines(&self, _width: u16) -> Vec<Line<'static>> {
        let (title, _) = feedback_title_and_placeholder(self.category);
        vec![Line::from(vec![gutter(), title.bold()])]
    }
}

pub(crate) fn should_show_feedback_connectivity_details(
    category: FeedbackCategory,
    diagnostics: &FeedbackDiagnostics,
) -> bool {
    category != FeedbackCategory::GoodResult && !diagnostics.is_empty()
}

fn gutter() -> Span<'static> {
    "▌ ".cyan()
}

fn feedback_title_and_placeholder(category: FeedbackCategory) -> (String, String) {
    match category {
        FeedbackCategory::BadResult => (
            "Что пошло не так?".to_string(),
            "(необязательно) Опишите, почему ответ не устроил".to_string(),
        ),
        FeedbackCategory::GoodResult => (
            "Что понравилось?".to_string(),
            "(необязательно) Укажите, какие детали оказались полезными".to_string(),
        ),
        FeedbackCategory::Bug => (
            "Опишите ошибку".to_string(),
            "(необязательно) Расскажите, где и как повторяется сбой".to_string(),
        ),
        FeedbackCategory::SafetyCheck => (
            "Проверка безопасности?".to_string(),
            "(необязательно) Объясните, почему этот запрос должен пройти".to_string(),
        ),
        FeedbackCategory::Other => (
            "Что хотите сообщить?".to_string(),
            "(необязательно) Поделитесь идеей, замечанием или наблюдением".to_string(),
        ),
    }
}

pub(crate) fn feedback_classification(category: FeedbackCategory) -> &'static str {
    match category {
        FeedbackCategory::BadResult => "bad_result",
        FeedbackCategory::GoodResult => "good_result",
        FeedbackCategory::Bug => "bug",
        FeedbackCategory::SafetyCheck => "safety_check",
        FeedbackCategory::Other => "other",
    }
}

pub(crate) fn feedback_success_cell(
    category: FeedbackCategory,
    include_logs: bool,
    thread_id: &str,
    feedback_audience: FeedbackAudience,
) -> history_cell::PlainHistoryCell {
    let prefix = if include_logs {
        "• Отзыв отправлен."
    } else {
        "• Отзыв сохранен (без логов)."
    };
    let issue_url = issue_url_for_category(category, thread_id, feedback_audience);
    let mut lines = vec![Line::from(match issue_url.as_ref() {
        Some(_) if feedback_audience == FeedbackAudience::OpenAiEmployee => {
            format!("{prefix} Сообщите об этом в #codex-feedback:")
        }
        Some(_) => format!("{prefix} Создайте обращение по ссылке ниже:"),
        None => format!("{prefix} Спасибо за отзыв!"),
    })];
    match issue_url {
        Some(url) if feedback_audience == FeedbackAudience::OpenAiEmployee => {
            lines.extend([
                "".into(),
                Line::from(vec!["  ".into(), url.cyan().underlined()]),
                "".into(),
                Line::from("  Поделитесь этим и добавьте немного деталей о проблеме:"),
                Line::from(vec![
                    "    ".into(),
                    format!("https://go/codex-feedback/{thread_id}").bold(),
                ]),
            ]);
        }
        Some(url) => {
            lines.extend([
                "".into(),
                Line::from(vec!["  ".into(), url.cyan().underlined()]),
                "".into(),
                Line::from(vec![
                    "  Или укажите ID треда ".into(),
                    thread_id.to_string().bold(),
                    " в существующем обращении.".into(),
                ]),
            ]);
        }
        None => {
            lines.extend([
                "".into(),
                Line::from(vec!["  ID треда: ".into(), thread_id.to_string().bold()]),
            ]);
        }
    }
    history_cell::PlainHistoryCell::new(lines)
}

fn issue_url_for_category(
    category: FeedbackCategory,
    thread_id: &str,
    feedback_audience: FeedbackAudience,
) -> Option<String> {
    // Only certain categories provide a follow-up link. We intentionally keep
    // the external GitHub behavior identical while routing internal users to
    // the internal go link.
    match category {
        FeedbackCategory::Bug
        | FeedbackCategory::BadResult
        | FeedbackCategory::SafetyCheck
        | FeedbackCategory::Other => Some(match feedback_audience {
            FeedbackAudience::OpenAiEmployee => slack_feedback_url(thread_id),
            FeedbackAudience::External => {
                format!("{BASE_CLI_BUG_ISSUE_URL}&steps=Uploaded%20thread:%20{thread_id}")
            }
        }),
        FeedbackCategory::GoodResult => None,
    }
}

/// Build the internal follow-up URL.
///
/// We accept a `thread_id` so the call site stays symmetric with the external
/// path, but we currently point to a fixed channel without prefilling text.
fn slack_feedback_url(_thread_id: &str) -> String {
    CODEX_FEEDBACK_INTERNAL_URL.to_string()
}

// Build the selection popup params for feedback categories.
pub(crate) fn feedback_selection_params(
    app_event_tx: AppEventSender,
) -> super::SelectionViewParams {
    super::SelectionViewParams {
        title: Some("Как прошла сессия?".to_string()),
        items: vec![
            make_feedback_item(
                app_event_tx.clone(),
                "Ошибка",
                "Сбой, сообщение об ошибке, зависание или некорректное поведение интерфейса.",
                FeedbackCategory::Bug,
            ),
            make_feedback_item(
                app_event_tx.clone(),
                "Плохой результат",
                "Ответ был неточным, неполным или не помог.",
                FeedbackCategory::BadResult,
            ),
            make_feedback_item(
                app_event_tx.clone(),
                "Хороший результат",
                "Ответ оказался полезным, точным и понятным.",
                FeedbackCategory::GoodResult,
            ),
            make_feedback_item(
                app_event_tx.clone(),
                "Проверка безопасности",
                "Безопасный запрос блокируется системой безопасности или получает отказ.",
                FeedbackCategory::SafetyCheck,
            ),
            make_feedback_item(
                app_event_tx,
                "Другое",
                "Медленная работа, идея по функции, замечание по UX или другое наблюдение.",
                FeedbackCategory::Other,
            ),
        ],
        ..Default::default()
    }
}

/// Build the selection popup params shown when feedback is disabled.
pub(crate) fn feedback_disabled_params() -> super::SelectionViewParams {
    super::SelectionViewParams {
        title: Some("Сбор отзывов отключён".to_string()),
        subtitle: Some("Эта возможность отключена в настройках.".to_string()),
        footer_hint: Some(standard_popup_hint_line()),
        items: vec![super::SelectionItem {
            name: "Закрыть".to_string(),
            dismiss_on_select: true,
            ..Default::default()
        }],
        ..Default::default()
    }
}

fn make_feedback_item(
    app_event_tx: AppEventSender,
    name: &str,
    description: &str,
    category: FeedbackCategory,
) -> super::SelectionItem {
    let action: super::SelectionAction = Box::new(move |_sender: &AppEventSender| {
        app_event_tx.send(AppEvent::OpenFeedbackConsent { category });
    });
    super::SelectionItem {
        name: name.to_string(),
        description: Some(description.to_string()),
        actions: vec![action],
        dismiss_on_select: true,
        ..Default::default()
    }
}

/// Build the upload consent popup params for a given feedback category.
pub(crate) fn feedback_upload_consent_params(
    app_event_tx: AppEventSender,
    category: FeedbackCategory,
    rollout_path: Option<std::path::PathBuf>,
    feedback_diagnostics: &FeedbackDiagnostics,
) -> super::SelectionViewParams {
    use super::popup_consts::standard_popup_hint_line;
    let yes_action: super::SelectionAction = Box::new({
        let tx = app_event_tx.clone();
        move |sender: &AppEventSender| {
            let _ = sender;
            tx.send(AppEvent::OpenFeedbackNote {
                category,
                include_logs: true,
            });
        }
    });

    let no_action: super::SelectionAction = Box::new({
        let tx = app_event_tx;
        move |sender: &AppEventSender| {
            let _ = sender;
            tx.send(AppEvent::OpenFeedbackNote {
                category,
                include_logs: false,
            });
        }
    });

    // Build header listing files that would be sent if user consents.
    let mut header_lines: Vec<Box<dyn crate::render::renderable::Renderable>> = vec![
        Line::from("Поделиться логами?".bold()).into(),
        Line::from("").into(),
        Line::from("Будут отправлены файлы:".dim()).into(),
        Line::from(vec!["  • ".into(), "codex-logs.log".into()]).into(),
    ];
    if let Some(path) = rollout_path.as_deref()
        && let Some(name) = path.file_name().map(|s| s.to_string_lossy().to_string())
    {
        header_lines.push(Line::from(vec!["  • ".into(), name.into()]).into());
    }
    if !feedback_diagnostics.is_empty() {
        header_lines.push(
            Line::from(vec![
                "  • ".into(),
                FEEDBACK_DIAGNOSTICS_ATTACHMENT_FILENAME.into(),
            ])
            .into(),
        );
    }
    if should_show_feedback_connectivity_details(category, feedback_diagnostics) {
        header_lines.push(Line::from("").into());
        header_lines.push(Line::from("Диагностика подключения".bold()).into());
        for diagnostic in feedback_diagnostics.diagnostics() {
            header_lines
                .push(Line::from(vec!["  - ".into(), diagnostic.headline.clone().into()]).into());
            for detail in &diagnostic.details {
                header_lines.push(Line::from(vec!["    - ".dim(), detail.clone().into()]).into());
            }
        }
    }

    super::SelectionViewParams {
        footer_hint: Some(standard_popup_hint_line()),
        items: vec![
            super::SelectionItem {
                name: "Да".to_string(),
                description: Some(
                    "Поделитесь логами текущей сессии с командой для диагностики.".to_string(),
                ),
                actions: vec![yes_action],
                dismiss_on_select: true,
                ..Default::default()
            },
            super::SelectionItem {
                name: "Нет".to_string(),
                description: Some("Отправить сообщение без логов.".to_string()),
                actions: vec![no_action],
                dismiss_on_select: true,
                ..Default::default()
            },
        ],
        header: Box::new(crate::render::renderable::ColumnRenderable::with(
            header_lines,
        )),
        ..Default::default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::app_event::AppEvent;
    use crate::app_event_sender::AppEventSender;
    use codex_feedback::feedback_diagnostics::FeedbackDiagnostic;
    use pretty_assertions::assert_eq;

    fn render(view: &FeedbackNoteView, width: u16) -> String {
        let height = view.desired_height(width);
        let area = Rect::new(0, 0, width, height);
        let mut buf = Buffer::empty(area);
        view.render(area, &mut buf);

        let mut lines: Vec<String> = (0..area.height)
            .map(|row| {
                let mut line = String::new();
                for col in 0..area.width {
                    let symbol = buf[(area.x + col, area.y + row)].symbol();
                    if symbol.is_empty() {
                        line.push(' ');
                    } else {
                        line.push_str(symbol);
                    }
                }
                line.trim_end().to_string()
            })
            .collect();

        while lines.first().is_some_and(|l| l.trim().is_empty()) {
            lines.remove(0);
        }
        while lines.last().is_some_and(|l| l.trim().is_empty()) {
            lines.pop();
        }
        lines.join("\n")
    }

    fn render_cell(cell: &impl history_cell::HistoryCell, width: u16) -> String {
        cell.display_lines(width)
            .into_iter()
            .map(|line| {
                line.spans
                    .into_iter()
                    .map(|span| span.content.into_owned())
                    .collect::<String>()
                    .trim_end()
                    .to_string()
            })
            .collect::<Vec<_>>()
            .join("\n")
    }

    fn make_view(category: FeedbackCategory) -> FeedbackNoteView {
        let (tx_raw, _rx) = tokio::sync::mpsc::unbounded_channel::<AppEvent>();
        let tx = AppEventSender::new(tx_raw);
        FeedbackNoteView::new(category, tx, /*include_logs*/ true)
    }

    #[test]
    fn feedback_view_bad_result() {
        let view = make_view(FeedbackCategory::BadResult);
        let rendered = render(&view, /*width*/ 60);
        insta::assert_snapshot!("feedback_view_bad_result", rendered);
    }

    #[test]
    fn feedback_view_good_result() {
        let view = make_view(FeedbackCategory::GoodResult);
        let rendered = render(&view, /*width*/ 60);
        insta::assert_snapshot!("feedback_view_good_result", rendered);
    }

    #[test]
    fn feedback_view_bug() {
        let view = make_view(FeedbackCategory::Bug);
        let rendered = render(&view, /*width*/ 60);
        insta::assert_snapshot!("feedback_view_bug", rendered);
    }

    #[test]
    fn feedback_view_other() {
        let view = make_view(FeedbackCategory::Other);
        let rendered = render(&view, /*width*/ 60);
        insta::assert_snapshot!("feedback_view_other", rendered);
    }

    #[test]
    fn feedback_view_safety_check() {
        let view = make_view(FeedbackCategory::SafetyCheck);
        let rendered = render(&view, /*width*/ 60);
        insta::assert_snapshot!("feedback_view_safety_check", rendered);
    }

    #[test]
    fn feedback_view_with_connectivity_diagnostics() {
        let (tx_raw, _rx) = tokio::sync::mpsc::unbounded_channel::<AppEvent>();
        let tx = AppEventSender::new(tx_raw);
        let view = FeedbackNoteView::new(FeedbackCategory::Bug, tx, /*include_logs*/ false);
        let rendered = render(&view, /*width*/ 60);

        insta::assert_snapshot!("feedback_view_with_connectivity_diagnostics", rendered);
    }

    #[test]
    fn submit_feedback_emits_submit_event_with_trimmed_note() {
        let (tx_raw, mut rx) = tokio::sync::mpsc::unbounded_channel::<AppEvent>();
        let tx = AppEventSender::new(tx_raw);
        let mut view = FeedbackNoteView::new(FeedbackCategory::Bug, tx, /*include_logs*/ true);
        view.textarea.insert_str("  something broke  ");

        view.submit();

        let event = rx.try_recv().expect("submit feedback event");
        assert!(matches!(
            event,
            AppEvent::SubmitFeedback {
                category: FeedbackCategory::Bug,
                reason: Some(reason),
                include_logs: true,
            } if reason == "something broke"
        ));
        assert_eq!(view.is_complete(), true);
    }

    #[test]
    fn submit_feedback_omits_empty_note() {
        let (tx_raw, mut rx) = tokio::sync::mpsc::unbounded_channel::<AppEvent>();
        let tx = AppEventSender::new(tx_raw);
        let mut view = FeedbackNoteView::new(
            FeedbackCategory::GoodResult,
            tx,
            /*include_logs*/ false,
        );

        view.submit();

        let event = rx.try_recv().expect("submit feedback event");
        assert!(matches!(
            event,
            AppEvent::SubmitFeedback {
                category: FeedbackCategory::GoodResult,
                reason: None,
                include_logs: false,
            }
        ));
    }

    #[test]
    fn should_show_feedback_connectivity_details_only_for_non_good_result_with_diagnostics() {
        let diagnostics = FeedbackDiagnostics::new(vec![FeedbackDiagnostic {
            headline: "Proxy environment variables are set and may affect connectivity."
                .to_string(),
            details: vec!["HTTP_PROXY = http://proxy.example.com:8080".to_string()],
        }]);

        assert_eq!(
            should_show_feedback_connectivity_details(FeedbackCategory::Bug, &diagnostics),
            true
        );
        assert_eq!(
            should_show_feedback_connectivity_details(FeedbackCategory::GoodResult, &diagnostics),
            false
        );
        assert_eq!(
            should_show_feedback_connectivity_details(
                FeedbackCategory::BadResult,
                &FeedbackDiagnostics::default()
            ),
            false
        );
    }

    #[test]
    fn issue_url_available_for_bug_bad_result_safety_check_and_other() {
        let bug_url = issue_url_for_category(
            FeedbackCategory::Bug,
            "thread-1",
            FeedbackAudience::OpenAiEmployee,
        );
        let expected_slack_url = "http://go/codex-feedback-internal".to_string();
        assert_eq!(bug_url.as_deref(), Some(expected_slack_url.as_str()));

        let bad_result_url = issue_url_for_category(
            FeedbackCategory::BadResult,
            "thread-2",
            FeedbackAudience::OpenAiEmployee,
        );
        assert!(bad_result_url.is_some());

        let other_url = issue_url_for_category(
            FeedbackCategory::Other,
            "thread-3",
            FeedbackAudience::OpenAiEmployee,
        );
        assert!(other_url.is_some());

        let safety_check_url = issue_url_for_category(
            FeedbackCategory::SafetyCheck,
            "thread-4",
            FeedbackAudience::OpenAiEmployee,
        );
        assert!(safety_check_url.is_some());

        assert!(
            issue_url_for_category(
                FeedbackCategory::GoodResult,
                "t",
                FeedbackAudience::OpenAiEmployee
            )
            .is_none()
        );
        let bug_url_non_employee =
            issue_url_for_category(FeedbackCategory::Bug, "t", FeedbackAudience::External);
        let expected_external_url = "https://github.com/Perdonus/lavilas-code/issues/new?template=3-cli.yml&steps=Uploaded%20thread:%20t";
        assert_eq!(bug_url_non_employee.as_deref(), Some(expected_external_url));
    }

    #[test]
    fn feedback_success_cell_matches_external_bug_copy() {
        let rendered = render_cell(
            &feedback_success_cell(
                FeedbackCategory::Bug,
                /*include_logs*/ true,
                "thread-1",
                FeedbackAudience::External,
            ),
            /*width*/ 120,
        );
        assert_eq!(
            rendered,
            "• Отзыв отправлен. Создайте обращение по ссылке ниже:\n\n  https://github.com/Perdonus/lavilas-code/issues/new?template=3-cli.yml&steps=Uploaded%20thread:%20thread-1\n\n  Или укажите ID треда thread-1 в существующем обращении."
        );
    }

    #[test]
    fn feedback_success_cell_matches_employee_bug_copy() {
        let rendered = render_cell(
            &feedback_success_cell(
                FeedbackCategory::Bug,
                /*include_logs*/ true,
                "thread-2",
                FeedbackAudience::OpenAiEmployee,
            ),
            /*width*/ 120,
        );
        assert_eq!(
            rendered,
            "• Отзыв отправлен. Сообщите об этом в #codex-feedback:\n\n  http://go/codex-feedback-internal\n\n  Поделитесь этим и добавьте немного деталей о проблеме:\n    https://go/codex-feedback/thread-2"
        );
    }

    #[test]
    fn feedback_success_cell_matches_good_result_copy() {
        let rendered = render_cell(
            &feedback_success_cell(
                FeedbackCategory::GoodResult,
                /*include_logs*/ false,
                "thread-3",
                FeedbackAudience::External,
            ),
            /*width*/ 120,
        );
        assert_eq!(
            rendered,
            "• Отзыв сохранен (без логов). Спасибо за отзыв!\n\n  ID треда: thread-3"
        );
    }

    #[test]
    fn feedback_success_cell_uses_issue_links_for_remaining_categories() {
        for category in [
            FeedbackCategory::BadResult,
            FeedbackCategory::SafetyCheck,
            FeedbackCategory::Other,
        ] {
            let rendered = render_cell(
                &feedback_success_cell(
                    category,
                    /*include_logs*/ false,
                    "thread-4",
                    FeedbackAudience::External,
                ),
                /*width*/ 120,
            );
            assert!(rendered.contains("Создайте обращение по ссылке ниже:"));
            assert!(rendered.contains("thread-4"));
        }
    }
}
