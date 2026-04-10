use std::io;
use std::path::Path;
use std::path::PathBuf;

use codex_model_provider_info::ModelProviderInfo;
use codex_model_provider_info::WireApi;
use codex_models_manager::bundled_models_response;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::openai_models::ModelPreset;
use codex_protocol::openai_models::ModelsResponse;
use codex_protocol::openai_models::ReasoningEffort;
use codex_protocol::openai_models::ReasoningEffortPreset;
use codex_protocol::request_user_input::RequestUserInputEvent;
use codex_protocol::request_user_input::RequestUserInputQuestion;
use serde::Deserialize;
use serde::Serialize;

use crate::ui_preferences::default_profile_model;
use crate::ui_preferences::ensure_profile_model_catalog;
use crate::ui_preferences::normalize_profile_model;
use crate::ui_preferences::profiles_dir;
use crate::ui_preferences::repair_profile_model_catalog;

pub(crate) const PROFILE_NAME_QUESTION_ID: &str = "profile_name";
pub(crate) const API_KEY_QUESTION_ID: &str = "api_key";
pub(crate) const BASE_URL_QUESTION_ID: &str = "base_url";

#[derive(Clone, Copy, Debug)]
pub(crate) struct AccountProviderSpec {
    pub(crate) id: &'static str,
    pub(crate) name_en: &'static str,
    pub(crate) name_ru: &'static str,
    pub(crate) base_url: &'static str,
    pub(crate) wire_api: &'static str,
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
        wire_api: "responses",
        api_key_optional: true,
        builtin_model_provider_id: Some("openai"),
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "openai",
        name_en: "OpenAI API",
        name_ru: "OpenAI API",
        base_url: "https://api.openai.com/v1",
        wire_api: "responses",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "openrouter",
        name_en: "OpenRouter",
        name_ru: "OpenRouter",
        base_url: "https://openrouter.ai/api/v1",
        wire_api: "chat_completions",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "gemini",
        name_en: "Gemini",
        name_ru: "Gemini",
        base_url: "https://generativelanguage.googleapis.com/v1beta/openai",
        wire_api: "chat_completions",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "mistral",
        name_en: "Mistral",
        name_ru: "Mistral",
        base_url: "https://api.mistral.ai/v1",
        wire_api: "chat_completions",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "groq",
        name_en: "Groq",
        name_ru: "Groq",
        base_url: "https://api.groq.com/openai/v1",
        wire_api: "chat_completions",
        api_key_optional: false,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "ollama",
        name_en: "Ollama",
        name_ru: "Ollama",
        base_url: "http://127.0.0.1:11434/v1",
        wire_api: "chat_completions",
        api_key_optional: true,
        builtin_model_provider_id: None,
        requires_base_url: false,
    },
    AccountProviderSpec {
        id: "custom",
        name_en: "Custom OpenAI-compatible API",
        name_ru: "Кастомный OpenAI-compatible API",
        base_url: "",
        wire_api: "chat_completions",
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
        "codex" | "codex-oauth" | "oauth" | "chatgpt" | "openai-oauth" => "codex_oauth".to_string(),
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

fn derived_profile_model_catalog_path(profile_path: &Path) -> Option<PathBuf> {
    let stem = profile_path.file_stem()?.to_str()?;
    Some(profile_path.with_file_name(format!("{stem}.models.json")))
}

fn normalize_sidecar_path(profile_path: &Path, candidate: &Path) -> PathBuf {
    if candidate.is_absolute() {
        return candidate.to_path_buf();
    }
    profile_path
        .parent()
        .map(|parent| parent.join(candidate))
        .unwrap_or_else(|| candidate.to_path_buf())
}

fn normalized_profile_model_catalog_path(
    profile_path: &Path,
    model_catalog_json: Option<&Path>,
) -> Option<PathBuf> {
    model_catalog_json
        .filter(|path| !path.as_os_str().is_empty())
        .map(|path| normalize_sidecar_path(profile_path, path))
        .or_else(|| derived_profile_model_catalog_path(profile_path))
}

fn wire_api_from_spec(spec: AccountProviderSpec) -> io::Result<WireApi> {
    match spec.wire_api.trim().to_ascii_lowercase().as_str() {
        "responses" => Ok(WireApi::Responses),
        "chat_completions" => Ok(WireApi::ChatCompletions),
        other => Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!(
                "unsupported wire_api `{other}` in provider spec `{}`",
                spec.id
            ),
        )),
    }
}

pub(crate) fn build_custom_model_provider_info(
    profile: &StoredAccountProfile,
    spec: AccountProviderSpec,
) -> io::Result<ModelProviderInfo> {
    if spec.builtin_model_provider_id.is_some() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("provider `{}` is built-in and not custom", spec.id),
        ));
    }

    let base_url = profile
        .base_url
        .as_ref()
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned)
        .or_else(|| (!spec.base_url.trim().is_empty()).then(|| spec.base_url.trim().to_string()));
    if spec.requires_base_url && base_url.is_none() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("provider `{}` requires a base_url", spec.id),
        ));
    }

    let name = profile.name.trim();
    let token = profile
        .experimental_bearer_token
        .as_ref()
        .map(String::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned);

    Ok(ModelProviderInfo {
        name: if name.is_empty() {
            provider_display_name(spec.id, false)
        } else {
            name.to_string()
        },
        base_url,
        env_key: None,
        env_key_instructions: None,
        experimental_bearer_token: token,
        auth: None,
        wire_api: wire_api_from_spec(spec)?,
        query_params: None,
        http_headers: None,
        env_http_headers: None,
        request_max_retries: None,
        stream_max_retries: None,
        stream_idle_timeout_ms: None,
        websocket_connect_timeout_ms: None,
        requires_openai_auth: false,
        supports_websockets: false,
    })
}

pub(crate) fn profile_model_catalog_sidecar_path(
    profile_path: &Path,
    profile: &StoredAccountProfile,
) -> Option<PathBuf> {
    normalized_profile_model_catalog_path(profile_path, profile.model_catalog_json.as_deref())
}

const PROVIDER_MODEL_VARIANT_SUFFIXES: [&str; 4] = ["-with-tools", "-tools", "-latest", "-fast"];

fn candidate_model_slug_matches(requested_slug: &str, candidate_slug: &str) -> bool {
    if candidate_slug.eq_ignore_ascii_case(requested_slug) {
        return true;
    }
    if requested_slug.starts_with(candidate_slug) {
        return true;
    }

    let requested_tail = requested_slug.rsplit('/').next().unwrap_or(requested_slug);
    let candidate_tail = candidate_slug.rsplit('/').next().unwrap_or(candidate_slug);
    if candidate_tail.eq_ignore_ascii_case(requested_tail) {
        return true;
    }
    if requested_tail.starts_with(candidate_tail) {
        return true;
    }

    PROVIDER_MODEL_VARIANT_SUFFIXES.iter().any(|suffix| {
        requested_tail
            .strip_suffix(suffix)
            .is_some_and(|base| !base.is_empty() && candidate_tail.eq_ignore_ascii_case(base))
    })
}

fn find_bundled_sidecar_model_metadata(
    requested_slug: &str,
    bundled_models: &[ModelInfo],
) -> Option<ModelInfo> {
    bundled_models
        .iter()
        .filter(|candidate| candidate_model_slug_matches(requested_slug, candidate.slug.as_str()))
        .max_by_key(|candidate| candidate.slug.len())
        .cloned()
}

const LEGACY_SYNTHETIC_REASONING_DESCRIPTIONS: [&str; 4] = [
    "Отключает отдельный бюджет размышлений ради более прямого ответа",
    "Быстрее отвечает и тратит меньше бюджета размышлений",
    "Сбалансированный режим для повседневной разработки",
    "Глубже разбирает сложные и неоднозначные задачи",
];

fn inferred_openai_reasoning_levels(provider: &str) -> Option<Vec<ReasoningEffortPreset>> {
    if !(provider.eq_ignore_ascii_case("openai") || provider.eq_ignore_ascii_case("codex_oauth")) {
        return None;
    }

    Some(vec![
        ReasoningEffortPreset {
            effort: ReasoningEffort::Low,
            description: "Быстрее отвечает и тратит меньше бюджета размышлений".to_string(),
        },
        ReasoningEffortPreset {
            effort: ReasoningEffort::Medium,
            description: "Сбалансированный режим для повседневной разработки".to_string(),
        },
        ReasoningEffortPreset {
            effort: ReasoningEffort::High,
            description: "Глубже разбирает сложные и неоднозначные задачи".to_string(),
        },
        ReasoningEffortPreset {
            effort: ReasoningEffort::XHigh,
            description: "Максимальный бюджет размышлений для тяжёлых случаев".to_string(),
        },
    ])
}

fn reasoning_effort_order(effort: ReasoningEffort) -> usize {
    match effort {
        ReasoningEffort::None => 0,
        ReasoningEffort::Minimal => 1,
        ReasoningEffort::Low => 2,
        ReasoningEffort::Medium => 3,
        ReasoningEffort::High => 4,
        ReasoningEffort::XHigh => 5,
    }
}

fn merge_reasoning_levels(
    supported_reasoning_levels: &mut Vec<ReasoningEffortPreset>,
    incoming: &[ReasoningEffortPreset],
) -> bool {
    let mut changed = false;
    for option in incoming {
        if supported_reasoning_levels
            .iter()
            .any(|existing| existing.effort == option.effort)
        {
            continue;
        }
        supported_reasoning_levels.push(option.clone());
        changed = true;
    }
    if changed {
        supported_reasoning_levels.sort_by_key(|option| reasoning_effort_order(option.effort));
    }
    changed
}

fn normalize_reasoning_default(model: &mut ModelInfo) -> bool {
    if model.supported_reasoning_levels.is_empty() {
        return false;
    }

    let preferred_default = match model.default_reasoning_level {
        Some(default_effort)
            if model
                .supported_reasoning_levels
                .iter()
                .any(|option| option.effort == default_effort) =>
        {
            return false;
        }
        Some(ReasoningEffort::XHigh)
            if model
                .supported_reasoning_levels
                .iter()
                .any(|option| option.effort == ReasoningEffort::XHigh) =>
        {
            ReasoningEffort::XHigh
        }
        Some(ReasoningEffort::Medium)
            if model
                .supported_reasoning_levels
                .iter()
                .any(|option| option.effort == ReasoningEffort::Medium) =>
        {
            ReasoningEffort::Medium
        }
        Some(ReasoningEffort::High)
            if model
                .supported_reasoning_levels
                .iter()
                .any(|option| option.effort == ReasoningEffort::High) =>
        {
            ReasoningEffort::High
        }
        Some(ReasoningEffort::None)
            if model
                .supported_reasoning_levels
                .iter()
                .any(|option| option.effort == ReasoningEffort::None) =>
        {
            ReasoningEffort::None
        }
        _ if model
            .supported_reasoning_levels
            .iter()
            .any(|option| option.effort == ReasoningEffort::Medium) =>
        {
            ReasoningEffort::Medium
        }
        _ if model
            .supported_reasoning_levels
            .iter()
            .any(|option| option.effort == ReasoningEffort::High) =>
        {
            ReasoningEffort::High
        }
        _ => model.supported_reasoning_levels[0].effort,
    };

    model.default_reasoning_level = Some(preferred_default);
    true
}

fn is_legacy_synthetic_reasoning_description(description: &str) -> bool {
    LEGACY_SYNTHETIC_REASONING_DESCRIPTIONS
        .iter()
        .any(|candidate| candidate == &description)
}

fn strip_legacy_synthetic_reasoning_metadata(
    provider: &str,
    model: &mut ModelInfo,
    bundled_metadata_present: bool,
) -> bool {
    if bundled_metadata_present
        || provider.eq_ignore_ascii_case("openai")
        || provider.eq_ignore_ascii_case("codex_oauth")
        || model.supported_reasoning_levels.is_empty()
        || !model
            .supported_reasoning_levels
            .iter()
            .all(|option| is_legacy_synthetic_reasoning_description(option.description.as_str()))
    {
        return false;
    }

    model.supported_reasoning_levels.clear();
    model.default_reasoning_level = None;
    true
}

fn strip_non_openai_default_reasoning_without_supported_levels(
    provider: &str,
    model: &mut ModelInfo,
) -> bool {
    if provider.eq_ignore_ascii_case("openai")
        || provider.eq_ignore_ascii_case("codex_oauth")
        || !model.supported_reasoning_levels.is_empty()
        || model.default_reasoning_level.is_none()
    {
        return false;
    }

    model.default_reasoning_level = None;
    true
}

fn repair_sidecar_model_metadata(
    provider: &str,
    model: &mut ModelInfo,
    bundled_models: &[ModelInfo],
) -> bool {
    let mut changed = false;
    let original_slug = model.slug.clone();
    let normalized_slug = normalize_profile_model(provider, original_slug.as_str());
    if normalized_slug != original_slug {
        model.slug = normalized_slug.clone();
        if model.display_name == original_slug {
            model.display_name = normalized_slug;
        }
        changed = true;
    }

    if model.display_name.trim().is_empty() {
        model.display_name = model.slug.clone();
        changed = true;
    }

    let bundled = find_bundled_sidecar_model_metadata(model.slug.as_str(), bundled_models);
    if let Some(bundled) = bundled.as_ref() {
        changed |= merge_reasoning_levels(
            &mut model.supported_reasoning_levels,
            &bundled.supported_reasoning_levels,
        );
        if model.default_reasoning_level.is_none() && bundled.default_reasoning_level.is_some() {
            model.default_reasoning_level = bundled.default_reasoning_level;
            changed = true;
        }
        if !model.supports_reasoning_summaries && bundled.supports_reasoning_summaries {
            model.supports_reasoning_summaries = true;
            changed = true;
        }
        if !model.support_verbosity && bundled.support_verbosity {
            model.support_verbosity = true;
            changed = true;
        }
        if model.default_verbosity.is_none() && bundled.default_verbosity.is_some() {
            model.default_verbosity = bundled.default_verbosity.clone();
            changed = true;
        }
        if model.apply_patch_tool_type.is_none() && bundled.apply_patch_tool_type.is_some() {
            model.apply_patch_tool_type = bundled.apply_patch_tool_type.clone();
            changed = true;
        }
        if !model.supports_parallel_tool_calls && bundled.supports_parallel_tool_calls {
            model.supports_parallel_tool_calls = true;
            changed = true;
        }
        if model.input_modalities.is_empty() && !bundled.input_modalities.is_empty() {
            model.input_modalities = bundled.input_modalities.clone();
            changed = true;
        }
        if !model.supports_search_tool && bundled.supports_search_tool {
            model.supports_search_tool = true;
            changed = true;
        }
    }

    changed |= strip_legacy_synthetic_reasoning_metadata(provider, model, bundled.is_some());
    changed |= strip_non_openai_default_reasoning_without_supported_levels(provider, model);

    if let Some(levels) = inferred_openai_reasoning_levels(provider) {
        changed |= merge_reasoning_levels(&mut model.supported_reasoning_levels, &levels);
    }

    if model.default_reasoning_level.is_none() && !model.supported_reasoning_levels.is_empty() {
        model.default_reasoning_level = Some(ReasoningEffort::Medium);
        changed = true;
    }

    changed |= normalize_reasoning_default(model);

    changed
}

fn repair_sidecar_models(provider: &str, models: &mut [ModelInfo]) -> bool {
    let bundled_models = bundled_models_response()
        .map(|response| response.models)
        .unwrap_or_default();
    let mut changed = false;
    for model in models {
        changed |= repair_sidecar_model_metadata(provider, model, &bundled_models);
    }
    changed
}

pub(crate) fn read_profile_model_catalog_sidecar(
    profile_path: &Path,
    profile: &StoredAccountProfile,
) -> io::Result<Option<(ModelsResponse, Vec<ModelPreset>)>> {
    let Some(path) = profile_model_catalog_sidecar_path(profile_path, profile) else {
        return Ok(None);
    };

    let contents = match std::fs::read_to_string(&path) {
        Ok(contents) => contents,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(None),
        Err(err) => return Err(err),
    };

    let mut models_response = serde_json::from_str::<ModelsResponse>(&contents)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?;
    if repair_sidecar_models(profile.provider.as_str(), &mut models_response.models) {
        let repaired_body = serde_json::to_string_pretty(&models_response)
            .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
            + "\n";
        std::fs::write(&path, repaired_body)?;
    }
    let presets = models_response
        .models
        .clone()
        .into_iter()
        .map(ModelPreset::from)
        .collect::<Vec<_>>();
    Ok(Some((models_response, presets)))
}

pub(crate) fn write_profile_model_catalog_sidecar(
    profile_path: &Path,
    profile: &StoredAccountProfile,
    models: &[ModelInfo],
) -> io::Result<PathBuf> {
    let path = profile_model_catalog_sidecar_path(profile_path, profile).ok_or_else(|| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            format!(
                "cannot derive sidecar model catalog path from `{}`",
                profile_path.display()
            ),
        )
    })?;
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let payload = ModelsResponse {
        models: models.to_vec(),
    };
    let body = serde_json::to_string_pretty(&payload)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
        + "\n";
    std::fs::write(&path, body)?;
    Ok(path)
}

fn write_stored_profile_file(
    profile_path: &Path,
    profile: &StoredAccountProfile,
) -> io::Result<()> {
    let body = serde_json::to_string_pretty(profile)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?
        + "\n";
    std::fs::write(profile_path, body)
}

fn repair_profile_catalogs(profile_path: &Path, profile: &StoredAccountProfile) -> io::Result<()> {
    let mut candidates = Vec::new();
    if let Some(path) =
        normalized_profile_model_catalog_path(profile_path, profile.model_catalog_json.as_deref())
    {
        candidates.push(path);
    }
    if let Some(path) = derived_profile_model_catalog_path(profile_path)
        && !candidates.iter().any(|candidate| candidate == &path)
    {
        candidates.push(path);
    }

    for candidate in candidates {
        match repair_profile_model_catalog(candidate.as_path(), profile.provider.as_str()) {
            Ok(_) => {}
            Err(err) if err.kind() == io::ErrorKind::NotFound => {}
            Err(err) => return Err(err),
        }
    }

    Ok(())
}

pub(crate) fn load_stored_profile(profile_path: &Path) -> io::Result<StoredAccountProfile> {
    let contents = std::fs::read_to_string(profile_path)?;
    let mut profile = serde_json::from_str::<StoredAccountProfile>(&contents)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?;
    let normalized_model =
        normalize_profile_model(profile.provider.as_str(), profile.model.as_str());
    let changed = normalized_model != profile.model;
    if changed {
        profile.model = normalized_model;
    }
    repair_profile_catalogs(profile_path, &profile)?;
    if changed {
        write_stored_profile_file(profile_path, &profile)?;
    }
    Ok(profile)
}

pub(crate) fn save_stored_profile(
    codex_home: &Path,
    profile_key: &str,
    profile: &StoredAccountProfile,
) -> io::Result<PathBuf> {
    let dir = profiles_dir(codex_home);
    std::fs::create_dir_all(&dir)?;
    let path = stored_profile_path(codex_home, profile_key);
    let mut profile = profile.clone();
    profile.model = normalize_profile_model(profile.provider.as_str(), profile.model.as_str());
    write_stored_profile_file(&path, &profile)?;
    repair_profile_catalogs(&path, &profile)?;
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
        name: if requested_name.trim().is_empty() {
            profile_key.clone()
        } else {
            requested_name.trim().to_string()
        },
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

#[cfg(test)]
mod tests {
    use super::*;
    use codex_protocol::config_types::ReasoningSummary;
    use codex_protocol::openai_models::ConfigShellToolType;
    use codex_protocol::openai_models::ModelVisibility;
    use codex_protocol::openai_models::ReasoningEffort;
    use codex_protocol::openai_models::ReasoningEffortPreset;
    use codex_protocol::openai_models::TruncationPolicyConfig;
    use codex_protocol::openai_models::WebSearchToolType;
    use tempfile::tempdir;

    fn test_profile(provider: &str) -> StoredAccountProfile {
        StoredAccountProfile {
            provider: provider.to_string(),
            name: "profile".to_string(),
            base_url: None,
            model: "model".to_string(),
            model_catalog_json: None,
            config_profile: Some("profile".to_string()),
            model_provider_id: Some("profile-provider".to_string()),
            experimental_bearer_token: None,
        }
    }

    fn test_model_info(slug: &str) -> ModelInfo {
        ModelInfo {
            slug: slug.to_string(),
            display_name: slug.to_string(),
            description: None,
            default_reasoning_level: Some(ReasoningEffort::Medium),
            supported_reasoning_levels: vec![ReasoningEffortPreset {
                effort: ReasoningEffort::Medium,
                description: "default".to_string(),
            }],
            shell_type: ConfigShellToolType::Default,
            visibility: ModelVisibility::List,
            supported_in_api: true,
            priority: 1,
            availability_nux: None,
            upgrade: None,
            base_instructions: "base".to_string(),
            model_messages: None,
            supports_reasoning_summaries: false,
            default_reasoning_summary: ReasoningSummary::Auto,
            support_verbosity: false,
            default_verbosity: None,
            apply_patch_tool_type: None,
            web_search_tool_type: WebSearchToolType::Text,
            truncation_policy: TruncationPolicyConfig::bytes(1024),
            supports_parallel_tool_calls: true,
            supports_image_detail_original: false,
            context_window: None,
            auto_compact_token_limit: None,
            effective_context_window_percent: 95,
            experimental_supported_tools: Vec::new(),
            input_modalities: Vec::new(),
            used_fallback_model_metadata: false,
            supports_search_tool: false,
        }
    }

    #[test]
    fn load_stored_profile_preserves_legacy_mistral_model_and_sidecar() {
        let codex_home = tempdir().expect("tempdir");
        let profiles = profiles_dir(codex_home.path());
        std::fs::create_dir_all(&profiles).expect("profiles dir");

        let profile_path = profiles.join("mistral1.json");
        let sidecar_path = profiles.join("mistral1.models.json");
        std::fs::write(
            &profile_path,
            serde_json::json!({
                "provider": "mistral",
                "name": "mistral1",
                "model": "mistral-vibe-cli",
                "model_catalog_json": sidecar_path,
                "config_profile": "mistral1",
                "model_provider_id": "mistral1-provider",
                "experimental_bearer_token": "secret"
            })
            .to_string(),
        )
        .expect("write profile");
        std::fs::write(
            &sidecar_path,
            serde_json::json!({
                "models": [{
                    "slug": "mistral-vibe-cli",
                    "display_name": "mistral-vibe-cli"
                }]
            })
            .to_string(),
        )
        .expect("write sidecar");

        let loaded = load_stored_profile(&profile_path).expect("load repaired profile");
        assert_eq!(loaded.model, "mistral-vibe-cli");
        assert!(
            std::fs::read_to_string(&profile_path)
                .expect("profile contents")
                .contains("\"mistral-vibe-cli\"")
        );
        assert!(
            std::fs::read_to_string(&sidecar_path)
                .expect("sidecar contents")
                .contains("\"mistral-vibe-cli\"")
        );
    }

    #[test]
    fn load_stored_profile_repairs_legacy_gemini_model_and_sidecar() {
        let codex_home = tempdir().expect("tempdir");
        let profiles = profiles_dir(codex_home.path());
        std::fs::create_dir_all(&profiles).expect("profiles dir");

        let profile_path = profiles.join("gemini1.json");
        let sidecar_path = profiles.join("gemini1.models.json");
        std::fs::write(
            &profile_path,
            serde_json::json!({
                "provider": "gemini",
                "name": "gemini1",
                "model": "models/gemini-2.5-pro",
                "model_catalog_json": sidecar_path,
                "config_profile": "gemini1",
                "model_provider_id": "gemini1-provider",
                "experimental_bearer_token": "secret"
            })
            .to_string(),
        )
        .expect("write profile");
        std::fs::write(
            &sidecar_path,
            serde_json::json!({
                "models": [{
                    "slug": "models/gemini-2.5-pro",
                    "display_name": "models/gemini-2.5-pro"
                }]
            })
            .to_string(),
        )
        .expect("write sidecar");

        let loaded = load_stored_profile(&profile_path).expect("load repaired profile");
        assert_eq!(loaded.model, "gemini-2.5-pro");
        assert!(
            !std::fs::read_to_string(&profile_path)
                .expect("profile contents")
                .contains("models/gemini-2.5-pro")
        );
        assert!(
            !std::fs::read_to_string(&sidecar_path)
                .expect("sidecar contents")
                .contains("models/gemini-2.5-pro")
        );
    }

    #[test]
    fn profile_model_catalog_sidecar_path_uses_derived_fallback() {
        let profile = test_profile("openai");
        let profile_path = Path::new("/tmp/profiles/openai.json");
        let sidecar = profile_model_catalog_sidecar_path(profile_path, &profile).expect("path");
        assert_eq!(sidecar, PathBuf::from("/tmp/profiles/openai.models.json"));
    }

    #[test]
    fn profile_model_catalog_sidecar_path_normalizes_relative_path() {
        let mut profile = test_profile("openai");
        profile.model_catalog_json = Some(PathBuf::from("custom.models.json"));
        let profile_path = Path::new("/tmp/profiles/openai.json");
        let sidecar = profile_model_catalog_sidecar_path(profile_path, &profile).expect("path");
        assert_eq!(sidecar, PathBuf::from("/tmp/profiles/custom.models.json"));
    }

    #[test]
    fn write_and_read_profile_model_catalog_sidecar_roundtrip() {
        let codex_home = tempdir().expect("tempdir");
        let profile_path = codex_home.path().join("Profiles/openai-profile.json");
        let profile = test_profile("openai");
        let models = vec![test_model_info("gpt-5.4")];

        let sidecar_path =
            write_profile_model_catalog_sidecar(profile_path.as_path(), &profile, &models)
                .expect("write sidecar");
        assert_eq!(
            sidecar_path,
            codex_home
                .path()
                .join("Profiles/openai-profile.models.json")
        );

        let loaded = read_profile_model_catalog_sidecar(profile_path.as_path(), &profile)
            .expect("read sidecar")
            .expect("present sidecar");
        assert_eq!(loaded.0.models.len(), 1);
        assert_eq!(loaded.0.models[0].slug, "gpt-5.4");
        assert_eq!(loaded.1.len(), 1);
        assert_eq!(loaded.1[0].model, "gpt-5.4");
    }

    #[test]
    fn read_profile_model_catalog_sidecar_repairs_openai_reasoning_metadata() {
        let codex_home = tempdir().expect("tempdir");
        let profile_path = codex_home.path().join("Profiles/openai-profile.json");
        let profile = test_profile("openai");
        std::fs::create_dir_all(profile_path.parent().expect("profiles dir")).expect("mkdirs");

        let mut model = test_model_info("gpt-5.4");
        model.supported_reasoning_levels.clear();
        model.default_reasoning_level = None;
        model.supports_reasoning_summaries = false;
        write_profile_model_catalog_sidecar(profile_path.as_path(), &profile, &[model])
            .expect("write sidecar");

        let loaded = read_profile_model_catalog_sidecar(profile_path.as_path(), &profile)
            .expect("read sidecar")
            .expect("present sidecar");
        assert!(loaded.0.models[0].supports_reasoning_summaries);
        assert!(
            loaded.1[0]
                .supported_reasoning_efforts
                .iter()
                .any(|option| option.effort == ReasoningEffort::XHigh)
        );
    }

    #[test]
    fn read_profile_model_catalog_sidecar_merges_missing_openai_xhigh_metadata() {
        let codex_home = tempdir().expect("tempdir");
        let profile_path = codex_home.path().join("Profiles/openai-profile.json");
        let profile = test_profile("openai");
        std::fs::create_dir_all(profile_path.parent().expect("profiles dir")).expect("mkdirs");

        let mut model = test_model_info("gpt-5.4");
        model.supported_reasoning_levels = vec![
            ReasoningEffortPreset {
                effort: ReasoningEffort::Low,
                description: "low".to_string(),
            },
            ReasoningEffortPreset {
                effort: ReasoningEffort::Medium,
                description: "medium".to_string(),
            },
            ReasoningEffortPreset {
                effort: ReasoningEffort::High,
                description: "high".to_string(),
            },
        ];
        write_profile_model_catalog_sidecar(profile_path.as_path(), &profile, &[model])
            .expect("write sidecar");

        let loaded = read_profile_model_catalog_sidecar(profile_path.as_path(), &profile)
            .expect("read sidecar")
            .expect("present sidecar");
        assert!(
            loaded.1[0]
                .supported_reasoning_efforts
                .iter()
                .any(|option| option.effort == ReasoningEffort::XHigh)
        );
    }

    #[test]
    fn read_profile_model_catalog_sidecar_clears_empty_gemini_reasoning_default() {
        let codex_home = tempdir().expect("tempdir");
        let profile_path = codex_home.path().join("Profiles/gemini-profile.json");
        let profile = test_profile("gemini");
        std::fs::create_dir_all(profile_path.parent().expect("profiles dir")).expect("mkdirs");

        let mut model = test_model_info("models/gemini-2.5-pro");
        model.display_name = "models/gemini-2.5-pro".to_string();
        model.supported_reasoning_levels.clear();
        model.default_reasoning_level = Some(ReasoningEffort::Medium);
        write_profile_model_catalog_sidecar(profile_path.as_path(), &profile, &[model])
            .expect("write sidecar");

        let loaded = read_profile_model_catalog_sidecar(profile_path.as_path(), &profile)
            .expect("read sidecar")
            .expect("present sidecar");
        assert_eq!(loaded.0.models[0].slug, "gemini-2.5-pro");
        assert_eq!(loaded.1[0].model, "gemini-2.5-pro");
        assert_eq!(loaded.0.models[0].default_reasoning_level, None);
        assert!(loaded.1[0].supported_reasoning_efforts.is_empty());
        assert!(
            !std::fs::read_to_string(profile_path.with_file_name("gemini-profile.models.json"))
                .expect("sidecar contents")
                .contains("models/gemini-2.5-pro")
        );
    }

    #[test]
    fn read_profile_model_catalog_sidecar_strips_legacy_mistral_reasoning_levels() {
        let codex_home = tempdir().expect("tempdir");
        let profile_path = codex_home.path().join("Profiles/mistral-profile.json");
        let profile = test_profile("mistral");
        std::fs::create_dir_all(profile_path.parent().expect("profiles dir")).expect("mkdirs");

        let mut model = test_model_info("devstral-latest");
        model.supported_reasoning_levels = vec![
            ReasoningEffortPreset {
                effort: ReasoningEffort::None,
                description: "Отключает отдельный бюджет размышлений ради более прямого ответа"
                    .to_string(),
            },
            ReasoningEffortPreset {
                effort: ReasoningEffort::High,
                description: "Глубже разбирает сложные и неоднозначные задачи".to_string(),
            },
        ];
        model.default_reasoning_level = Some(ReasoningEffort::High);
        write_profile_model_catalog_sidecar(profile_path.as_path(), &profile, &[model])
            .expect("write sidecar");

        let loaded = read_profile_model_catalog_sidecar(profile_path.as_path(), &profile)
            .expect("read sidecar")
            .expect("present sidecar");
        assert_eq!(loaded.0.models[0].default_reasoning_level, None);
        assert!(loaded.1[0].supported_reasoning_efforts.is_empty());
    }

    #[test]
    fn load_stored_profile_drops_non_gemini_sidecar_entries() {
        let codex_home = tempdir().expect("tempdir");
        let profiles = profiles_dir(codex_home.path());
        std::fs::create_dir_all(&profiles).expect("profiles dir");

        let profile_path = profiles.join("gemini1.json");
        let sidecar_path = profiles.join("gemini1.models.json");
        std::fs::write(
            &profile_path,
            serde_json::json!({
                "provider": "gemini",
                "name": "gemini1",
                "model": "gemini-2.5-pro",
                "model_catalog_json": sidecar_path,
                "config_profile": "gemini1",
                "model_provider_id": "gemini1-provider",
                "experimental_bearer_token": "secret"
            })
            .to_string(),
        )
        .expect("write profile");
        std::fs::write(
            &sidecar_path,
            serde_json::json!({
                "models": [
                    {
                        "slug": "models/gemma-4-31b-it",
                        "display_name": "models/gemma-4-31b-it"
                    },
                    {
                        "slug": "models/gemini-2.5-pro",
                        "display_name": "models/gemini-2.5-pro"
                    }
                ]
            })
            .to_string(),
        )
        .expect("write sidecar");

        let _ = load_stored_profile(&profile_path).expect("load repaired profile");
        let contents = std::fs::read_to_string(&sidecar_path).expect("sidecar contents");
        assert!(!contents.contains("gemma-4-31b-it"));
        assert!(contents.contains("gemini-2.5-pro"));
    }

    #[test]
    fn build_custom_model_provider_info_uses_spec_defaults() {
        let mut profile = test_profile("mistral");
        profile.name = "Mistral Personal".to_string();
        profile.experimental_bearer_token = Some(" secret ".to_string());
        let spec = account_provider_spec("mistral").expect("provider spec");

        let provider = build_custom_model_provider_info(&profile, spec).expect("provider");
        assert_eq!(provider.name, "Mistral Personal");
        assert_eq!(
            provider.base_url.as_deref(),
            Some("https://api.mistral.ai/v1")
        );
        assert_eq!(
            provider.experimental_bearer_token.as_deref(),
            Some("secret")
        );
        assert_eq!(provider.wire_api, WireApi::ChatCompletions);
        assert!(!provider.requires_openai_auth);
    }

    #[test]
    fn build_custom_model_provider_info_rejects_missing_required_base_url() {
        let profile = test_profile("custom");
        let spec = account_provider_spec("custom").expect("provider spec");

        let err = build_custom_model_provider_info(&profile, spec).expect_err("missing base url");
        assert_eq!(err.kind(), io::ErrorKind::InvalidInput);
    }
}
