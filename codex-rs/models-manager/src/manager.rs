use super::cache::ModelsCacheManager;
use crate::collaboration_mode_presets::CollaborationModesConfig;
use crate::collaboration_mode_presets::builtin_collaboration_mode_presets;
use crate::config::ModelsManagerConfig;
use crate::model_info;
use codex_api::ModelsClient;
use codex_api::RequestTelemetry;
use codex_api::ReqwestTransport;
use codex_api::TransportError;
use codex_api::api_bridge::map_api_error;
use codex_app_server_protocol::AuthMode;
use codex_feedback::FeedbackRequestTags;
use codex_feedback::emit_feedback_request_tags_with_auth_env;
use codex_login::AuthEnvTelemetry;
use codex_login::AuthManager;
use codex_login::CodexAuth;
use codex_login::auth_provider_from_auth;
use codex_login::collect_auth_env_telemetry;
use codex_login::default_client::build_reqwest_client;
use codex_login::required_auth_manager_for_provider;
use codex_model_provider_info::ModelProviderInfo;
use codex_otel::TelemetryAuthMode;
use codex_protocol::config_types::CollaborationModeMask;
use codex_protocol::error::CodexErr;
use codex_protocol::error::Result as CoreResult;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::openai_models::ModelPreset;
use codex_protocol::openai_models::ModelsResponse;
use codex_response_debug_context::extract_response_debug_context;
use codex_response_debug_context::telemetry_transport_error_message;
use http::HeaderMap;
use serde::Deserialize;
use std::collections::HashSet;
use std::fmt;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;
use tokio::sync::TryLockError;
use tokio::time::timeout;
use tracing::error;
use tracing::info;
use tracing::instrument;

const MODEL_CACHE_FILE: &str = "models_cache.json";
const DEFAULT_MODEL_CACHE_TTL: Duration = Duration::from_secs(300);
const MODELS_REFRESH_TIMEOUT: Duration = Duration::from_secs(5);
const MODELS_ENDPOINT: &str = "/models";
const PROVIDER_MODEL_VARIANT_SUFFIXES: [&str; 4] = ["-with-tools", "-tools", "-latest", "-fast"];
const MISTRAL_LEGACY_BASE_MODEL: &str = "mistral-vibe-cli";
const MISTRAL_LEGACY_LATEST_MODEL: &str = "mistral-vibe-cli-latest";
const MISTRAL_LEGACY_TOOL_MODEL: &str = "mistral-vibe-cli-with-tools";
const MISTRAL_LEGACY_FAST_MODEL: &str = "mistral-vibe-cli-fast";
const MISTRAL_DEFAULT_MODEL: &str = "devstral-latest";
const MISTRAL_FAST_MODEL: &str = "devstral-small-latest";

#[derive(Debug, Deserialize)]
struct OpenAiCompatibleModelsEnvelope {
    #[serde(default)]
    data: Vec<OpenAiCompatibleModelEntry>,
    #[serde(default)]
    models: Vec<OpenAiCompatibleModelEntry>,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum OpenAiCompatibleModelEntry {
    String(String),
    Object(OpenAiCompatibleModelObject),
}

#[derive(Debug, Default, Deserialize)]
struct OpenAiCompatibleModelObject {
    #[serde(default)]
    id: Option<String>,
    #[serde(default)]
    slug: Option<String>,
    #[serde(default)]
    model: Option<String>,
    #[serde(default)]
    name: Option<String>,
    #[serde(default, rename = "baseModelId")]
    base_model_id: Option<String>,
    #[serde(default, rename = "base_model_id")]
    base_model_id_snake: Option<String>,
    #[serde(default, rename = "supportedGenerationMethods")]
    supported_generation_methods: Vec<String>,
    #[serde(default, rename = "supported_generation_methods")]
    supported_generation_methods_snake: Vec<String>,
    #[serde(default)]
    capabilities: Option<OpenAiCompatibleModelCapabilities>,
    #[serde(default, rename = "type")]
    kind: Option<String>,
    #[serde(default)]
    archived: Option<bool>,
}

#[derive(Debug, Default, Deserialize)]
struct OpenAiCompatibleModelCapabilities {
    #[serde(default)]
    completion_chat: Option<bool>,
    #[serde(default)]
    chat_completion: Option<bool>,
}

fn candidate_model_slug_matches(requested_slug: &str, candidate_slug: &str) -> bool {
    let requested_normalized = model_info::normalize_provider_model_alias_slug(requested_slug)
        .or_else(|| model_info::canonicalize_provider_model_slug(requested_slug))
        .unwrap_or_else(|| requested_slug.trim().to_string());
    let candidate_normalized = model_info::normalize_provider_model_alias_slug(candidate_slug)
        .or_else(|| model_info::canonicalize_provider_model_slug(candidate_slug))
        .unwrap_or_else(|| candidate_slug.trim().to_string());

    if candidate_normalized.eq_ignore_ascii_case(requested_normalized.as_str()) {
        return true;
    }
    if requested_normalized.starts_with(candidate_normalized.as_str()) {
        return true;
    }

    let requested_tail = requested_normalized
        .rsplit('/')
        .next()
        .unwrap_or(requested_normalized.as_str());
    let candidate_tail = candidate_normalized
        .rsplit('/')
        .next()
        .unwrap_or(candidate_normalized.as_str());
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

fn find_bundled_provider_model_metadata(
    requested_slug: &str,
    bundled_models: &[ModelInfo],
) -> Option<ModelInfo> {
    bundled_models
        .iter()
        .filter(|candidate| candidate_model_slug_matches(requested_slug, candidate.slug.as_str()))
        .max_by_key(|candidate| candidate.slug.len())
        .cloned()
}

fn normalize_mistral_legacy_model_slug(model: &str) -> Option<String> {
    let trimmed = model.trim();
    if trimmed.is_empty() {
        return None;
    }

    let (prefix, tail) = trimmed
        .rsplit_once('/')
        .map_or((None, trimmed), |(prefix, tail)| (Some(prefix), tail));
    let normalized_tail = match tail.to_ascii_lowercase().as_str() {
        MISTRAL_LEGACY_BASE_MODEL | MISTRAL_LEGACY_LATEST_MODEL | MISTRAL_LEGACY_TOOL_MODEL => {
            Some(MISTRAL_DEFAULT_MODEL)
        }
        MISTRAL_LEGACY_FAST_MODEL => Some(MISTRAL_FAST_MODEL),
        _ => None,
    }?;

    Some(match prefix {
        Some(prefix) => format!("{prefix}/{normalized_tail}"),
        None => normalized_tail.to_string(),
    })
}

fn provider_uses_gemini_api(provider: &ModelProviderInfo) -> bool {
    provider.uses_gemini_api()
}

fn normalize_provider_catalog_slug(provider: &ModelProviderInfo, slug: &str) -> String {
    let trimmed = slug.trim();

    if provider_uses_gemini_api(provider) {
        if let Some(normalized) = model_info::normalize_provider_model_alias_slug(trimmed) {
            return normalized
                .rsplit('/')
                .next()
                .unwrap_or(normalized.as_str())
                .to_string();
        }

        if trimmed
            .get(..7)
            .is_some_and(|prefix| prefix.eq_ignore_ascii_case("models/"))
        {
            let normalized = trimmed[7..].trim();
            if !normalized.is_empty() {
                return normalized.to_ascii_lowercase();
            }
        }
    }

    if provider_uses_mistral_api(provider)
        && let Some(normalized) = normalize_mistral_legacy_model_slug(trimmed)
    {
        return normalized;
    }

    trimmed.to_string()
}

fn provider_catalog_slug_allowed(provider: &ModelProviderInfo, slug: &str) -> bool {
    let normalized = normalize_provider_catalog_slug(provider, slug);
    let tail = normalized
        .rsplit('/')
        .next()
        .unwrap_or(normalized.as_str())
        .to_ascii_lowercase();

    if provider_uses_gemini_api(provider) {
        return tail.starts_with("gemini-");
    }

    true
}

fn openai_compatible_model_slug(
    provider: &ModelProviderInfo,
    value: OpenAiCompatibleModelObject,
) -> Option<String> {
    let OpenAiCompatibleModelObject {
        id,
        slug,
        model,
        name,
        base_model_id,
        base_model_id_snake,
        supported_generation_methods,
        supported_generation_methods_snake,
        capabilities,
        kind: _kind,
        archived,
    } = value;

    if archived == Some(true) {
        return None;
    }

    let slug = if provider_uses_gemini_api(provider) {
        base_model_id
            .or(base_model_id_snake)
            .or(id)
            .or(slug)
            .or(model)
            .or(name)
    } else {
        id.or(slug).or(model).or(name)
    }?;

    let normalized_slug = normalize_provider_catalog_slug(provider, slug.as_str());
    if normalized_slug.is_empty()
        || !provider_catalog_slug_allowed(provider, normalized_slug.as_str())
    {
        return None;
    }

    if provider_uses_mistral_api(provider)
        && let Some(capabilities) = capabilities.as_ref()
        && matches!(
            capabilities
                .completion_chat
                .or(capabilities.chat_completion),
            Some(false)
        )
    {
        return None;
    }

    if provider_uses_gemini_api(provider) {
        let supported_generation_methods = supported_generation_methods
            .into_iter()
            .chain(supported_generation_methods_snake)
            .map(|method| method.to_ascii_lowercase())
            .collect::<Vec<_>>();
        if !supported_generation_methods.is_empty()
            && !supported_generation_methods.iter().any(|method| {
                method == "generatecontent"
                    || method == "generatemessage"
                    || method == "chat"
                    || method == "chatcompletions"
            })
        {
            return None;
        }
    }

    Some(normalized_slug)
}

fn collect_openai_compatible_model_slugs(
    provider: &ModelProviderInfo,
    entries: impl IntoIterator<Item = OpenAiCompatibleModelEntry>,
) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut models = Vec::new();

    for entry in entries {
        let slug = match entry {
            OpenAiCompatibleModelEntry::String(value) => Some(value),
            OpenAiCompatibleModelEntry::Object(value) => {
                openai_compatible_model_slug(provider, value)
            }
        };
        let Some(slug) =
            slug.map(|value| normalize_provider_catalog_slug(provider, value.as_str()))
        else {
            continue;
        };
        if slug.is_empty()
            || !provider_catalog_slug_allowed(provider, slug.as_str())
            || !seen.insert(slug.clone())
        {
            continue;
        }
        models.push(slug);
    }

    models
}

fn fallback_provider_model_info(slug: &str) -> ModelInfo {
    let mut model = model_info::compatibility_model_info_from_slug(slug)
        .unwrap_or_else(|| model_info::model_info_from_slug(slug));
    model.slug = slug.to_string();
    if model.display_name.trim().is_empty() || model.display_name == model.slug {
        model.display_name = slug.to_string();
    }
    model.visibility = codex_protocol::openai_models::ModelVisibility::List;
    model.supported_in_api = true;
    if model.default_reasoning_level.is_none()
        || model.default_reasoning_level
            == Some(codex_protocol::openai_models::ReasoningEffort::None)
    {
        model.default_reasoning_level =
            Some(codex_protocol::openai_models::ReasoningEffort::Medium);
    }
    model
}

fn provider_uses_mistral_api(provider: &ModelProviderInfo) -> bool {
    provider.uses_mistral_api()
}

fn enrich_provider_catalog_model(
    provider: &ModelProviderInfo,
    requested_slug: &str,
    index: usize,
    bundled_models: &[ModelInfo],
) -> ModelInfo {
    let (presented_slug, metadata_slug) = if provider_uses_mistral_api(provider) {
        let presented_slug = requested_slug.to_string();
        let metadata_slug = model_info::normalize_provider_model_alias_slug(requested_slug)
            .or_else(|| model_info::canonicalize_provider_model_slug(requested_slug))
            .unwrap_or_else(|| presented_slug.clone());
        (presented_slug, metadata_slug)
    } else {
        let canonical_slug = model_info::normalize_provider_model_alias_slug(requested_slug)
            .or_else(|| model_info::canonicalize_provider_model_slug(requested_slug))
            .unwrap_or_else(|| requested_slug.to_string());
        (canonical_slug.clone(), canonical_slug)
    };
    let display_name_override = (presented_slug != metadata_slug).then_some(presented_slug.clone());
    let mut model = find_bundled_provider_model_metadata(metadata_slug.as_str(), bundled_models)
        .map(|candidate| ModelInfo {
            slug: presented_slug.clone(),
            ..candidate
        })
        .unwrap_or_else(|| {
            let mut model = fallback_provider_model_info(metadata_slug.as_str());
            model.slug = presented_slug.clone();
            model
        });

    model.slug = presented_slug.clone();
    if let Some(display_name) = display_name_override {
        model.display_name = display_name;
    } else if model.display_name.trim().is_empty() || model.display_name == metadata_slug {
        model.display_name = presented_slug;
    }
    model.visibility = codex_protocol::openai_models::ModelVisibility::List;
    model.supported_in_api = true;
    model.priority = i32::try_from(index).unwrap_or(i32::MAX);
    model
}

#[derive(Clone)]
struct ModelsRequestTelemetry {
    auth_mode: Option<String>,
    auth_header_attached: bool,
    auth_header_name: Option<&'static str>,
    auth_env: AuthEnvTelemetry,
}

impl RequestTelemetry for ModelsRequestTelemetry {
    fn on_request(
        &self,
        attempt: u64,
        status: Option<http::StatusCode>,
        error: Option<&TransportError>,
        duration: Duration,
    ) {
        let success = status.is_some_and(|code| code.is_success()) && error.is_none();
        let error_message = error.map(telemetry_transport_error_message);
        let response_debug = error
            .map(extract_response_debug_context)
            .unwrap_or_default();
        let status = status.map(|status| status.as_u16());
        tracing::event!(
            target: "codex_otel.log_only",
            tracing::Level::INFO,
            event.name = "codex.api_request",
            duration_ms = %duration.as_millis(),
            http.response.status_code = status,
            success = success,
            error.message = error_message.as_deref(),
            attempt = attempt,
            endpoint = MODELS_ENDPOINT,
            auth.header_attached = self.auth_header_attached,
            auth.header_name = self.auth_header_name,
            auth.env_openai_api_key_present = self.auth_env.openai_api_key_env_present,
            auth.env_codex_api_key_present = self.auth_env.codex_api_key_env_present,
            auth.env_codex_api_key_enabled = self.auth_env.codex_api_key_env_enabled,
            auth.env_provider_key_name = self.auth_env.provider_env_key_name.as_deref(),
            auth.env_provider_key_present = self.auth_env.provider_env_key_present,
            auth.env_refresh_token_url_override_present = self.auth_env.refresh_token_url_override_present,
            auth.request_id = response_debug.request_id.as_deref(),
            auth.cf_ray = response_debug.cf_ray.as_deref(),
            auth.error = response_debug.auth_error.as_deref(),
            auth.error_code = response_debug.auth_error_code.as_deref(),
            auth.mode = self.auth_mode.as_deref(),
        );
        tracing::event!(
            target: "codex_otel.trace_safe",
            tracing::Level::INFO,
            event.name = "codex.api_request",
            duration_ms = %duration.as_millis(),
            http.response.status_code = status,
            success = success,
            error.message = error_message.as_deref(),
            attempt = attempt,
            endpoint = MODELS_ENDPOINT,
            auth.header_attached = self.auth_header_attached,
            auth.header_name = self.auth_header_name,
            auth.env_openai_api_key_present = self.auth_env.openai_api_key_env_present,
            auth.env_codex_api_key_present = self.auth_env.codex_api_key_env_present,
            auth.env_codex_api_key_enabled = self.auth_env.codex_api_key_env_enabled,
            auth.env_provider_key_name = self.auth_env.provider_env_key_name.as_deref(),
            auth.env_provider_key_present = self.auth_env.provider_env_key_present,
            auth.env_refresh_token_url_override_present = self.auth_env.refresh_token_url_override_present,
            auth.request_id = response_debug.request_id.as_deref(),
            auth.cf_ray = response_debug.cf_ray.as_deref(),
            auth.error = response_debug.auth_error.as_deref(),
            auth.error_code = response_debug.auth_error_code.as_deref(),
            auth.mode = self.auth_mode.as_deref(),
        );
        emit_feedback_request_tags_with_auth_env(
            &FeedbackRequestTags {
                endpoint: MODELS_ENDPOINT,
                auth_header_attached: self.auth_header_attached,
                auth_header_name: self.auth_header_name,
                auth_mode: self.auth_mode.as_deref(),
                auth_retry_after_unauthorized: None,
                auth_recovery_mode: None,
                auth_recovery_phase: None,
                auth_connection_reused: None,
                auth_request_id: response_debug.request_id.as_deref(),
                auth_cf_ray: response_debug.cf_ray.as_deref(),
                auth_error: response_debug.auth_error.as_deref(),
                auth_error_code: response_debug.auth_error_code.as_deref(),
                auth_recovery_followup_success: None,
                auth_recovery_followup_status: None,
            },
            &self.auth_env,
        );
    }
}

/// Strategy for refreshing available models.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RefreshStrategy {
    /// Always fetch from the network, ignoring cache.
    Online,
    /// Only use cached data, never fetch from the network.
    Offline,
    /// Use cache if available and fresh, otherwise fetch from the network.
    OnlineIfUncached,
}

impl RefreshStrategy {
    const fn as_str(self) -> &'static str {
        match self {
            Self::Online => "online",
            Self::Offline => "offline",
            Self::OnlineIfUncached => "online_if_uncached",
        }
    }
}

impl fmt::Display for RefreshStrategy {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// How the manager's base catalog is sourced for the lifetime of the process.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum CatalogMode {
    /// Start from bundled `models.json` and allow cache/network refresh updates.
    Default,
    /// Use a caller-provided catalog as authoritative and do not mutate it via refresh.
    Custom,
}

/// Coordinates remote model discovery plus cached metadata on disk.
#[derive(Debug)]
pub struct ModelsManager {
    codex_home: PathBuf,
    remote_models: RwLock<Vec<ModelInfo>>,
    catalog_mode: RwLock<CatalogMode>,
    collaboration_modes_config: CollaborationModesConfig,
    base_auth_manager: Arc<AuthManager>,
    auth_manager: RwLock<Arc<AuthManager>>,
    etag: RwLock<Option<String>>,
    cache_manager: RwLock<ModelsCacheManager>,
    provider: RwLock<ModelProviderInfo>,
}

impl ModelsManager {
    /// Construct a manager scoped to the provided `AuthManager`.
    ///
    /// Uses `codex_home` to store cached model metadata and initializes with bundled catalog
    /// When `model_catalog` is provided, it becomes the authoritative remote model list and
    /// background refreshes from `/models` are disabled.
    pub fn new(
        codex_home: PathBuf,
        auth_manager: Arc<AuthManager>,
        model_catalog: Option<ModelsResponse>,
        collaboration_modes_config: CollaborationModesConfig,
    ) -> Self {
        Self::new_with_provider(
            codex_home,
            auth_manager,
            model_catalog,
            collaboration_modes_config,
            ModelProviderInfo::create_openai_provider(/*base_url*/ None),
        )
    }

    /// Construct a manager with an explicit provider used for remote model refreshes.
    pub fn new_with_provider(
        codex_home: PathBuf,
        auth_manager: Arc<AuthManager>,
        model_catalog: Option<ModelsResponse>,
        collaboration_modes_config: CollaborationModesConfig,
        provider: ModelProviderInfo,
    ) -> Self {
        let provider_auth_manager =
            required_auth_manager_for_provider(auth_manager.clone(), &provider);
        let cache_manager = ModelsCacheManager::new(
            Self::cache_path_for_provider(codex_home.as_path(), &provider),
            DEFAULT_MODEL_CACHE_TTL,
        );
        let catalog_mode = if model_catalog.is_some() {
            CatalogMode::Custom
        } else {
            CatalogMode::Default
        };
        let remote_models = Self::initial_remote_models(model_catalog, &provider);
        Self {
            codex_home,
            remote_models: RwLock::new(remote_models),
            catalog_mode: RwLock::new(catalog_mode),
            collaboration_modes_config,
            base_auth_manager: auth_manager,
            auth_manager: RwLock::new(provider_auth_manager),
            etag: RwLock::new(None),
            cache_manager: RwLock::new(cache_manager),
            provider: RwLock::new(provider),
        }
    }

    pub async fn reconfigure(
        &self,
        model_catalog: Option<ModelsResponse>,
        provider: ModelProviderInfo,
    ) {
        let provider_auth_manager =
            required_auth_manager_for_provider(self.base_auth_manager.clone(), &provider);
        let catalog_mode = if model_catalog.is_some() {
            CatalogMode::Custom
        } else {
            CatalogMode::Default
        };
        let remote_models = Self::initial_remote_models(model_catalog, &provider);
        let cache_manager = ModelsCacheManager::new(
            Self::cache_path_for_provider(self.codex_home.as_path(), &provider),
            DEFAULT_MODEL_CACHE_TTL,
        );

        *self.remote_models.write().await = remote_models;
        *self.catalog_mode.write().await = catalog_mode;
        *self.auth_manager.write().await = provider_auth_manager;
        *self.cache_manager.write().await = cache_manager;
        *self.provider.write().await = provider;
        *self.etag.write().await = None;
    }

    /// List all available models, refreshing according to the specified strategy.
    ///
    /// Returns model presets sorted by priority and filtered by auth mode and visibility.
    #[instrument(
        level = "info",
        skip(self),
        fields(refresh_strategy = %refresh_strategy)
    )]
    pub async fn list_models(&self, refresh_strategy: RefreshStrategy) -> Vec<ModelPreset> {
        if let Err(err) = self.refresh_available_models(refresh_strategy).await {
            error!("failed to refresh available models: {err}");
        }
        let remote_models = self.get_remote_models().await;
        let chatgpt_mode = matches!(
            self.auth_manager.read().await.auth_mode(),
            Some(AuthMode::Chatgpt)
        );
        self.build_available_models(remote_models, chatgpt_mode)
    }

    /// List collaboration mode presets.
    ///
    /// Returns a static set of presets seeded with the configured model.
    pub fn list_collaboration_modes(&self) -> Vec<CollaborationModeMask> {
        self.list_collaboration_modes_for_config(self.collaboration_modes_config)
    }

    pub fn list_collaboration_modes_for_config(
        &self,
        collaboration_modes_config: CollaborationModesConfig,
    ) -> Vec<CollaborationModeMask> {
        builtin_collaboration_mode_presets(collaboration_modes_config)
    }

    /// Attempt to list models without blocking, using the current cached state.
    ///
    /// Returns an error if the internal lock cannot be acquired.
    pub fn try_list_models(&self) -> Result<Vec<ModelPreset>, TryLockError> {
        let remote_models = self.try_get_remote_models()?;
        let chatgpt_mode = matches!(
            self.auth_manager.try_read()?.auth_mode(),
            Some(AuthMode::Chatgpt)
        );
        Ok(self.build_available_models(remote_models, chatgpt_mode))
    }

    // todo(aibrahim): should be visible to core only and sent on session_configured event
    /// Get the model identifier to use, refreshing according to the specified strategy.
    ///
    /// If `model` is provided, returns it directly. Otherwise selects the default based on
    /// auth mode and available models.
    #[instrument(
        level = "info",
        skip(self, model),
        fields(
            model.provided = model.is_some(),
            refresh_strategy = %refresh_strategy
        )
    )]
    pub async fn get_default_model(
        &self,
        model: &Option<String>,
        refresh_strategy: RefreshStrategy,
    ) -> String {
        if let Some(model) = model.as_ref() {
            return model.to_string();
        }
        if let Err(err) = self.refresh_available_models(refresh_strategy).await {
            error!("failed to refresh available models: {err}");
        }
        let remote_models = self.get_remote_models().await;
        let chatgpt_mode = matches!(
            self.auth_manager.read().await.auth_mode(),
            Some(AuthMode::Chatgpt)
        );
        let available = self.build_available_models(remote_models, chatgpt_mode);
        available
            .iter()
            .find(|model| model.is_default)
            .or_else(|| available.first())
            .map(|model| model.model.clone())
            .unwrap_or_default()
    }

    // todo(aibrahim): look if we can tighten it to pub(crate)
    /// Look up model metadata, applying remote overrides and config adjustments.
    #[instrument(level = "info", skip(self, config), fields(model = model))]
    pub async fn get_model_info(&self, model: &str, config: &ModelsManagerConfig) -> ModelInfo {
        let remote_models = self.get_remote_models().await;
        Self::construct_model_info_from_candidates(model, &remote_models, config)
    }

    fn find_model_by_longest_prefix(model: &str, candidates: &[ModelInfo]) -> Option<ModelInfo> {
        let mut best: Option<ModelInfo> = None;
        for candidate in candidates {
            if !model.starts_with(&candidate.slug) {
                continue;
            }
            let is_better_match = if let Some(current) = best.as_ref() {
                candidate.slug.len() > current.slug.len()
            } else {
                true
            };
            if is_better_match {
                best = Some(candidate.clone());
            }
        }
        best
    }

    /// Retry metadata lookup for a single namespaced slug like `namespace/model-name`.
    ///
    /// This only strips one leading namespace segment and only when the namespace is ASCII
    /// alphanumeric/underscore (`\\w+`) to avoid broadly matching arbitrary aliases.
    fn find_model_by_namespaced_suffix(model: &str, candidates: &[ModelInfo]) -> Option<ModelInfo> {
        let (namespace, suffix) = model.split_once('/')?;
        if suffix.contains('/') {
            return None;
        }
        if !namespace
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '_')
        {
            return None;
        }
        Self::find_model_by_longest_prefix(suffix, candidates)
    }

    /// Retry metadata lookup for common provider-specific model suffixes like
    /// `-with-tools`, `-latest`, and `-fast`.
    fn find_model_by_variant_suffix(model: &str, candidates: &[ModelInfo]) -> Option<ModelInfo> {
        const VARIANT_SUFFIXES: [&str; 4] = ["-with-tools", "-tools", "-latest", "-fast"];

        for suffix in VARIANT_SUFFIXES {
            let Some(base_slug) = model.strip_suffix(suffix) else {
                continue;
            };
            if base_slug.is_empty() {
                continue;
            }
            if let Some(found) = Self::find_model_by_longest_prefix(base_slug, candidates) {
                return Some(found);
            }
        }

        None
    }

    fn normalize_legacy_model_slug(model: &str) -> Option<String> {
        normalize_mistral_legacy_model_slug(model)
    }

    fn construct_model_info_from_candidates(
        model: &str,
        candidates: &[ModelInfo],
        config: &ModelsManagerConfig,
    ) -> ModelInfo {
        let canonical_model =
            Self::normalize_legacy_model_slug(model).unwrap_or_else(|| model.to_string());
        let is_legacy_mistral_tool_alias = canonical_model != model;

        // First use the normal longest-prefix match. If that misses, allow a narrowly scoped
        // retry for namespaced slugs like `custom/gpt-5.3-codex`.
        let remote = Self::find_model_by_longest_prefix(&canonical_model, candidates)
            .or_else(|| Self::find_model_by_namespaced_suffix(&canonical_model, candidates))
            .or_else(|| Self::find_model_by_variant_suffix(&canonical_model, candidates));
        let mut model_info = if let Some(remote) = remote {
            ModelInfo {
                slug: canonical_model.clone(),
                used_fallback_model_metadata: false,
                ..remote
            }
        } else if let Some(compatibility) =
            model_info::compatibility_model_info_from_slug(&canonical_model)
        {
            compatibility
        } else {
            model_info::model_info_from_slug(&canonical_model)
        };

        if is_legacy_mistral_tool_alias {
            model_info.supports_parallel_tool_calls = true;
            model_info.supports_search_tool = true;
        }

        model_info::with_config_overrides(model_info, config)
    }

    /// Refresh models if the provided ETag differs from the cached ETag.
    ///
    /// Uses `Online` strategy to fetch latest models when ETags differ.
    pub async fn refresh_if_new_etag(&self, etag: String) {
        let current_etag = self.get_etag().await;
        if current_etag.clone().is_some() && current_etag.as_deref() == Some(etag.as_str()) {
            let cache_manager = self.cache_manager.read().await.clone();
            if let Err(err) = cache_manager.renew_cache_ttl().await {
                error!("failed to renew cache TTL: {err}");
            }
            return;
        }
        if let Err(err) = self.refresh_available_models(RefreshStrategy::Online).await {
            error!("failed to refresh available models: {err}");
        }
    }

    /// Refresh available models according to the specified strategy.
    async fn refresh_available_models(&self, refresh_strategy: RefreshStrategy) -> CoreResult<()> {
        // don't override the custom model catalog if one was provided by the user
        if matches!(*self.catalog_mode.read().await, CatalogMode::Custom) {
            return Ok(());
        }

        if !self.can_attempt_remote_refresh().await {
            if matches!(
                refresh_strategy,
                RefreshStrategy::Offline | RefreshStrategy::OnlineIfUncached
            ) {
                self.try_load_cache().await;
            }
            return Ok(());
        }

        match refresh_strategy {
            RefreshStrategy::Offline => {
                // Only try to load from cache, never fetch
                self.try_load_cache().await;
                Ok(())
            }
            RefreshStrategy::OnlineIfUncached => {
                // Try cache first, fall back to online if unavailable
                if self.try_load_cache().await {
                    info!("models cache: using cached models for OnlineIfUncached");
                    return Ok(());
                }
                info!("models cache: cache miss, fetching remote models");
                self.fetch_and_update_models().await
            }
            RefreshStrategy::Online => {
                // Always fetch from network
                self.fetch_and_update_models().await
            }
        }
    }

    async fn fetch_and_update_models(&self) -> CoreResult<()> {
        let _timer =
            codex_otel::start_global_timer("codex.remote_models.fetch_update.duration_ms", &[]);
        let provider = self.provider.read().await.clone();
        let auth_manager = self.auth_manager.read().await.clone();
        let cache_manager = self.cache_manager.read().await.clone();
        let auth = auth_manager.auth().await;
        let auth_mode = auth.as_ref().map(CodexAuth::auth_mode);
        let api_provider = provider.to_api_provider(auth_mode)?;
        let api_auth = auth_provider_from_auth(auth.clone(), &provider)?;
        let auth_env =
            collect_auth_env_telemetry(&provider, auth_manager.codex_api_key_env_enabled());
        let transport = ReqwestTransport::new(build_reqwest_client());
        let request_telemetry: Arc<dyn RequestTelemetry> = Arc::new(ModelsRequestTelemetry {
            auth_mode: auth_mode.map(|mode| TelemetryAuthMode::from(mode).to_string()),
            auth_header_attached: api_auth.auth_header_attached(),
            auth_header_name: api_auth.auth_header_name(),
            auth_env,
        });
        let client = ModelsClient::new(transport, api_provider, api_auth)
            .with_telemetry(Some(request_telemetry));

        let client_version = crate::client_version_to_whole();
        let (body, etag) = timeout(
            MODELS_REFRESH_TIMEOUT,
            client.list_models_raw(&client_version, HeaderMap::new()),
        )
        .await
        .map_err(|_| CodexErr::Timeout)?
        .map_err(map_api_error)?;
        let models = Self::parse_remote_models_response(&provider, &body)?;

        self.apply_remote_models(models.clone()).await;
        *self.etag.write().await = etag.clone();
        cache_manager
            .persist_cache(&models, etag, client_version)
            .await;
        Ok(())
    }

    fn parse_remote_models_response(
        provider: &ModelProviderInfo,
        body: &[u8],
    ) -> CoreResult<Vec<ModelInfo>> {
        if let Ok(ModelsResponse { models }) = serde_json::from_slice::<ModelsResponse>(body) {
            return Ok(models);
        }

        let slugs = if let Ok(envelope) =
            serde_json::from_slice::<OpenAiCompatibleModelsEnvelope>(body)
        {
            let entries = if !envelope.data.is_empty() {
                envelope.data
            } else {
                envelope.models
            };
            collect_openai_compatible_model_slugs(provider, entries)
        } else if let Ok(entries) = serde_json::from_slice::<Vec<OpenAiCompatibleModelEntry>>(body)
        {
            collect_openai_compatible_model_slugs(provider, entries)
        } else {
            return Err(CodexErr::Stream(
                format!(
                    "failed to decode models response: provider /models response is not compatible with Codex or OpenAI model listings; body: {}",
                    String::from_utf8_lossy(body)
                ),
                None,
            ));
        };

        let bundled_models = crate::bundled_models_response()
            .map(|response| response.models)
            .unwrap_or_default();
        Ok(slugs
            .into_iter()
            .enumerate()
            .map(|(index, slug)| {
                enrich_provider_catalog_model(provider, slug.as_str(), index, &bundled_models)
            })
            .collect())
    }

    async fn get_etag(&self) -> Option<String> {
        self.etag.read().await.clone()
    }

    /// Replace the cached remote models and rebuild the derived presets list.
    async fn apply_remote_models(&self, models: Vec<ModelInfo>) {
        *self.remote_models.write().await = models;
    }

    fn load_remote_models_from_file() -> Result<Vec<ModelInfo>, std::io::Error> {
        Ok(crate::bundled_models_response()?.models)
    }

    fn initial_remote_models(
        model_catalog: Option<ModelsResponse>,
        provider: &ModelProviderInfo,
    ) -> Vec<ModelInfo> {
        model_catalog
            .map(|catalog| catalog.models)
            .unwrap_or_else(|| {
                if provider.is_openai() {
                    Self::load_remote_models_from_file().unwrap_or_default()
                } else {
                    Vec::new()
                }
            })
    }

    /// Attempt to satisfy the refresh from the cache when it matches the provider and TTL.
    async fn try_load_cache(&self) -> bool {
        let _timer =
            codex_otel::start_global_timer("codex.remote_models.load_cache.duration_ms", &[]);
        let client_version = crate::client_version_to_whole();
        info!(client_version, "models cache: evaluating cache eligibility");
        let cache_manager = self.cache_manager.read().await.clone();
        let cache = match cache_manager.load_fresh(&client_version).await {
            Some(cache) => cache,
            None => {
                info!("models cache: no usable cache entry");
                return false;
            }
        };
        let models = cache.models.clone();
        *self.etag.write().await = cache.etag.clone();
        self.apply_remote_models(models.clone()).await;
        info!(
            models_count = models.len(),
            etag = ?cache.etag,
            "models cache: cache entry applied"
        );
        true
    }

    /// Build picker-ready presets from the active catalog snapshot.
    fn build_available_models(
        &self,
        mut remote_models: Vec<ModelInfo>,
        chatgpt_mode: bool,
    ) -> Vec<ModelPreset> {
        remote_models.sort_by(|a, b| a.priority.cmp(&b.priority));

        let mut seen_slugs = HashSet::new();
        remote_models.retain_mut(|model| {
            if let Some(canonical_slug) = Self::normalize_legacy_model_slug(&model.slug) {
                model.slug = canonical_slug;
            }
            seen_slugs.insert(model.slug.clone())
        });

        let mut presets: Vec<ModelPreset> = remote_models.into_iter().map(Into::into).collect();
        presets = ModelPreset::filter_by_auth(presets, chatgpt_mode);

        ModelPreset::mark_default_by_picker_visibility(&mut presets);

        presets
    }

    async fn get_remote_models(&self) -> Vec<ModelInfo> {
        self.remote_models.read().await.clone()
    }

    fn try_get_remote_models(&self) -> Result<Vec<ModelInfo>, TryLockError> {
        Ok(self.remote_models.try_read()?.clone())
    }

    /// Construct a manager with a specific provider for testing.
    pub fn with_provider_for_tests(
        codex_home: PathBuf,
        auth_manager: Arc<AuthManager>,
        provider: ModelProviderInfo,
    ) -> Self {
        Self::new_with_provider(
            codex_home,
            auth_manager,
            /*model_catalog*/ None,
            CollaborationModesConfig::default(),
            provider,
        )
    }

    fn cache_path_for_provider(
        codex_home: &std::path::Path,
        provider: &ModelProviderInfo,
    ) -> PathBuf {
        codex_home.join(Self::cache_file_name_for_provider(provider))
    }

    fn cache_file_name_for_provider(provider: &ModelProviderInfo) -> String {
        let fingerprint = format!(
            "{}-{}-{}",
            provider.name,
            provider.base_url.as_deref().unwrap_or_default(),
            provider.wire_api
        );
        let mut slug = String::with_capacity(fingerprint.len());
        let mut previous_dash = false;
        for ch in fingerprint.chars() {
            let lowered = ch.to_ascii_lowercase();
            if lowered.is_ascii_alphanumeric() {
                slug.push(lowered);
                previous_dash = false;
            } else if !previous_dash {
                slug.push('-');
                previous_dash = true;
            }
        }
        let slug = slug.trim_matches('-');
        if slug.is_empty() {
            MODEL_CACHE_FILE.to_string()
        } else {
            format!("models_cache_{slug}.json")
        }
    }

    async fn can_attempt_remote_refresh(&self) -> bool {
        let provider = self.provider.read().await.clone();
        let auth_manager = self.auth_manager.read().await.clone();
        if provider.has_command_auth()
            || provider.experimental_bearer_token.is_some()
            || auth_manager.auth_mode().is_some()
        {
            return true;
        }

        match provider.api_key() {
            Ok(Some(_)) => true,
            Ok(None) => !provider.requires_openai_auth,
            Err(_) => false,
        }
    }

    /// Get model identifier without consulting remote state or cache.
    pub fn get_model_offline_for_tests(model: Option<&str>) -> String {
        if let Some(model) = model {
            return model.to_string();
        }
        let mut models = Self::load_remote_models_from_file().unwrap_or_default();
        models.sort_by(|a, b| a.priority.cmp(&b.priority));
        let presets: Vec<ModelPreset> = models.into_iter().map(Into::into).collect();
        presets
            .iter()
            .find(|preset| preset.show_in_picker)
            .or_else(|| presets.first())
            .map(|preset| preset.model.clone())
            .unwrap_or_default()
    }

    /// Build `ModelInfo` without consulting remote state or cache.
    pub fn construct_model_info_offline_for_tests(
        model: &str,
        config: &ModelsManagerConfig,
    ) -> ModelInfo {
        let candidates: &[ModelInfo] = if let Some(model_catalog) = config.model_catalog.as_ref() {
            &model_catalog.models
        } else {
            &[]
        };
        Self::construct_model_info_from_candidates(model, candidates, config)
    }
}

#[cfg(test)]
#[path = "manager_tests.rs"]
mod tests;
