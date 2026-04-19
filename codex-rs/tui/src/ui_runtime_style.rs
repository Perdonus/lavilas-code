use std::path::PathBuf;
use std::sync::Mutex;
use std::sync::OnceLock;

use codex_core::config::find_codex_home;
use ratatui::style::Modifier;
use ratatui::style::Style;
use ratatui::text::Line;
use ratatui::text::Span;

use crate::ui_appearance::apply_text_formats;
use crate::ui_appearance::best_terminal_color;
use crate::ui_appearance::resolve_color_choice_label_rgb;
use crate::ui_appearance::resolve_color_choice_rgb;
use crate::ui_appearance::visible_terminal_rgb;
use crate::ui_preferences::SelectionHighlightTextFormats;
use crate::ui_preferences::UiColorChoice;
use crate::ui_preferences::UiPreferences;
use crate::ui_preferences::load_ui_preferences;
use crate::ui_preferences::settings_path as ui_settings_path;
use crate::ui_preferences::ui_preferences_revision;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum RuntimeTextRole {
    Reply,
    Reasoning,
    Command,
    CommandOutput,
}

#[derive(Clone)]
struct CachedRuntimeUiPreferences {
    settings_path: Option<PathBuf>,
    revision: u64,
    preferences: UiPreferences,
}

impl Default for CachedRuntimeUiPreferences {
    fn default() -> Self {
        Self {
            settings_path: None,
            revision: 0,
            preferences: UiPreferences::default(),
        }
    }
}

static RUNTIME_UI_PREFERENCES: OnceLock<Mutex<CachedRuntimeUiPreferences>> = OnceLock::new();

fn runtime_preferences() -> UiPreferences {
    if cfg!(test) {
        return UiPreferences::default();
    }

    let mutex =
        RUNTIME_UI_PREFERENCES.get_or_init(|| Mutex::new(CachedRuntimeUiPreferences::default()));
    let mut cache = match mutex.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    };

    let codex_home = find_codex_home().ok();
    let settings_path = codex_home
        .as_ref()
        .map(|codex_home| ui_settings_path(codex_home.as_path()));
    let revision = ui_preferences_revision();

    if cache.settings_path != settings_path || cache.revision != revision {
        cache.settings_path = settings_path;
        cache.revision = revision;
        cache.preferences = codex_home
            .as_ref()
            .map(|codex_home| load_ui_preferences(codex_home.as_path()))
            .unwrap_or_default();
    }

    cache.preferences.clone()
}

fn role_choice(
    preferences: &UiPreferences,
    role: RuntimeTextRole,
) -> (UiColorChoice, SelectionHighlightTextFormats, Style) {
    match role {
        RuntimeTextRole::Reply => (
            preferences.reply_text_color.clone(),
            preferences.reply_text_formats,
            Style::default(),
        ),
        RuntimeTextRole::Reasoning => (
            preferences.reasoning_text_color.clone(),
            preferences.reasoning_text_formats,
            Style::default()
                .add_modifier(Modifier::DIM)
                .add_modifier(Modifier::ITALIC),
        ),
        RuntimeTextRole::Command => (
            preferences.command_text_color.clone(),
            preferences.command_text_formats,
            Style::default(),
        ),
        RuntimeTextRole::CommandOutput => (
            preferences.command_output_text_color.clone(),
            preferences.command_output_text_formats,
            Style::default().add_modifier(Modifier::DIM),
        ),
    }
}

pub(crate) fn runtime_text_style(role: RuntimeTextRole) -> Style {
    let preferences = runtime_preferences();
    let (choice, formats, default_style) = role_choice(&preferences, role);
    let fallback_preset = preferences.selection_highlight_preset;
    let style = if matches!(choice, UiColorChoice::Auto) {
        default_style
    } else {
        let rgb = resolve_color_choice_rgb(&choice, fallback_preset);
        default_style.fg(best_terminal_color(visible_terminal_rgb(rgb)))
    };
    apply_text_formats(style, formats)
}

fn mix_channel(a: u8, b: u8, weight: f32) -> u8 {
    let weight = weight.clamp(0.0, 1.0);
    (((a as f32) * (1.0 - weight)) + ((b as f32) * weight)).round() as u8
}

fn mix_rgb(left: (u8, u8, u8), right: (u8, u8, u8), weight: f32) -> (u8, u8, u8) {
    (
        mix_channel(left.0, right.0, weight),
        mix_channel(left.1, right.1, weight),
        mix_channel(left.2, right.2, weight),
    )
}

fn gradient_weight(index: usize, total: usize) -> f32 {
    if total <= 1 {
        0.0
    } else {
        index as f32 / (total - 1) as f32
    }
}

fn patch_gradient_line_for_role(
    line: Line<'static>,
    choice: &UiColorChoice,
    fallback_preset: crate::ui_preferences::SelectionHighlightPreset,
    base_style: Style,
    only_plain: bool,
) -> Line<'static> {
    let start_rgb = resolve_color_choice_label_rgb(choice, fallback_preset, false);
    let end_rgb = resolve_color_choice_label_rgb(choice, fallback_preset, true);
    let total_chars = line
        .spans
        .iter()
        .filter(|span| !(only_plain && (span.style.fg.is_some() || span.style.bg.is_some())))
        .map(|span| span.content.chars().filter(|ch| !ch.is_control()).count())
        .sum::<usize>()
        .max(1);

    let mut char_index = 0usize;
    let spans = line
        .spans
        .into_iter()
        .flat_map(|span| {
            if only_plain && (span.style.fg.is_some() || span.style.bg.is_some()) {
                return vec![span];
            }

            let patched_style = span.style.patch(base_style);

            span.content
                .chars()
                .map(|ch| {
                    let mut style = patched_style;
                    if !ch.is_whitespace() {
                        let rgb = mix_rgb(start_rgb, end_rgb, gradient_weight(char_index, total_chars));
                        style = style.fg(best_terminal_color(visible_terminal_rgb(rgb)));
                    }
                    char_index += usize::from(!ch.is_control());
                    Span::styled(ch.to_string(), style)
                })
                .collect::<Vec<_>>()
        })
        .collect();

    Line {
        style: line.style,
        alignment: line.alignment,
        spans,
    }
}

pub(crate) fn patch_line_for_role(
    line: Line<'static>,
    role: RuntimeTextRole,
    only_plain: bool,
) -> Line<'static> {
    let preferences = runtime_preferences();
    let (choice, formats, default_style) = role_choice(&preferences, role);
    let fallback_preset = preferences.selection_highlight_preset;
    if matches!(choice, UiColorChoice::Gradient { .. }) {
        return patch_gradient_line_for_role(
            line,
            &choice,
            fallback_preset,
            apply_text_formats(default_style, formats),
            only_plain,
        );
    }

    let style = if matches!(choice, UiColorChoice::Auto) {
        apply_text_formats(default_style, formats)
    } else {
        let rgb = resolve_color_choice_rgb(&choice, fallback_preset);
        apply_text_formats(
            default_style.fg(best_terminal_color(visible_terminal_rgb(rgb))),
            formats,
        )
    };
    let spans = line
        .spans
        .into_iter()
        .map(|span| patch_span_for_role(span, style, only_plain))
        .collect();
    Line {
        style: line.style,
        alignment: line.alignment,
        spans,
    }
}

pub(crate) fn patch_lines_for_role(
    lines: Vec<Line<'static>>,
    role: RuntimeTextRole,
    only_plain: bool,
) -> Vec<Line<'static>> {
    lines
        .into_iter()
        .map(|line| patch_line_for_role(line, role, only_plain))
        .collect()
}

pub(crate) fn patch_span_for_role(
    span: Span<'static>,
    role_style: Style,
    only_plain: bool,
) -> Span<'static> {
    if only_plain && (span.style.fg.is_some() || span.style.bg.is_some()) {
        return span;
    }

    let style = span.style.patch(role_style);
    Span { style, ..span }
}
