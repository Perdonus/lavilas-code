use crossterm::event::MouseButton;
use crossterm::event::MouseEvent;
use crossterm::event::MouseEventKind;
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::widgets::WidgetRef;
use codex_core::config::find_codex_home;

use super::popup_consts::MAX_POPUP_ROWS;
use super::scroll_state::ScrollState;
use super::selection_popup_common::ColumnWidthMode;
use super::selection_popup_common::GenericDisplayRow;
use super::selection_popup_common::render_rows;
use super::selection_popup_common::visible_row_line_counts;
use super::slash_commands;
use crate::bottom_pane::prompt_args::command_prefix;
use crate::render::Insets;
use crate::render::RectExt;
use crate::slash_command::SlashCommand;
use crate::ui_preferences::load_ui_preferences;

// Hide alias commands in the default popup list so each unique action appears once.
// `quit` is an alias of `exit`, so we skip `quit` here.
// `approvals` is an alias of `permissions`.
const ALIAS_COMMANDS: &[SlashCommand] = &[SlashCommand::Quit, SlashCommand::Approvals];

/// A selectable item in the popup.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum CommandItem {
    Builtin(SlashCommand),
}

pub(crate) struct CommandPopup {
    command_filter: String,
    builtins: Vec<(&'static str, SlashCommand)>,
    ui_language_is_ru: bool,
    state: ScrollState,
}

#[derive(Clone, Copy, Debug, Default)]
pub(crate) struct CommandPopupFlags {
    pub(crate) collaboration_modes_enabled: bool,
    pub(crate) connectors_enabled: bool,
    pub(crate) plugins_command_enabled: bool,
    pub(crate) fast_command_enabled: bool,
    pub(crate) personality_command_enabled: bool,
    pub(crate) realtime_conversation_enabled: bool,
    pub(crate) windows_degraded_sandbox_active: bool,
}

impl From<CommandPopupFlags> for slash_commands::BuiltinCommandFlags {
    fn from(value: CommandPopupFlags) -> Self {
        Self {
            collaboration_modes_enabled: value.collaboration_modes_enabled,
            connectors_enabled: value.connectors_enabled,
            plugins_command_enabled: value.plugins_command_enabled,
            fast_command_enabled: value.fast_command_enabled,
            personality_command_enabled: value.personality_command_enabled,
            realtime_conversation_enabled: value.realtime_conversation_enabled,
            allow_elevate_sandbox: value.windows_degraded_sandbox_active,
        }
    }
}

impl CommandPopup {
    pub(crate) fn new(flags: CommandPopupFlags) -> Self {
        Self::new_with_language(flags, current_ui_language_is_ru())
    }

    pub(crate) fn new_with_language(flags: CommandPopupFlags, ui_language_is_ru: bool) -> Self {
        // Keep built-in availability in sync with the composer.
        let builtins: Vec<(&'static str, SlashCommand)> =
            slash_commands::builtins_for_input(flags.into())
                .into_iter()
                .filter(|(name, _)| !name.starts_with("debug"))
                .filter(|(_, cmd)| *cmd != SlashCommand::Apps)
                .collect();
        Self {
            command_filter: String::new(),
            builtins,
            ui_language_is_ru,
            state: ScrollState::new(),
        }
    }

    fn filter_prefers_english(&self) -> bool {
        self.command_filter
            .chars()
            .next()
            .is_some_and(|ch| ch.is_ascii_alphabetic())
    }

    /// Update the filter string based on the current composer text. The text
    /// passed in is expected to start with the configured command prefix. Everything after
    /// the first prefix character on the first line becomes the active filter that is used
    /// to narrow down the list of available commands.
    pub(crate) fn on_composer_text_change(&mut self, text: String) {
        let first_line = text.lines().next().unwrap_or("");

        if let Some(stripped) = first_line.strip_prefix(command_prefix()) {
            // Extract the *first* token (sequence of non-whitespace
            // characters) after the prefix so that `/clear something` still
            // shows the help for `/clear`.
            let token = stripped.trim_start();
            let cmd_token = token.split_whitespace().next().unwrap_or("");

            // Update the filter keeping the original case (commands are all
            // lower-case for now but this may change in the future).
            self.command_filter = cmd_token.to_string();
        } else {
            // The composer no longer starts with the configured prefix. Reset the filter so the
            // popup shows the *full* command list if it is still displayed
            // for some reason.
            self.command_filter.clear();
        }

        // Reset or clamp selected index based on new filtered list.
        let matches_len = self.filtered_items().len();
        self.state.clamp_selection(matches_len);
        self.state
            .ensure_visible(matches_len, MAX_POPUP_ROWS.min(matches_len));
    }

    /// Determine the preferred height of the popup for a given width.
    /// Accounts for wrapped descriptions so that long tooltips don't overflow.
    pub(crate) fn calculate_required_height(&self, width: u16) -> u16 {
        use super::selection_popup_common::measure_rows_height;
        let rows = self.rows_from_matches(self.filtered());

        measure_rows_height(&rows, &self.state, MAX_POPUP_ROWS, width)
    }

    /// Build the display label used in popup rows:
    /// canonical English key first, then discoverable aliases in parentheses.
    fn display_name(&self, cmd: SlashCommand) -> String {
        if self.ui_language_is_ru {
            if self.filter_prefers_english() {
                return format!("{} ({})", cmd.command_en(), cmd.command_ru());
            }
            return cmd.command_ru().to_string();
        }

        let aliases = cmd.popup_aliases();
        if aliases.is_empty() {
            cmd.command().to_string()
        } else {
            format!("{} ({})", cmd.command(), aliases.join(", "))
        }
    }

    pub(crate) fn inserted_command(&self, cmd: SlashCommand) -> String {
        if self.ui_language_is_ru && !self.filter_prefers_english() {
            cmd.command_ru().to_string()
        } else {
            cmd.command_en().to_string()
        }
    }

    fn indices_for(offset: usize, filter_chars: usize) -> Option<Vec<usize>> {
        Some((offset..offset + filter_chars).collect())
    }

    /// Compute exact/prefix matches over canonical command keys and aliases.
    ///
    /// Priority order keeps canonical keys primary while still making aliases
    /// discoverable:
    /// 1) canonical exact
    /// 2) canonical prefix
    /// 3) alias exact
    /// 4) alias prefix
    fn filtered(&self) -> Vec<(CommandItem, Option<Vec<usize>>)> {
        let filter = self.command_filter.trim();
        let mut out: Vec<(CommandItem, Option<Vec<usize>>)> = Vec::new();
        if filter.is_empty() {
            for (_, cmd) in self.builtins.iter() {
                if ALIAS_COMMANDS.contains(cmd) {
                    continue;
                }
                out.push((CommandItem::Builtin(*cmd), None));
            }
            return out;
        }

        let filter_lower = filter.to_lowercase();
        let mut exact_canonical: Vec<(CommandItem, Option<Vec<usize>>)> = Vec::new();
        let mut prefix_canonical: Vec<(CommandItem, Option<Vec<usize>>)> = Vec::new();
        let mut exact_alias: Vec<(CommandItem, Option<Vec<usize>>)> = Vec::new();
        let mut prefix_alias: Vec<(CommandItem, Option<Vec<usize>>)> = Vec::new();

        for (_, cmd) in self.builtins.iter() {
            let item = CommandItem::Builtin(*cmd);
            let canonical = cmd.command_en();
            let canonical_lower = canonical.to_lowercase();

            if canonical_lower == filter_lower {
                exact_canonical.push((item, None));
                continue;
            }

            if canonical_lower.starts_with(&filter_lower) {
                prefix_canonical.push((item, None));
                continue;
            }

            let mut matched_exact_alias = false;
            let mut matched_prefix_alias = false;

            for alias in cmd.popup_aliases() {
                let alias_lower = alias.to_lowercase();
                if alias_lower == filter_lower {
                    matched_exact_alias = true;
                    break;
                }

                if !matched_prefix_alias && alias_lower.starts_with(&filter_lower) {
                    matched_prefix_alias = true;
                }
            }

            if matched_exact_alias {
                exact_alias.push((item, None));
                continue;
            }

            if matched_prefix_alias {
                prefix_alias.push((item, None));
            }
        }

        out.extend(exact_canonical);
        out.extend(prefix_canonical);
        out.extend(exact_alias);
        out.extend(prefix_alias);
        out
    }

    fn filtered_items(&self) -> Vec<CommandItem> {
        self.filtered().into_iter().map(|(c, _)| c).collect()
    }

    fn rows_from_matches(
        &self,
        matches: Vec<(CommandItem, Option<Vec<usize>>)>,
    ) -> Vec<GenericDisplayRow> {
        matches
            .into_iter()
            .map(|(item, indices)| {
                let CommandItem::Builtin(cmd) = item;
                let name = format!("{}{}", command_prefix(), self.display_name(cmd));
                GenericDisplayRow {
                    name,
                    name_prefix_spans: Vec::new(),
                    match_indices: indices.map(|v| v.into_iter().map(|i| i + 1).collect()),
                    display_shortcut: None,
                    description: None,
                    category_tag: None,
                    category_tags: Vec::new(),
                    wrap_indent: None,
                    is_disabled: false,
                    disabled_reason: None,
                }
            })
            .collect()
    }

    /// Move the selection cursor one step up.
    pub(crate) fn move_up(&mut self) {
        let len = self.filtered_items().len();
        self.state.move_up_wrap(len);
        self.state.ensure_visible(len, MAX_POPUP_ROWS.min(len));
    }

    /// Move the selection cursor one step down.
    pub(crate) fn move_down(&mut self) {
        let matches_len = self.filtered_items().len();
        self.state.move_down_wrap(matches_len);
        self.state
            .ensure_visible(matches_len, MAX_POPUP_ROWS.min(matches_len));
    }

    /// Return currently selected command, if any.
    pub(crate) fn selected_item(&self) -> Option<CommandItem> {
        let matches = self.filtered_items();
        self.state
            .selected_idx
            .and_then(|idx| matches.get(idx).copied())
    }

    fn first_visible_index(&self, total_rows: usize) -> usize {
        let visible_items = MAX_POPUP_ROWS.min(total_rows);
        let mut start_idx = self.state.scroll_top.min(total_rows.saturating_sub(1));
        if let Some(sel) = self.state.selected_idx {
            if sel < start_idx {
                start_idx = sel;
            } else if visible_items > 0 {
                let bottom = start_idx + visible_items - 1;
                if sel > bottom {
                    start_idx = sel + 1 - visible_items;
                }
            }
        }
        start_idx
    }

    fn point_in_rect(rect: Rect, column: u16, row: u16) -> bool {
        column >= rect.x
            && column < rect.x.saturating_add(rect.width)
            && row >= rect.y
            && row < rect.y.saturating_add(rect.height)
    }

    pub(crate) fn handle_mouse_event(
        &mut self,
        area: Rect,
        mouse_event: MouseEvent,
    ) -> Option<CommandItem> {
        if area.width == 0 || area.height == 0 {
            return None;
        }

        match mouse_event.kind {
            MouseEventKind::ScrollUp
                if Self::point_in_rect(area, mouse_event.column, mouse_event.row) =>
            {
                self.move_up();
                None
            }
            MouseEventKind::ScrollDown
                if Self::point_in_rect(area, mouse_event.column, mouse_event.row) =>
            {
                self.move_down();
                None
            }
            MouseEventKind::Down(MouseButton::Left) => {
                let list_area = area.inset(Insets::tlbr(
                    /*top*/ 0, /*left*/ 2, /*bottom*/ 0, /*right*/ 0,
                ));
                if !Self::point_in_rect(list_area, mouse_event.column, mouse_event.row) {
                    return None;
                }

                let rows = self.rows_from_matches(self.filtered());
                if rows.is_empty() {
                    return None;
                }

                let start_idx = self.first_visible_index(rows.len());
                let visible_row_heights = visible_row_line_counts(
                    &rows,
                    &self.state,
                    MAX_POPUP_ROWS,
                    list_area.width,
                    ColumnWidthMode::AutoVisible,
                );

                let mut cursor_y = list_area.y;
                for (offset, height) in visible_row_heights.iter().enumerate() {
                    let next_y = cursor_y.saturating_add(*height);
                    if mouse_event.row >= cursor_y && mouse_event.row < next_y {
                        self.state.selected_idx = Some(start_idx + offset);
                        self.state
                            .ensure_visible(rows.len(), MAX_POPUP_ROWS.min(rows.len()));
                        return self.selected_item();
                    }
                    cursor_y = next_y;
                }
                None
            }
            _ => None,
        }
    }
}

fn current_ui_language_is_ru() -> bool {
    find_codex_home()
        .ok()
        .map(|codex_home| load_ui_preferences(codex_home.as_path()).language.is_ru())
        .unwrap_or(true)
}

impl WidgetRef for CommandPopup {
    fn render_ref(&self, area: Rect, buf: &mut Buffer) {
        let rows = self.rows_from_matches(self.filtered());
        render_rows(
            area.inset(Insets::tlbr(
                /*top*/ 0, /*left*/ 2, /*bottom*/ 0, /*right*/ 0,
            )),
            buf,
            &rows,
            &self.state,
            MAX_POPUP_ROWS,
            "ничего не найдено",
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pretty_assertions::assert_eq;

    #[test]
    fn filter_includes_init_when_typing_prefix() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        // Simulate the composer line starting with '/in' so the popup filters
        // matching commands by prefix.
        popup.on_composer_text_change("/in".to_string());

        // Access the filtered list via the selected command and ensure that
        // one of the matches is the new "init" command.
        let matches = popup.filtered_items();
        let has_init = matches.iter().any(|item| match item {
            CommandItem::Builtin(cmd) => cmd.command() == "init",
        });
        assert!(
            has_init,
            "expected '/init' to appear among filtered commands"
        );
    }

    #[test]
    fn selecting_init_by_exact_match() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/init".to_string());

        // When an exact match exists, the selected command should be that
        // command by default.
        let selected = popup.selected_item();
        match selected {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "init"),
            None => panic!("expected a selected command for exact match"),
        }
    }

    #[test]
    fn model_is_first_suggestion_for_mo() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/mo".to_string());
        let matches = popup.filtered_items();
        match matches.first() {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "model"),
            None => panic!("expected at least one match for '/mo'"),
        }
    }

    #[test]
    fn russian_alias_filters_to_model_command() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), true);
        popup.on_composer_text_change("/мод".to_string());

        let matches = popup.filtered_items();
        match matches.first() {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "model"),
            None => panic!("expected at least one match for Russian alias"),
        }
    }

    #[test]
    fn russian_alias_highlighting_targets_alias_segment() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), true);
        popup.on_composer_text_change("/мод".to_string());

        let model_match = popup
            .filtered()
            .into_iter()
            .find(|(item, _)| *item == CommandItem::Builtin(SlashCommand::Model))
            .expect("expected /model to match by Russian alias");

        let rows = popup.rows_from_matches(vec![model_match]);
        assert_eq!(rows[0].name, "/модель");
        assert_eq!(rows[0].match_indices, None);
    }

    #[test]
    fn filtered_commands_keep_presentation_order_for_prefix() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/m".to_string());

        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();
        assert_eq!(cmds, vec!["model", "mention", "mcp"]);
    }

    #[test]
    fn prefix_filter_limits_matches_for_ac() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/ac".to_string());

        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();
        assert!(
            !cmds.contains(&"compact"),
            "expected prefix search for '/ac' to exclude 'compact', got {cmds:?}"
        );
    }

    #[test]
    fn quit_hidden_in_empty_filter_but_shown_for_prefix() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/".to_string());
        let items = popup.filtered_items();
        assert!(!items.contains(&CommandItem::Builtin(SlashCommand::Quit)));

        popup.on_composer_text_change("/qu".to_string());
        let items = popup.filtered_items();
        assert!(items.contains(&CommandItem::Builtin(SlashCommand::Quit)));
    }

    #[test]
    fn collab_command_hidden_when_collaboration_modes_disabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        popup.on_composer_text_change("/".to_string());

        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();
        assert!(
            !cmds.contains(&"collab"),
            "expected '/collab' to be hidden when collaboration modes are disabled, got {cmds:?}"
        );
        assert!(
            !cmds.contains(&"plan"),
            "expected '/plan' to be hidden when collaboration modes are disabled, got {cmds:?}"
        );
    }

    #[test]
    fn collab_command_visible_when_collaboration_modes_enabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags {
            collaboration_modes_enabled: true,
            connectors_enabled: false,
            plugins_command_enabled: false,
            fast_command_enabled: false,
            personality_command_enabled: true,
            realtime_conversation_enabled: false,
            windows_degraded_sandbox_active: false,
        }, false);
        popup.on_composer_text_change("/collab".to_string());

        match popup.selected_item() {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "collab"),
            other => panic!("expected collab to be selected for exact match, got {other:?}"),
        }
    }

    #[test]
    fn plan_command_visible_when_collaboration_modes_enabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags {
            collaboration_modes_enabled: true,
            connectors_enabled: false,
            plugins_command_enabled: false,
            fast_command_enabled: false,
            personality_command_enabled: true,
            realtime_conversation_enabled: false,
            windows_degraded_sandbox_active: false,
        }, false);
        popup.on_composer_text_change("/plan".to_string());

        match popup.selected_item() {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "plan"),
            other => panic!("expected plan to be selected for exact match, got {other:?}"),
        }
    }

    #[test]
    fn personality_command_hidden_when_disabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags {
            collaboration_modes_enabled: true,
            connectors_enabled: false,
            plugins_command_enabled: false,
            fast_command_enabled: false,
            personality_command_enabled: false,
            realtime_conversation_enabled: false,
            windows_degraded_sandbox_active: false,
        }, false);
        popup.on_composer_text_change("/pers".to_string());

        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();
        assert!(
            !cmds.contains(&"personality"),
            "expected '/personality' to be hidden when disabled, got {cmds:?}"
        );
    }

    #[test]
    fn personality_command_visible_when_enabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags {
            collaboration_modes_enabled: true,
            connectors_enabled: false,
            plugins_command_enabled: false,
            fast_command_enabled: false,
            personality_command_enabled: true,
            realtime_conversation_enabled: false,
            windows_degraded_sandbox_active: false,
        }, false);
        popup.on_composer_text_change("/personality".to_string());

        match popup.selected_item() {
            Some(CommandItem::Builtin(cmd)) => assert_eq!(cmd.command(), "personality"),
            other => panic!("expected personality to be selected for exact match, got {other:?}"),
        }
    }

    #[test]
    fn settings_command_visible_when_audio_device_selection_is_disabled() {
        let mut popup = CommandPopup::new_with_language(CommandPopupFlags {
            collaboration_modes_enabled: false,
            connectors_enabled: false,
            plugins_command_enabled: false,
            fast_command_enabled: false,
            personality_command_enabled: true,
            realtime_conversation_enabled: true,
            windows_degraded_sandbox_active: false,
        }, false);
        popup.on_composer_text_change("/aud".to_string());

        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();

        assert!(
            cmds.contains(&"settings"),
            "expected '/settings' to stay visible when audio device selection is disabled, got {cmds:?}"
        );
    }

    #[test]
    fn debug_commands_are_hidden_from_popup() {
        let popup = CommandPopup::new_with_language(CommandPopupFlags::default(), false);
        let cmds: Vec<&str> = popup
            .filtered_items()
            .into_iter()
            .map(|item| match item {
                CommandItem::Builtin(cmd) => cmd.command(),
            })
            .collect();

        assert!(
            !cmds.iter().any(|name| name.starts_with("debug")),
            "expected no /debug* command in popup menu, got {cmds:?}"
        );
    }
}
