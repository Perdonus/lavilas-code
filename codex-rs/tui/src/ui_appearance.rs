use ratatui::style::Color;
use ratatui::style::Modifier;
use ratatui::style::Style;
use ratatui::text::Span;

use crate::color::blend;
use crate::color::is_light;
use crate::terminal_palette::best_color;
use crate::ui_preferences::SelectionHighlightPreset;
use crate::ui_preferences::SelectionHighlightTextFormat;
use crate::ui_preferences::SelectionHighlightTextFormats;
use crate::ui_preferences::UiColorChoice;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct NamedColor {
    pub(crate) name_ru: &'static str,
    pub(crate) name_en: &'static str,
    pub(crate) hex: &'static str,
    pub(crate) rgb: (u8, u8, u8),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct SelectionPaletteSpec {
    pub(crate) fill_bg: (u8, u8, u8),
    pub(crate) fill_bg_emphasis: (u8, u8, u8),
    pub(crate) fill_fg: (u8, u8, u8),
    pub(crate) fill_secondary_fg: (u8, u8, u8),
    pub(crate) fill_secondary_fg_emphasis: (u8, u8, u8),
    pub(crate) text_fg: (u8, u8, u8),
    pub(crate) text_fg_emphasis: (u8, u8, u8),
    pub(crate) text_secondary_fg: (u8, u8, u8),
    pub(crate) text_secondary_fg_emphasis: (u8, u8, u8),
    pub(crate) mono_text_fg: (u8, u8, u8),
    pub(crate) mono_text_secondary_fg: (u8, u8, u8),
}

const NAMED_COLORS: &[NamedColor] = &[
    NamedColor { name_ru: "Белый", name_en: "White", hex: "#ffffff", rgb: (255, 255, 255) },
    NamedColor { name_ru: "Фарфор", name_en: "Porcelain", hex: "#f7f6f2", rgb: (247, 246, 242) },
    NamedColor { name_ru: "Жемчужный", name_en: "Pearl", hex: "#f0ece4", rgb: (240, 236, 228) },
    NamedColor { name_ru: "Серебро", name_en: "Silver", hex: "#d8dce3", rgb: (216, 220, 227) },
    NamedColor { name_ru: "Графит", name_en: "Graphite", hex: "#495057", rgb: (73, 80, 87) },
    NamedColor { name_ru: "Антрацит", name_en: "Anthracite", hex: "#2f3438", rgb: (47, 52, 56) },
    NamedColor { name_ru: "Уголь", name_en: "Coal", hex: "#17191c", rgb: (23, 25, 28) },
    NamedColor { name_ru: "Песочный", name_en: "Sand", hex: "#e9ddc8", rgb: (233, 221, 200) },
    NamedColor { name_ru: "Кремовый", name_en: "Cream", hex: "#f2dfb4", rgb: (242, 223, 180) },
    NamedColor { name_ru: "Янтарный", name_en: "Amber", hex: "#f0d6a2", rgb: (240, 214, 162) },
    NamedColor { name_ru: "Медовый", name_en: "Honey", hex: "#e3c678", rgb: (227, 198, 120) },
    NamedColor { name_ru: "Персиковый", name_en: "Peach", hex: "#efc5ae", rgb: (239, 197, 174) },
    NamedColor { name_ru: "Коралловый", name_en: "Coral", hex: "#e89c91", rgb: (232, 156, 145) },
    NamedColor { name_ru: "Розовый", name_en: "Rose", hex: "#efc1d2", rgb: (239, 193, 210) },
    NamedColor { name_ru: "Пудровый", name_en: "Powder Rose", hex: "#e3afc2", rgb: (227, 175, 194) },
    NamedColor { name_ru: "Лиловый", name_en: "Lilac", hex: "#d8c2e8", rgb: (216, 194, 232) },
    NamedColor { name_ru: "Лавандовый", name_en: "Lavender", hex: "#c7b4e5", rgb: (199, 180, 229) },
    NamedColor { name_ru: "Небесный", name_en: "Sky", hex: "#bfd8ef", rgb: (191, 216, 239) },
    NamedColor { name_ru: "Ледяной", name_en: "Ice", hex: "#d7ecf3", rgb: (215, 236, 243) },
    NamedColor { name_ru: "Океанский", name_en: "Ocean", hex: "#5a8fb3", rgb: (90, 143, 179) },
    NamedColor { name_ru: "Сапфировый", name_en: "Sapphire", hex: "#326f9c", rgb: (50, 111, 156) },
    NamedColor { name_ru: "Мятный", name_en: "Mint", hex: "#bfe4ce", rgb: (191, 228, 206) },
    NamedColor { name_ru: "Шалфей", name_en: "Sage", hex: "#9cc4ac", rgb: (156, 196, 172) },
    NamedColor { name_ru: "Изумрудный", name_en: "Emerald", hex: "#4f8c73", rgb: (79, 140, 115) },
    NamedColor { name_ru: "Лаймовый", name_en: "Lime", hex: "#bfdc7a", rgb: (191, 220, 122) },
    NamedColor { name_ru: "Бирюзовый", name_en: "Turquoise", hex: "#81d2cf", rgb: (129, 210, 207) },
    NamedColor { name_ru: "Голубой", name_en: "Azure", hex: "#75b8dd", rgb: (117, 184, 221) },
    NamedColor { name_ru: "Индиго", name_en: "Indigo", hex: "#5261a8", rgb: (82, 97, 168) },
    NamedColor { name_ru: "Сливовый", name_en: "Plum", hex: "#875b8d", rgb: (135, 91, 141) },
    NamedColor { name_ru: "Какао", name_en: "Cocoa", hex: "#8a6d5a", rgb: (138, 109, 90) },
    NamedColor { name_ru: "Шоколадный", name_en: "Chocolate", hex: "#6d4c41", rgb: (109, 76, 65) },
    NamedColor { name_ru: "Красный", name_en: "Red", hex: "#d35b62", rgb: (211, 91, 98) },
];

pub(crate) fn selection_preset_color(preset: SelectionHighlightPreset) -> NamedColor {
    match preset {
        SelectionHighlightPreset::Light => NamedColor {
            name_ru: "Светлый",
            name_en: "Light",
            hex: "#ffffff",
            rgb: (255, 255, 255),
        },
        SelectionHighlightPreset::Graphite => NamedColor {
            name_ru: "Графит",
            name_en: "Graphite",
            hex: "#495057",
            rgb: (73, 80, 87),
        },
        SelectionHighlightPreset::Amber => NamedColor {
            name_ru: "Янтарь",
            name_en: "Amber",
            hex: "#f0d6a2",
            rgb: (240, 214, 162),
        },
        SelectionHighlightPreset::Mint => NamedColor {
            name_ru: "Мята",
            name_en: "Mint",
            hex: "#bfe4ce",
            rgb: (191, 228, 206),
        },
        SelectionHighlightPreset::Rose => NamedColor {
            name_ru: "Роза",
            name_en: "Rose",
            hex: "#efc1d2",
            rgb: (239, 193, 210),
        },
    }
}

pub(crate) fn parse_hex_color(hex: &str) -> Option<(u8, u8, u8)> {
    let hex = hex.trim().strip_prefix('#').unwrap_or(hex.trim());
    if hex.len() != 6 || !hex.chars().all(|ch| ch.is_ascii_hexdigit()) {
        return None;
    }
    let r = u8::from_str_radix(&hex[0..2], 16).ok()?;
    let g = u8::from_str_radix(&hex[2..4], 16).ok()?;
    let b = u8::from_str_radix(&hex[4..6], 16).ok()?;
    Some((r, g, b))
}

pub(crate) fn describe_color_choice(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_ru: bool,
) -> String {
    match choice {
        UiColorChoice::Auto => {
            let named = selection_preset_color(fallback_preset);
            if is_ru {
                format!("Авто · {}", named.name_ru)
            } else {
                format!("Auto · {}", named.name_en)
            }
        }
        UiColorChoice::Preset(preset) => {
            let named = selection_preset_color(*preset);
            if is_ru {
                named.name_ru.to_string()
            } else {
                named.name_en.to_string()
            }
        }
        UiColorChoice::Custom(hex) => {
            let named = named_color_for_hex(hex);
            if is_ru {
                format!("{} {}", named.name_ru, hex.to_ascii_uppercase())
            } else {
                format!("{} {}", named.name_en, hex.to_ascii_uppercase())
            }
        }
    }
}

pub(crate) fn named_color_for_hex(hex: &str) -> NamedColor {
    let Some(rgb) = parse_hex_color(hex) else {
        return NamedColor {
            name_ru: "Свой цвет",
            name_en: "Custom",
            hex: "#000000",
            rgb: (0, 0, 0),
        };
    };
    *NAMED_COLORS
        .iter()
        .min_by(|a, b| {
            crate::color::perceptual_distance(a.rgb, rgb)
                .partial_cmp(&crate::color::perceptual_distance(b.rgb, rgb))
                .unwrap_or(std::cmp::Ordering::Equal)
        })
        .unwrap_or(&NAMED_COLORS[0])
}

pub(crate) fn resolve_color_choice_rgb(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> (u8, u8, u8) {
    match choice {
        UiColorChoice::Auto => selection_preset_color(fallback_preset).rgb,
        UiColorChoice::Preset(preset) => selection_preset_color(*preset).rgb,
        UiColorChoice::Custom(hex) => parse_hex_color(hex).unwrap_or(selection_preset_color(fallback_preset).rgb),
    }
}

pub(crate) fn selection_palette_for_choice(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> SelectionPaletteSpec {
    let base = resolve_color_choice_rgb(choice, fallback_preset);
    let fill_fg = if is_light(base) { (15, 15, 15) } else { (250, 250, 250) };
    let fill_secondary_fg = if is_light(base) { (58, 58, 58) } else { (220, 223, 228) };
    let fill_secondary_fg_emphasis = if is_light(base) { (42, 42, 42) } else { (238, 240, 244) };
    let text_base = if base == (255, 255, 255) {
        (245, 245, 245)
    } else {
        blend(base, if is_light(base) { (255, 255, 255) } else { (240, 240, 240) }, if is_light(base) { 0.78 } else { 0.92 })
    };
    let text_emphasis = if is_light(base) {
        blend(base, (255, 255, 255), 0.88)
    } else {
        blend(base, (255, 255, 255), 0.97)
    };
    let text_secondary = if is_light(base) {
        blend(base, (70, 70, 70), 0.72)
    } else {
        blend(base, (180, 185, 195), 0.82)
    };
    let text_secondary_emphasis = if is_light(base) {
        blend(base, (55, 55, 55), 0.8)
    } else {
        blend(base, (220, 225, 230), 0.9)
    };
    let fill_bg_emphasis = if is_light(base) {
        blend(base, (214, 214, 214), 0.88)
    } else {
        blend(base, (28, 30, 33), 0.9)
    };
    let mono_fg = if is_light(base) {
        blend(text_base, (110, 95, 70), 0.75)
    } else {
        blend(text_base, (250, 250, 250), 0.92)
    };
    let mono_secondary = if is_light(base) {
        blend(text_secondary, (105, 90, 72), 0.7)
    } else {
        blend(text_secondary, (215, 220, 228), 0.9)
    };

    SelectionPaletteSpec {
        fill_bg: base,
        fill_bg_emphasis,
        fill_fg,
        fill_secondary_fg,
        fill_secondary_fg_emphasis,
        text_fg: text_base,
        text_fg_emphasis: text_emphasis,
        text_secondary_fg: text_secondary,
        text_secondary_fg_emphasis: text_secondary_emphasis,
        mono_text_fg: mono_fg,
        mono_text_secondary_fg: mono_secondary,
    }
}

pub(crate) fn best_terminal_color(rgb: (u8, u8, u8)) -> Color {
    best_color(rgb)
}

pub(crate) fn styled_color_label_spans(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_ru: bool,
) -> Vec<Span<'static>> {
    let label = describe_color_choice(choice, fallback_preset, is_ru);
    let rgb = resolve_color_choice_rgb(choice, fallback_preset);
    vec![Span::styled(label, Style::default().fg(best_terminal_color(rgb)))]
}

pub(crate) fn apply_text_formats(mut style: Style, formats: SelectionHighlightTextFormats) -> Style {
    if formats.contains(SelectionHighlightTextFormat::Bold) {
        style = style.add_modifier(Modifier::BOLD);
    }
    if formats.contains(SelectionHighlightTextFormat::Semibold) {
        style = style.add_modifier(Modifier::BOLD);
    }
    if formats.contains(SelectionHighlightTextFormat::Italic) {
        style = style.add_modifier(Modifier::ITALIC);
    }
    if formats.contains(SelectionHighlightTextFormat::Underlined) {
        style = style.add_modifier(Modifier::UNDERLINED);
    }
    if formats.contains(SelectionHighlightTextFormat::Dim) {
        style = style.add_modifier(Modifier::DIM);
    }
    if formats.contains(SelectionHighlightTextFormat::Reversed) {
        style = style.add_modifier(Modifier::REVERSED);
    }
    if formats.contains(SelectionHighlightTextFormat::CrossedOut) {
        style = style.add_modifier(Modifier::CROSSED_OUT);
    }
    style
}

pub(crate) fn format_preview_label(
    format: SelectionHighlightTextFormat,
    is_ru: bool,
) -> (&'static str, Style, Option<&'static str>) {
    match (is_ru, format) {
        (true, SelectionHighlightTextFormat::Bold) => ("Жирный", Style::default().add_modifier(Modifier::BOLD), None),
        (false, SelectionHighlightTextFormat::Bold) => ("Bold", Style::default().add_modifier(Modifier::BOLD), None),
        (true, SelectionHighlightTextFormat::Semibold) => ("Полужирный", Style::default().add_modifier(Modifier::BOLD), None),
        (false, SelectionHighlightTextFormat::Semibold) => ("Semi-bold", Style::default().add_modifier(Modifier::BOLD), None),
        (true, SelectionHighlightTextFormat::Italic) => ("Курсив", Style::default().add_modifier(Modifier::ITALIC), None),
        (false, SelectionHighlightTextFormat::Italic) => ("Italic", Style::default().add_modifier(Modifier::ITALIC), None),
        (true, SelectionHighlightTextFormat::Underlined) => ("Подчёркнутый", Style::default().add_modifier(Modifier::UNDERLINED), None),
        (false, SelectionHighlightTextFormat::Underlined) => ("Underlined", Style::default().add_modifier(Modifier::UNDERLINED), None),
        (true, SelectionHighlightTextFormat::Mono) => ("Моно", Style::default(), Some("</>")),
        (false, SelectionHighlightTextFormat::Mono) => ("Monospace", Style::default(), Some("</>")),
        (true, SelectionHighlightTextFormat::Dim) => ("Приглушённый", Style::default().add_modifier(Modifier::DIM), None),
        (false, SelectionHighlightTextFormat::Dim) => ("Dim", Style::default().add_modifier(Modifier::DIM), None),
        (true, SelectionHighlightTextFormat::Reversed) => ("Инверсия", Style::default().add_modifier(Modifier::REVERSED), None),
        (false, SelectionHighlightTextFormat::Reversed) => ("Reversed", Style::default().add_modifier(Modifier::REVERSED), None),
        (true, SelectionHighlightTextFormat::CrossedOut) => ("Зачёркнутый", Style::default().add_modifier(Modifier::CROSSED_OUT), None),
        (false, SelectionHighlightTextFormat::CrossedOut) => ("Crossed out", Style::default().add_modifier(Modifier::CROSSED_OUT), None),
    }
}

pub(crate) fn color_preview_description(choice: &UiColorChoice, fallback_preset: SelectionHighlightPreset, is_ru: bool) -> String {
    let named = match choice {
        UiColorChoice::Custom(hex) => named_color_for_hex(hex),
        UiColorChoice::Preset(preset) => selection_preset_color(*preset),
        UiColorChoice::Auto => selection_preset_color(fallback_preset),
    };
    if is_ru {
        format!("Оттенок: {} · {}", named.name_ru, named.hex.to_ascii_uppercase())
    } else {
        format!("Shade: {} · {}", named.name_en, named.hex.to_ascii_uppercase())
    }
}
