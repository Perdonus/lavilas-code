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
    pub(crate) fill_secondary_bg: (u8, u8, u8),
    pub(crate) fill_secondary_bg_emphasis: (u8, u8, u8),
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
    NamedColor { name_ru: "Дымчатый", name_en: "Smoke", hex: "#8d96a3", rgb: (141, 150, 163) },
    NamedColor { name_ru: "Стальной", name_en: "Steel", hex: "#6a7280", rgb: (106, 114, 128) },
    NamedColor { name_ru: "Сланцевый", name_en: "Slate", hex: "#58616d", rgb: (88, 97, 109) },
    NamedColor { name_ru: "Тень", name_en: "Shadow", hex: "#414952", rgb: (65, 73, 82) },
    NamedColor { name_ru: "Графит", name_en: "Graphite", hex: "#495057", rgb: (73, 80, 87) },
    NamedColor { name_ru: "Антрацит", name_en: "Anthracite", hex: "#2f3438", rgb: (47, 52, 56) },
    NamedColor { name_ru: "Чернильный", name_en: "Ink", hex: "#263042", rgb: (38, 48, 66) },
    NamedColor { name_ru: "Оникс", name_en: "Onyx", hex: "#202428", rgb: (32, 36, 40) },
    NamedColor { name_ru: "Уголь", name_en: "Coal", hex: "#17191c", rgb: (23, 25, 28) },
    NamedColor { name_ru: "Глубокий графит", name_en: "Deep Graphite", hex: "#30363d", rgb: (48, 54, 61) },
    NamedColor { name_ru: "Тёмный шифер", name_en: "Dark Slate", hex: "#37404a", rgb: (55, 64, 74) },
    NamedColor { name_ru: "Базальт", name_en: "Basalt", hex: "#2b3138", rgb: (43, 49, 56) },
    NamedColor { name_ru: "Вулканический", name_en: "Volcanic", hex: "#23282d", rgb: (35, 40, 45) },
    NamedColor { name_ru: "Сумеречный", name_en: "Dusk", hex: "#3d4357", rgb: (61, 67, 87) },
    NamedColor { name_ru: "Тёмная хвоя", name_en: "Dark Pine", hex: "#223a34", rgb: (34, 58, 52) },
    NamedColor { name_ru: "Тёмный мох", name_en: "Dark Moss", hex: "#364637", rgb: (54, 70, 55) },
    NamedColor { name_ru: "Ночной бордо", name_en: "Night Bordeaux", hex: "#4c2933", rgb: (76, 41, 51) },
    NamedColor { name_ru: "Тёмная слива", name_en: "Dark Plum", hex: "#432b49", rgb: (67, 43, 73) },
    NamedColor { name_ru: "Полночный", name_en: "Midnight", hex: "#1f2a44", rgb: (31, 42, 68) },
    NamedColor { name_ru: "Ночной синий", name_en: "Navy", hex: "#24324f", rgb: (36, 50, 79) },
    NamedColor { name_ru: "Петрольный", name_en: "Petrol", hex: "#335b63", rgb: (51, 91, 99) },
    NamedColor { name_ru: "Глубокая волна", name_en: "Deep Sea", hex: "#22485e", rgb: (34, 72, 94) },
    NamedColor { name_ru: "Лесной", name_en: "Forest", hex: "#355a46", rgb: (53, 90, 70) },
    NamedColor { name_ru: "Моховой", name_en: "Moss", hex: "#51684a", rgb: (81, 104, 74) },
    NamedColor { name_ru: "Хвойный", name_en: "Pine", hex: "#28453b", rgb: (40, 69, 59) },
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
    NamedColor { name_ru: "Баклажановый", name_en: "Aubergine", hex: "#5c3b63", rgb: (92, 59, 99) },
    NamedColor { name_ru: "Бордовый", name_en: "Bordeaux", hex: "#6a3946", rgb: (106, 57, 70) },
    NamedColor { name_ru: "Шёлковица", name_en: "Mulberry", hex: "#4b324f", rgb: (75, 50, 79) },
    NamedColor { name_ru: "Какао", name_en: "Cocoa", hex: "#8a6d5a", rgb: (138, 109, 90) },
    NamedColor { name_ru: "Шоколадный", name_en: "Chocolate", hex: "#6d4c41", rgb: (109, 76, 65) },
    NamedColor { name_ru: "Ржавчина", name_en: "Rust", hex: "#7b4d3d", rgb: (123, 77, 61) },
    NamedColor { name_ru: "Красный", name_en: "Red", hex: "#d35b62", rgb: (211, 91, 98) },
];

pub(crate) fn all_named_colors() -> &'static [NamedColor] {
    NAMED_COLORS
}

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
                format!("Авто · {}", named.name_ru)
            }
        }
        UiColorChoice::Preset(preset) => {
            let named = selection_preset_color(*preset);
            if is_ru {
                named.name_ru.to_string()
            } else {
                named.name_ru.to_string()
            }
        }
        UiColorChoice::Custom(hex) => {
            let named = named_color_for_hex(hex);
            if is_ru {
                format!("{} {}", named.name_ru, hex.to_ascii_uppercase())
            } else {
                format!("{} {}", named.name_ru, hex.to_ascii_uppercase())
            }
        }
        UiColorChoice::Gradient { start, end } => {
            let start_named = named_color_for_hex(start);
            let end_named = named_color_for_hex(end);
            if is_ru {
                format!("Градиент · {} → {}", start_named.name_ru, end_named.name_ru)
            } else {
                format!("Градиент · {} → {}", start_named.name_ru, end_named.name_ru)
            }
        }
    }
}

pub(crate) fn named_color_for_hex(hex: &str) -> NamedColor {
    let Some(rgb) = parse_hex_color(hex) else {
        return NamedColor {
            name_ru: "Свой цвет",
            name_en: "Свой цвет",
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

fn resolve_color_choice_pair_rgb(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> ((u8, u8, u8), (u8, u8, u8)) {
    match choice {
        UiColorChoice::Auto => {
            let rgb = selection_preset_color(fallback_preset).rgb;
            (rgb, rgb)
        }
        UiColorChoice::Preset(preset) => {
            let rgb = selection_preset_color(*preset).rgb;
            (rgb, rgb)
        }
        UiColorChoice::Custom(hex) => {
            let rgb = parse_hex_color(hex).unwrap_or(selection_preset_color(fallback_preset).rgb);
            (rgb, rgb)
        }
        UiColorChoice::Gradient { start, end } => (
            parse_hex_color(start).unwrap_or(selection_preset_color(fallback_preset).rgb),
            parse_hex_color(end).unwrap_or(selection_preset_color(fallback_preset).rgb),
        ),
    }
}

pub(crate) fn resolve_color_choice_rgb(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> (u8, u8, u8) {
    let (start, end) = resolve_color_choice_pair_rgb(choice, fallback_preset);
    if start == end {
        start
    } else {
        blend(start, end, 0.5)
    }
}

pub(crate) fn resolve_color_choice_label_rgb(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_secondary: bool,
) -> (u8, u8, u8) {
    let (start, end) = resolve_color_choice_pair_rgb(choice, fallback_preset);
    if is_secondary { end } else { start }
}

pub(crate) fn selection_palette_for_choice(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
) -> SelectionPaletteSpec {
    let (primary_base, secondary_base) = resolve_color_choice_pair_rgb(choice, fallback_preset);
    let fill_fg = if is_light(primary_base) {
        (15, 15, 15)
    } else {
        (250, 250, 250)
    };
    let fill_secondary_fg = if is_light(secondary_base) {
        (58, 58, 58)
    } else {
        (220, 223, 228)
    };
    let fill_secondary_fg_emphasis = if is_light(secondary_base) {
        (42, 42, 42)
    } else {
        (238, 240, 244)
    };
    let text_base = if primary_base == (255, 255, 255) {
        (245, 245, 245)
    } else {
        blend(
            primary_base,
            if is_light(primary_base) {
                (255, 255, 255)
            } else {
                (240, 240, 240)
            },
            if is_light(primary_base) { 0.78 } else { 0.92 },
        )
    };
    let text_emphasis = if is_light(primary_base) {
        blend(primary_base, (255, 255, 255), 0.88)
    } else {
        blend(primary_base, (255, 255, 255), 0.97)
    };
    let text_secondary = if is_light(secondary_base) {
        blend(secondary_base, (70, 70, 70), 0.72)
    } else {
        blend(secondary_base, (180, 185, 195), 0.82)
    };
    let text_secondary_emphasis = if is_light(secondary_base) {
        blend(secondary_base, (55, 55, 55), 0.8)
    } else {
        blend(secondary_base, (220, 225, 230), 0.9)
    };
    let fill_bg_emphasis = if is_light(primary_base) {
        blend(primary_base, (214, 214, 214), 0.88)
    } else {
        blend(primary_base, (28, 30, 33), 0.9)
    };
    let fill_secondary_bg_emphasis = if is_light(secondary_base) {
        blend(secondary_base, (214, 214, 214), 0.88)
    } else {
        blend(secondary_base, (28, 30, 33), 0.9)
    };
    let mono_fg = if is_light(primary_base) {
        blend(text_base, (92, 64, 42), 0.62)
    } else {
        blend(text_base, (128, 214, 255), 0.36)
    };
    let mono_secondary = if is_light(secondary_base) {
        blend(text_secondary, (92, 64, 42), 0.56)
    } else {
        blend(text_secondary, (128, 214, 255), 0.3)
    };

    SelectionPaletteSpec {
        fill_bg: primary_base,
        fill_bg_emphasis,
        fill_secondary_bg: secondary_base,
        fill_secondary_bg_emphasis,
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

fn apply_foreground_accent(mut style: Style, tint: (u8, u8, u8), weight: f32) -> Style {
    let accented = style
        .fg
        .and_then(color_to_rgb)
        .map(|current| best_terminal_color(blend(current, tint, weight)))
        .unwrap_or_else(|| best_terminal_color(tint));
    style.fg = Some(accented);
    style
}

pub(crate) fn styled_color_label_spans(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_ru: bool,
) -> Vec<Span<'static>> {
    match choice {
        UiColorChoice::Gradient { start, end } => {
            let start_named = named_color_for_hex(start);
            let end_named = named_color_for_hex(end);
            vec![
                Span::styled(
                    if is_ru {
                        start_named.name_ru.to_string()
                    } else {
                        start_named.name_en.to_string()
                    },
                    Style::default().fg(best_terminal_color(start_named.rgb)),
                ),
                Span::raw(" → "),
                Span::styled(
                    if is_ru {
                        end_named.name_ru.to_string()
                    } else {
                        end_named.name_en.to_string()
                    },
                    Style::default().fg(best_terminal_color(end_named.rgb)),
                ),
            ]
        }
        _ => {
            let label = describe_color_choice(choice, fallback_preset, is_ru);
            let rgb = resolve_color_choice_rgb(choice, fallback_preset);
            vec![Span::styled(label, Style::default().fg(best_terminal_color(rgb)))]
        }
    }
}

pub(crate) fn styled_choice_label_spans(
    label: &str,
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_secondary: bool,
) -> Vec<Span<'static>> {
    if let UiColorChoice::Gradient { .. } = choice {
        let primary_rgb = resolve_color_choice_label_rgb(choice, fallback_preset, false);
        let secondary_rgb = resolve_color_choice_label_rgb(choice, fallback_preset, true);
        let characters = label.chars().collect::<Vec<_>>();
        if characters.len() <= 1 {
            return vec![Span::styled(
                label.to_string(),
                Style::default().fg(best_terminal_color(if is_secondary {
                    secondary_rgb
                } else {
                    primary_rgb
                })),
            )];
        }

        let midpoint = (characters.len() / 2).max(1);
        let left = characters[..midpoint].iter().collect::<String>();
        let right = characters[midpoint..].iter().collect::<String>();
        let mut spans = Vec::new();
        if !left.is_empty() {
            spans.push(Span::styled(
                left,
                Style::default().fg(best_terminal_color(primary_rgb)),
            ));
        }
        if !right.is_empty() {
            spans.push(Span::styled(
                right,
                Style::default().fg(best_terminal_color(secondary_rgb)),
            ));
        }
        return spans;
    }

    let rgb = resolve_color_choice_label_rgb(choice, fallback_preset, is_secondary);
    vec![Span::styled(
        label.to_string(),
        Style::default().fg(best_terminal_color(rgb)),
    )]
}

pub(crate) fn color_swatch_spans(
    choice: &UiColorChoice,
    fallback_preset: SelectionHighlightPreset,
    is_secondary: bool,
) -> Vec<Span<'static>> {
    match choice {
        UiColorChoice::Gradient { start, end } => {
            let start_rgb = parse_hex_color(start).unwrap_or(selection_preset_color(fallback_preset).rgb);
            let end_rgb = parse_hex_color(end).unwrap_or(selection_preset_color(fallback_preset).rgb);
            vec![
                Span::styled("■", Style::default().fg(best_terminal_color(start_rgb))),
                Span::styled("■", Style::default().fg(best_terminal_color(end_rgb))),
            ]
        }
        _ => {
            let rgb = resolve_color_choice_label_rgb(choice, fallback_preset, is_secondary);
            vec![Span::styled("■", Style::default().fg(best_terminal_color(rgb)))]
        }
    }
}

pub(crate) fn apply_text_formats(mut style: Style, formats: SelectionHighlightTextFormats) -> Style {
    if formats.contains(SelectionHighlightTextFormat::Bold) {
        style = style.add_modifier(Modifier::BOLD);
        style = apply_foreground_accent(style, (244, 246, 250), 0.34);
    } else if formats.contains(SelectionHighlightTextFormat::Semibold) {
        style = style.add_modifier(Modifier::BOLD);
        style = apply_foreground_accent(style, (220, 226, 236), 0.24);
    }
    if formats.contains(SelectionHighlightTextFormat::Italic) {
        style = style.add_modifier(Modifier::ITALIC);
        style = apply_foreground_accent(style, (193, 168, 235), 0.24);
    }
    if formats.contains(SelectionHighlightTextFormat::Underlined) {
        style = style.add_modifier(Modifier::UNDERLINED);
        style = apply_foreground_accent(style, (255, 210, 120), 0.18);
    }
    if formats.contains(SelectionHighlightTextFormat::Dim) {
        style = style.add_modifier(Modifier::DIM);
        style = apply_foreground_accent(style, (168, 176, 189), 0.12);
    }
    if formats.contains(SelectionHighlightTextFormat::Reversed) {
        style = style.add_modifier(Modifier::REVERSED);
    }
    if formats.contains(SelectionHighlightTextFormat::CrossedOut) {
        style = style.add_modifier(Modifier::CROSSED_OUT);
        style = apply_foreground_accent(style, (227, 130, 136), 0.18);
    }
    if formats.contains(SelectionHighlightTextFormat::Mono) {
        style = apply_foreground_accent(style, (120, 193, 255), 0.3);
    }
    style
}

pub(crate) fn format_preview_label(
    format: SelectionHighlightTextFormat,
    is_ru: bool,
) -> (&'static str, Style, Option<&'static str>) {
    match (is_ru, format) {
        (true, SelectionHighlightTextFormat::Bold) => (
            "Жирный",
            Style::default()
                .add_modifier(Modifier::BOLD)
                .fg(best_terminal_color((244, 246, 250))),
            None,
        ),
        (false, SelectionHighlightTextFormat::Bold) => (
            "Жирный",
            Style::default()
                .add_modifier(Modifier::BOLD)
                .fg(best_terminal_color((244, 246, 250))),
            None,
        ),
        (true, SelectionHighlightTextFormat::Semibold) => (
            "Полужирный",
            Style::default()
                .add_modifier(Modifier::BOLD)
                .fg(best_terminal_color((214, 218, 224))),
            Some("◧"),
        ),
        (false, SelectionHighlightTextFormat::Semibold) => (
            "Полужирный",
            Style::default()
                .add_modifier(Modifier::BOLD)
                .fg(best_terminal_color((214, 218, 224))),
            Some("◧"),
        ),
        (true, SelectionHighlightTextFormat::Italic) => (
            "Курсив",
            Style::default()
                .add_modifier(Modifier::ITALIC)
                .fg(best_terminal_color((193, 168, 235))),
            None,
        ),
        (false, SelectionHighlightTextFormat::Italic) => (
            "Курсив",
            Style::default()
                .add_modifier(Modifier::ITALIC)
                .fg(best_terminal_color((193, 168, 235))),
            None,
        ),
        (true, SelectionHighlightTextFormat::Underlined) => (
            "Подчёркнутый",
            Style::default()
                .add_modifier(Modifier::UNDERLINED)
                .fg(best_terminal_color((255, 210, 120))),
            None,
        ),
        (false, SelectionHighlightTextFormat::Underlined) => (
            "Подчёркнутый",
            Style::default()
                .add_modifier(Modifier::UNDERLINED)
                .fg(best_terminal_color((255, 210, 120))),
            None,
        ),
        (true, SelectionHighlightTextFormat::Mono) => (
            "Моно",
            Style::default().fg(best_terminal_color((120, 193, 255))),
            Some("</>"),
        ),
        (false, SelectionHighlightTextFormat::Mono) => (
            "Моно",
            Style::default().fg(best_terminal_color((120, 193, 255))),
            Some("</>"),
        ),
        (true, SelectionHighlightTextFormat::Dim) => ("Приглушённый", Style::default().add_modifier(Modifier::DIM), None),
        (false, SelectionHighlightTextFormat::Dim) => ("Приглушённый", Style::default().add_modifier(Modifier::DIM), None),
        (true, SelectionHighlightTextFormat::Reversed) => ("Инверсия", Style::default().add_modifier(Modifier::REVERSED), None),
        (false, SelectionHighlightTextFormat::Reversed) => ("Инверсия", Style::default().add_modifier(Modifier::REVERSED), None),
        (true, SelectionHighlightTextFormat::CrossedOut) => (
            "Зачёркнутый",
            Style::default()
                .add_modifier(Modifier::CROSSED_OUT)
                .fg(best_terminal_color((227, 130, 136))),
            None,
        ),
        (false, SelectionHighlightTextFormat::CrossedOut) => (
            "Зачёркнутый",
            Style::default()
                .add_modifier(Modifier::CROSSED_OUT)
                .fg(best_terminal_color((227, 130, 136))),
            None,
        ),
    }
}

pub(crate) fn color_preview_description(choice: &UiColorChoice, fallback_preset: SelectionHighlightPreset, is_ru: bool) -> String {
    match choice {
        UiColorChoice::Auto => {
            let named = selection_preset_color(fallback_preset);
            if is_ru {
                format!("Авто · {} · {}", named.name_ru, named.hex.to_ascii_uppercase())
            } else {
                format!("Авто · {} · {}", named.name_ru, named.hex.to_ascii_uppercase())
            }
        }
        UiColorChoice::Gradient { start, end } => {
            let start_named = named_color_for_hex(start);
            let end_named = named_color_for_hex(end);
            if is_ru {
                format!(
                    "Градиент: {} {} → {} {}",
                    start_named.name_ru,
                    start.to_ascii_uppercase(),
                    end_named.name_ru,
                    end.to_ascii_uppercase()
                )
            } else {
                format!(
                    "Градиент: {} {} → {} {}",
                    start_named.name_ru,
                    start.to_ascii_uppercase(),
                    end_named.name_ru,
                    end.to_ascii_uppercase()
                )
            }
        }
        _ => {
            let named = match choice {
                UiColorChoice::Custom(hex) => named_color_for_hex(hex),
                UiColorChoice::Preset(preset) => selection_preset_color(*preset),
                UiColorChoice::Auto | UiColorChoice::Gradient { .. } => selection_preset_color(fallback_preset),
            };
            if is_ru {
                format!("Оттенок: {} · {}", named.name_ru, named.hex.to_ascii_uppercase())
            } else {
                format!("Оттенок: {} · {}", named.name_ru, named.hex.to_ascii_uppercase())
            }
        }
    }
}
