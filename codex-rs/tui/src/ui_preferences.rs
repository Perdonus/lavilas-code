use std::collections::HashSet;
use std::io;
use std::path::Path;
use std::path::PathBuf;

pub(crate) const MISTRAL_DEFAULT_PROFILE_MODEL: &str = "mistral-vibe-cli";
pub(crate) const MISTRAL_LEGACY_TOOL_MODEL: &str = "mistral-vibe-cli-with-tools";
const MISTRAL_COMPATIBILITY_SUFFIXES: [&str; 4] = ["-with-tools", "-tools", "-latest", "-fast"];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum UiLanguage {
    Ru,
    En,
}

impl UiLanguage {
    pub(crate) fn from_code(code: &str) -> Self {
        match code.trim().to_ascii_lowercase().as_str() {
            "en" => Self::En,
            _ => Self::Ru,
        }
    }

    pub(crate) fn code(self) -> &'static str {
        match self {
            Self::Ru => "ru",
            Self::En => "en",
        }
    }

    pub(crate) fn is_ru(self) -> bool {
        matches!(self, Self::Ru)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct UiPreferences {
    pub(crate) language: UiLanguage,
    pub(crate) command_prefix: char,
    pub(crate) hidden_commands: Vec<String>,
}

impl Default for UiPreferences {
    fn default() -> Self {
        Self {
            language: UiLanguage::Ru,
            command_prefix: '/',
            hidden_commands: Vec::new(),
        }
    }
}

#[derive(Clone, Copy)]
struct ProfileCatalogSeed {
    model: &'static str,
    description: &'static str,
    default_reasoning_level: &'static str,
    supports_parallel_tool_calls: bool,
    supports_image_input: bool,
}

pub(crate) fn profiles_dir(codex_home: &Path) -> PathBuf {
    codex_home.join("Profiles")
}

pub(crate) fn settings_path(codex_home: &Path) -> PathBuf {
    profiles_dir(codex_home).join("settings.json")
}

pub(crate) fn profile_model_catalog_path(codex_home: &Path, profile_stem: &str) -> PathBuf {
    profiles_dir(codex_home).join(format!("{profile_stem}.models.json"))
}

pub(crate) fn default_profile_model(provider: &str) -> String {
    profile_catalog_seeds(provider)
        .first()
        .map(|seed| seed.model.to_string())
        .unwrap_or_else(|| "custom-model".to_string())
}

pub(crate) fn normalize_profile_model(provider: &str, model: &str) -> String {
    if provider.eq_ignore_ascii_case("mistral")
        && let Some(base) = MISTRAL_COMPATIBILITY_SUFFIXES.iter().find_map(|suffix| {
            let base = model.strip_suffix(suffix)?;
            base.eq_ignore_ascii_case(MISTRAL_DEFAULT_PROFILE_MODEL)
                .then_some(MISTRAL_DEFAULT_PROFILE_MODEL)
        })
    {
        return base.to_string();
    }

    if provider.eq_ignore_ascii_case("mistral")
        && model.eq_ignore_ascii_case(MISTRAL_LEGACY_TOOL_MODEL)
    {
        MISTRAL_DEFAULT_PROFILE_MODEL.to_string()
    } else {
        model.to_string()
    }
}

pub(crate) fn repair_profile_model_catalog(path: &Path, provider: &str) -> io::Result<bool> {
    if !path.exists() {
        return Ok(false);
    }

    let contents = std::fs::read_to_string(path)?;
    let mut payload: serde_json::Value = serde_json::from_str(&contents)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?;
    let Some(models) = payload
        .get_mut("models")
        .and_then(serde_json::Value::as_array_mut)
    else {
        return Ok(false);
    };

    let mut changed = false;
    for entry in &mut *models {
        let Some(model) = entry.as_object_mut() else {
            continue;
        };
        let Some(current_slug) = model
            .get("slug")
            .and_then(serde_json::Value::as_str)
            .map(str::to_string)
        else {
            continue;
        };

        let normalized_slug = normalize_profile_model(provider, &current_slug);
        if normalized_slug == current_slug {
            continue;
        }

        model.insert(
            "slug".to_string(),
            serde_json::Value::String(normalized_slug.clone()),
        );
        if model
            .get("display_name")
            .and_then(serde_json::Value::as_str)
            .is_some_and(|display_name| display_name == current_slug)
        {
            model.insert(
                "display_name".to_string(),
                serde_json::Value::String(normalized_slug),
            );
        }
        changed = true;
    }

    let mut seen_slugs = HashSet::new();
    let original_len = models.len();
    models.retain(|entry| {
        let Some(slug) = entry
            .get("slug")
            .and_then(serde_json::Value::as_str)
            .map(str::to_string)
        else {
            return true;
        };
        seen_slugs.insert(slug)
    });
    if models.len() != original_len {
        changed = true;
    }

    if !changed {
        return Ok(false);
    }

    let body = serde_json::to_string_pretty(&payload)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
        + "\n";
    std::fs::write(path, body)?;
    Ok(true)
}

pub(crate) fn ensure_profile_model_catalog(
    codex_home: &Path,
    profile_stem: &str,
    provider: &str,
) -> io::Result<PathBuf> {
    let dir = profiles_dir(codex_home);
    std::fs::create_dir_all(&dir)?;

    let path = profile_model_catalog_path(codex_home, profile_stem);
    if path.exists() {
        repair_profile_model_catalog(&path, provider)?;
        return Ok(path);
    }

    let models = profile_catalog_seeds(provider)
        .into_iter()
        .map(seed_model_info)
        .collect::<Vec<_>>();
    let payload = serde_json::json!({
        "models": models,
    });
    let body = serde_json::to_string_pretty(&payload)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
        + "\n";
    std::fs::write(&path, body)?;
    Ok(path)
}

pub(crate) fn load_ui_preferences(codex_home: &Path) -> UiPreferences {
    let value = load_settings_json(codex_home);
    let language = value
        .get("language")
        .and_then(serde_json::Value::as_str)
        .map(UiLanguage::from_code)
        .unwrap_or(UiLanguage::Ru);
    let command_prefix = value
        .get("command_prefix")
        .and_then(serde_json::Value::as_str)
        .and_then(|value| value.chars().next())
        .filter(|c| c.is_ascii() && !c.is_ascii_whitespace())
        .unwrap_or('/');
    let hidden_commands = value
        .get("hidden_commands")
        .and_then(serde_json::Value::as_array)
        .map(|values| {
            values
                .iter()
                .filter_map(serde_json::Value::as_str)
                .map(str::to_ascii_lowercase)
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();

    UiPreferences {
        language,
        command_prefix,
        hidden_commands,
    }
}

pub(crate) fn save_ui_language(codex_home: &Path, language: UiLanguage) -> io::Result<()> {
    persist_setting(
        codex_home,
        "language",
        serde_json::Value::String(language.code().to_string()),
    )
}

pub(crate) fn save_command_prefix(codex_home: &Path, prefix: char) -> io::Result<()> {
    persist_setting(
        codex_home,
        "command_prefix",
        serde_json::Value::String(prefix.to_string()),
    )
}

pub(crate) fn save_hidden_commands(
    codex_home: &Path,
    hidden_commands: &[String],
) -> io::Result<()> {
    let hidden_commands = hidden_commands
        .iter()
        .cloned()
        .map(serde_json::Value::String)
        .collect::<Vec<_>>();
    persist_setting(
        codex_home,
        "hidden_commands",
        serde_json::Value::Array(hidden_commands),
    )
}

fn persist_setting(codex_home: &Path, key: &str, value: serde_json::Value) -> io::Result<()> {
    let mut json = load_settings_json(codex_home);
    if !json.is_object() {
        json = serde_json::json!({});
    }
    if let Some(object) = json.as_object_mut() {
        object.insert(key.to_string(), value);
    }
    save_settings_json(codex_home, &json)
}

fn load_settings_json(codex_home: &Path) -> serde_json::Value {
    let path = settings_path(codex_home);
    match std::fs::read_to_string(path) {
        Ok(contents) => serde_json::from_str::<serde_json::Value>(&contents)
            .unwrap_or_else(|_| serde_json::json!({})),
        Err(_) => serde_json::json!({}),
    }
}

fn save_settings_json(codex_home: &Path, value: &serde_json::Value) -> io::Result<()> {
    let dir = profiles_dir(codex_home);
    std::fs::create_dir_all(&dir)?;
    let path = settings_path(codex_home);
    let payload = serde_json::to_string_pretty(value).unwrap_or_else(|_| "{}".to_string()) + "\n";
    std::fs::write(path, payload)
}

fn profile_catalog_seeds(provider: &str) -> Vec<ProfileCatalogSeed> {
    match provider {
        "codex_oauth" | "openai" => vec![
            ProfileCatalogSeed {
                model: "gpt-5.3-codex",
                description: "Стартовый профиль Lavilas Codex для основного coding-потока.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
            ProfileCatalogSeed {
                model: "gpt-5.4",
                description: "Более сильный универсальный профиль для сложных задач.",
                default_reasoning_level: "high",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
        ],
        "openrouter" => vec![
            ProfileCatalogSeed {
                model: "openai/gpt-5.3-codex",
                description: "Совместимый coding-профиль через OpenRouter.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
            ProfileCatalogSeed {
                model: "anthropic/claude-sonnet-4",
                description: "Сильный общий профиль для кода и анализа через OpenRouter.",
                default_reasoning_level: "high",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
        ],
        "gemini" => vec![
            ProfileCatalogSeed {
                model: "gemini-2.5-pro",
                description: "Базовый профиль Gemini для кода и длинного контекста.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
            ProfileCatalogSeed {
                model: "gemini-2.5-flash",
                description: "Быстрый профиль Gemini для повседневной работы.",
                default_reasoning_level: "low",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
        ],
        "anthropic" => vec![
            ProfileCatalogSeed {
                model: "claude-sonnet-4-0",
                description: "Стартовый профиль Anthropic для кода и ревью.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
            ProfileCatalogSeed {
                model: "claude-opus-4-1",
                description: "Тяжёлый профиль Anthropic для сложных инженерных задач.",
                default_reasoning_level: "high",
                supports_parallel_tool_calls: true,
                supports_image_input: true,
            },
        ],
        "mistral" => vec![
            ProfileCatalogSeed {
                model: MISTRAL_DEFAULT_PROFILE_MODEL,
                description: "Подготовленный профиль Mistral для tool-use, MCP и CLI без фейкового alias-а модели.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: false,
            },
            ProfileCatalogSeed {
                model: "mistral-large-latest",
                description: "Тяжёлый Mistral-профиль для сложного кода и анализа.",
                default_reasoning_level: "high",
                supports_parallel_tool_calls: true,
                supports_image_input: false,
            },
            ProfileCatalogSeed {
                model: "mistral-small-latest",
                description: "Быстрый Mistral-профиль для коротких проходов.",
                default_reasoning_level: "low",
                supports_parallel_tool_calls: false,
                supports_image_input: false,
            },
        ],
        "groq" => vec![
            ProfileCatalogSeed {
                model: "llama-3.3-70b-versatile",
                description: "Стартовый профиль Groq для быстрых coding-задач.",
                default_reasoning_level: "low",
                supports_parallel_tool_calls: true,
                supports_image_input: false,
            },
            ProfileCatalogSeed {
                model: "qwen/qwen3-32b",
                description: "Альтернативный Groq-профиль для более спокойного reasoning.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: false,
            },
        ],
        "ollama" => vec![
            ProfileCatalogSeed {
                model: "qwen2.5-coder:32b",
                description: "Локальный coding-профиль Ollama с упором на код.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: false,
                supports_image_input: false,
            },
            ProfileCatalogSeed {
                model: "llama3.1:8b",
                description: "Лёгкий локальный профиль Ollama для простых проходов.",
                default_reasoning_level: "low",
                supports_parallel_tool_calls: false,
                supports_image_input: false,
            },
        ],
        _ => vec![
            ProfileCatalogSeed {
                model: "custom-model-with-tools",
                description: "Стартовый кастомный профиль с поддержкой tool-use.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: true,
                supports_image_input: false,
            },
            ProfileCatalogSeed {
                model: "custom-model",
                description: "Стартовый кастомный профиль без расширенного tool-use.",
                default_reasoning_level: "medium",
                supports_parallel_tool_calls: false,
                supports_image_input: false,
            },
        ],
    }
}

fn seed_model_info(seed: ProfileCatalogSeed) -> serde_json::Value {
    let input_modalities = if seed.supports_image_input {
        vec!["text", "image"]
    } else {
        vec!["text"]
    };
    serde_json::json!({
        "slug": seed.model,
        "display_name": seed.model,
        "description": seed.description,
        "default_reasoning_level": seed.default_reasoning_level,
        "supported_reasoning_levels": supported_reasoning_levels(seed.default_reasoning_level),
        "shell_type": "shell_command",
        "visibility": "list",
        "supported_in_api": true,
        "priority": 0,
        "availability_nux": null,
        "upgrade": null,
        "base_instructions": "You are Lavilas Codex, a coding assistant focused on software tasks.",
        "supports_reasoning_summaries": false,
        "default_reasoning_summary": "none",
        "support_verbosity": true,
        "default_verbosity": "low",
        "apply_patch_tool_type": "freeform",
        "truncation_policy": {
            "mode": "tokens",
            "limit": 10000
        },
        "supports_parallel_tool_calls": seed.supports_parallel_tool_calls,
        "context_window": 128000,
        "experimental_supported_tools": [],
        "input_modalities": input_modalities,
    })
}

fn supported_reasoning_levels(default_reasoning_level: &str) -> Vec<serde_json::Value> {
    let mut levels = vec![
        serde_json::json!({
            "effort": "low",
            "description": "Быстрее отвечает и тратит меньше бюджета размышлений"
        }),
        serde_json::json!({
            "effort": "medium",
            "description": "Сбалансированный режим для повседневной разработки"
        }),
        serde_json::json!({
            "effort": "high",
            "description": "Глубже разбирает сложные и неоднозначные задачи"
        }),
    ];
    if default_reasoning_level == "xhigh" {
        levels.push(serde_json::json!({
            "effort": "xhigh",
            "description": "Максимальный бюджет размышлений для тяжёлых случаев"
        }));
    }
    levels
}

#[cfg(test)]
mod tests {
    use super::UiLanguage;
    use super::default_profile_model;
    use super::ensure_profile_model_catalog;
    use super::load_ui_preferences;
    use super::profile_model_catalog_path;
    use super::save_command_prefix;
    use super::save_hidden_commands;
    use super::save_ui_language;
    use tempfile::tempdir;

    #[test]
    fn ui_preferences_round_trip() {
        let codex_home = tempdir().expect("tempdir");
        save_ui_language(codex_home.path(), UiLanguage::En).expect("save language");
        save_command_prefix(codex_home.path(), '!').expect("save prefix");
        save_hidden_commands(
            codex_home.path(),
            &["model".to_string(), "profiles".to_string()],
        )
        .expect("save hidden commands");

        let preferences = load_ui_preferences(codex_home.path());
        assert_eq!(preferences.language, UiLanguage::En);
        assert_eq!(preferences.command_prefix, '!');
        assert_eq!(preferences.hidden_commands, vec!["model", "profiles"]);
    }

    #[test]
    fn profile_catalog_helper_creates_sidecar_file() {
        use super::MISTRAL_DEFAULT_PROFILE_MODEL;
        let codex_home = tempdir().expect("tempdir");
        let catalog_path =
            ensure_profile_model_catalog(codex_home.path(), "mistral-profile", "mistral")
                .expect("catalog path");
        assert_eq!(
            catalog_path,
            profile_model_catalog_path(codex_home.path(), "mistral-profile")
        );
        let contents = std::fs::read_to_string(&catalog_path).expect("catalog contents");
        assert!(contents.contains(MISTRAL_DEFAULT_PROFILE_MODEL));
        assert_eq!(
            default_profile_model("mistral"),
            MISTRAL_DEFAULT_PROFILE_MODEL
        );
    }

    #[test]
    fn profile_catalog_helper_repairs_legacy_mistral_sidecar() {
        use super::MISTRAL_DEFAULT_PROFILE_MODEL;
        use super::MISTRAL_LEGACY_TOOL_MODEL;
        let codex_home = tempdir().expect("tempdir");
        let catalog_path = profile_model_catalog_path(codex_home.path(), "mistral-profile");
        std::fs::create_dir_all(catalog_path.parent().expect("catalog dir")).expect("mkdirs");
        std::fs::write(
            &catalog_path,
            serde_json::json!({
                "models": [{
                    "slug": MISTRAL_LEGACY_TOOL_MODEL,
                    "display_name": MISTRAL_LEGACY_TOOL_MODEL,
                }]
            })
            .to_string(),
        )
        .expect("write legacy sidecar");

        ensure_profile_model_catalog(codex_home.path(), "mistral-profile", "mistral")
            .expect("repair catalog");

        let contents = std::fs::read_to_string(&catalog_path).expect("catalog contents");
        assert!(contents.contains(MISTRAL_DEFAULT_PROFILE_MODEL));
        assert!(!contents.contains(MISTRAL_LEGACY_TOOL_MODEL));
        assert_eq!(
            normalize_profile_model("mistral", "mistral-vibe-cli-fast"),
            MISTRAL_DEFAULT_PROFILE_MODEL
        );
    }

    #[test]
    fn profile_catalog_helper_deduplicates_repaired_mistral_entries() {
        use super::MISTRAL_DEFAULT_PROFILE_MODEL;
        use super::MISTRAL_LEGACY_TOOL_MODEL;
        let codex_home = tempdir().expect("tempdir");
        let catalog_path = profile_model_catalog_path(codex_home.path(), "mistral-profile");
        std::fs::create_dir_all(catalog_path.parent().expect("catalog dir")).expect("mkdirs");
        std::fs::write(
            &catalog_path,
            serde_json::json!({
                "models": [
                    {
                        "slug": MISTRAL_LEGACY_TOOL_MODEL,
                        "display_name": MISTRAL_LEGACY_TOOL_MODEL,
                        "supports_parallel_tool_calls": true,
                    },
                    {
                        "slug": MISTRAL_DEFAULT_PROFILE_MODEL,
                        "display_name": MISTRAL_DEFAULT_PROFILE_MODEL,
                        "supports_parallel_tool_calls": false,
                    }
                ]
            })
            .to_string(),
        )
        .expect("write duplicate sidecar");

        ensure_profile_model_catalog(codex_home.path(), "mistral-profile", "mistral")
            .expect("repair catalog");

        let payload: serde_json::Value = serde_json::from_str(
            &std::fs::read_to_string(&catalog_path).expect("catalog contents"),
        )
        .expect("parse repaired catalog");
        let models = payload["models"].as_array().expect("models array");
        assert_eq!(models.len(), 1);
        assert_eq!(models[0]["slug"], MISTRAL_DEFAULT_PROFILE_MODEL);
        assert_eq!(models[0]["supports_parallel_tool_calls"], true);
    }
}
