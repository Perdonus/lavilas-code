use std::io;
use std::path::Path;
use std::path::PathBuf;

use codex_protocol::request_user_input::RequestUserInputEvent;
use codex_protocol::request_user_input::RequestUserInputQuestion;
use serde::Deserialize;
use serde::Serialize;

use crate::ui_preferences::default_profile_model;
use crate::ui_preferences::ensure_profile_model_catalog;
use crate::ui_preferences::profiles_dir;

pub(crate) const PROFILE_NAME_QUESTION_ID: &str = "profile_name";
pub(crate) const API_KEY_QUESTION_ID: &str = "api_key";
pub(crate) const BASE_URL_QUESTION_ID: &str = "base_url";

#[derive(Clone, Copy, Debug)]
pub(crate) struct AccountProviderSpec {
    pub(crate) id: &'static str,
    pub(crate) name_en: &'static str,
    pub(crate) name_ru: &'static str,
    pub(crate) base_url: &'static str,
    pub(crate) api_key_optional: bool,
    pub(crate) builtin_model_provider_id: Option<&'static str>,
    pub(crate) requires_base_url: bool,
}

const ACCOUNT_PROVIDER_SPECS: [AccountProviderSpec; 8] = [
    AccountProviderSpec {
        id: "codex_oauth",
        name_en: "Codex OAuth",
        name_ru: "Codex OAuth",
        base_url: "",
        api_key_optional: true,
        builtin_model_provider_id: Some("openai"),
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "openai",
        name_en: "OpenAI API",
        name_ru: "OpenAI API",
        base_url: "https://api.openai.com/v1",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "openrouter",
        name_en: "OpenRouter",
        name_ru: "OpenRouter",
        base_url: "https://openrouter.ai/api/v1",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "gemini",
        name_en: "Gemini",
        name_ru: "Gemini",
        base_url: "https://generativelanguage.googleapis.com/v1beta/openai",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "mistral",
        name_en: "Mistral",
        name_ru: "Mistral",
        base_url: "https://api.mistral.ai/v1",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "groq",
        name_en: "Groq",
        name_ru: "Groq",
        base_url: "https://api.groq.com/openai/v1",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "ollama",
        name_en: "Ollama",
        name_ru: "Ollama",
        base_url: "http://127.0.0.1:11434/v1",
        api_key_optional: true,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "custom",
        name_en: "Custom OpenAI-compatible API",
        name_ru: "Кастомный OpenAI-compatible API",
        base_url: "",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: true,
    },
];

#[derive(Debug, Clone, Serialize, Deserialize)]
pub(crate) struct StoredAccountProfile {
    pub(crate) provider: String,
    pub(crate) name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) base_url: Option<String>,
    pub(crate) model: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) model_catalog_json: Option<PathBuf>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) config_profile: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) model_provider_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub(crate) experimental_bearer_token: Option<String>,
}

pub(crate) fn supported_account_providers() -> &'static [AccountProviderSpec] {
    &ACCOUNT_PROVIDER_SPECS
}

pub(crate) fn account_provider_spec(provider: &str) -> Option<AccountProviderSpec> {
    let normalized = match provider.trim().to_ascii_lowercase().as_str() {
        "codex" | "codex-oauth" | "oauth" | "chatgpt" | "openai-oauth" => {
            "codex_oauth".to_string()
        }
        "custom-openai" | "custom-api" | "custom-openai-compatible" => "custom".to_string(),
        other => other.to_string(),
    };
    ACCOUNT_PROVIDER_SPECS
        .iter()
        .copied()
        .find(|spec| spec.id == normalized)
}

pub(crate) fn provider_display_name(provider: &str, is_ru: bool) -> String {
    account_provider_spec(provider)
        .map(|spec| if is_ru { spec.name_ru } else { spec.name_en })
        .unwrap_or(provider)
        .to_string()
}

pub(crate) fn sanitize_profile_key(profile_name: &str, provider: &str) -> String {
    let normalized = if profile_name.trim().is_empty() {
        format!("{provider}-profile")
    } else {
        profile_name
            .chars()
            .map(|ch| {
                if ch.is_ascii_alphanumeric() || ch == '-' || ch == '_' {
                    ch
                } else {
                    '-'
                }
            })
            .collect::<String>()
    };
    let normalized = normalized
        .trim_matches('-')
        .trim_matches('_')
        .to_ascii_lowercase();
    if normalized.is_empty() {
        format!("{provider}-profile")
    } else {
        normalized
    }
}

pub(crate) fn stored_profile_path(codex_home: &Path, profile_key: &str) -> PathBuf {
    profiles_dir(codex_home).join(format!("{profile_key}.json"))
}

pub(crate) fn load_stored_profile(profile_path: &Path) -> io::Result<StoredAccountProfile> {
    let contents = std::fs::read_to_string(profile_path)?;
    serde_json::from_str::<StoredAccountProfile>(&contents)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))
}

pub(crate) fn save_stored_profile(
    codex_home: &Path,
    profile_key: &str,
    profile: &StoredAccountProfile,
) -> io::Result<PathBuf> {
    let dir = profiles_dir(codex_home);
    std::fs::create_dir_all(&dir)?;
    let path = stored_profile_path(codex_home, profile_key);
    let body = serde_json::to_string_pretty(profile)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
        + "\n";
    std::fs::write(&path, body)?;
    Ok(path)
}

pub(crate) fn create_or_update_stored_profile(
    codex_home: &Path,
    provider: &str,
    requested_name: &str,
    base_url_override: Option<String>,
    api_key: Option<String>,
) -> io::Result<(String, StoredAccountProfile, PathBuf)> {
    let spec = account_provider_spec(provider).ok_or_else(|| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("unsupported provider `{provider}`"),
        )
    })?;
    let profile_key = sanitize_profile_key(requested_name, provider);
    let model_seed_provider = spec.builtin_model_provider_id.unwrap_or(spec.id);
    let model_catalog_path =
        ensure_profile_model_catalog(codex_home, &profile_key, model_seed_provider)?;
    let base_url = if spec.builtin_model_provider_id.is_none() {
        base_url_override
            .and_then(|value| {
                let trimmed = value.trim();
                (!trimmed.is_empty()).then(|| trimmed.to_string())
            })
            .or_else(|| (!spec.base_url.is_empty()).then(|| spec.base_url.to_string()))
    } else {
        None
    };
    let stored = StoredAccountProfile {
        provider: spec.id.to_string(),
        name: requested_name
            .trim()
            .is_empty()
            .then(|| profile_key.clone())
            .unwrap_or_else(|| requested_name.trim().to_string()),
        base_url,
        model: default_profile_model(model_seed_provider),
        model_catalog_json: Some(model_catalog_path),
        config_profile: Some(profile_key.clone()),
        model_provider_id: Some(
            spec.builtin_model_provider_id
                .map(str::to_string)
                .unwrap_or_else(|| format!("{profile_key}-provider")),
        ),
        experimental_bearer_token: if spec.builtin_model_provider_id.is_none() {
            api_key.and_then(|value| {
                let trimmed = value.trim();
                (!trimmed.is_empty()).then(|| trimmed.to_string())
            })
        } else {
            None
        },
    };
    let path = save_stored_profile(codex_home, &profile_key, &stored)?;
    Ok((profile_key, stored, path))
}

pub(crate) fn stored_profile_has_saved_key(profile: &StoredAccountProfile) -> bool {
    profile.experimental_bearer_token.is_some()
        || account_provider_spec(profile.provider.as_str())
            .is_some_and(|spec| spec.api_key_optional)
}

pub(crate) fn build_create_profile_request(
    request_id: String,
    provider: &str,
    suggested_profile_name: Option<&str>,
    is_ru: bool,
) -> RequestUserInputEvent {
    let display_name = provider_display_name(provider, is_ru);
    let fallback_name = suggested_profile_name
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned)
        .unwrap_or_else(|| sanitize_profile_key("", provider));
    let key_prompt = match account_provider_spec(provider) {
        Some(spec) if spec.builtin_model_provider_id.is_some() && is_ru => {
            "API-ключ не нужен. Оставьте поле пустым и будет использоваться стандартный вход Codex/OpenAI.".to_string()
        }
        Some(spec) if spec.builtin_model_provider_id.is_some() => {
            "No API key is required. Leave this blank to use the standard Codex/OpenAI login.".to_string()
        }
        Some(spec) if spec.api_key_optional && is_ru => {
            "Введите API-ключ. Для локальных провайдеров вроде Ollama поле можно оставить пустым.".to_string()
        }
        Some(spec) if spec.api_key_optional => {
            "Enter the API key. For local providers like Ollama this field may be left empty.".to_string()
        }
        _ if is_ru => format!("Введите API-ключ для {display_name}."),
        _ => format!("Enter the API key for {display_name}."),
    };
    let base_url_prompt = match account_provider_spec(provider) {
        Some(spec) if spec.builtin_model_provider_id.is_some() => None,
        Some(spec) if spec.requires_base_url && is_ru => Some(
            "Введите base URL OpenAI-compatible API. Это поле обязательно для кастомного провайдера."
                .to_string(),
        ),
        Some(spec) if spec.requires_base_url => Some(
            "Enter the OpenAI-compatible API base URL. This is required for the custom provider."
                .to_string(),
        ),
        Some(spec) if is_ru => Some(format!(
            "Base URL API. Можно оставить пустым, тогда будет использовано значение по умолчанию: `{}`.",
            spec.base_url
        )),
        Some(spec) => Some(format!(
            "API base URL. Leave it empty to use the default: `{}`.",
            spec.base_url
        )),
        None => None,
    };

    let mut questions = vec![
        RequestUserInputQuestion {
            id: PROFILE_NAME_QUESTION_ID.to_string(),
            header: if is_ru {
                "Профиль".to_string()
            } else {
                "Profile".to_string()
            },
            question: if is_ru {
                format!(
                    "Название профиля. Можно оставить пустым, тогда будет использовано `{fallback_name}`."
                )
            } else {
                format!("Profile name. Leave it empty to use `{fallback_name}`.")
            },
            is_other: false,
            is_secret: false,
            options: None,
        },
        RequestUserInputQuestion {
            id: API_KEY_QUESTION_ID.to_string(),
            header: if is_ru {
                "API-ключ".to_string()
            } else {
                "API key".to_string()
            },
            question: key_prompt,
            is_other: false,
            is_secret: true,
            options: None,
        },
    ];

    if let Some(base_url_prompt) = base_url_prompt {
        questions.push(RequestUserInputQuestion {
            id: BASE_URL_QUESTION_ID.to_string(),
            header: "Base URL".to_string(),
            question: base_url_prompt,
            is_other: false,
            is_secret: false,
            options: None,
        });
    }

    RequestUserInputEvent {
        call_id: request_id.clone(),
        turn_id: request_id,
        questions,
    }
}
