use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
// Note: Table-based layout previously used Constraint; the manual renderer
// below no longer requires it.
use ratatui::style::Color;
use ratatui::style::Modifier;
use ratatui::style::Style;
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::text::Span;
use ratatui::widgets::Block;
use ratatui::widgets::BorderType;
use ratatui::widgets::Borders;
use ratatui::widgets::Widget;
use std::borrow::Cow;
use std::path::PathBuf;
use std::sync::atomic::AtomicU16;
use std::sync::atomic::AtomicU8;
use std::sync::atomic::Ordering;
use std::sync::Mutex;
use std::sync::OnceLock;
use unicode_width::UnicodeWidthChar;
use unicode_width::UnicodeWidthStr;

use codex_core::config::find_codex_home;

use crate::key_hint::KeyBinding;
use crate::line_truncation::truncate_line_with_ellipsis_if_overflow;
use crate::render::Insets;
use crate::render::RectExt as _;
use crate::style::user_message_style;
use crate::ui_appearance::apply_text_formats;
use crate::ui_appearance::best_terminal_color;
use crate::ui_appearance::resolve_color_choice_label_rgb;
use crate::ui_appearance::selection_preset_color;
use crate::ui_preferences::UiColorChoice;
use crate::ui_preferences::UiPreferences;
use crate::ui_preferences::load_ui_preferences;
use crate::ui_preferences::settings_path as ui_settings_path;
use crate::ui_preferences::ui_preferences_revision;
use crate::ui_preferences::SelectionHighlightPreset;
use crate::ui_preferences::SelectionHighlightTextFormat;
use crate::ui_preferences::SelectionHighlightTextFormats;

use super::scroll_state::ScrollState;

/// Render-ready representation of one row in a selection popup.
///
/// This type contains presentation-focused fields that are intentionally more
/// concrete than source domain models. `match_indices` are character offsets
/// into `name`, and `wrap_indent` is interpreted in terminal cell columns.
#[derive(Default)]
pub(crate) struct GenericDisplayRow {
    pub name: String,
    pub name_prefix_spans: Vec<Span<'static>>,
    pub display_shortcut: Option<KeyBinding>,
    pub match_indices: Option<Vec<usize>>, // indices to bold (char positions)
    pub description: Option<String>,       // optional grey text after the name
    pub category_tag: Option<String>,      // optional right-side category label
    pub category_tags: Vec<String>,        // optional right-side badge labels
    pub disabled_reason: Option<String>,   // optional disabled message
    pub is_disabled: bool,
    pub wrap_indent: Option<usize>, // optional indent for wrapped lines
}

/// Controls how selection rows choose the split between left/right name/description columns.
///
/// Callers should use the same mode for both measurement and rendering, or the
/// popup can reserve the wrong number of lines and clip content.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
#[cfg_attr(not(test), allow(dead_code))]
pub(crate) enum ColumnWidthMode {
    /// Derive column placement from only the visible viewport rows.
    #[default]
    AutoVisible,
    /// Derive column placement from all rows so scrolling does not shift columns.
    AutoAllRows,
    /// Use a fixed two-column split: 30% left (name), 70% right (description).
    Fixed,
}

// Fixed split used by explicitly fixed column mode: 30% label, 70%
// description.
const FIXED_LEFT_COLUMN_NUMERATOR: usize = 3;
const FIXED_LEFT_COLUMN_DENOMINATOR: usize = 10;

const MENU_SURFACE_INSET_V: u16 = 1;
const MENU_SURFACE_INSET_H: u16 = 2;
static SELECTION_HIGHLIGHT_PRESET: AtomicU8 = AtomicU8::new(SelectionHighlightPreset::Light as u8);
static SELECTION_HIGHLIGHT_FILL: AtomicU8 = AtomicU8::new(1);
static SELECTION_HIGHLIGHT_TEXT_FORMATS: AtomicU16 = AtomicU16::new(0);
static POPUP_UI_PREFERENCES: OnceLock<Mutex<CachedPopupUiPreferences>> = OnceLock::new();

#[derive(Clone, Copy)]
struct SelectionHighlightPalette {
    fill_bg: Color,
    fill_bg_emphasis: Color,
    fill_secondary_bg: Color,
    fill_secondary_bg_emphasis: Color,
    fill_fg: Color,
    fill_secondary_fg: Color,
    fill_secondary_fg_emphasis: Color,
    text_fg: Color,
    text_fg_emphasis: Color,
    text_secondary_fg: Color,
    text_secondary_fg_emphasis: Color,
    mono_text_fg: Color,
    mono_text_secondary_fg: Color,
}

#[derive(Clone)]
struct CachedPopupUiPreferences {
    settings_path: Option<PathBuf>,
    revision: u64,
    preferences: UiPreferences,
}

impl Default for CachedPopupUiPreferences {
    fn default() -> Self {
        Self {
            settings_path: None,
            revision: 0,
            preferences: UiPreferences::default(),
        }
    }
}

fn popup_ui_preferences() -> UiPreferences {
    if cfg!(test) {
        return UiPreferences::default();
    }

    let mutex = POPUP_UI_PREFERENCES.get_or_init(|| Mutex::new(CachedPopupUiPreferences::default()));
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

fn parse_hex_color(hex: &str) -> Option<Color> {
    let raw = hex.trim();
    let raw = raw.strip_prefix('#').unwrap_or(raw);
    if raw.len() != 6 || !raw.chars().all(|ch| ch.is_ascii_hexdigit()) {
        return None;
    }
    let r = u8::from_str_radix(&raw[0..2], 16).ok()?;
    let g = u8::from_str_radix(&raw[2..4], 16).ok()?;
    let b = u8::from_str_radix(&raw[4..6], 16).ok()?;
    Some(Color::Rgb(r, g, b))
}

fn color_to_rgb(color: Color) -> Option<(u8, u8, u8)> {
    match color {
        Color::Rgb(r, g, b) => Some((r, g, b)),
        Color::Black => Some((0, 0, 0)),
        Color::White => Some((255, 255, 255)),
        Color::Gray => Some((160, 160, 160)),
        Color::DarkGray => Some((96, 96, 96)),
        Color::Red => Some((220, 76, 70)),
        Color::Green => Some((77, 182, 96)),
        Color::Yellow => Some((240, 201, 76)),
        Color::Blue => Some((79, 140, 255)),
        Color::Magenta => Some((203, 105, 190)),
        Color::Cyan => Some((80, 197, 215)),
        _ => None,
    }
}

fn rgb_to_color((r, g, b): (u8, u8, u8)) -> Color {
    Color::Rgb(r, g, b)
}

fn mix_channel(a: u8, b: u8, weight: f32) -> u8 {
    let weight = weight.clamp(0.0, 1.0);
    (((a as f32) * (1.0 - weight)) + ((b as f32) * weight)).round() as u8
}

fn mix_color(left: Color, right: Color, weight: f32) -> Color {
    match (color_to_rgb(left), color_to_rgb(right)) {
        (Some((lr, lg, lb)), Some((rr, rg, rb))) => rgb_to_color((
            mix_channel(lr, rr, weight),
            mix_channel(lg, rg, weight),
            mix_channel(lb, rb, weight),
        )),
        _ => left,
    }
}

fn lighten_color(color: Color, weight: f32) -> Color {
    mix_color(color, Color::White, weight)
}

fn darken_color(color: Color, weight: f32) -> Color {
    mix_color(color, Color::Black, weight)
}

fn color_luminance(color: Color) -> Option<f32> {
    let (r, g, b) = color_to_rgb(color)?;
    Some(((0.2126 * r as f32) + (0.7152 * g as f32) + (0.0722 * b as f32)) / 255.0)
}

fn contrast_text_color(background: Color) -> Color {
    if color_luminance(background).unwrap_or(0.0) >= 0.62 {
        Color::Black
    } else {
        Color::White
    }
}

fn selection_highlight_palette_for_preset(preset: SelectionHighlightPreset) -> SelectionHighlightPalette {
    match preset {
        SelectionHighlightPreset::Light => SelectionHighlightPalette {
            fill_bg: Color::White,
            fill_bg_emphasis: Color::Rgb(236, 236, 236),
            fill_secondary_bg: Color::White,
            fill_secondary_bg_emphasis: Color::Rgb(236, 236, 236),
            fill_fg: Color::Black,
            fill_secondary_fg: Color::Rgb(62, 62, 62),
            fill_secondary_fg_emphasis: Color::Rgb(47, 47, 47),
            text_fg: Color::White,
            text_fg_emphasis: Color::Rgb(245, 245, 245),
            text_secondary_fg: Color::Rgb(214, 214, 214),
            text_secondary_fg_emphasis: Color::Rgb(230, 230, 230),
            mono_text_fg: Color::Rgb(239, 239, 239),
            mono_text_secondary_fg: Color::Rgb(206, 206, 206),
        },
        SelectionHighlightPreset::Graphite => SelectionHighlightPalette {
            fill_bg: Color::Rgb(73, 78, 87),
            fill_bg_emphasis: Color::Rgb(61, 66, 74),
            fill_secondary_bg: Color::Rgb(73, 78, 87),
            fill_secondary_bg_emphasis: Color::Rgb(61, 66, 74),
            fill_fg: Color::White,
            fill_secondary_fg: Color::Rgb(223, 227, 234),
            fill_secondary_fg_emphasis: Color::Rgb(235, 238, 243),
            text_fg: Color::Rgb(205, 210, 220),
            text_fg_emphasis: Color::Rgb(228, 232, 239),
            text_secondary_fg: Color::Rgb(163, 170, 183),
            text_secondary_fg_emphasis: Color::Rgb(188, 195, 207),
            mono_text_fg: Color::Rgb(218, 222, 228),
            mono_text_secondary_fg: Color::Rgb(189, 194, 201),
        },
        SelectionHighlightPreset::Amber => SelectionHighlightPalette {
            fill_bg: Color::Rgb(248, 229, 191),
            fill_bg_emphasis: Color::Rgb(240, 214, 162),
            fill_secondary_bg: Color::Rgb(248, 229, 191),
            fill_secondary_bg_emphasis: Color::Rgb(240, 214, 162),
            fill_fg: Color::Black,
            fill_secondary_fg: Color::Rgb(78, 61, 35),
            fill_secondary_fg_emphasis: Color::Rgb(66, 51, 29),
            text_fg: Color::Rgb(247, 224, 166),
            text_fg_emphasis: Color::Rgb(255, 213, 138),
            text_secondary_fg: Color::Rgb(221, 193, 129),
            text_secondary_fg_emphasis: Color::Rgb(236, 205, 142),
            mono_text_fg: Color::Rgb(228, 214, 181),
            mono_text_secondary_fg: Color::Rgb(204, 187, 151),
        },
        SelectionHighlightPreset::Mint => SelectionHighlightPalette {
            fill_bg: Color::Rgb(212, 241, 223),
            fill_bg_emphasis: Color::Rgb(191, 232, 207),
            fill_secondary_bg: Color::Rgb(212, 241, 223),
            fill_secondary_bg_emphasis: Color::Rgb(191, 232, 207),
            fill_fg: Color::Black,
            fill_secondary_fg: Color::Rgb(34, 73, 55),
            fill_secondary_fg_emphasis: Color::Rgb(28, 62, 47),
            text_fg: Color::Rgb(190, 238, 208),
            text_fg_emphasis: Color::Rgb(163, 228, 187),
            text_secondary_fg: Color::Rgb(153, 208, 177),
            text_secondary_fg_emphasis: Color::Rgb(171, 220, 191),
            mono_text_fg: Color::Rgb(192, 226, 204),
            mono_text_secondary_fg: Color::Rgb(161, 198, 177),
        },
        SelectionHighlightPreset::Rose => SelectionHighlightPalette {
            fill_bg: Color::Rgb(246, 213, 224),
            fill_bg_emphasis: Color::Rgb(239, 193, 210),
            fill_secondary_bg: Color::Rgb(246, 213, 224),
            fill_secondary_bg_emphasis: Color::Rgb(239, 193, 210),
            fill_fg: Color::Black,
            fill_secondary_fg: Color::Rgb(87, 51, 67),
            fill_secondary_fg_emphasis: Color::Rgb(74, 43, 57),
            text_fg: Color::Rgb(244, 198, 217),
            text_fg_emphasis: Color::Rgb(236, 178, 202),
            text_secondary_fg: Color::Rgb(220, 165, 189),
            text_secondary_fg_emphasis: Color::Rgb(231, 181, 202),
            mono_text_fg: Color::Rgb(228, 200, 211),
            mono_text_secondary_fg: Color::Rgb(205, 175, 188),
        },
    }
}

fn custom_selection_highlight_palette(base: Color) -> SelectionHighlightPalette {
    let fill_fg = contrast_text_color(base);
    let fill_bg_emphasis = if matches!(fill_fg, Color::Black) {
        darken_color(base, 0.08)
    } else {
        lighten_color(base, 0.08)
    };
    let fill_secondary_fg = mix_color(fill_fg, base, 0.28);
    let fill_secondary_fg_emphasis = mix_color(fill_fg, fill_bg_emphasis, 0.22);
    let text_fg_emphasis = if color_luminance(base).unwrap_or(0.0) >= 0.55 {
        darken_color(base, 0.14)
    } else {
        lighten_color(base, 0.14)
    };
    let text_secondary_fg = mix_color(base, Color::Gray, 0.32);
    let text_secondary_fg_emphasis = mix_color(text_fg_emphasis, Color::Gray, 0.22);
    let mono_text_fg = mix_color(base, contrast_text_color(base), 0.12);
    let mono_text_secondary_fg = mix_color(text_secondary_fg, contrast_text_color(base), 0.14);

    SelectionHighlightPalette {
        fill_bg: base,
        fill_bg_emphasis,
        fill_secondary_bg: base,
        fill_secondary_bg_emphasis: fill_bg_emphasis,
        fill_fg,
        fill_secondary_fg,
        fill_secondary_fg_emphasis,
        text_fg: base,
        text_fg_emphasis,
        text_secondary_fg,
        text_secondary_fg_emphasis,
        mono_text_fg,
        mono_text_secondary_fg,
    }
}

fn selection_highlight_palette_from_choice(
    choice: UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> SelectionHighlightPalette {
    match choice {
        UiColorChoice::Auto => selection_highlight_palette_for_preset(fallback_preset),
        UiColorChoice::Preset(preset) => selection_highlight_palette_for_preset(preset),
        UiColorChoice::Custom(hex) => parse_hex_color(&hex)
            .map(custom_selection_highlight_palette)
            .unwrap_or_else(|| selection_highlight_palette_for_preset(fallback_preset)),
        UiColorChoice::Gradient { start, .. } => parse_hex_color(&start)
            .map(custom_selection_highlight_palette)
            .unwrap_or_else(|| selection_highlight_palette_for_preset(fallback_preset)),
    }
}

fn terminal_safe_color(color: Color) -> Color {
    color_to_rgb(color).map(best_terminal_color).unwrap_or(color)
}

fn contrast_delta(foreground: Color, background: Color) -> Option<f32> {
    Some((color_luminance(foreground)? - color_luminance(background)?).abs())
}

fn adjust_foreground_for_background(
    foreground: Color,
    background: Color,
    is_secondary: bool,
) -> Color {
    let minimum_delta = if is_secondary { 0.24 } else { 0.34 };
    if contrast_delta(foreground, background).unwrap_or(minimum_delta) >= minimum_delta {
        return terminal_safe_color(foreground);
    }

    let target = contrast_text_color(background);
    for weight in [0.18, 0.32, 0.48, 0.64, 0.82, 1.0] {
        let candidate = mix_color(foreground, target, weight);
        if contrast_delta(candidate, background).unwrap_or(0.0) >= minimum_delta {
            return terminal_safe_color(candidate);
        }
    }

    terminal_safe_color(target)
}

fn resolve_text_color_choice(
    choice: UiColorChoice,
    is_secondary: bool,
    fallback_preset: SelectionHighlightPreset,
) -> Option<Color> {
    match &choice {
        UiColorChoice::Auto => None,
        _ => Some(ensure_visible_text_color(
            best_terminal_color(resolve_color_choice_label_rgb(
                &choice,
                fallback_preset,
                is_secondary,
            )),
            is_secondary,
        )),
    }
}

fn ensure_visible_text_color(color: Color, is_secondary: bool) -> Color {
    if let Some(background) = crate::terminal_palette::default_bg().map(rgb_to_color) {
        return adjust_foreground_for_background(color, background, is_secondary);
    }

    let Some(luminance) = color_luminance(color) else {
        return color;
    };
    let minimum = if is_secondary { 0.22 } else { 0.16 };
    if luminance >= minimum {
        return color;
    }
    let lighten_weight = if is_secondary { 0.42 } else { 0.34 };
    lighten_color(color, lighten_weight)
}

fn apply_terminal_safe_formats(
    style: Style,
    formats: SelectionHighlightTextFormats,
    _allow_dim: bool,
    _allow_reversed: bool,
) -> Style {
    apply_text_formats(style, formats)
}

fn unselected_row_styles() -> (Style, Style) {
    let preferences = popup_ui_preferences();
    let primary_formats = preferences.list_primary_text_formats;
    let secondary_formats = preferences.list_secondary_text_formats;
    let mut primary = Style::default();
    if let Some(color) = resolve_text_color_choice(
        preferences.list_primary_color,
        false,
        current_selection_highlight_preset(),
    ) {
        primary = primary.fg(color);
    }
    primary = apply_terminal_safe_formats(primary, primary_formats, true, true);

    let secondary_choice = preferences.list_secondary_color.clone();
    let mut secondary = Style::default();
    if let Some(color) = resolve_text_color_choice(
        secondary_choice.clone(),
        true,
        current_selection_highlight_preset(),
    ) {
        secondary = secondary.fg(color);
    } else {
        secondary = secondary.dim();
    }
    secondary = apply_terminal_safe_formats(
        secondary,
        secondary_formats,
        secondary_choice != UiColorChoice::Auto,
        true,
    );

    (primary, secondary)
}

fn style_is_plain(style: Style) -> bool {
    style.fg.is_none()
        && style.bg.is_none()
        && style.add_modifier.is_empty()
        && style.sub_modifier.is_empty()
}

fn style_has_visible_background(style: Style) -> bool {
    style.bg.is_some_and(|background| background != Color::Reset)
}

fn apply_base_style_to_prefix_span(mut span: Span<'static>, base_style: Style) -> Span<'static> {
    if style_is_plain(span.style) {
        span.style = span.style.patch(base_style);
    }
    span
}

fn spans_end_with_whitespace(spans: &[Span<'_>]) -> bool {
    spans
        .last()
        .and_then(|span| span.content.chars().last())
        .is_some_and(char::is_whitespace)
}

fn span_is_secondary(style: Style, normal_secondary_style: Style) -> bool {
    style.add_modifier.contains(Modifier::DIM)
        || matches!(style.fg, Some(Color::Gray | Color::DarkGray))
        || normal_secondary_style
            .fg
            .is_some_and(|foreground| style.fg == Some(foreground))
}

fn state_badge_kind(label: &str) -> Option<bool> {
    let normalized = label.trim().to_lowercase();
    if normalized.is_empty() {
        return None;
    }

    let enabled = [
        "✓",
        "✔",
        "enabled",
        "active",
        "current",
        "selected",
        "on",
        "true",
        "yes",
        "вкл",
        "включ",
        "актив",
        "текущ",
        "выбран",
        "да",
    ];
    if enabled.iter().any(|token| normalized.contains(token)) {
        return Some(true);
    }

    let disabled = [
        "✕",
        "✖",
        "✗",
        "disabled",
        "inactive",
        "off",
        "false",
        "no",
        "выкл",
        "откл",
        "неактив",
        "нет",
    ];
    if disabled.iter().any(|token| normalized.contains(token)) {
        return Some(false);
    }

    None
}

pub(crate) fn tag_has_state_badge(label: &str) -> bool {
    state_badge_kind(label).is_some()
}

fn badge_display_text(label: &str) -> Cow<'_, str> {
    match state_badge_kind(label) {
        Some(true) => Cow::Borrowed("✓"),
        Some(false) => Cow::Borrowed("✕"),
        None => Cow::Borrowed(label.trim()),
    }
}

/// Apply the shared "menu surface" padding used by bottom-pane overlays.
///
/// Rendering code should generally call [`render_menu_surface`] and then lay
/// out content inside the returned inset rect.
pub(crate) fn menu_surface_inset(area: Rect) -> Rect {
    area.inset(Insets::vh(MENU_SURFACE_INSET_V, MENU_SURFACE_INSET_H))
}

/// Total vertical padding introduced by the menu surface treatment.
pub(crate) const fn menu_surface_padding_height() -> u16 {
    MENU_SURFACE_INSET_V * 2
}

/// Paint the shared menu background and return the inset content area.
///
/// This keeps the surface treatment consistent across selection-style overlays
/// (for example `/model`, approvals, and request-user-input). Callers should
/// render all inner content in the returned rect, not the original area.
pub(crate) fn render_menu_surface(area: Rect, buf: &mut Buffer) -> Rect {
    if area.is_empty() {
        return area;
    }
    Block::default()
        .style(user_message_style())
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(Color::Gray))
        .render(area, buf);
    menu_surface_inset(area)
}

/// Wrap a styled line while preserving span styles.
///
/// The function clamps `width` to at least one terminal cell so callers can use
/// it safely with narrow layouts.
pub(crate) fn wrap_styled_line<'a>(line: &'a Line<'a>, width: u16) -> Vec<Line<'a>> {
    use crate::wrapping::RtOptions;
    use crate::wrapping::word_wrap_line;

    let width = width.max(1) as usize;
    let opts = RtOptions::new(width)
        .initial_indent(Line::from(""))
        .subsequent_indent(Line::from(""));
    word_wrap_line(line, opts)
}

fn line_to_owned(line: Line<'_>) -> Line<'static> {
    Line {
        style: line.style,
        alignment: line.alignment,
        spans: line
            .spans
            .into_iter()
            .map(|span| Span {
                style: span.style,
                content: Cow::Owned(span.content.into_owned()),
            })
            .collect(),
    }
}

fn compute_desc_col(
    rows_all: &[GenericDisplayRow],
    start_idx: usize,
    visible_items: usize,
    content_width: u16,
    col_width_mode: ColumnWidthMode,
) -> usize {
    if content_width <= 1 {
        return 0;
    }

    let max_desc_col = content_width.saturating_sub(1) as usize;
    // Reuse the existing fixed split constants to derive the auto cap:
    // if fixed mode is 30/70 (label/description), auto mode caps label width
    // at 70% to keep at least 30% available for descriptions.
    let max_auto_desc_col = max_desc_col.min(
        ((content_width as usize * (FIXED_LEFT_COLUMN_DENOMINATOR - FIXED_LEFT_COLUMN_NUMERATOR))
            / FIXED_LEFT_COLUMN_DENOMINATOR)
            .max(1),
    );
    match col_width_mode {
        ColumnWidthMode::Fixed => ((content_width as usize * FIXED_LEFT_COLUMN_NUMERATOR)
            / FIXED_LEFT_COLUMN_DENOMINATOR)
            .clamp(1, max_desc_col),
        ColumnWidthMode::AutoVisible | ColumnWidthMode::AutoAllRows => {
            let max_name_width = match col_width_mode {
                ColumnWidthMode::AutoVisible => rows_all
                    .iter()
                    .enumerate()
                    .skip(start_idx)
                    .take(visible_items)
                    .map(|(_, row)| {
                        let mut spans = row.name_prefix_spans.clone();
                        spans.push(row.name.clone().into());
                        if row.disabled_reason.is_some() {
                            spans.push(" (недоступно)".dim());
                        }
                        Line::from(spans).width()
                    })
                    .max()
                    .unwrap_or(0),
                ColumnWidthMode::AutoAllRows => rows_all
                    .iter()
                    .map(|row| {
                        let mut spans = row.name_prefix_spans.clone();
                        spans.push(row.name.clone().into());
                        if row.disabled_reason.is_some() {
                            spans.push(" (недоступно)".dim());
                        }
                        Line::from(spans).width()
                    })
                    .max()
                    .unwrap_or(0),
                ColumnWidthMode::Fixed => 0,
            };

            max_name_width.saturating_add(2).min(max_auto_desc_col)
        }
    }
}

/// Determine how many spaces to indent wrapped lines for a row.
fn wrap_indent(row: &GenericDisplayRow, desc_col: usize, max_width: u16) -> usize {
    let max_indent = max_width.saturating_sub(1) as usize;
    let indent = row.wrap_indent.unwrap_or_else(|| {
        if row.description.is_some() || row.disabled_reason.is_some() {
            desc_col
        } else {
            0
        }
    });
    indent.min(max_indent)
}

fn should_wrap_name_in_column(row: &GenericDisplayRow) -> bool {
    // This path intentionally targets plain option rows that opt into wrapped
    // labels. Styled/fuzzy-matched rows keep the legacy combined-line path.
    row.wrap_indent.is_some()
        && row.description.is_some()
        && row.disabled_reason.is_none()
        && row.match_indices.is_none()
        && row.display_shortcut.is_none()
        && row.category_tag.is_none()
        && row.category_tags.is_empty()
        && row.name_prefix_spans.is_empty()
}

fn wrap_two_column_row(row: &GenericDisplayRow, desc_col: usize, width: u16) -> Vec<Line<'static>> {
    let Some(description) = row.description.as_deref() else {
        return Vec::new();
    };
    let (primary_style, secondary_style) = unselected_row_styles();

    let width = width.max(1);
    let max_desc_col = width.saturating_sub(1) as usize;
    if max_desc_col == 0 {
        // No valid description column exists at this width; let callers fall
        // back to single-line wrapping path.
        return Vec::new();
    }

    let desc_col = desc_col.clamp(1, max_desc_col);
    let left_width = desc_col.saturating_sub(2).max(1);
    let right_width = width.saturating_sub(desc_col as u16).max(1) as usize;
    let name_wrap_indent = row
        .wrap_indent
        .unwrap_or(0)
        .min(left_width.saturating_sub(1));

    let name_subsequent_indent = " ".repeat(name_wrap_indent);
    let name_options = textwrap::Options::new(left_width)
        .initial_indent("")
        .subsequent_indent(name_subsequent_indent.as_str());
    let name_lines = textwrap::wrap(row.name.as_str(), name_options);

    let desc_options = textwrap::Options::new(right_width).initial_indent("");
    let desc_lines = textwrap::wrap(description, desc_options);

    let rows = name_lines.len().max(desc_lines.len()).max(1);
    let mut out = Vec::with_capacity(rows);
    for idx in 0..rows {
        let mut spans: Vec<Span<'static>> = Vec::new();
        if let Some(name) = name_lines.get(idx) {
            spans.push(Span::styled(name.to_string(), primary_style));
        }

        if let Some(desc) = desc_lines.get(idx) {
            let left_used = spans
                .iter()
                .map(|span| UnicodeWidthStr::width(span.content.as_ref()))
                .sum::<usize>();
            let gap = if left_used == 0 {
                desc_col
            } else {
                desc_col.saturating_sub(left_used).max(2)
            };
            if gap > 0 {
                spans.push(" ".repeat(gap).into());
            }
            spans.push(Span::styled(desc.to_string(), secondary_style));
        }

        out.push(Line::from(spans));
    }

    out
}

fn wrap_standard_row(row: &GenericDisplayRow, desc_col: usize, width: u16) -> Vec<Line<'static>> {
    use crate::wrapping::RtOptions;
    use crate::wrapping::word_wrap_line;

    let full_line = build_full_line(row, desc_col, width.max(1) as usize);
    let continuation_indent = wrap_indent(row, desc_col, width);
    let options = RtOptions::new(width.max(1) as usize)
        .initial_indent(Line::from(""))
        .subsequent_indent(Line::from(" ".repeat(continuation_indent)));
    word_wrap_line(&full_line, options)
        .into_iter()
        .map(line_to_owned)
        .collect()
}

fn wrap_row_lines(row: &GenericDisplayRow, desc_col: usize, width: u16) -> Vec<Line<'static>> {
    if should_wrap_name_in_column(row) {
        let wrapped = wrap_two_column_row(row, desc_col, width);
        if !wrapped.is_empty() {
            return wrapped;
        }
    }

    wrap_standard_row(row, desc_col, width)
}

fn current_selection_highlight_preset() -> SelectionHighlightPreset {
    match SELECTION_HIGHLIGHT_PRESET.load(Ordering::Relaxed) {
        x if x == SelectionHighlightPreset::Graphite as u8 => SelectionHighlightPreset::Graphite,
        x if x == SelectionHighlightPreset::Amber as u8 => SelectionHighlightPreset::Amber,
        x if x == SelectionHighlightPreset::Mint as u8 => SelectionHighlightPreset::Mint,
        x if x == SelectionHighlightPreset::Rose as u8 => SelectionHighlightPreset::Rose,
        _ => SelectionHighlightPreset::Light,
    }
}

pub(crate) fn set_selection_highlight_preset(preset: SelectionHighlightPreset) {
    SELECTION_HIGHLIGHT_PRESET.store(preset as u8, Ordering::Relaxed);
}

pub(crate) fn set_selection_highlight_fill(fill: bool) {
    SELECTION_HIGHLIGHT_FILL.store(u8::from(fill), Ordering::Relaxed);
}

pub(crate) fn set_selection_highlight_text_formats(formats: SelectionHighlightTextFormats) {
    SELECTION_HIGHLIGHT_TEXT_FORMATS.store(formats.bits(), Ordering::Relaxed);
}

fn current_selection_highlight_fill() -> bool {
    SELECTION_HIGHLIGHT_FILL.load(Ordering::Relaxed) != 0
}

fn current_selection_highlight_text_formats() -> SelectionHighlightTextFormats {
    SelectionHighlightTextFormats::from_bits(SELECTION_HIGHLIGHT_TEXT_FORMATS.load(Ordering::Relaxed))
}

fn selection_highlight_palette() -> SelectionHighlightPalette {
    let preferences = popup_ui_preferences();
    selection_highlight_palette_from_choice(
        preferences.selection_highlight_color,
        current_selection_highlight_preset(),
    )
}

fn selection_highlight_base_style(is_secondary: bool) -> Style {
    let preferences = popup_ui_preferences();
    let palette = selection_highlight_palette();
    let formats = current_selection_highlight_text_formats();
    let fill = current_selection_highlight_fill();
    let selection_choice = match (fill, preferences.selection_highlight_color.clone()) {
        (false, UiColorChoice::Auto) => {
            UiColorChoice::Preset(current_selection_highlight_preset())
        }
        (_, choice) => choice,
    };

    let mut style = if fill {
        let bg = if is_secondary {
            palette.fill_secondary_bg
        } else {
            palette.fill_bg
        };
        let fg = if is_secondary {
            palette.fill_secondary_fg
        } else {
            palette.fill_fg
        };
        Style::default().fg(fg).bg(bg)
    } else {
        let direct_text_color = resolve_text_color_choice(
            selection_choice,
            is_secondary,
            current_selection_highlight_preset(),
        );
        let base = direct_text_color.unwrap_or_else(|| {
            if is_secondary {
                palette.text_secondary_fg_emphasis
            } else {
                palette.text_fg_emphasis
            }
        });
        Style::default()
            .fg(ensure_visible_text_color(base, is_secondary))
            .bg(Color::Reset)
    };

    apply_terminal_safe_formats(style, formats, !fill, !fill)
}

pub(crate) fn selection_highlight_style() -> Style {
    selection_highlight_base_style(/*is_secondary*/ false)
}

fn selected_row_style() -> Style {
    selection_highlight_style()
}

fn selected_secondary_row_style() -> Style {
    selection_highlight_base_style(/*is_secondary*/ true)
}

fn row_default_foreground(
    is_secondary: bool,
    normal_primary_style: Style,
    normal_secondary_style: Style,
) -> Option<Color> {
    if is_secondary {
        normal_secondary_style.fg
    } else {
        normal_primary_style.fg
    }
}

fn badge_style(label: &str) -> Style {
    let normalized = label.trim().to_lowercase();
    if let Some(enabled) = state_badge_kind(label) {
        if enabled {
            Style::default().fg(Color::Black).bg(Color::Green).bold()
        } else {
            Style::default().fg(Color::White).bg(Color::Red).bold()
        }
    } else if normalized.contains("fast") || normalized.contains("быстр") {
        Style::default().fg(Color::Black).bg(Color::Green).bold()
    } else if normalized.contains("latest")
        || normalized.contains("основ")
        || normalized.contains("актуал")
    {
        Style::default().fg(Color::Black).bg(Color::Yellow).bold()
    } else if normalized.contains("tools") || normalized.contains("инстру") {
        Style::default().fg(Color::Black).bg(Color::Cyan).bold()
    } else if normalized.contains("current") || normalized.contains("текущ") {
        Style::default().fg(Color::Black).bg(Color::White).bold()
    } else if normalized.contains("default") || normalized.contains("умолч") {
        Style::default().fg(Color::Black).bg(Color::Gray).bold()
    } else if normalized.contains("repair") || normalized.contains("почин") {
        Style::default().fg(Color::Black).bg(Color::Yellow).bold()
    } else if normalized.contains("openai")
        || normalized.contains("mistral")
        || normalized.contains("gemini")
        || normalized.contains("claude")
        || normalized.contains("anthropic")
        || normalized.contains("groq")
    {
        Style::default().fg(Color::White).bg(Color::Blue).bold()
    } else {
        Style::default().fg(Color::White).bg(Color::DarkGray).bold()
    }
}

fn badge_spans(tags: &[String]) -> Vec<Span<'static>> {
    let mut spans = Vec::new();
    for tag in tags.iter().filter(|tag| !tag.trim().is_empty()) {
        spans.push(" ".into());
        spans.push(Span::styled(
            format!(" {} ", badge_display_text(tag)),
            badge_style(tag),
        ));
    }
    spans
}

fn apply_row_state_style(lines: &mut [Line<'static>], selected: bool, is_disabled: bool) {
    if selected {
        let selected_primary_style = selected_row_style();
        let selected_secondary_style = selected_secondary_row_style();
        let normal_primary_style = unselected_row_styles().0;
        let normal_secondary_style = unselected_row_styles().1;
        for (line_idx, line) in lines.iter_mut().enumerate() {
            line.spans.iter_mut().for_each(|span| {
                if style_has_visible_background(span.style) {
                    return;
                }
                let explicit_fg = span.style.fg;
                let is_secondary = span_is_secondary(span.style, normal_secondary_style);
                let is_whitespace = span.content.trim().is_empty();
                let mut style = span.style.patch(if is_secondary {
                    selected_secondary_style
                } else {
                    selected_primary_style
                });
                style.add_modifier.remove(Modifier::DIM);
                style.sub_modifier.remove(Modifier::DIM);
                if is_whitespace || is_secondary {
                    style.add_modifier.remove(Modifier::UNDERLINED);
                    style.sub_modifier.remove(Modifier::UNDERLINED);
                    style.add_modifier.remove(Modifier::CROSSED_OUT);
                    style.sub_modifier.remove(Modifier::CROSSED_OUT);
                }
                if line_idx > 0 {
                    style.add_modifier.remove(Modifier::UNDERLINED);
                    style.sub_modifier.remove(Modifier::UNDERLINED);
                    style.add_modifier.remove(Modifier::CROSSED_OUT);
                    style.sub_modifier.remove(Modifier::CROSSED_OUT);
                }
                if let Some(explicit_fg) = explicit_fg {
                    style.fg = match style.bg {
                        Some(background) if background != Color::Reset => {
                            Some(adjust_foreground_for_background(
                                explicit_fg,
                                background,
                                is_secondary,
                            ))
                        }
                        _ => {
                            let default_fg =
                                row_default_foreground(is_secondary, normal_primary_style, normal_secondary_style);
                            if default_fg == Some(explicit_fg) {
                                style.fg
                            } else {
                                Some(explicit_fg)
                            }
                        }
                    };
                }
                span.style = style;
            });
        }
    }
    if is_disabled {
        for line in lines.iter_mut() {
            line.spans.iter_mut().for_each(|span| {
                span.style = span.style.dim();
            });
        }
    }
}

pub(crate) fn legacy_selection_row_style(dim: bool) -> Style {
    let (primary, secondary) = unselected_row_styles();
    if dim { secondary } else { primary }
}

fn compute_item_window_start(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_items: usize,
) -> usize {
    if rows_all.is_empty() || max_items == 0 {
        return 0;
    }

    let mut start_idx = state.scroll_top.min(rows_all.len().saturating_sub(1));
    if let Some(sel) = state.selected_idx {
        if sel < start_idx {
            start_idx = sel;
        } else {
            let bottom = start_idx.saturating_add(max_items.saturating_sub(1));
            if sel > bottom {
                start_idx = sel + 1 - max_items;
            }
        }
    }
    start_idx
}

fn is_selected_visible_in_wrapped_viewport(
    rows_all: &[GenericDisplayRow],
    start_idx: usize,
    max_items: usize,
    selected_idx: usize,
    desc_col: usize,
    width: u16,
    viewport_height: u16,
) -> bool {
    if viewport_height == 0 {
        return false;
    }

    let mut used_lines = 0usize;
    let viewport_height = viewport_height as usize;
    for (idx, row) in rows_all.iter().enumerate().skip(start_idx).take(max_items) {
        let row_lines = wrap_row_lines(row, desc_col, width).len().max(1);
        // Keep rendering semantics in sync: always show the first row, even if
        // it overflows the viewport.
        if used_lines > 0 && used_lines.saturating_add(row_lines) > viewport_height {
            break;
        }
        if idx == selected_idx {
            return true;
        }
        used_lines = used_lines.saturating_add(row_lines);
        if used_lines >= viewport_height {
            break;
        }
    }
    false
}

fn adjust_start_for_wrapped_selection_visibility(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_items: usize,
    desc_measure_items: usize,
    width: u16,
    viewport_height: u16,
    col_width_mode: ColumnWidthMode,
) -> usize {
    let mut start_idx = compute_item_window_start(rows_all, state, max_items);
    let Some(sel) = state.selected_idx else {
        return start_idx;
    };
    if viewport_height == 0 {
        return start_idx;
    }

    // If wrapped row heights push the selected item out of view, advance the
    // item window until the selected row is visible.
    while start_idx < sel {
        let desc_col = compute_desc_col(
            rows_all,
            start_idx,
            desc_measure_items,
            width,
            col_width_mode,
        );
        if is_selected_visible_in_wrapped_viewport(
            rows_all,
            start_idx,
            max_items,
            sel,
            desc_col,
            width,
            viewport_height,
        ) {
            break;
        }
        start_idx = start_idx.saturating_add(1);
    }
    start_idx
}

/// Build the full display line for a row with the description padded to start
/// at `desc_col`. Applies fuzzy-match bolding when indices are present and
/// dims the description.
fn build_full_line(row: &GenericDisplayRow, desc_col: usize, total_width: usize) -> Line<'static> {
    let preferences = popup_ui_preferences();
    let (primary_style, secondary_style) = unselected_row_styles();
    let combined_description = match (&row.description, &row.disabled_reason) {
        (Some(desc), Some(reason)) => Some(format!("{desc} (недоступно: {reason})")),
        (Some(desc), None) => Some(desc.clone()),
        (None, Some(reason)) => Some(format!("недоступно: {reason}")),
        (None, None) => None,
    };

    // Enforce single-line name: allow at most desc_col - 2 cells for name,
    // reserving two spaces before the description column.
    let name_prefix_width = Line::from(row.name_prefix_spans.clone()).width();
    let name_limit = combined_description
        .as_ref()
        .map(|_| desc_col.saturating_sub(2).saturating_sub(name_prefix_width))
        .unwrap_or(usize::MAX);

    let mut name_spans: Vec<Span> = Vec::with_capacity(row.name.len());
    let mut used_width = 0usize;
    let mut truncated = false;

    if let Some(idxs) = row.match_indices.as_ref() {
        let mut idx_iter = idxs.iter().peekable();
        for (char_idx, ch) in row.name.chars().enumerate() {
            let ch_w = UnicodeWidthChar::width(ch).unwrap_or(0);
            let next_width = used_width.saturating_add(ch_w);
            if next_width > name_limit {
                truncated = true;
                break;
            }
            used_width = next_width;

            if idx_iter.peek().is_some_and(|next| **next == char_idx) {
                idx_iter.next();
                name_spans.push(Span::styled(
                    ch.to_string(),
                    primary_style.add_modifier(Modifier::BOLD),
                ));
            } else {
                name_spans.push(Span::styled(ch.to_string(), primary_style));
            }
        }
    } else {
        for ch in row.name.chars() {
            let ch_w = UnicodeWidthChar::width(ch).unwrap_or(0);
            let next_width = used_width.saturating_add(ch_w);
            if next_width > name_limit {
                truncated = true;
                break;
            }
            used_width = next_width;
            name_spans.push(Span::styled(ch.to_string(), primary_style));
        }
    }

    if truncated {
        // If there is at least one cell available, add an ellipsis.
        // When name_limit is 0, we still show an ellipsis to indicate truncation.
        name_spans.push(Span::styled("…", primary_style));
    }

    if row.disabled_reason.is_some() {
        name_spans.push(Span::styled(" (недоступно)", secondary_style));
    }

    let mut full_spans: Vec<Span> = row
        .name_prefix_spans
        .clone()
        .into_iter()
        .map(|span| apply_base_style_to_prefix_span(span, primary_style))
        .collect();
    if !row.name_prefix_spans.is_empty()
        && !name_spans.is_empty()
        && !spans_end_with_whitespace(&full_spans)
    {
        full_spans.push(Span::raw(" "));
    }
    full_spans.extend(name_spans);
    if let Some(display_shortcut) = row.display_shortcut {
        full_spans.push(Span::styled(" (", secondary_style));
        let shortcut: Span<'static> = display_shortcut.into();
        full_spans.push(apply_base_style_to_prefix_span(shortcut, secondary_style));
        full_spans.push(Span::styled(")", secondary_style));
    }
    if let Some(desc) = combined_description.as_ref() {
        let this_name_width = Line::from(full_spans.clone()).width();
        let gap = desc_col.saturating_sub(this_name_width);
        if gap > 0 {
            full_spans.push(" ".repeat(gap).into());
        }
        let text_style = secondary_style;
        let mut whitespace_style = text_style;
        whitespace_style.add_modifier.remove(Modifier::UNDERLINED);
        whitespace_style.add_modifier.remove(Modifier::CROSSED_OUT);
        whitespace_style.sub_modifier.remove(Modifier::UNDERLINED);
        whitespace_style.sub_modifier.remove(Modifier::CROSSED_OUT);
        let mut current = String::new();
        let mut in_whitespace = false;
        for ch in desc.chars() {
            let is_whitespace = ch.is_whitespace();
            if current.is_empty() {
                in_whitespace = is_whitespace;
            }
            if is_whitespace != in_whitespace {
                let style = if in_whitespace {
                    whitespace_style
                } else {
                    text_style
                };
                full_spans.push(Span::styled(current.clone(), style));
                current.clear();
                in_whitespace = is_whitespace;
            }
            current.push(ch);
        }
        if !current.is_empty() {
            let style = if in_whitespace {
                whitespace_style
            } else {
                text_style
            };
            full_spans.push(Span::styled(current, style));
        }
    }
    if !row.category_tags.is_empty() {
        let badges = badge_spans(&row.category_tags);
        let content_width = Line::from(full_spans.clone()).width();
        let badge_width = Line::from(badges.clone()).width();
        let gap = total_width
            .saturating_sub(content_width.saturating_add(badge_width))
            .max(1);
        if gap > 0 {
            full_spans.push(" ".repeat(gap).into());
        }
        full_spans.extend(badges);
    } else if let Some(tag) = row.category_tag.as_deref().filter(|tag| !tag.is_empty()) {
        let tag_width = UnicodeWidthStr::width(tag);
        let content_width = Line::from(full_spans.clone()).width();
        let gap = total_width
            .saturating_sub(content_width.saturating_add(tag_width))
            .max(2);
        if gap > 0 {
            full_spans.push(" ".repeat(gap).into());
        }
        full_spans.push(Span::styled(tag.to_string(), secondary_style));
    }
    Line::from(full_spans)
}

/// Render a list of rows using the provided ScrollState, with shared styling
/// and behavior for selection popups.
/// Returns the number of terminal lines actually rendered (including the
/// single-line empty placeholder when shown).
fn render_rows_inner(
    area: Rect,
    buf: &mut Buffer,
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    empty_message: &str,
    col_width_mode: ColumnWidthMode,
) -> u16 {
    if rows_all.is_empty() {
        if area.height > 0 {
            Line::from(empty_message.dim().italic()).render(area, buf);
        }
        // Count the placeholder line only when there is vertical space to draw it.
        return u16::from(area.height > 0);
    }

    let max_items = max_results.min(rows_all.len());
    if max_items == 0 {
        return 0;
    }
    let desc_measure_items = max_items.min(area.height.max(1) as usize);

    // Keep item-window semantics, then correct for wrapped row heights so the
    // selected row remains visible in a line-based viewport.
    let start_idx = adjust_start_for_wrapped_selection_visibility(
        rows_all,
        state,
        max_items,
        desc_measure_items,
        area.width,
        area.height,
        col_width_mode,
    );

    let desc_col = compute_desc_col(
        rows_all,
        start_idx,
        desc_measure_items,
        area.width,
        col_width_mode,
    );

    // Render items, wrapping descriptions and aligning wrapped lines under the
    // shared description column. Stop when we run out of vertical space.
    let mut cur_y = area.y;
    let mut rendered_lines: u16 = 0;
    for (i, row) in rows_all.iter().enumerate().skip(start_idx).take(max_items) {
        if cur_y >= area.y + area.height {
            break;
        }

        let mut wrapped = wrap_row_lines(row, desc_col, area.width);
        apply_row_state_style(
            &mut wrapped,
            Some(i) == state.selected_idx && !row.is_disabled,
            row.is_disabled,
        );

        // Render the wrapped lines.
        for line in wrapped {
            if cur_y >= area.y + area.height {
                break;
            }
            line.render(
                Rect {
                    x: area.x,
                    y: cur_y,
                    width: area.width,
                    height: 1,
                },
                buf,
            );
            cur_y = cur_y.saturating_add(1);
            rendered_lines = rendered_lines.saturating_add(1);
        }
    }

    rendered_lines
}

/// Render a list of rows using the provided ScrollState, with shared styling
/// and behavior for selection popups.
/// Description alignment is computed from visible rows only, which allows the
/// layout to adapt tightly to the current viewport.
///
/// This function should be paired with [`measure_rows_height`] when reserving
/// space; pairing it with a different measurement mode can cause clipping.
/// Returns the number of terminal lines actually rendered.
pub(crate) fn render_rows(
    area: Rect,
    buf: &mut Buffer,
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    empty_message: &str,
) -> u16 {
    render_rows_inner(
        area,
        buf,
        rows_all,
        state,
        max_results,
        empty_message,
        ColumnWidthMode::AutoVisible,
    )
}

/// Render a list of rows using the provided ScrollState, with shared styling
/// and behavior for selection popups.
/// This mode keeps column placement stable while scrolling by sizing the
/// description column against the full dataset.
///
/// This function should be paired with
/// [`measure_rows_height_stable_col_widths`] so reserved and rendered heights
/// stay in sync.
/// Returns the number of terminal lines actually rendered.
pub(crate) fn render_rows_stable_col_widths(
    area: Rect,
    buf: &mut Buffer,
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    empty_message: &str,
) -> u16 {
    render_rows_inner(
        area,
        buf,
        rows_all,
        state,
        max_results,
        empty_message,
        ColumnWidthMode::AutoAllRows,
    )
}

/// Render a list of rows using the provided ScrollState and explicit
/// [`ColumnWidthMode`] behavior.
///
/// This is the low-level entry point for callers that need to thread a mode
/// through higher-level configuration.
/// Returns the number of terminal lines actually rendered.
pub(crate) fn render_rows_with_col_width_mode(
    area: Rect,
    buf: &mut Buffer,
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    empty_message: &str,
    col_width_mode: ColumnWidthMode,
) -> u16 {
    render_rows_inner(
        area,
        buf,
        rows_all,
        state,
        max_results,
        empty_message,
        col_width_mode,
    )
}

/// Render rows as a single line each (no wrapping), truncating overflow with an ellipsis.
///
/// This path always uses viewport-local width alignment and is best for dense
/// list UIs where multi-line descriptions would add too much vertical churn.
/// Returns the number of terminal lines actually rendered.
pub(crate) fn render_rows_single_line(
    area: Rect,
    buf: &mut Buffer,
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    empty_message: &str,
) -> u16 {
    if rows_all.is_empty() {
        if area.height > 0 {
            Line::from(empty_message.dim().italic()).render(area, buf);
        }
        // Count the placeholder line only when there is vertical space to draw it.
        return u16::from(area.height > 0);
    }

    let visible_items = max_results
        .min(rows_all.len())
        .min(area.height.max(1) as usize);

    let mut start_idx = state.scroll_top.min(rows_all.len().saturating_sub(1));
    if let Some(sel) = state.selected_idx {
        if sel < start_idx {
            start_idx = sel;
        } else if visible_items > 0 {
            let bottom = start_idx + visible_items - 1;
            if sel > bottom {
                start_idx = sel + 1 - visible_items;
            }
        }
    }

    let desc_col = compute_desc_col(
        rows_all,
        start_idx,
        visible_items,
        area.width,
        ColumnWidthMode::AutoVisible,
    );

    let mut cur_y = area.y;
    let mut rendered_lines: u16 = 0;
    for (i, row) in rows_all
        .iter()
        .enumerate()
        .skip(start_idx)
        .take(visible_items)
    {
        if cur_y >= area.y + area.height {
            break;
        }

        let mut lines = vec![build_full_line(row, desc_col, area.width.max(1) as usize)];
        apply_row_state_style(
            &mut lines,
            Some(i) == state.selected_idx && !row.is_disabled,
            row.is_disabled,
        );
        let full_line =
            truncate_line_with_ellipsis_if_overflow(lines.remove(0), area.width as usize);
        full_line.render(
            Rect {
                x: area.x,
                y: cur_y,
                width: area.width,
                height: 1,
            },
            buf,
        );
        cur_y = cur_y.saturating_add(1);
        rendered_lines = rendered_lines.saturating_add(1);
    }

    rendered_lines
}

/// Compute the number of terminal rows required to render up to `max_results`
/// items from `rows_all` given the current scroll/selection state and the
/// available `width`. Accounts for description wrapping and alignment so the
/// caller can allocate sufficient vertical space.
///
/// This function matches [`render_rows`] semantics (`AutoVisible` column
/// sizing). Mixing it with stable or fixed render modes can under- or
/// over-estimate required height.
pub(crate) fn measure_rows_height(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    width: u16,
) -> u16 {
    measure_rows_height_inner(
        rows_all,
        state,
        max_results,
        width,
        ColumnWidthMode::AutoVisible,
    )
}

/// Measures selection-row height while using full-dataset column alignment.
/// This should be paired with [`render_rows_stable_col_widths`] so layout
/// reservation matches rendering behavior.
pub(crate) fn measure_rows_height_stable_col_widths(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    width: u16,
) -> u16 {
    measure_rows_height_inner(
        rows_all,
        state,
        max_results,
        width,
        ColumnWidthMode::AutoAllRows,
    )
}

/// Measure selection-row height using explicit [`ColumnWidthMode`] behavior.
///
/// This is the low-level companion to [`render_rows_with_col_width_mode`].
pub(crate) fn measure_rows_height_with_col_width_mode(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    width: u16,
    col_width_mode: ColumnWidthMode,
) -> u16 {
    measure_rows_height_inner(rows_all, state, max_results, width, col_width_mode)
}

pub(crate) fn visible_row_line_counts(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    width: u16,
    col_width_mode: ColumnWidthMode,
) -> Vec<u16> {
    if rows_all.is_empty() {
        return Vec::new();
    }

    let content_width = width.saturating_sub(1).max(1);
    let visible_items = max_results.min(rows_all.len());
    let mut start_idx = state.scroll_top.min(rows_all.len().saturating_sub(1));
    if let Some(sel) = state.selected_idx {
        if sel < start_idx {
            start_idx = sel;
        } else if visible_items > 0 {
            let bottom = start_idx + visible_items - 1;
            if sel > bottom {
                start_idx = sel + 1 - visible_items;
            }
        }
    }

    let desc_col = compute_desc_col(
        rows_all,
        start_idx,
        visible_items,
        content_width,
        col_width_mode,
    );

    rows_all
        .iter()
        .enumerate()
        .skip(start_idx)
        .take(visible_items)
        .map(|(_, row)| wrap_row_lines(row, desc_col, content_width).len() as u16)
        .collect()
}

fn measure_rows_height_inner(
    rows_all: &[GenericDisplayRow],
    state: &ScrollState,
    max_results: usize,
    width: u16,
    col_width_mode: ColumnWidthMode,
) -> u16 {
    if rows_all.is_empty() {
        return 1; // placeholder "no matches" line
    }

    let content_width = width.saturating_sub(1).max(1);

    let visible_items = max_results.min(rows_all.len());
    let mut start_idx = state.scroll_top.min(rows_all.len().saturating_sub(1));
    if let Some(sel) = state.selected_idx {
        if sel < start_idx {
            start_idx = sel;
        } else if visible_items > 0 {
            let bottom = start_idx + visible_items - 1;
            if sel > bottom {
                start_idx = sel + 1 - visible_items;
            }
        }
    }

    let desc_col = compute_desc_col(
        rows_all,
        start_idx,
        visible_items,
        content_width,
        col_width_mode,
    );

    let mut total: u16 = 0;
    for row in rows_all
        .iter()
        .enumerate()
        .skip(start_idx)
        .take(visible_items)
        .map(|(_, r)| r)
    {
        let wrapped_lines = wrap_row_lines(row, desc_col, content_width).len();
        total = total.saturating_add(wrapped_lines as u16);
    }
    total.max(1)
}

#[cfg(test)]
mod tests {
    use super::*;
    use pretty_assertions::assert_eq;

    #[test]
    fn default_selected_row_style_uses_light_preset() {
        let style = selected_row_style();
        assert_eq!(style.fg, Some(Color::Black));
        assert_eq!(style.bg, Some(Color::White));
    }

    #[test]
    fn text_only_selection_style_clears_background_fill() {
        set_selection_highlight_preset(SelectionHighlightPreset::Mint);
        set_selection_highlight_fill(false);
        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());

        let style = selection_highlight_style();
        assert_eq!(style.bg, Some(Color::Reset));
        assert!(matches!(style.fg, Some(Color::Rgb(..))));

        set_selection_highlight_fill(true);
        set_selection_highlight_preset(SelectionHighlightPreset::Light);
    }

    #[test]
    fn visible_text_guard_preserves_light_colors() {
        let color = Color::Rgb(240, 236, 230);
        assert_eq!(ensure_visible_text_color(color, false), color);
        assert_eq!(ensure_visible_text_color(color, true), color);
    }

    #[test]
    fn selected_secondary_spans_drop_dim_and_keep_contrast() {
        set_selection_highlight_preset(SelectionHighlightPreset::Rose);
        set_selection_highlight_fill(false);
        set_selection_highlight_text_formats(
            SelectionHighlightTextFormats::empty()
                .with_toggled(SelectionHighlightTextFormat::Italic),
        );

        let mut lines = vec![Line::from(vec![
            Span::styled("Name", Style::default()),
            Span::styled(" description", Style::default().dim()),
        ])];
        apply_row_state_style(&mut lines, true, false);

        assert!(!lines[0].spans[1].style.add_modifier.contains(Modifier::DIM));
        assert_eq!(lines[0].spans[1].style.bg, Some(Color::Reset));
        assert!(lines[0].spans[1].style.add_modifier.contains(Modifier::ITALIC));

        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());
        set_selection_highlight_fill(true);
        set_selection_highlight_preset(SelectionHighlightPreset::Light);
    }

    #[test]
    fn selected_rows_retone_explicit_foreground_when_fill_would_hide_it() {
        set_selection_highlight_preset(SelectionHighlightPreset::Light);
        set_selection_highlight_fill(true);
        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());

        let mut lines = vec![Line::from(vec![Span::styled(
            "Белый swatch",
            Style::default().fg(Color::White),
        )])];
        apply_row_state_style(&mut lines, true, false);

        assert_ne!(lines[0].spans[0].style.fg, Some(Color::White));
        assert_eq!(lines[0].spans[0].style.bg, Some(Color::White));
    }

    #[test]
    fn text_only_selection_recolors_default_row_text() {
        set_selection_highlight_preset(SelectionHighlightPreset::Mint);
        set_selection_highlight_fill(false);
        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());

        let normal_primary = unselected_row_styles().0.fg;
        let expected_selected = selection_highlight_style().fg;
        let mut lines = vec![Line::from(vec![Span::styled(
            "plain row",
            unselected_row_styles().0,
        )])];
        apply_row_state_style(&mut lines, true, false);

        assert_eq!(lines[0].spans[0].style.bg, Some(Color::Reset));
        assert_eq!(lines[0].spans[0].style.fg, expected_selected);
        assert_ne!(lines[0].spans[0].style.fg, normal_primary);

        set_selection_highlight_fill(true);
        set_selection_highlight_preset(SelectionHighlightPreset::Light);
    }

    #[test]
    fn text_only_selection_preserves_explicit_color_examples() {
        set_selection_highlight_preset(SelectionHighlightPreset::Graphite);
        set_selection_highlight_fill(false);
        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());

        let explicit = Color::Rgb(79, 140, 255);
        let mut lines = vec![Line::from(vec![Span::styled(
            "preview",
            Style::default().fg(explicit),
        )])];
        apply_row_state_style(&mut lines, true, false);

        assert_eq!(lines[0].spans[0].style.fg, Some(explicit));
        assert_eq!(lines[0].spans[0].style.bg, Some(Color::Reset));

        set_selection_highlight_fill(true);
        set_selection_highlight_preset(SelectionHighlightPreset::Light);
    }

    #[test]
    fn explicit_secondary_choice_keeps_light_color_without_forced_darkening() {
        let color = resolve_text_color_choice(
            UiColorChoice::Custom("#ffffff".to_string()),
            true,
            SelectionHighlightPreset::Graphite,
        );

        assert_eq!(color, Some(Color::White));
    }

    #[test]
    fn one_cell_width_falls_back_without_panic_for_wrapped_two_column_rows() {
        let row = GenericDisplayRow {
            name: "1. Very long option label".to_string(),
            description: Some("Very long description".to_string()),
            wrap_indent: Some(4),
            ..Default::default()
        };

        let two_col = wrap_two_column_row(&row, /*desc_col*/ 0, /*width*/ 1);
        assert_eq!(two_col.len(), 0);
    }

    #[test]
    fn state_badges_use_compact_icons_with_green_and_red_pills() {
        let positive = badge_spans(&["enabled".to_string()]);
        let negative = badge_spans(&["disabled".to_string()]);

        assert_eq!(positive[1].content.as_ref(), " ✓ ");
        assert_eq!(positive[1].style.bg, Some(Color::Green));
        assert_eq!(negative[1].content.as_ref(), " ✕ ");
        assert_eq!(negative[1].style.bg, Some(Color::Red));
    }

    #[test]
    fn unsupported_text_formats_are_sanitized_in_text_only_mode() {
        set_selection_highlight_fill(false);
        set_selection_highlight_text_formats(
            SelectionHighlightTextFormats::empty()
                .with_toggled(SelectionHighlightTextFormat::Dim)
                .with_toggled(SelectionHighlightTextFormat::Reversed)
                .with_toggled(SelectionHighlightTextFormat::CrossedOut),
        );

        let style = selection_highlight_style();
        assert!(!style.add_modifier.contains(Modifier::DIM));
        assert!(!style.add_modifier.contains(Modifier::REVERSED));
        assert!(style.add_modifier.contains(Modifier::CROSSED_OUT));

        set_selection_highlight_fill(true);
        set_selection_highlight_text_formats(SelectionHighlightTextFormats::empty());
    }
}
