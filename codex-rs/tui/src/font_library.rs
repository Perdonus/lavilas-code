use crate::ui_preferences::StoredFontProfile;
use crate::ui_preferences::fonts_dir;
use crate::terminal_font::materialize_terminal_font_installation;
use crate::terminal_font::remove_materialized_terminal_font;
use regex_lite::Regex;
use reqwest::header::HeaderMap;
use reqwest::header::HeaderValue;
use reqwest::header::USER_AGENT;
use serde::Deserialize;
use serde::Serialize;
use std::collections::hash_map::DefaultHasher;
use std::collections::BTreeSet;
use std::collections::HashSet;
use std::fs;
use std::hash::Hash;
use std::hash::Hasher;
use std::io;
use std::path::Path;
use std::path::PathBuf;
use std::sync::OnceLock;
use thiserror::Error;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum FontCategory {
    Monospace,
    Sans,
    Serif,
    Display,
    Handwriting,
}

impl FontCategory {
    pub(crate) const fn display_name_ru(self) -> &'static str {
        match self {
            Self::Monospace => "Моно",
            Self::Sans => "Гротеск",
            Self::Serif => "Антиква",
            Self::Display => "Акцентный",
            Self::Handwriting => "Рукописный",
        }
    }

    pub(crate) const fn display_name_en(self) -> &'static str {
        match self {
            Self::Monospace => "Monospace",
            Self::Sans => "Sans",
            Self::Serif => "Serif",
            Self::Display => "Display",
            Self::Handwriting => "Handwriting",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub(crate) struct GoogleFontCatalogEntry {
    pub(crate) family: String,
    pub(crate) category: String,
    #[serde(default)]
    pub(crate) subsets: Vec<String>,
    #[serde(default)]
    pub(crate) popularity: u32,
}

#[derive(Debug, Deserialize)]
struct GoogleFontCatalogFile {
    fonts: Vec<GoogleFontCatalogEntry>,
}

static GOOGLE_FONT_CATALOG: OnceLock<Vec<GoogleFontCatalogEntry>> = OnceLock::new();
static TERMINAL_SAFE_GOOGLE_FONT_CATALOG: OnceLock<Vec<GoogleFontCatalogEntry>> = OnceLock::new();

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) struct FontCatalogEntry {
    pub(crate) id: &'static str,
    pub(crate) family: &'static str,
    pub(crate) category: FontCategory,
    pub(crate) description_ru: &'static str,
    pub(crate) description_en: &'static str,
    pub(crate) preview: &'static str,
    pub(crate) tags: &'static [&'static str],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct FontAsset {
    pub(crate) filename: String,
    pub(crate) url: String,
    pub(crate) format: String,
    pub(crate) subset: Option<String>,
    pub(crate) weight: Option<String>,
    pub(crate) style: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct FontDownloadPlan {
    pub(crate) id: String,
    pub(crate) family: String,
    pub(crate) source: String,
    pub(crate) assets: Vec<FontAsset>,
    pub(crate) terminal_note: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct InstalledFontRecord {
    pub(crate) profile: StoredFontProfile,
    pub(crate) active: bool,
    pub(crate) available: bool,
    pub(crate) missing_files: Vec<String>,
    pub(crate) terminal_note: &'static str,
}

#[derive(Debug, Error)]
pub(crate) enum FontLibraryError {
    #[error("font catalog entry `{0}` not found")]
    CatalogEntryMissing(String),
    #[error("Google Fonts stylesheet did not contain downloadable files for `{0}`")]
    EmptyStylesheet(String),
    #[error("Google Fonts family `{0}` not found in the local catalog")]
    GoogleFontMissing(String),
    #[error("invalid font family name")]
    InvalidFamily,
    #[error("font family `{0}` is not a terminal-safe monospace family")]
    UnsupportedTerminalCategory(String),
    #[error("network request failed: {0}")]
    Network(String),
    #[error("I/O failed: {0}")]
    Io(#[from] io::Error),
    #[error("HTTP {status} while downloading `{url}`")]
    HttpStatus { status: u16, url: String },
}

const FONT_CATALOG: [FontCatalogEntry; 4] = [
    FontCatalogEntry {
        id: "jetbrains-mono",
        family: "JetBrains Mono",
        category: FontCategory::Monospace,
        description_ru: "Чёткий кодовый моношрифт с хорошей читаемостью на слабых терминалах.",
        description_en: "Clean coding monospace that stays legible in low-end terminals.",
        preview: "JetBrains Mono 0OoIl1 [] {} =>",
        tags: &["mono", "coding", "developer"],
    },
    FontCatalogEntry {
        id: "ibm-plex-mono",
        family: "IBM Plex Mono",
        category: FontCategory::Monospace,
        description_ru: "Сдержанный технический моношрифт без лишней декоративности.",
        description_en: "Restrained technical monospace without decorative noise.",
        preview: "IBM Plex Mono /var/log/system.log",
        tags: &["mono", "coding", "clean"],
    },
    FontCatalogEntry {
        id: "roboto-mono",
        family: "Roboto Mono",
        category: FontCategory::Monospace,
        description_ru: "Нейтральный системный моношрифт с широкой совместимостью.",
        description_en: "Neutral system-like monospace with broad compatibility.",
        preview: "Roboto Mono cargo test --workspace",
        tags: &["mono", "system", "safe"],
    },
    FontCatalogEntry {
        id: "geist-mono",
        family: "Geist Mono",
        category: FontCategory::Monospace,
        description_ru: "Современный моношрифт с мягкими формами и плотной сеткой.",
        description_en: "Modern monospace with soft forms and a compact rhythm.",
        preview: "Geist Mono SELECT * FROM sessions;",
        tags: &["mono", "modern", "ui"],
    },
];

const PREFERRED_SUBSETS: &[&str] = &["cyrillic", "cyrillic-ext", "latin", "latin-ext"];
const TERMINAL_FONT_NOTE: &str = "Lavilas Codex работает только с моноширинными гарнитурами для терминала. Он дополнительно пытается выложить шрифт в пользовательский каталог шрифтов ОС, но терминал всё равно может потребовать ручной выбор гарнитуры.";

#[derive(Debug, Serialize)]
struct StoredFontManifest {
    id: String,
    family: String,
    source: String,
    files: Vec<String>,
    terminal_note: &'static str,
}

pub(crate) fn font_catalog() -> &'static [FontCatalogEntry] {
    &FONT_CATALOG
}

pub(crate) fn featured_fonts() -> Vec<FontCatalogEntry> {
    FONT_CATALOG.to_vec()
}

pub(crate) fn find_font_catalog_entry(id: &str) -> Option<&'static FontCatalogEntry> {
    FONT_CATALOG.iter().find(|entry| entry.id.eq_ignore_ascii_case(id.trim()))
}

pub(crate) fn search_font_catalog(query: &str) -> Vec<&'static FontCatalogEntry> {
    let normalized = query.trim().to_lowercase();
    if normalized.is_empty() {
        return FONT_CATALOG.iter().collect();
    }

    FONT_CATALOG
        .iter()
        .filter(|entry| {
            entry.id.contains(&normalized)
                || entry.family.to_lowercase().contains(&normalized)
                || entry.description_ru.to_lowercase().contains(&normalized)
                || entry.description_en.to_lowercase().contains(&normalized)
                || entry.tags.iter().any(|tag| tag.contains(&normalized))
        })
        .collect()
}

pub(crate) fn search_featured_fonts(query: &str) -> Vec<FontCatalogEntry> {
    search_font_catalog(query)
        .into_iter()
        .copied()
        .collect()
}

pub(crate) fn terminal_font_note() -> &'static str {
    TERMINAL_FONT_NOTE
}

pub(crate) fn google_font_catalog() -> &'static [GoogleFontCatalogEntry] {
    TERMINAL_SAFE_GOOGLE_FONT_CATALOG
        .get_or_init(|| {
            raw_google_font_catalog()
                .iter()
                .filter(|entry| google_font_category(entry.category.as_str()) == FontCategory::Monospace)
                .cloned()
                .collect()
        })
        .as_slice()
}

pub(crate) fn google_font_category(category: &str) -> FontCategory {
    match category.trim().to_ascii_lowercase().as_str() {
        "monospace" => FontCategory::Monospace,
        "serif" => FontCategory::Serif,
        "display" => FontCategory::Display,
        "handwriting" => FontCategory::Handwriting,
        "sans serif" | "sans-serif" | "sans" => FontCategory::Sans,
        _ => FontCategory::Sans,
    }
}

pub(crate) fn google_font_preview(family: &str, category: &str) -> String {
    match google_font_category(category) {
        FontCategory::Monospace => format!("{family}  AaBb 0123 [] {{}} =>"),
        FontCategory::Serif => format!("{family}  Съешь ещё этих мягких булок"),
        FontCategory::Display => format!("{family}  Build • Review • Deploy"),
        FontCategory::Handwriting => format!("{family}  Привет, terminal world"),
        FontCategory::Sans => format!("{family}  Settings, prompts and answers"),
    }
}

pub(crate) fn find_google_font(family: &str) -> Option<&'static GoogleFontCatalogEntry> {
    let family = family.trim();
    if family.is_empty() {
        return None;
    }
    google_font_catalog()
        .iter()
        .find(|entry| entry.family.eq_ignore_ascii_case(family))
}

pub(crate) fn search_google_fonts(query: &str) -> Vec<GoogleFontCatalogEntry> {
    let normalized = query.trim().to_ascii_lowercase();
    let mut fonts = google_font_catalog()
        .iter()
        .filter(|entry| google_font_category(entry.category.as_str()) == FontCategory::Monospace)
        .filter(|entry| {
            if normalized.is_empty() {
                return true;
            }
            entry.family.to_ascii_lowercase().contains(normalized.as_str())
                || entry.category.to_ascii_lowercase().contains(normalized.as_str())
                || entry
                    .subsets
                    .iter()
                    .any(|subset| subset.to_ascii_lowercase().contains(normalized.as_str()))
        })
        .cloned()
        .collect::<Vec<_>>();

    fonts.sort_by(|left, right| {
        let left_family = left.family.to_ascii_lowercase();
        let right_family = right.family.to_ascii_lowercase();

        let left_rank = if normalized.is_empty() {
            0u8
        } else if left_family == normalized {
            0
        } else if left_family.starts_with(normalized.as_str()) {
            1
        } else if left_family.contains(normalized.as_str()) {
            2
        } else if left
            .category
            .to_ascii_lowercase()
            .starts_with(normalized.as_str())
        {
            3
        } else {
            4
        };
        let right_rank = if normalized.is_empty() {
            0u8
        } else if right_family == normalized {
            0
        } else if right_family.starts_with(normalized.as_str()) {
            1
        } else if right_family.contains(normalized.as_str()) {
            2
        } else if right
            .category
            .to_ascii_lowercase()
            .starts_with(normalized.as_str())
        {
            3
        } else {
            4
        };

        left_rank
            .cmp(&right_rank)
            .then_with(|| left_family.cmp(&right_family))
    });

    fonts
}

pub(crate) fn list_installed_fonts(
    codex_home: &Path,
    installed_fonts: &[StoredFontProfile],
    active_font_id: Option<&str>,
) -> Vec<InstalledFontRecord> {
    installed_fonts
        .iter()
        .cloned()
        .map(|profile| {
            let missing_files = profile
                .files
                .iter()
                .filter(|file| !fonts_dir(codex_home).join(file).exists())
                .cloned()
                .collect::<Vec<_>>();
            InstalledFontRecord {
                active: active_font_id
                    .is_some_and(|active| active.eq_ignore_ascii_case(profile.id.as_str())),
                available: missing_files.is_empty(),
                missing_files,
                profile,
                terminal_note: TERMINAL_FONT_NOTE,
            }
        })
        .collect()
}

pub(crate) fn resolve_font_files(codex_home: &Path, profile: &StoredFontProfile) -> Vec<PathBuf> {
    profile
        .files
        .iter()
        .map(|file| fonts_dir(codex_home).join(file))
        .collect()
}

pub(crate) fn remove_installed_font(codex_home: &Path, font_id: &str) -> io::Result<()> {
    let target = fonts_dir(codex_home).join(stable_font_id(font_id));
    if target.exists() {
        fs::remove_dir_all(target)?;
    }
    let _ = remove_materialized_terminal_font(font_id);
    Ok(())
}

pub(crate) async fn install_catalog_font(
    codex_home: &Path,
    font_id: &str,
) -> Result<StoredFontProfile, FontLibraryError> {
    let entry = find_font_catalog_entry(font_id)
        .ok_or_else(|| FontLibraryError::CatalogEntryMissing(font_id.to_string()))?;
    install_google_font_family_with_id(codex_home, entry.id, entry.family).await
}

pub(crate) async fn build_google_font_plan(
    id_hint: &str,
    family: &str,
) -> Result<FontDownloadPlan, FontLibraryError> {
    let family = family.trim();
    if family.is_empty() {
        return Err(FontLibraryError::InvalidFamily);
    }
    let catalog_entry = find_google_font_any(family)
        .ok_or_else(|| FontLibraryError::GoogleFontMissing(family.to_string()))?;
    if google_font_category(catalog_entry.category.as_str()) != FontCategory::Monospace {
        return Err(FontLibraryError::UnsupportedTerminalCategory(
            catalog_entry.family.clone(),
        ));
    }
    let family = catalog_entry.family.as_str();

    let css_url = google_fonts_css_url(family);
    let css = google_fonts_client()?
        .get(&css_url)
        .send()
        .await
        .map_err(|err| FontLibraryError::Network(err.to_string()))?;
    let status = css.status();
    if !status.is_success() {
        return Err(FontLibraryError::HttpStatus {
            status: status.as_u16(),
            url: css_url,
        });
    }
    let stylesheet = css
        .text()
        .await
        .map_err(|err| FontLibraryError::Network(err.to_string()))?;

    let mut assets = parse_google_fonts_stylesheet(id_hint, &stylesheet);
    if assets.is_empty() {
        return Err(FontLibraryError::EmptyStylesheet(family.to_string()));
    }

    let preferred_assets = filter_preferred_subsets(&assets);
    if !preferred_assets.is_empty() {
        assets = preferred_assets;
    }

    Ok(FontDownloadPlan {
        id: stable_font_id(id_hint),
        family: family.to_string(),
        source: css_url,
        assets,
        terminal_note: TERMINAL_FONT_NOTE,
    })
}

pub(crate) async fn install_google_font_family(
    codex_home: &Path,
    family: &str,
) -> Result<StoredFontProfile, FontLibraryError> {
    install_google_font_family_with_id(codex_home, family, family).await
}

pub(crate) async fn download_google_font_family(
    codex_home: &Path,
    family: &str,
) -> Result<StoredFontProfile, FontLibraryError> {
    install_google_font_family(codex_home, family).await
}

async fn install_google_font_family_with_id(
    codex_home: &Path,
    id_hint: &str,
    family: &str,
) -> Result<StoredFontProfile, FontLibraryError> {
    let plan = build_google_font_plan(id_hint, family).await?;
    materialize_font_plan(codex_home, &plan).await
}

async fn materialize_font_plan(
    codex_home: &Path,
    plan: &FontDownloadPlan,
) -> Result<StoredFontProfile, FontLibraryError> {
    let root = fonts_dir(codex_home);
    fs::create_dir_all(&root)?;

    let temp_dir = root.join(format!(".{}-{}", plan.id, Uuid::new_v4()));
    fs::create_dir_all(&temp_dir)?;

    let client = google_fonts_client()?;
    let mut stored_files = Vec::with_capacity(plan.assets.len());

    for asset in &plan.assets {
        let response = client
            .get(&asset.url)
            .send()
            .await
            .map_err(|err| FontLibraryError::Network(err.to_string()))?;
        let status = response.status();
        if !status.is_success() {
            return Err(FontLibraryError::HttpStatus {
                status: status.as_u16(),
                url: asset.url.clone(),
            });
        }
        let bytes = response
            .bytes()
            .await
            .map_err(|err| FontLibraryError::Network(err.to_string()))?;
        fs::write(temp_dir.join(&asset.filename), &bytes)?;
        stored_files.push(format!("{}/{}", plan.id, asset.filename));
    }

    let manifest = StoredFontManifest {
        id: plan.id.clone(),
        family: plan.family.clone(),
        source: plan.source.clone(),
        files: stored_files.clone(),
        terminal_note: plan.terminal_note,
    };
    let manifest_body = serde_json::to_string_pretty(&manifest)
        .map_err(|err| io::Error::other(err.to_string()))?
        + "\n";
    fs::write(temp_dir.join("font.json"), manifest_body)?;

    let target_dir = root.join(&plan.id);
    if target_dir.exists() {
        fs::remove_dir_all(&target_dir)?;
    }
    fs::rename(&temp_dir, &target_dir)?;
    let installable_files = plan
        .assets
        .iter()
        .map(|asset| asset.filename.clone())
        .collect::<Vec<_>>();
    let _ = materialize_terminal_font_installation(&plan.id, &target_dir, &installable_files);

    Ok(StoredFontProfile {
        id: plan.id.clone(),
        family: plan.family.clone(),
        source: plan.source.clone(),
        files: stored_files,
    })
}

fn google_fonts_client() -> Result<reqwest::Client, FontLibraryError> {
    let mut headers = HeaderMap::new();
    headers.insert(
        USER_AGENT,
        HeaderValue::from_static(
            "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36",
        ),
    );
    reqwest::Client::builder()
        .default_headers(headers)
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .map_err(|err| FontLibraryError::Network(err.to_string()))
}

fn raw_google_font_catalog() -> &'static [GoogleFontCatalogEntry] {
    GOOGLE_FONT_CATALOG
        .get_or_init(|| {
            let parsed = serde_json::from_str::<GoogleFontCatalogFile>(include_str!(
                "../assets/google-fonts-catalog.json"
            ))
            .unwrap_or_else(|_| GoogleFontCatalogFile { fonts: Vec::new() });
            parsed.fonts
        })
        .as_slice()
}

fn find_google_font_any(family: &str) -> Option<&'static GoogleFontCatalogEntry> {
    raw_google_font_catalog()
        .iter()
        .find(|entry| entry.family.eq_ignore_ascii_case(family))
}

fn google_fonts_css_url(family: &str) -> String {
    let family_name = family
        .split_whitespace()
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>()
        .join(" ");
    let encoded_family = url::form_urlencoded::byte_serialize(family_name.as_bytes())
        .collect::<String>()
        .replace("%20", "+");
    format!(
        "https://fonts.googleapis.com/css2?family={encoded_family}:ital,wght@0,400;0,500;0,700;1,400&display=swap"
    )
}

fn parse_google_fonts_stylesheet(id_hint: &str, stylesheet: &str) -> Vec<FontAsset> {
    let block_re = Regex::new(r"(?s)(?:/\*\s*(?P<subset>[^*]+?)\s*\*/\s*)?@font-face\s*\{(?P<body>.*?)\}")
        .expect("valid google fonts block regex");
    let src_re = Regex::new(r#"url\((?P<url>[^)]+)\)\s*format\('(?P<format>[^']+)'\)"#)
        .expect("valid google fonts src regex");

    let mut seen_urls = HashSet::new();
    let mut assets = Vec::new();
    for captures in block_re.captures_iter(stylesheet) {
        let subset = captures.name("subset").map(|value| value.as_str().trim().to_string());
        let Some(body) = captures.name("body").map(|value| value.as_str()) else {
            continue;
        };
        let Some(src) = src_re.captures(body) else {
            continue;
        };
        let Some(url_match) = src.name("url") else {
            continue;
        };
        let url = url_match
            .as_str()
            .trim()
            .trim_matches('"')
            .trim_matches('\'')
            .to_string();
        if !seen_urls.insert(url.clone()) {
            continue;
        }

        let format = src
            .name("format")
            .map(|value| value.as_str().trim().to_ascii_lowercase())
            .unwrap_or_else(|| "woff2".to_string());
        let weight = css_property(body, "font-weight");
        let style = css_property(body, "font-style");
        let filename = font_asset_filename(id_hint, subset.as_deref(), weight.as_deref(), style.as_deref(), &url, &format);
        assets.push(FontAsset {
            filename,
            url,
            format,
            subset,
            weight,
            style,
        });
    }
    assets
}

fn filter_preferred_subsets(assets: &[FontAsset]) -> Vec<FontAsset> {
    let preferred = assets
        .iter()
        .filter(|asset| {
            asset.subset.as_deref().is_some_and(|subset| {
                PREFERRED_SUBSETS
                    .iter()
                    .any(|candidate| candidate.eq_ignore_ascii_case(subset.trim()))
            })
        })
        .cloned()
        .collect::<Vec<_>>();

    if preferred.is_empty() {
        return Vec::new();
    }

    let mut unique = BTreeSet::new();
    preferred
        .into_iter()
        .filter(|asset| unique.insert(asset.filename.clone()))
        .collect()
}

fn css_property(body: &str, name: &str) -> Option<String> {
    body.lines()
        .find_map(|line| {
            let trimmed = line.trim();
            let prefix = format!("{name}:");
            trimmed
                .strip_prefix(prefix.as_str())
                .map(|value| value.trim().trim_end_matches(';').to_string())
        })
}

fn font_asset_filename(
    id_hint: &str,
    subset: Option<&str>,
    weight: Option<&str>,
    style: Option<&str>,
    url: &str,
    format: &str,
) -> String {
    let id = sanitize_font_id(id_hint);
    let subset = subset
        .map(sanitize_font_id)
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| "core".to_string());
    let weight = weight
        .map(sanitize_font_id)
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| "400".to_string());
    let style = style
        .map(sanitize_font_id)
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| "normal".to_string());
    let ext = font_extension(url, format);
    format!("{id}-{subset}-{weight}-{style}.{ext}")
}

fn font_extension(url: &str, format: &str) -> &'static str {
    let stem = url.split('?').next().unwrap_or(url);
    if stem.ends_with(".ttf") {
        "ttf"
    } else if stem.ends_with(".otf") {
        "otf"
    } else if stem.ends_with(".woff") {
        "woff"
    } else if stem.ends_with(".woff2") {
        "woff2"
    } else if format.eq_ignore_ascii_case("truetype") {
        "ttf"
    } else if format.eq_ignore_ascii_case("opentype") {
        "otf"
    } else {
        "woff2"
    }
}

fn sanitize_font_id(raw: &str) -> String {
    let mut slug = String::with_capacity(raw.len());
    let mut last_dash = false;
    for ch in raw.trim().chars() {
        let lower = ch.to_ascii_lowercase();
        if lower.is_ascii_alphanumeric() {
            slug.push(lower);
            last_dash = false;
        } else if !last_dash {
            slug.push('-');
            last_dash = true;
        }
    }
    while slug.ends_with('-') {
        slug.pop();
    }
    slug.trim_start_matches('-').to_string()
}

fn stable_font_id(raw: &str) -> String {
    let slug = sanitize_font_id(raw);
    if !slug.is_empty() {
        return slug;
    }

    let mut hasher = DefaultHasher::new();
    raw.hash(&mut hasher);
    format!("font-{:016x}", hasher.finish())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn search_font_catalog_matches_family_and_tags() {
        let direct = search_font_catalog("jetbrains");
        assert!(!direct.is_empty());
        assert_eq!(direct[0].family, "JetBrains Mono");

        let tagged = search_font_catalog("system");
        assert!(tagged.iter().any(|entry| entry.family == "Roboto Mono"));
    }

    #[test]
    fn google_font_catalog_is_monospace_only() {
        assert!(!google_font_catalog().is_empty());
        assert!(google_font_catalog()
            .iter()
            .all(|entry| google_font_category(entry.category.as_str()) == FontCategory::Monospace));
    }

    #[test]
    fn parse_google_fonts_stylesheet_extracts_unique_assets() {
        let css = r#"
        /* latin */
        @font-face {
            font-family: 'Demo Font';
            font-style: normal;
            font-weight: 400;
            src: url(https://fonts.gstatic.com/demo-latin.woff2) format('woff2');
        }
        /* cyrillic */
        @font-face {
            font-family: 'Demo Font';
            font-style: normal;
            font-weight: 400;
            src: url(https://fonts.gstatic.com/demo-cyrillic.woff2) format('woff2');
        }
        /* latin */
        @font-face {
            font-family: 'Demo Font';
            font-style: normal;
            font-weight: 400;
            src: url(https://fonts.gstatic.com/demo-latin.woff2) format('woff2');
        }
        "#;
        let assets = parse_google_fonts_stylesheet("demo-font", css);
        assert_eq!(assets.len(), 2);
        assert!(assets.iter().any(|asset| asset.filename.contains("latin")));
        assert!(assets.iter().any(|asset| asset.filename.contains("cyrillic")));
    }

    #[test]
    fn sanitize_font_id_keeps_stable_slug() {
        assert_eq!(sanitize_font_id(" JetBrains Mono "), "jetbrains-mono");
        assert_eq!(sanitize_font_id("PT Serif"), "pt-serif");
        assert_eq!(sanitize_font_id("***"), "");
    }

    #[test]
    fn stable_font_id_avoids_empty_directory_names() {
        let generated = stable_font_id("***");
        assert!(generated.starts_with("font-"));
        assert!(!generated.is_empty());
    }
}
