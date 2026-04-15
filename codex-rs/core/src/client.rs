//! Session- and turn-scoped helpers for talking to model provider APIs.
//!
//! `ModelClient` is intended to live for the lifetime of a Codex session and holds the stable
//! configuration and state needed to talk to a provider (auth, provider selection, conversation id,
//! and transport fallback state).
//!
//! Per-turn settings (model selection, reasoning controls, telemetry context, and turn metadata)
//! are passed explicitly to streaming and unary methods so that the turn lifetime is visible at the
//! call site.
//!
//! A [`ModelClientSession`] is created per turn and is used to stream one or more Responses API
//! requests during that turn. It caches a Responses WebSocket connection (opened lazily) and stores
//! per-turn state such as the `x-codex-turn-state` token used for sticky routing.
//!
//! WebSocket prewarm is a v2-only `response.create` with `generate=false`; it waits for completion
//! so the next request can reuse the same connection and `previous_response_id`.
//!
//! Turn execution performs prewarm as a best-effort step before the first stream request so the
//! subsequent request can reuse the same connection.
//!
//! ## Retry-Budget Tradeoff
//!
//! WebSocket prewarm is treated as the first websocket connection attempt for a turn. If it
//! fails, normal stream retry/fallback logic handles recovery on the same turn.

use std::borrow::Cow;
use std::collections::HashMap;
use std::collections::HashSet;
use std::sync::Arc;
use std::sync::Mutex as StdMutex;
use std::sync::OnceLock;
use std::sync::RwLock as StdRwLock;
use std::sync::atomic::AtomicBool;
use std::sync::atomic::AtomicU64;
use std::sync::atomic::Ordering;

use codex_api::CompactClient as ApiCompactClient;
use codex_api::CompactionInput as ApiCompactionInput;
use codex_api::MemoriesClient as ApiMemoriesClient;
use codex_api::MemorySummarizeInput as ApiMemorySummarizeInput;
use codex_api::MemorySummarizeOutput as ApiMemorySummarizeOutput;
use codex_api::RawMemory as ApiRawMemory;
use codex_api::RequestTelemetry;
use codex_api::ReqwestTransport;
use codex_api::ResponseCreateWsRequest;
use codex_api::ResponsesApiRequest;
use codex_api::ResponsesClient as ApiResponsesClient;
use codex_api::ResponsesOptions as ApiResponsesOptions;
use codex_api::ResponsesWebsocketClient as ApiWebSocketResponsesClient;
use codex_api::ResponsesWebsocketConnection as ApiWebSocketConnection;
use codex_api::SseTelemetry;
use codex_api::TransportError;
use codex_api::WebsocketTelemetry;
use codex_api::build_conversation_headers;
use codex_api::common::Reasoning;
use codex_api::common::ResponsesWsRequest;
use codex_api::create_text_param_for_request;
use codex_api::error::ApiError;
use codex_api::requests::responses::Compression;
use codex_api::response_create_client_metadata;
use codex_app_server_protocol::AuthMode;
use codex_login::AuthManager;
use codex_login::CodexAuth;
use codex_login::RefreshTokenError;
use codex_login::UnauthorizedRecovery;
use codex_login::default_client::build_reqwest_client;
use codex_otel::SessionTelemetry;
use codex_otel::current_span_w3c_trace_context;

use codex_protocol::ThreadId;
use codex_protocol::config_types::ReasoningSummary as ReasoningSummaryConfig;
use codex_protocol::config_types::ServiceTier;
use codex_protocol::config_types::Verbosity as VerbosityConfig;
use codex_protocol::models::ContentItem;
use codex_protocol::models::FunctionCallOutputPayload;
use codex_protocol::models::LocalShellAction;
use codex_protocol::models::ResponseItem;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::openai_models::ReasoningEffort as ReasoningEffortConfig;
use codex_protocol::protocol::SessionSource;
use codex_protocol::protocol::SubAgentSource;
use codex_protocol::protocol::TokenUsage;
use codex_protocol::protocol::W3cTraceContext;
use codex_tools::ToolSpec;
use codex_tools::create_tools_json_for_responses_api;
use codex_utils_cache::sha1_digest;
use codex_utils_stream_parser::strip_hidden_reasoning_tags;
use eventsource_stream::Event;
use eventsource_stream::EventStreamError;
use futures::StreamExt;
use http::HeaderMap as ApiHeaderMap;
use http::HeaderValue;
use http::Method;
use http::StatusCode as HttpStatusCode;
use reqwest::StatusCode;
use serde::Deserialize;
use serde::Serialize;
use serde_json::Value as JsonValue;
use serde_json::json;
use std::time::Duration;
use std::time::Instant;
use tokio::sync::mpsc;
use tokio::sync::oneshot;
use tokio::sync::oneshot::error::TryRecvError;
use tokio_tungstenite::tungstenite::Error;
use tokio_tungstenite::tungstenite::Message;
use tracing::instrument;
use tracing::trace;
use tracing::warn;

use crate::client_common::Prompt;
use crate::client_common::ResponseEvent;
use crate::client_common::ResponseStream;
use crate::context_manager::ContextManager;
use crate::context_manager::is_user_turn_boundary;
use crate::contextual_user_message::ENVIRONMENT_CONTEXT_FRAGMENT;
use crate::flags::CODEX_RS_SSE_FIXTURE;
use crate::util::emit_feedback_auth_recovery_tags;
use codex_api::api_bridge::CoreAuthProvider;
use codex_api::api_bridge::map_api_error;
use codex_client::HttpTransport;
use codex_client::run_with_retry;
use codex_client::sse_stream;
use codex_feedback::FeedbackRequestTags;
use codex_feedback::emit_feedback_request_tags_with_auth_env;
use codex_instructions::AGENTS_MD_FRAGMENT;
use codex_instructions::SKILL_FRAGMENT;
use codex_login::api_bridge::auth_provider_from_auth;
use codex_login::auth_env_telemetry::AuthEnvTelemetry;
use codex_login::auth_env_telemetry::collect_auth_env_telemetry;
use codex_login::provider_auth::auth_manager_for_provider;
#[cfg(test)]
use codex_model_provider_info::DEFAULT_WEBSOCKET_CONNECT_TIMEOUT_MS;
use codex_model_provider_info::ModelProviderInfo;
use codex_model_provider_info::WireApi;
use codex_models_manager::model_info::normalize_provider_model_for_family;
use codex_protocol::error::CodexErr;
use codex_protocol::error::Result;
use codex_response_debug_context::extract_response_debug_context;
use codex_response_debug_context::extract_response_debug_context_from_api_error;
use codex_response_debug_context::telemetry_api_error_message;
use codex_response_debug_context::telemetry_transport_error_message;

pub const OPENAI_BETA_HEADER: &str = "OpenAI-Beta";
pub const X_CODEX_TURN_STATE_HEADER: &str = "x-codex-turn-state";
pub const X_CODEX_TURN_METADATA_HEADER: &str = "x-codex-turn-metadata";
pub const X_CODEX_PARENT_THREAD_ID_HEADER: &str = "x-codex-parent-thread-id";
pub const X_CODEX_WINDOW_ID_HEADER: &str = "x-codex-window-id";
pub const X_OPENAI_SUBAGENT_HEADER: &str = "x-openai-subagent";
pub const X_RESPONSESAPI_INCLUDE_TIMING_METRICS_HEADER: &str =
    "x-responsesapi-include-timing-metrics";
const RESPONSES_WEBSOCKETS_V2_BETA_HEADER_VALUE: &str = "responses_websockets=2026-02-06";
const RESPONSES_ENDPOINT: &str = "/responses";
const RESPONSES_COMPACT_ENDPOINT: &str = "/responses/compact";
const MEMORIES_SUMMARIZE_ENDPOINT: &str = "/memories/trace_summarize";
const GOOGLE_THOUGHT_SIGNATURE_PREFIX: &str = "google-thought-signature:";
const OPENROUTER_API_HOST: &str = "openrouter.ai";
#[cfg(test)]
pub(crate) const WEBSOCKET_CONNECT_TIMEOUT: Duration =
    Duration::from_millis(DEFAULT_WEBSOCKET_CONNECT_TIMEOUT_MS);

fn provider_uses_mistral_api(provider: &ModelProviderInfo) -> bool {
    provider.uses_mistral_api()
}

fn provider_uses_gemini_api(provider: &ModelProviderInfo) -> bool {
    provider.uses_gemini_api()
}

fn provider_uses_openrouter_api(provider: &ModelProviderInfo) -> bool {
    provider.name.eq_ignore_ascii_case("openrouter")
        || provider
            .base_url
            .as_deref()
            .is_some_and(|base_url| base_url.to_ascii_lowercase().contains(OPENROUTER_API_HOST))
}

fn effective_wire_api(provider: &ModelProviderInfo) -> WireApi {
    provider.effective_wire_api()
}

fn normalize_request_model_for_provider<'a>(
    provider: &ModelProviderInfo,
    model: &'a str,
) -> Cow<'a, str> {
    let family = if provider_uses_gemini_api(provider) {
        "gemini"
    } else if provider_uses_mistral_api(provider) {
        "mistral"
    } else {
        return Cow::Borrowed(model);
    };

    let normalized = normalize_provider_model_for_family(family, model);
    if normalized.eq(model) {
        Cow::Borrowed(model)
    } else {
        Cow::Owned(normalized)
    }
}

fn chat_completions_instructions_role(_provider: &ModelProviderInfo) -> &'static str {
    "system"
}

fn normalize_chat_completions_role<'a>(provider: &ModelProviderInfo, role: &'a str) -> &'a str {
    if role.eq_ignore_ascii_case("developer") || role.eq_ignore_ascii_case("system") {
        chat_completions_instructions_role(provider)
    } else {
        role
    }
}

fn mistral_tool_call_id_is_valid(call_id: &str) -> bool {
    call_id.len() == 9 && call_id.bytes().all(|byte| byte.is_ascii_alphanumeric())
}

fn normalize_chat_completions_tool_call_id(provider: &ModelProviderInfo, call_id: &str) -> String {
    if !provider_uses_mistral_api(provider) || mistral_tool_call_id_is_valid(call_id) {
        return call_id.to_string();
    }

    let digest = sha1_digest(call_id.as_bytes());
    let mut normalized = String::with_capacity(9);
    for byte in digest {
        use std::fmt::Write;

        let _ = write!(&mut normalized, "{byte:02x}");
        if normalized.len() >= 9 {
            normalized.truncate(9);
            break;
        }
    }
    normalized
}

fn encode_google_thought_signature(thought_signature: &str) -> String {
    format!("{GOOGLE_THOUGHT_SIGNATURE_PREFIX}{thought_signature}")
}

fn decode_google_thought_signature(id: Option<&String>) -> Option<&str> {
    id.and_then(|value| value.strip_prefix(GOOGLE_THOUGHT_SIGNATURE_PREFIX))
}

fn parse_decimal_after_marker(text: &str, marker: &str) -> Option<u32> {
    let start = text.find(marker)?.checked_add(marker.len())?;
    let digits = text[start..]
        .chars()
        .skip_while(|ch| !ch.is_ascii_digit())
        .take_while(|ch| ch.is_ascii_digit())
        .collect::<String>();
    digits.parse().ok()
}

fn parse_openrouter_affordable_max_tokens(body: Option<&str>) -> Option<u32> {
    let body = body?.trim();
    if body.is_empty() {
        return None;
    }

    let lowercase = body.to_ascii_lowercase();
    if !lowercase.contains("fewer max_tokens") || !lowercase.contains("can only afford") {
        return None;
    }

    parse_decimal_after_marker(&lowercase, "can only afford ").filter(|max_tokens| *max_tokens > 0)
}

fn parse_openrouter_affordable_prompt_tokens(body: Option<&str>) -> Option<u32> {
    let body = body?.trim();
    if body.is_empty() {
        return None;
    }

    let lowercase = body.to_ascii_lowercase();
    if !lowercase.contains("prompt tokens limit exceeded") {
        return None;
    }

    parse_decimal_after_marker(&lowercase, ">").filter(|prompt_tokens| *prompt_tokens > 0)
}

/// Session-scoped state shared by all [`ModelClient`] clones.
///
/// This is intentionally kept minimal so `ModelClient` does not need to hold a full `Config`. Most
/// configuration is per turn and is passed explicitly to streaming/unary methods.
#[derive(Debug)]
struct ModelClientState {
    conversation_id: ThreadId,
    window_generation: AtomicU64,
    runtime_config: StdRwLock<ModelClientRuntimeConfig>,
    disable_websockets: AtomicBool,
    cached_websocket_session: StdMutex<WebsocketSession>,
    chat_completions_max_tokens_caps: StdMutex<HashMap<String, u32>>,
    chat_completions_prompt_tokens_caps: StdMutex<HashMap<String, u32>>,
}

#[derive(Debug, Clone)]
struct ModelClientRuntimeConfig {
    auth_manager: Option<Arc<AuthManager>>,
    provider: ModelProviderInfo,
    auth_env_telemetry: AuthEnvTelemetry,
    session_source: SessionSource,
    model_verbosity: Option<VerbosityConfig>,
    enable_request_compression: bool,
    include_timing_metrics: bool,
    beta_features_header: Option<String>,
}

/// Resolved API client setup for a single request attempt.
///
/// Keeping this as a single bundle ensures prewarm and normal request paths
/// share the same auth/provider setup flow.
struct CurrentClientSetup {
    auth: Option<CodexAuth>,
    api_provider: codex_api::Provider,
    api_auth: CoreAuthProvider,
}

#[derive(Clone, Copy)]
struct RequestRouteTelemetry {
    endpoint: &'static str,
}

impl RequestRouteTelemetry {
    fn for_endpoint(endpoint: &'static str) -> Self {
        Self { endpoint }
    }
}

/// A session-scoped client for model-provider API calls.
///
/// This holds configuration and state that should be shared across turns within a Codex session
/// (auth, provider selection, conversation id, and transport fallback state).
///
/// WebSocket fallback is session-scoped: once a turn activates the HTTP fallback, subsequent turns
/// will also use HTTP for the remainder of the session.
///
/// Turn-scoped settings (model selection, reasoning controls, telemetry context, and turn
/// metadata) are passed explicitly to the relevant methods to keep turn lifetime visible at the
/// call site.
#[derive(Debug, Clone)]
pub struct ModelClient {
    state: Arc<ModelClientState>,
}

/// A turn-scoped streaming session created from a [`ModelClient`].
///
/// The session establishes a Responses WebSocket connection lazily and reuses it across multiple
/// requests within the turn. It also caches per-turn state:
///
/// - The last full request, so subsequent calls can reuse incremental websocket request payloads
///   only when the current request is an incremental extension of the previous one.
/// - The `x-codex-turn-state` sticky-routing token, which must be replayed for all requests within
///   the same turn.
///
/// Create a fresh `ModelClientSession` for each Codex turn. Reusing it across turns would replay
/// the previous turn's sticky-routing token into the next turn, which violates the client/server
/// contract and can cause routing bugs.
pub struct ModelClientSession {
    client: ModelClient,
    websocket_session: WebsocketSession,
    /// Turn state for sticky routing.
    ///
    /// This is an `OnceLock` that stores the turn state value received from the server
    /// on turn start via the `x-codex-turn-state` response header. Once set, this value
    /// should be sent back to the server in the `x-codex-turn-state` request header for
    /// all subsequent requests within the same turn to maintain sticky routing.
    ///
    /// This is a contract between the client and server: we receive it at turn start,
    /// keep sending it unchanged between turn requests (e.g., for retries, incremental
    /// appends, or continuation requests), and must not send it between different turns.
    turn_state: Arc<OnceLock<String>>,
}

#[derive(Debug, Clone)]
struct LastResponse {
    response_id: String,
    items_added: Vec<ResponseItem>,
}

#[derive(Debug, Default)]
struct WebsocketSession {
    connection: Option<ApiWebSocketConnection>,
    last_request: Option<ResponsesApiRequest>,
    last_response_rx: Option<oneshot::Receiver<LastResponse>>,
    connection_reused: StdMutex<bool>,
}

impl WebsocketSession {
    fn set_connection_reused(&self, connection_reused: bool) {
        *self
            .connection_reused
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = connection_reused;
    }

    fn connection_reused(&self) -> bool {
        *self
            .connection_reused
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
    }
}

enum WebsocketStreamOutcome {
    Stream(ResponseStream),
    FallbackToHttp,
}

impl ModelClient {
    #[allow(clippy::too_many_arguments)]
    /// Creates a new session-scoped `ModelClient`.
    ///
    /// All arguments are expected to be stable for the lifetime of a Codex session. Per-turn values
    /// are passed to [`ModelClientSession::stream`] (and other turn-scoped methods) explicitly.
    pub fn new(
        auth_manager: Option<Arc<AuthManager>>,
        conversation_id: ThreadId,
        provider: ModelProviderInfo,
        session_source: SessionSource,
        model_verbosity: Option<VerbosityConfig>,
        enable_request_compression: bool,
        include_timing_metrics: bool,
        beta_features_header: Option<String>,
    ) -> Self {
        let auth_manager = auth_manager_for_provider(auth_manager, &provider);
        let codex_api_key_env_enabled = auth_manager
            .as_ref()
            .is_some_and(|manager| manager.codex_api_key_env_enabled());
        let auth_env_telemetry = collect_auth_env_telemetry(&provider, codex_api_key_env_enabled);
        Self {
            state: Arc::new(ModelClientState {
                conversation_id,
                window_generation: AtomicU64::new(0),
                runtime_config: StdRwLock::new(ModelClientRuntimeConfig {
                    auth_manager,
                    provider,
                    auth_env_telemetry,
                    session_source,
                    model_verbosity,
                    enable_request_compression,
                    include_timing_metrics,
                    beta_features_header,
                }),
                disable_websockets: AtomicBool::new(false),
                cached_websocket_session: StdMutex::new(WebsocketSession::default()),
                chat_completions_max_tokens_caps: StdMutex::new(HashMap::new()),
                chat_completions_prompt_tokens_caps: StdMutex::new(HashMap::new()),
            }),
        }
    }

    fn runtime_config(&self) -> ModelClientRuntimeConfig {
        self.state
            .runtime_config
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .clone()
    }

    /// Creates a fresh turn-scoped streaming session.
    ///
    /// This constructor does not perform network I/O itself; the session opens a websocket lazily
    /// when the first stream request is issued.
    pub fn new_session(&self) -> ModelClientSession {
        ModelClientSession {
            client: self.clone(),
            websocket_session: self.take_cached_websocket_session(),
            turn_state: Arc::new(OnceLock::new()),
        }
    }

    pub(crate) fn auth_manager(&self) -> Option<Arc<AuthManager>> {
        self.runtime_config().auth_manager
    }

    #[allow(clippy::too_many_arguments)]
    pub(crate) fn reconfigure(
        &self,
        auth_manager: Option<Arc<AuthManager>>,
        provider: ModelProviderInfo,
        session_source: SessionSource,
        model_verbosity: Option<VerbosityConfig>,
        enable_request_compression: bool,
        include_timing_metrics: bool,
        beta_features_header: Option<String>,
    ) {
        let auth_manager = auth_manager_for_provider(auth_manager, &provider);
        let codex_api_key_env_enabled = auth_manager
            .as_ref()
            .is_some_and(|manager| manager.codex_api_key_env_enabled());
        let auth_env_telemetry = collect_auth_env_telemetry(&provider, codex_api_key_env_enabled);
        *self
            .state
            .runtime_config
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = ModelClientRuntimeConfig {
            auth_manager,
            provider,
            auth_env_telemetry,
            session_source,
            model_verbosity,
            enable_request_compression,
            include_timing_metrics,
            beta_features_header,
        };
        self.state
            .disable_websockets
            .store(false, Ordering::Relaxed);
        self.store_cached_websocket_session(WebsocketSession::default());
    }

    pub(crate) fn set_window_generation(&self, window_generation: u64) {
        self.state
            .window_generation
            .store(window_generation, Ordering::Relaxed);
        self.store_cached_websocket_session(WebsocketSession::default());
    }

    pub(crate) fn advance_window_generation(&self) {
        self.state.window_generation.fetch_add(1, Ordering::Relaxed);
        self.store_cached_websocket_session(WebsocketSession::default());
    }

    fn current_window_id(&self) -> String {
        let conversation_id = self.state.conversation_id;
        let window_generation = self.state.window_generation.load(Ordering::Relaxed);
        format!("{conversation_id}:{window_generation}")
    }

    fn take_cached_websocket_session(&self) -> WebsocketSession {
        let mut cached_websocket_session = self
            .state
            .cached_websocket_session
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        std::mem::take(&mut *cached_websocket_session)
    }

    fn store_cached_websocket_session(&self, websocket_session: WebsocketSession) {
        *self
            .state
            .cached_websocket_session
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = websocket_session;
    }

    fn chat_completions_max_tokens_cache_key(
        provider: &ModelProviderInfo,
        model: &str,
    ) -> Option<String> {
        if !provider_uses_openrouter_api(provider) {
            return None;
        }

        let base_url = provider.base_url.as_deref()?.trim_end_matches('/');
        if base_url.is_empty() {
            return None;
        }

        Some(format!("{base_url}::{}", model.trim().to_ascii_lowercase()))
    }

    fn cached_chat_completions_max_tokens_cap(
        &self,
        provider: &ModelProviderInfo,
        model: &str,
    ) -> Option<u32> {
        let key = Self::chat_completions_max_tokens_cache_key(provider, model)?;
        self.state
            .chat_completions_max_tokens_caps
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .get(&key)
            .copied()
    }

    fn remember_chat_completions_max_tokens_cap(
        &self,
        provider: &ModelProviderInfo,
        model: &str,
        max_tokens: u32,
    ) -> bool {
        if max_tokens == 0 {
            return false;
        }

        let Some(key) = Self::chat_completions_max_tokens_cache_key(provider, model) else {
            return false;
        };

        let mut caps = self
            .state
            .chat_completions_max_tokens_caps
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        match caps.get_mut(&key) {
            Some(existing) if *existing <= max_tokens => false,
            Some(existing) => {
                *existing = max_tokens;
                true
            }
            None => {
                caps.insert(key, max_tokens);
                true
            }
        }
    }

    fn cached_chat_completions_prompt_tokens_cap(
        &self,
        provider: &ModelProviderInfo,
        model: &str,
    ) -> Option<u32> {
        let key = Self::chat_completions_max_tokens_cache_key(provider, model)?;
        self.state
            .chat_completions_prompt_tokens_caps
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .get(&key)
            .copied()
    }

    fn remember_chat_completions_prompt_tokens_cap(
        &self,
        provider: &ModelProviderInfo,
        model: &str,
        prompt_tokens: u32,
    ) -> bool {
        if prompt_tokens == 0 {
            return false;
        }

        let Some(key) = Self::chat_completions_max_tokens_cache_key(provider, model) else {
            return false;
        };

        let mut caps = self
            .state
            .chat_completions_prompt_tokens_caps
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        match caps.get_mut(&key) {
            Some(existing) if *existing <= prompt_tokens => false,
            Some(existing) => {
                *existing = prompt_tokens;
                true
            }
            None => {
                caps.insert(key, prompt_tokens);
                true
            }
        }
    }

    pub(crate) fn force_http_fallback(
        &self,
        session_telemetry: &SessionTelemetry,
        _model_info: &ModelInfo,
    ) -> bool {
        let websocket_enabled = self.responses_websocket_enabled();
        let activated =
            websocket_enabled && !self.state.disable_websockets.swap(true, Ordering::Relaxed);
        if activated {
            warn!("falling back to HTTP");
            session_telemetry.counter(
                "codex.transport.fallback_to_http",
                /*inc*/ 1,
                &[("from_wire_api", "responses_websocket")],
            );
        }

        self.store_cached_websocket_session(WebsocketSession::default());
        activated
    }

    /// Compacts the current conversation history using the Compact endpoint.
    ///
    /// This is a unary call (no streaming) that returns a new list of
    /// `ResponseItem`s representing the compacted transcript.
    ///
    /// The model selection and telemetry context are passed explicitly to keep `ModelClient`
    /// session-scoped.
    pub async fn compact_conversation_history(
        &self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        session_telemetry: &SessionTelemetry,
    ) -> Result<Vec<ResponseItem>> {
        if prompt.input.is_empty() {
            return Ok(Vec::new());
        }
        let runtime_config = self.runtime_config();
        if matches!(
            effective_wire_api(&runtime_config.provider),
            WireApi::ChatCompletions
        ) {
            warn!(
                "remote compaction is unavailable for chat-completions providers; preserving existing history"
            );
            return Ok(prompt.input.clone());
        }
        let client_setup = self.current_client_setup().await?;
        let transport = ReqwestTransport::new(build_reqwest_client());
        let request_telemetry = Self::build_request_telemetry(
            session_telemetry,
            AuthRequestTelemetryContext::new(
                client_setup.auth.as_ref().map(CodexAuth::auth_mode),
                &client_setup.api_auth,
                PendingUnauthorizedRetry::default(),
            ),
            RequestRouteTelemetry::for_endpoint(RESPONSES_COMPACT_ENDPOINT),
            runtime_config.auth_env_telemetry,
        );
        let client =
            ApiCompactClient::new(transport, client_setup.api_provider, client_setup.api_auth)
                .with_telemetry(Some(request_telemetry));

        let instructions = prompt.base_instructions.text.clone();
        let input = prompt.get_formatted_input();
        let tools = create_tools_json_for_responses_api(&prompt.tools)?;
        let reasoning =
            Self::build_reasoning(&runtime_config.provider, model_info, effort, summary);
        let verbosity = if model_info.support_verbosity {
            runtime_config
                .model_verbosity
                .or(model_info.default_verbosity)
        } else {
            if runtime_config.model_verbosity.is_some() {
                warn!(
                    "model_verbosity is set but ignored as the model does not support verbosity: {}",
                    model_info.slug
                );
            }
            None
        };
        let text = create_text_param_for_request(verbosity, &prompt.output_schema);
        let request_model =
            normalize_request_model_for_provider(&runtime_config.provider, &model_info.slug);
        let payload = ApiCompactionInput {
            model: request_model.as_ref(),
            input: &input,
            instructions: &instructions,
            tools,
            parallel_tool_calls: prompt.parallel_tool_calls,
            reasoning,
            text,
        };

        let mut extra_headers = self.build_responses_identity_headers();
        extra_headers.extend(build_conversation_headers(Some(
            self.state.conversation_id.to_string(),
        )));
        client
            .compact_input(&payload, extra_headers)
            .await
            .map_err(map_api_error)
    }

    /// Builds memory summaries for each provided normalized raw memory.
    ///
    /// This is a unary call (no streaming) to `/v1/memories/trace_summarize`.
    ///
    /// The model selection, reasoning effort, and telemetry context are passed explicitly to keep
    /// `ModelClient` session-scoped.
    pub async fn summarize_memories(
        &self,
        raw_memories: Vec<ApiRawMemory>,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
        session_telemetry: &SessionTelemetry,
    ) -> Result<Vec<ApiMemorySummarizeOutput>> {
        if raw_memories.is_empty() {
            return Ok(Vec::new());
        }
        let runtime_config = self.runtime_config();
        if matches!(
            effective_wire_api(&runtime_config.provider),
            WireApi::ChatCompletions
        ) {
            warn!(
                "memory summarize endpoint is unavailable for chat-completions providers; using a local fallback summary"
            );
            return Ok(raw_memories
                .into_iter()
                .map(fallback_memory_summarize_output)
                .collect());
        }

        let client_setup = self.current_client_setup().await?;
        let transport = ReqwestTransport::new(build_reqwest_client());
        let request_telemetry = Self::build_request_telemetry(
            session_telemetry,
            AuthRequestTelemetryContext::new(
                client_setup.auth.as_ref().map(CodexAuth::auth_mode),
                &client_setup.api_auth,
                PendingUnauthorizedRetry::default(),
            ),
            RequestRouteTelemetry::for_endpoint(MEMORIES_SUMMARIZE_ENDPOINT),
            runtime_config.auth_env_telemetry,
        );
        let client =
            ApiMemoriesClient::new(transport, client_setup.api_provider, client_setup.api_auth)
                .with_telemetry(Some(request_telemetry));

        let request_model =
            normalize_request_model_for_provider(&runtime_config.provider, &model_info.slug);
        let payload = ApiMemorySummarizeInput {
            model: request_model.into_owned(),
            raw_memories,
            reasoning: effort.map(|effort| Reasoning {
                effort: Some(effort),
                summary: None,
            }),
        };

        client
            .summarize_input(&payload, self.build_subagent_headers())
            .await
            .map_err(map_api_error)
    }

    fn build_subagent_headers(&self) -> ApiHeaderMap {
        let runtime_config = self.runtime_config();
        let mut extra_headers = ApiHeaderMap::new();
        if let Some(subagent) = subagent_header_value(&runtime_config.session_source)
            && let Ok(val) = HeaderValue::from_str(&subagent)
        {
            extra_headers.insert(X_OPENAI_SUBAGENT_HEADER, val);
        }
        extra_headers
    }

    fn build_responses_identity_headers(&self) -> ApiHeaderMap {
        let runtime_config = self.runtime_config();
        let mut extra_headers = self.build_subagent_headers();
        if let Some(parent_thread_id) =
            parent_thread_id_header_value(&runtime_config.session_source)
            && let Ok(val) = HeaderValue::from_str(&parent_thread_id)
        {
            extra_headers.insert(X_CODEX_PARENT_THREAD_ID_HEADER, val);
        }
        if let Ok(val) = HeaderValue::from_str(&self.current_window_id()) {
            extra_headers.insert(X_CODEX_WINDOW_ID_HEADER, val);
        }
        extra_headers
    }

    fn build_ws_client_metadata(
        &self,
        turn_metadata_header: Option<&str>,
    ) -> HashMap<String, String> {
        let runtime_config = self.runtime_config();
        let mut client_metadata = HashMap::new();
        client_metadata.insert(
            X_CODEX_WINDOW_ID_HEADER.to_string(),
            self.current_window_id(),
        );
        if let Some(subagent) = subagent_header_value(&runtime_config.session_source) {
            client_metadata.insert(X_OPENAI_SUBAGENT_HEADER.to_string(), subagent);
        }
        if let Some(parent_thread_id) =
            parent_thread_id_header_value(&runtime_config.session_source)
        {
            client_metadata.insert(
                X_CODEX_PARENT_THREAD_ID_HEADER.to_string(),
                parent_thread_id,
            );
        }
        if let Some(turn_metadata_header) = parse_turn_metadata_header(turn_metadata_header)
            && let Ok(turn_metadata) = turn_metadata_header.to_str()
        {
            client_metadata.insert(
                X_CODEX_TURN_METADATA_HEADER.to_string(),
                turn_metadata.to_string(),
            );
        }
        client_metadata
    }

    /// Builds request telemetry for unary API calls (e.g., Compact endpoint).
    fn build_request_telemetry(
        session_telemetry: &SessionTelemetry,
        auth_context: AuthRequestTelemetryContext,
        request_route_telemetry: RequestRouteTelemetry,
        auth_env_telemetry: AuthEnvTelemetry,
    ) -> Arc<dyn RequestTelemetry> {
        let telemetry = Arc::new(ApiTelemetry::new(
            session_telemetry.clone(),
            auth_context,
            request_route_telemetry,
            auth_env_telemetry,
        ));
        let request_telemetry: Arc<dyn RequestTelemetry> = telemetry;
        request_telemetry
    }

    fn build_reasoning(
        provider: &ModelProviderInfo,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
    ) -> Option<Reasoning> {
        if provider.supports_reasoning_controls() || model_info.supports_reasoning_summaries {
            Some(Reasoning {
                effort: effort.or(model_info.default_reasoning_level),
                summary: if model_info.supports_reasoning_summaries
                    && summary != ReasoningSummaryConfig::None
                {
                    Some(summary)
                } else {
                    None
                },
            })
        } else {
            None
        }
    }

    fn build_chat_completions_reasoning_effort(
        provider: &ModelProviderInfo,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
    ) -> Option<ReasoningEffortConfig> {
        if !provider.model_supports_chat_completions_reasoning_effort(model_info.slug.as_str()) {
            return None;
        }

        let requested_effort = effort.or(model_info.default_reasoning_level);
        let supported_efforts = model_info
            .supported_reasoning_levels
            .iter()
            .map(|preset| preset.effort)
            .collect::<Vec<_>>();
        if supported_efforts.is_empty() {
            return None;
        }
        let default_supported = model_info
            .default_reasoning_level
            .filter(|effort| supported_efforts.contains(effort));
        let effort = requested_effort
            .filter(|effort| supported_efforts.contains(effort))
            .or(default_supported)
            .or_else(|| {
                supported_efforts
                    .get(supported_efforts.len().saturating_sub(1) / 2)
                    .copied()
            });

        if provider_uses_mistral_api(provider) {
            return match effort {
                Some(ReasoningEffortConfig::High | ReasoningEffortConfig::XHigh) => {
                    Some(ReasoningEffortConfig::High)
                }
                Some(
                    ReasoningEffortConfig::None
                    | ReasoningEffortConfig::Minimal
                    | ReasoningEffortConfig::Low
                    | ReasoningEffortConfig::Medium,
                ) => Some(ReasoningEffortConfig::None),
                None => None,
            };
        }

        if provider.supports_chat_completions_reasoning_effort() {
            effort
        } else {
            None
        }
    }

    /// Returns whether the Responses-over-WebSocket transport is active for this session.
    ///
    /// WebSocket use is controlled by provider capability and session-scoped fallback state.
    pub fn responses_websocket_enabled(&self) -> bool {
        let runtime_config = self.runtime_config();
        if matches!(
            effective_wire_api(&runtime_config.provider),
            WireApi::ChatCompletions
        ) || !runtime_config.provider.supports_websockets
            || self.state.disable_websockets.load(Ordering::Relaxed)
            || (*CODEX_RS_SSE_FIXTURE).is_some()
        {
            return false;
        }

        true
    }

    /// Returns auth + provider configuration resolved from the current session auth state.
    ///
    /// This centralizes setup used by both prewarm and normal request paths so they stay in
    /// lockstep when auth/provider resolution changes.
    async fn current_client_setup(&self) -> Result<CurrentClientSetup> {
        let runtime_config = self.runtime_config();
        let auth = match runtime_config.auth_manager.as_ref() {
            Some(manager) => manager.auth().await,
            None => None,
        };
        let provider = runtime_config.provider;
        let api_provider = provider.to_api_provider(auth.as_ref().map(CodexAuth::auth_mode))?;
        let api_auth = auth_provider_from_auth(auth.clone(), &provider)?;
        Ok(CurrentClientSetup {
            auth,
            api_provider,
            api_auth,
        })
    }

    /// Opens a websocket connection using the same header and telemetry wiring as normal turns.
    ///
    /// Both startup prewarm and in-turn `needs_new` reconnects call this path so handshake
    /// behavior remains consistent across both flows.
    #[allow(clippy::too_many_arguments)]
    async fn connect_websocket(
        &self,
        session_telemetry: &SessionTelemetry,
        api_provider: codex_api::Provider,
        api_auth: CoreAuthProvider,
        turn_state: Option<Arc<OnceLock<String>>>,
        turn_metadata_header: Option<&str>,
        auth_context: AuthRequestTelemetryContext,
        request_route_telemetry: RequestRouteTelemetry,
    ) -> std::result::Result<ApiWebSocketConnection, ApiError> {
        let headers = self.build_websocket_headers(turn_state.as_ref(), turn_metadata_header);
        let websocket_telemetry = ModelClientSession::build_websocket_telemetry(
            session_telemetry,
            auth_context,
            request_route_telemetry,
            self.runtime_config().auth_env_telemetry,
        );
        let websocket_connect_timeout = self.runtime_config().provider.websocket_connect_timeout();
        let start = Instant::now();
        let result = match tokio::time::timeout(
            websocket_connect_timeout,
            ApiWebSocketResponsesClient::new(api_provider, api_auth).connect(
                headers,
                codex_login::default_client::default_headers(),
                turn_state,
                Some(websocket_telemetry),
            ),
        )
        .await
        {
            Ok(result) => result,
            Err(_) => Err(ApiError::Transport(TransportError::Timeout)),
        };
        let error_message = result.as_ref().err().map(telemetry_api_error_message);
        let response_debug = result
            .as_ref()
            .err()
            .map(extract_response_debug_context_from_api_error)
            .unwrap_or_default();
        let status = result.as_ref().err().and_then(api_error_http_status);
        session_telemetry.record_websocket_connect(
            start.elapsed(),
            status,
            error_message.as_deref(),
            auth_context.auth_header_attached,
            auth_context.auth_header_name,
            auth_context.retry_after_unauthorized,
            auth_context.recovery_mode,
            auth_context.recovery_phase,
            request_route_telemetry.endpoint,
            /*connection_reused*/ false,
            response_debug.request_id.as_deref(),
            response_debug.cf_ray.as_deref(),
            response_debug.auth_error.as_deref(),
            response_debug.auth_error_code.as_deref(),
        );
        emit_feedback_request_tags_with_auth_env(
            &FeedbackRequestTags {
                endpoint: request_route_telemetry.endpoint,
                auth_header_attached: auth_context.auth_header_attached,
                auth_header_name: auth_context.auth_header_name,
                auth_mode: auth_context.auth_mode,
                auth_retry_after_unauthorized: Some(auth_context.retry_after_unauthorized),
                auth_recovery_mode: auth_context.recovery_mode,
                auth_recovery_phase: auth_context.recovery_phase,
                auth_connection_reused: Some(false),
                auth_request_id: response_debug.request_id.as_deref(),
                auth_cf_ray: response_debug.cf_ray.as_deref(),
                auth_error: response_debug.auth_error.as_deref(),
                auth_error_code: response_debug.auth_error_code.as_deref(),
                auth_recovery_followup_success: auth_context
                    .retry_after_unauthorized
                    .then_some(result.is_ok()),
                auth_recovery_followup_status: auth_context
                    .retry_after_unauthorized
                    .then_some(status)
                    .flatten(),
            },
            &self.runtime_config().auth_env_telemetry,
        );
        result
    }

    /// Builds websocket handshake headers for both prewarm and turn-time reconnect.
    ///
    /// Callers should pass the current turn-state lock when available so sticky-routing state is
    /// replayed on reconnect within the same turn.
    fn build_websocket_headers(
        &self,
        turn_state: Option<&Arc<OnceLock<String>>>,
        turn_metadata_header: Option<&str>,
    ) -> ApiHeaderMap {
        let runtime_config = self.runtime_config();
        let turn_metadata_header = parse_turn_metadata_header(turn_metadata_header);
        let conversation_id = self.state.conversation_id.to_string();
        let mut headers = build_responses_headers(
            runtime_config.beta_features_header.as_deref(),
            turn_state,
            turn_metadata_header.as_ref(),
        );
        if let Ok(header_value) = HeaderValue::from_str(&conversation_id) {
            headers.insert("x-client-request-id", header_value);
        }
        headers.extend(build_conversation_headers(Some(conversation_id)));
        headers.extend(self.build_responses_identity_headers());
        headers.insert(
            OPENAI_BETA_HEADER,
            HeaderValue::from_static(RESPONSES_WEBSOCKETS_V2_BETA_HEADER_VALUE),
        );
        if runtime_config.include_timing_metrics {
            headers.insert(
                X_RESPONSESAPI_INCLUDE_TIMING_METRICS_HEADER,
                HeaderValue::from_static("true"),
            );
        }
        headers
    }
}

impl Drop for ModelClientSession {
    fn drop(&mut self) {
        let websocket_session = std::mem::take(&mut self.websocket_session);
        self.client
            .store_cached_websocket_session(websocket_session);
    }
}

impl ModelClientSession {
    pub(crate) fn reset_websocket_session(&mut self) {
        self.websocket_session.connection = None;
        self.websocket_session.last_request = None;
        self.websocket_session.last_response_rx = None;
        self.websocket_session
            .set_connection_reused(/*connection_reused*/ false);
    }

    fn build_responses_request(
        &self,
        provider: &codex_api::Provider,
        prompt: &Prompt,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        service_tier: Option<ServiceTier>,
    ) -> Result<ResponsesApiRequest> {
        let runtime_config = self.client.runtime_config();
        let instructions = &prompt.base_instructions.text;
        let input = prompt.get_formatted_input();
        let tools = create_tools_json_for_responses_api(&prompt.tools)?;
        let default_reasoning_effort = model_info.default_reasoning_level;
        let reasoning = if runtime_config.provider.supports_reasoning_controls()
            || model_info.supports_reasoning_summaries
        {
            Some(Reasoning {
                effort: effort.or(default_reasoning_effort),
                summary: if model_info.supports_reasoning_summaries
                    && summary != ReasoningSummaryConfig::None
                {
                    Some(summary)
                } else {
                    None
                },
            })
        } else {
            None
        };
        let include = if reasoning.is_some() {
            vec!["reasoning.encrypted_content".to_string()]
        } else {
            Vec::new()
        };
        let verbosity = if model_info.support_verbosity {
            runtime_config
                .model_verbosity
                .or(model_info.default_verbosity)
        } else {
            if runtime_config.model_verbosity.is_some() {
                warn!(
                    "model_verbosity is set but ignored as the model does not support verbosity: {}",
                    model_info.slug
                );
            }
            None
        };
        let text = create_text_param_for_request(verbosity, &prompt.output_schema);
        let prompt_cache_key = Some(self.client.state.conversation_id.to_string());
        let request_model =
            normalize_request_model_for_provider(&runtime_config.provider, &model_info.slug);
        let request = ResponsesApiRequest {
            model: request_model.into_owned(),
            instructions: instructions.clone(),
            input,
            tools,
            tool_choice: "auto".to_string(),
            parallel_tool_calls: prompt.parallel_tool_calls,
            reasoning,
            store: provider.is_azure_responses_endpoint(),
            stream: true,
            include,
            service_tier: match service_tier {
                Some(ServiceTier::Fast) => Some("priority".to_string()),
                Some(service_tier) => Some(service_tier.to_string()),
                None => None,
            },
            prompt_cache_key,
            text,
        };
        Ok(request)
    }

    fn build_chat_completions_request(
        &self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        effort: Option<ReasoningEffortConfig>,
    ) -> Result<ChatCompletionsRequest> {
        let runtime_config = self.client.runtime_config();
        let request_model =
            normalize_request_model_for_provider(&runtime_config.provider, &model_info.slug);
        let input = trim_chat_completions_input_to_prompt_cap(
            prompt.get_formatted_input(),
            &prompt.base_instructions,
            self.client.cached_chat_completions_prompt_tokens_cap(
                &runtime_config.provider,
                request_model.as_ref(),
            ),
        );
        let chat_completions_tool_names = serializable_chat_completions_tool_names(&prompt.tools)?;
        let messages = build_chat_completions_messages_with_tools(
            &runtime_config.provider,
            &prompt.base_instructions.text,
            &input,
            Some(&chat_completions_tool_names),
        )?;
        let tools = create_tools_json_for_chat_completions(&prompt.tools)?;
        let has_tools = !tools.is_empty();
        let reasoning_effort = ModelClient::build_chat_completions_reasoning_effort(
            &runtime_config.provider,
            model_info,
            effort,
        );
        let supports_parallel_tool_calls = model_info.supports_parallel_tool_calls
            || runtime_config
                .provider
                .model_supports_parallel_tool_calls(model_info.slug.as_str());

        Ok(ChatCompletionsRequest {
            max_tokens: self.client.cached_chat_completions_max_tokens_cap(
                &runtime_config.provider,
                request_model.as_ref(),
            ),
            model: request_model.into_owned(),
            messages,
            tools,
            tool_choice: has_tools.then_some("auto".to_string()),
            parallel_tool_calls: has_tools
                .then_some(prompt.parallel_tool_calls && supports_parallel_tool_calls),
            reasoning_effort,
            stream: true,
        })
    }

    fn build_chat_completions_headers(&self, turn_metadata_header: Option<&str>) -> ApiHeaderMap {
        let runtime_config = self.client.runtime_config();
        let turn_metadata_header = parse_turn_metadata_header(turn_metadata_header);
        let mut headers = build_responses_headers(
            runtime_config.beta_features_header.as_deref(),
            /*turn_state*/ None,
            turn_metadata_header.as_ref(),
        );
        headers.extend(self.client.build_responses_identity_headers());
        headers
    }

    #[allow(clippy::too_many_arguments)]
    /// Builds shared Responses API transport options and request-body options.
    ///
    /// Keeping option construction in one place ensures request-scoped headers are consistent
    /// regardless of transport choice.
    fn build_responses_options(
        &self,
        turn_metadata_header: Option<&str>,
        compression: Compression,
    ) -> ApiResponsesOptions {
        let runtime_config = self.client.runtime_config();
        let turn_metadata_header = parse_turn_metadata_header(turn_metadata_header);
        let conversation_id = self.client.state.conversation_id.to_string();
        ApiResponsesOptions {
            conversation_id: Some(conversation_id),
            session_source: Some(runtime_config.session_source.clone()),
            extra_headers: {
                let mut headers = build_responses_headers(
                    runtime_config.beta_features_header.as_deref(),
                    Some(&self.turn_state),
                    turn_metadata_header.as_ref(),
                );
                headers.extend(self.client.build_responses_identity_headers());
                headers
            },
            compression,
            turn_state: Some(Arc::clone(&self.turn_state)),
        }
    }

    fn get_incremental_items(
        &self,
        request: &ResponsesApiRequest,
        last_response: Option<&LastResponse>,
        allow_empty_delta: bool,
    ) -> Option<Vec<ResponseItem>> {
        // Checks whether the current request is an incremental extension of the previous request.
        // We only reuse an incremental input delta when non-input request fields are unchanged and
        // `input` is a strict
        // extension of the previous known input. Server-returned output items are treated as part
        // of the baseline so we do not resend them.
        let previous_request = self.websocket_session.last_request.as_ref()?;
        let mut previous_without_input = previous_request.clone();
        previous_without_input.input.clear();
        let mut request_without_input = request.clone();
        request_without_input.input.clear();
        if previous_without_input != request_without_input {
            trace!(
                "incremental request failed, properties didn't match {previous_without_input:?} != {request_without_input:?}"
            );
            return None;
        }

        let mut baseline = previous_request.input.clone();
        if let Some(last_response) = last_response {
            baseline.extend(last_response.items_added.clone());
        }

        let baseline_len = baseline.len();
        if request.input.starts_with(&baseline)
            && (allow_empty_delta || baseline_len < request.input.len())
        {
            Some(request.input[baseline_len..].to_vec())
        } else {
            trace!("incremental request failed, items didn't match");
            None
        }
    }

    fn get_last_response(&mut self) -> Option<LastResponse> {
        self.websocket_session
            .last_response_rx
            .take()
            .and_then(|mut receiver| match receiver.try_recv() {
                Ok(last_response) => Some(last_response),
                Err(TryRecvError::Closed) | Err(TryRecvError::Empty) => None,
            })
    }

    fn prepare_websocket_request(
        &mut self,
        payload: ResponseCreateWsRequest,
        request: &ResponsesApiRequest,
    ) -> ResponsesWsRequest {
        let Some(last_response) = self.get_last_response() else {
            return ResponsesWsRequest::ResponseCreate(payload);
        };
        let Some(incremental_items) = self.get_incremental_items(
            request,
            Some(&last_response),
            /*allow_empty_delta*/ true,
        ) else {
            return ResponsesWsRequest::ResponseCreate(payload);
        };

        if last_response.response_id.is_empty() {
            trace!("incremental request failed, no previous response id");
            return ResponsesWsRequest::ResponseCreate(payload);
        }

        ResponsesWsRequest::ResponseCreate(ResponseCreateWsRequest {
            previous_response_id: Some(last_response.response_id),
            input: incremental_items,
            ..payload
        })
    }

    /// Opportunistically preconnects a websocket for this turn-scoped client session.
    ///
    /// This performs only connection setup; it never sends prompt payloads.
    pub async fn preconnect_websocket(
        &mut self,
        session_telemetry: &SessionTelemetry,
        _model_info: &ModelInfo,
    ) -> std::result::Result<(), ApiError> {
        if !self.client.responses_websocket_enabled() {
            return Ok(());
        }
        if self.websocket_session.connection.is_some() {
            return Ok(());
        }

        let client_setup = self.client.current_client_setup().await.map_err(|err| {
            ApiError::Stream(format!(
                "failed to build websocket prewarm client setup: {err}"
            ))
        })?;
        let auth_context = AuthRequestTelemetryContext::new(
            client_setup.auth.as_ref().map(CodexAuth::auth_mode),
            &client_setup.api_auth,
            PendingUnauthorizedRetry::default(),
        );
        let connection = self
            .client
            .connect_websocket(
                session_telemetry,
                client_setup.api_provider,
                client_setup.api_auth,
                Some(Arc::clone(&self.turn_state)),
                /*turn_metadata_header*/ None,
                auth_context,
                RequestRouteTelemetry::for_endpoint(RESPONSES_ENDPOINT),
            )
            .await?;
        self.websocket_session.connection = Some(connection);
        self.websocket_session
            .set_connection_reused(/*connection_reused*/ false);
        Ok(())
    }
    /// Returns a websocket connection for this turn.
    #[instrument(
        name = "model_client.websocket_connection",
        level = "info",
        skip_all,
        fields(
            provider = %self.client.runtime_config().provider.name,
            wire_api = %self.client.runtime_config().provider.wire_api,
            transport = "responses_websocket",
            api.path = "responses",
            turn.has_metadata_header = params.turn_metadata_header.is_some()
        )
    )]
    async fn websocket_connection(
        &mut self,
        params: WebsocketConnectParams<'_>,
    ) -> std::result::Result<&ApiWebSocketConnection, ApiError> {
        let WebsocketConnectParams {
            session_telemetry,
            api_provider,
            api_auth,
            turn_metadata_header,
            options,
            auth_context,
            request_route_telemetry,
        } = params;
        let needs_new = match self.websocket_session.connection.as_ref() {
            Some(conn) => conn.is_closed().await,
            None => true,
        };

        if needs_new {
            self.websocket_session.last_request = None;
            self.websocket_session.last_response_rx = None;
            let turn_state = options
                .turn_state
                .clone()
                .unwrap_or_else(|| Arc::clone(&self.turn_state));
            let new_conn = match self
                .client
                .connect_websocket(
                    session_telemetry,
                    api_provider,
                    api_auth,
                    Some(turn_state),
                    turn_metadata_header,
                    auth_context,
                    request_route_telemetry,
                )
                .await
            {
                Ok(new_conn) => new_conn,
                Err(err) => {
                    if matches!(err, ApiError::Transport(TransportError::Timeout)) {
                        self.reset_websocket_session();
                    }
                    return Err(err);
                }
            };
            self.websocket_session.connection = Some(new_conn);
            self.websocket_session
                .set_connection_reused(/*connection_reused*/ false);
        } else {
            self.websocket_session
                .set_connection_reused(/*connection_reused*/ true);
        }

        self.websocket_session
            .connection
            .as_ref()
            .ok_or(ApiError::Stream(
                "websocket connection is unavailable".to_string(),
            ))
    }

    fn responses_request_compression(&self, auth: Option<&CodexAuth>) -> Compression {
        let runtime_config = self.client.runtime_config();
        if runtime_config.enable_request_compression
            && auth.is_some_and(CodexAuth::is_chatgpt_auth)
            && runtime_config.provider.is_openai()
        {
            Compression::Zstd
        } else {
            Compression::None
        }
    }

    /// Streams a turn via the OpenAI Responses API.
    ///
    /// Handles SSE fixtures, reasoning summaries, verbosity, and the
    /// `text` controls used for output schemas.
    #[allow(clippy::too_many_arguments)]
    #[instrument(
        name = "model_client.stream_responses_api",
        level = "info",
        skip_all,
        fields(
            model = %model_info.slug,
            wire_api = %self.client.runtime_config().provider.wire_api,
            transport = "responses_http",
            http.method = "POST",
            api.path = "responses",
            turn.has_metadata_header = turn_metadata_header.is_some()
        )
    )]
    async fn stream_responses_api(
        &self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        session_telemetry: &SessionTelemetry,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        service_tier: Option<ServiceTier>,
        turn_metadata_header: Option<&str>,
    ) -> Result<ResponseStream> {
        if let Some(path) = &*CODEX_RS_SSE_FIXTURE {
            warn!(path, "Streaming from fixture");
            let runtime_config = self.client.runtime_config();
            let stream =
                codex_api::stream_from_fixture(path, runtime_config.provider.stream_idle_timeout())
                    .map_err(map_api_error)?;
            let (stream, _last_request_rx) = map_response_stream(stream, session_telemetry.clone());
            return Ok(stream);
        }

        let auth_manager = self.client.runtime_config().auth_manager;
        let mut auth_recovery = auth_manager
            .as_ref()
            .map(AuthManager::unauthorized_recovery);
        let mut pending_retry = PendingUnauthorizedRetry::default();
        loop {
            let client_setup = self.client.current_client_setup().await?;
            let transport = ReqwestTransport::new(build_reqwest_client());
            let request_auth_context = AuthRequestTelemetryContext::new(
                client_setup.auth.as_ref().map(CodexAuth::auth_mode),
                &client_setup.api_auth,
                pending_retry,
            );
            let (request_telemetry, sse_telemetry) = Self::build_streaming_telemetry(
                session_telemetry,
                request_auth_context,
                RequestRouteTelemetry::for_endpoint(RESPONSES_ENDPOINT),
                self.client.runtime_config().auth_env_telemetry,
            );
            let compression = self.responses_request_compression(client_setup.auth.as_ref());
            let options = self.build_responses_options(turn_metadata_header, compression);

            let request = self.build_responses_request(
                &client_setup.api_provider,
                prompt,
                model_info,
                effort,
                summary,
                service_tier,
            )?;
            let client = ApiResponsesClient::new(
                transport,
                client_setup.api_provider,
                client_setup.api_auth,
            )
            .with_telemetry(Some(request_telemetry), Some(sse_telemetry));
            let stream_result = client.stream_request(request, options).await;

            match stream_result {
                Ok(stream) => {
                    let (stream, _) = map_response_stream(stream, session_telemetry.clone());
                    return Ok(stream);
                }
                Err(ApiError::Transport(
                    unauthorized_transport @ TransportError::Http { status, .. },
                )) if status == StatusCode::UNAUTHORIZED => {
                    pending_retry = PendingUnauthorizedRetry::from_recovery(
                        handle_unauthorized(
                            unauthorized_transport,
                            &mut auth_recovery,
                            session_telemetry,
                        )
                        .await?,
                    );
                    continue;
                }
                Err(err) => return Err(map_api_error(err)),
            }
        }
    }

    #[allow(clippy::too_many_arguments)]
    #[instrument(
        name = "model_client.stream_chat_completions",
        level = "info",
        skip_all,
        fields(
            model = %model_info.slug,
            wire_api = "chat_completions",
            transport = "chat_completions_http",
            http.method = "POST",
            api.path = "chat/completions",
            turn.has_metadata_header = turn_metadata_header.is_some()
        )
    )]
    async fn stream_chat_completions(
        &self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        session_telemetry: &SessionTelemetry,
        effort: Option<ReasoningEffortConfig>,
        _summary: ReasoningSummaryConfig,
        _service_tier: Option<ServiceTier>,
        turn_metadata_header: Option<&str>,
    ) -> Result<ResponseStream> {
        if let Some(path) = &*CODEX_RS_SSE_FIXTURE {
            warn!(path, "Streaming from fixture");
            let runtime_config = self.client.runtime_config();
            let stream =
                codex_api::stream_from_fixture(path, runtime_config.provider.stream_idle_timeout())
                    .map_err(map_api_error)?;
            let (stream, _last_request_rx) = map_response_stream(stream, session_telemetry.clone());
            return Ok(stream);
        }

        let auth_manager = self.client.runtime_config().auth_manager;
        let mut auth_recovery = auth_manager
            .as_ref()
            .map(AuthManager::unauthorized_recovery);
        loop {
            let client_setup = self.client.current_client_setup().await?;
            let request = self.build_chat_completions_request(prompt, model_info, effort)?;
            let extra_headers = self.build_chat_completions_headers(turn_metadata_header);
            let custom_tool_names = chat_completions_custom_tool_names(&prompt.tools);
            match execute_chat_completions_streaming_request(
                &client_setup.api_provider,
                &client_setup.api_auth,
                &request,
                extra_headers,
                custom_tool_names,
                session_telemetry,
                self.client.runtime_config().provider.stream_idle_timeout(),
            )
            .await
            {
                Ok(stream) => return Ok(stream),
                Err(unauthorized_transport @ TransportError::Http { status, .. })
                    if status == StatusCode::UNAUTHORIZED =>
                {
                    handle_unauthorized(
                        unauthorized_transport,
                        &mut auth_recovery,
                        session_telemetry,
                    )
                    .await?;
                    continue;
                }
                Err(TransportError::Http {
                    status,
                    url,
                    headers,
                    body,
                }) if status == StatusCode::PAYMENT_REQUIRED => {
                    let provider = self.client.runtime_config().provider;
                    if let Some(max_tokens) =
                        parse_openrouter_affordable_max_tokens(body.as_deref())
                        && self.client.remember_chat_completions_max_tokens_cap(
                            &provider,
                            request.model.as_str(),
                            max_tokens,
                        )
                        && request.max_tokens != Some(max_tokens)
                    {
                        warn!(
                            model = %request.model,
                            max_tokens,
                            "retrying OpenRouter chat completions request with reduced max_tokens"
                        );
                        continue;
                    }

                    if let Some(prompt_tokens) =
                        parse_openrouter_affordable_prompt_tokens(body.as_deref())
                        && self.client.remember_chat_completions_prompt_tokens_cap(
                            &provider,
                            request.model.as_str(),
                            prompt_tokens,
                        )
                    {
                        warn!(
                            model = %request.model,
                            prompt_tokens,
                            "retrying OpenRouter chat completions request with reduced prompt history"
                        );
                        continue;
                    }

                    return Err(map_api_error(ApiError::Transport(TransportError::Http {
                        status,
                        url,
                        headers,
                        body,
                    })));
                }
                Err(err) => return Err(map_api_error(ApiError::Transport(err))),
            }
        }
    }

    /// Streams a turn via the Responses API over WebSocket transport.
    #[allow(clippy::too_many_arguments)]
    #[instrument(
        name = "model_client.stream_responses_websocket",
        level = "info",
        skip_all,
        fields(
            model = %model_info.slug,
            wire_api = %self.client.runtime_config().provider.wire_api,
            transport = "responses_websocket",
            api.path = "responses",
            turn.has_metadata_header = turn_metadata_header.is_some(),
            websocket.warmup = warmup
        )
    )]
    async fn stream_responses_websocket(
        &mut self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        session_telemetry: &SessionTelemetry,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        service_tier: Option<ServiceTier>,
        turn_metadata_header: Option<&str>,
        warmup: bool,
        request_trace: Option<W3cTraceContext>,
    ) -> Result<WebsocketStreamOutcome> {
        let auth_manager = self.client.runtime_config().auth_manager;

        let mut auth_recovery = auth_manager
            .as_ref()
            .map(AuthManager::unauthorized_recovery);
        let mut pending_retry = PendingUnauthorizedRetry::default();
        loop {
            let client_setup = self.client.current_client_setup().await?;
            let request_auth_context = AuthRequestTelemetryContext::new(
                client_setup.auth.as_ref().map(CodexAuth::auth_mode),
                &client_setup.api_auth,
                pending_retry,
            );
            let compression = self.responses_request_compression(client_setup.auth.as_ref());

            let options = self.build_responses_options(turn_metadata_header, compression);
            let request = self.build_responses_request(
                &client_setup.api_provider,
                prompt,
                model_info,
                effort,
                summary,
                service_tier,
            )?;
            let mut ws_payload = ResponseCreateWsRequest {
                client_metadata: response_create_client_metadata(
                    Some(self.client.build_ws_client_metadata(turn_metadata_header)),
                    request_trace.as_ref(),
                ),
                ..ResponseCreateWsRequest::from(&request)
            };
            if warmup {
                ws_payload.generate = Some(false);
            }

            match self
                .websocket_connection(WebsocketConnectParams {
                    session_telemetry,
                    api_provider: client_setup.api_provider,
                    api_auth: client_setup.api_auth,
                    turn_metadata_header,
                    options: &options,
                    auth_context: request_auth_context,
                    request_route_telemetry: RequestRouteTelemetry::for_endpoint(
                        RESPONSES_ENDPOINT,
                    ),
                })
                .await
            {
                Ok(_) => {}
                Err(ApiError::Transport(TransportError::Http { status, .. }))
                    if status == StatusCode::UPGRADE_REQUIRED =>
                {
                    return Ok(WebsocketStreamOutcome::FallbackToHttp);
                }
                Err(ApiError::Transport(
                    unauthorized_transport @ TransportError::Http { status, .. },
                )) if status == StatusCode::UNAUTHORIZED => {
                    pending_retry = PendingUnauthorizedRetry::from_recovery(
                        handle_unauthorized(
                            unauthorized_transport,
                            &mut auth_recovery,
                            session_telemetry,
                        )
                        .await?,
                    );
                    continue;
                }
                Err(err) => return Err(map_api_error(err)),
            }

            let ws_request = self.prepare_websocket_request(ws_payload, &request);
            self.websocket_session.last_request = Some(request);
            let stream_result = self.websocket_session.connection.as_ref().ok_or_else(|| {
                map_api_error(ApiError::Stream(
                    "websocket connection is unavailable".to_string(),
                ))
            })?;
            let stream_result = stream_result
                .stream_request(ws_request, self.websocket_session.connection_reused())
                .await
                .map_err(map_api_error)?;
            let (stream, last_request_rx) =
                map_response_stream(stream_result, session_telemetry.clone());
            self.websocket_session.last_response_rx = Some(last_request_rx);
            return Ok(WebsocketStreamOutcome::Stream(stream));
        }
    }

    /// Builds request and SSE telemetry for streaming API calls.
    fn build_streaming_telemetry(
        session_telemetry: &SessionTelemetry,
        auth_context: AuthRequestTelemetryContext,
        request_route_telemetry: RequestRouteTelemetry,
        auth_env_telemetry: AuthEnvTelemetry,
    ) -> (Arc<dyn RequestTelemetry>, Arc<dyn SseTelemetry>) {
        let telemetry = Arc::new(ApiTelemetry::new(
            session_telemetry.clone(),
            auth_context,
            request_route_telemetry,
            auth_env_telemetry,
        ));
        let request_telemetry: Arc<dyn RequestTelemetry> = telemetry.clone();
        let sse_telemetry: Arc<dyn SseTelemetry> = telemetry;
        (request_telemetry, sse_telemetry)
    }

    /// Builds telemetry for the Responses API WebSocket transport.
    fn build_websocket_telemetry(
        session_telemetry: &SessionTelemetry,
        auth_context: AuthRequestTelemetryContext,
        request_route_telemetry: RequestRouteTelemetry,
        auth_env_telemetry: AuthEnvTelemetry,
    ) -> Arc<dyn WebsocketTelemetry> {
        let telemetry = Arc::new(ApiTelemetry::new(
            session_telemetry.clone(),
            auth_context,
            request_route_telemetry,
            auth_env_telemetry,
        ));
        let websocket_telemetry: Arc<dyn WebsocketTelemetry> = telemetry;
        websocket_telemetry
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn prewarm_websocket(
        &mut self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        session_telemetry: &SessionTelemetry,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        service_tier: Option<ServiceTier>,
        turn_metadata_header: Option<&str>,
    ) -> Result<()> {
        if !self.client.responses_websocket_enabled() {
            return Ok(());
        }
        if self.websocket_session.last_request.is_some() {
            return Ok(());
        }

        match self
            .stream_responses_websocket(
                prompt,
                model_info,
                session_telemetry,
                effort,
                summary,
                service_tier,
                turn_metadata_header,
                /*warmup*/ true,
                current_span_w3c_trace_context(),
            )
            .await
        {
            Ok(WebsocketStreamOutcome::Stream(mut stream)) => {
                // Wait for the v2 warmup request to complete before sending the first turn request.
                while let Some(event) = stream.next().await {
                    match event {
                        Ok(ResponseEvent::Completed { .. }) => break,
                        Err(err) => return Err(err),
                        _ => {}
                    }
                }
                Ok(())
            }
            Ok(WebsocketStreamOutcome::FallbackToHttp) => {
                self.try_switch_fallback_transport(session_telemetry, model_info);
                Ok(())
            }
            Err(err) => Err(err),
        }
    }

    #[allow(clippy::too_many_arguments)]
    /// Streams a single model request within the current turn.
    ///
    /// The caller is responsible for passing per-turn settings explicitly (model selection,
    /// reasoning settings, telemetry context, and turn metadata). This method will prefer the
    /// Responses WebSocket transport when the provider supports it and it remains healthy, and will
    /// fall back to the HTTP Responses API transport otherwise.
    pub async fn stream(
        &mut self,
        prompt: &Prompt,
        model_info: &ModelInfo,
        session_telemetry: &SessionTelemetry,
        effort: Option<ReasoningEffortConfig>,
        summary: ReasoningSummaryConfig,
        service_tier: Option<ServiceTier>,
        turn_metadata_header: Option<&str>,
    ) -> Result<ResponseStream> {
        let runtime_config = self.client.runtime_config();
        match effective_wire_api(&runtime_config.provider) {
            WireApi::Responses => {
                if self.client.responses_websocket_enabled() {
                    let request_trace = current_span_w3c_trace_context();
                    match self
                        .stream_responses_websocket(
                            prompt,
                            model_info,
                            session_telemetry,
                            effort,
                            summary,
                            service_tier,
                            turn_metadata_header,
                            /*warmup*/ false,
                            request_trace,
                        )
                        .await?
                    {
                        WebsocketStreamOutcome::Stream(stream) => return Ok(stream),
                        WebsocketStreamOutcome::FallbackToHttp => {
                            self.try_switch_fallback_transport(session_telemetry, model_info);
                        }
                    }
                }

                self.stream_responses_api(
                    prompt,
                    model_info,
                    session_telemetry,
                    effort,
                    summary,
                    service_tier,
                    turn_metadata_header,
                )
                .await
            }
            WireApi::ChatCompletions => {
                self.stream_chat_completions(
                    prompt,
                    model_info,
                    session_telemetry,
                    effort,
                    summary,
                    service_tier,
                    turn_metadata_header,
                )
                .await
            }
        }
    }

    /// Permanently disables WebSockets for this Codex session and resets WebSocket state.
    ///
    /// This is used after exhausting the provider retry budget, to force subsequent requests onto
    /// the HTTP transport.
    ///
    /// Returns `true` if this call activated fallback, or `false` if fallback was already active.
    pub(crate) fn try_switch_fallback_transport(
        &mut self,
        session_telemetry: &SessionTelemetry,
        model_info: &ModelInfo,
    ) -> bool {
        let activated = self
            .client
            .force_http_fallback(session_telemetry, model_info);
        self.websocket_session = WebsocketSession::default();
        activated
    }
}

/// Parses per-turn metadata into an HTTP header value.
///
/// Invalid values are treated as absent so callers can compare and propagate
/// metadata with the same sanitization path used when constructing headers.
fn parse_turn_metadata_header(turn_metadata_header: Option<&str>) -> Option<HeaderValue> {
    turn_metadata_header.and_then(|value| HeaderValue::from_str(value).ok())
}

/// Builds the extra headers attached to Responses API requests.
///
/// These headers implement Codex-specific conventions:
///
/// - `x-codex-beta-features`: comma-separated beta feature keys enabled for the session.
/// - `x-codex-turn-state`: sticky routing token captured earlier in the turn.
/// - `x-codex-turn-metadata`: optional per-turn metadata for observability.
fn build_responses_headers(
    beta_features_header: Option<&str>,
    turn_state: Option<&Arc<OnceLock<String>>>,
    turn_metadata_header: Option<&HeaderValue>,
) -> ApiHeaderMap {
    let mut headers = ApiHeaderMap::new();
    if let Some(value) = beta_features_header
        && !value.is_empty()
        && let Ok(header_value) = HeaderValue::from_str(value)
    {
        headers.insert("x-codex-beta-features", header_value);
    }
    if let Some(turn_state) = turn_state
        && let Some(state) = turn_state.get()
        && let Ok(header_value) = HeaderValue::from_str(state)
    {
        headers.insert(X_CODEX_TURN_STATE_HEADER, header_value);
    }
    if let Some(header_value) = turn_metadata_header {
        headers.insert(X_CODEX_TURN_METADATA_HEADER, header_value.clone());
    }
    headers
}

fn subagent_header_value(session_source: &SessionSource) -> Option<String> {
    let SessionSource::SubAgent(subagent_source) = session_source else {
        return None;
    };
    match subagent_source {
        SubAgentSource::Review => Some("review".to_string()),
        SubAgentSource::Compact => Some("compact".to_string()),
        SubAgentSource::MemoryConsolidation => Some("memory_consolidation".to_string()),
        SubAgentSource::ThreadSpawn { .. } => Some("collab_spawn".to_string()),
        SubAgentSource::Other(label) => Some(label.clone()),
    }
}

fn parent_thread_id_header_value(session_source: &SessionSource) -> Option<String> {
    match session_source {
        SessionSource::SubAgent(SubAgentSource::ThreadSpawn {
            parent_thread_id, ..
        }) => Some(parent_thread_id.to_string()),
        SessionSource::Cli
        | SessionSource::VSCode
        | SessionSource::Exec
        | SessionSource::Mcp
        | SessionSource::Custom(_)
        | SessionSource::SubAgent(_)
        | SessionSource::Unknown => None,
    }
}

fn map_response_stream<S>(
    api_stream: S,
    session_telemetry: SessionTelemetry,
) -> (ResponseStream, oneshot::Receiver<LastResponse>)
where
    S: futures::Stream<Item = std::result::Result<ResponseEvent, ApiError>>
        + Unpin
        + Send
        + 'static,
{
    let (tx_event, rx_event) = mpsc::channel::<Result<ResponseEvent>>(1600);
    let (tx_last_response, rx_last_response) = oneshot::channel::<LastResponse>();

    tokio::spawn(async move {
        let mut logged_error = false;
        let mut tx_last_response = Some(tx_last_response);
        let mut items_added: Vec<ResponseItem> = Vec::new();
        let mut api_stream = api_stream;
        while let Some(event) = api_stream.next().await {
            match event {
                Ok(ResponseEvent::OutputItemDone(item)) => {
                    items_added.push(item.clone());
                    if tx_event
                        .send(Ok(ResponseEvent::OutputItemDone(item)))
                        .await
                        .is_err()
                    {
                        return;
                    }
                }
                Ok(ResponseEvent::Completed {
                    response_id,
                    token_usage,
                }) => {
                    if let Some(usage) = &token_usage {
                        session_telemetry.sse_event_completed(
                            usage.input_tokens,
                            usage.output_tokens,
                            Some(usage.cached_input_tokens),
                            Some(usage.reasoning_output_tokens),
                            usage.total_tokens,
                        );
                    }
                    if let Some(sender) = tx_last_response.take() {
                        let _ = sender.send(LastResponse {
                            response_id: response_id.clone(),
                            items_added: std::mem::take(&mut items_added),
                        });
                    }
                    if tx_event
                        .send(Ok(ResponseEvent::Completed {
                            response_id,
                            token_usage,
                        }))
                        .await
                        .is_err()
                    {
                        return;
                    }
                }
                Ok(event) => {
                    if tx_event.send(Ok(event)).await.is_err() {
                        return;
                    }
                }
                Err(err) => {
                    let mapped = map_api_error(err);
                    if !logged_error {
                        session_telemetry.see_event_completed_failed(&mapped);
                        logged_error = true;
                    }
                    if tx_event.send(Err(mapped)).await.is_err() {
                        return;
                    }
                }
            }
        }
    });

    (ResponseStream { rx_event }, rx_last_response)
}

/// Handles a 401 response by optionally refreshing ChatGPT tokens once.
///
/// When refresh succeeds, the caller should retry the API call; otherwise
/// the mapped `CodexErr` is returned to the caller.
#[derive(Clone, Copy, Debug)]
struct UnauthorizedRecoveryExecution {
    mode: &'static str,
    phase: &'static str,
}

#[derive(Clone, Copy, Debug, Default)]
struct PendingUnauthorizedRetry {
    retry_after_unauthorized: bool,
    recovery_mode: Option<&'static str>,
    recovery_phase: Option<&'static str>,
}

impl PendingUnauthorizedRetry {
    fn from_recovery(recovery: UnauthorizedRecoveryExecution) -> Self {
        Self {
            retry_after_unauthorized: true,
            recovery_mode: Some(recovery.mode),
            recovery_phase: Some(recovery.phase),
        }
    }
}

#[derive(Clone, Copy, Debug, Default)]
struct AuthRequestTelemetryContext {
    auth_mode: Option<&'static str>,
    auth_header_attached: bool,
    auth_header_name: Option<&'static str>,
    retry_after_unauthorized: bool,
    recovery_mode: Option<&'static str>,
    recovery_phase: Option<&'static str>,
}

impl AuthRequestTelemetryContext {
    fn new(
        auth_mode: Option<AuthMode>,
        api_auth: &CoreAuthProvider,
        retry: PendingUnauthorizedRetry,
    ) -> Self {
        Self {
            auth_mode: auth_mode.map(|mode| match mode {
                AuthMode::ApiKey => "ApiKey",
                AuthMode::Chatgpt | AuthMode::ChatgptAuthTokens => "Chatgpt",
            }),
            auth_header_attached: api_auth.auth_header_attached(),
            auth_header_name: api_auth.auth_header_name(),
            retry_after_unauthorized: retry.retry_after_unauthorized,
            recovery_mode: retry.recovery_mode,
            recovery_phase: retry.recovery_phase,
        }
    }
}

struct WebsocketConnectParams<'a> {
    session_telemetry: &'a SessionTelemetry,
    api_provider: codex_api::Provider,
    api_auth: CoreAuthProvider,
    turn_metadata_header: Option<&'a str>,
    options: &'a ApiResponsesOptions,
    auth_context: AuthRequestTelemetryContext,
    request_route_telemetry: RequestRouteTelemetry,
}

async fn handle_unauthorized(
    transport: TransportError,
    auth_recovery: &mut Option<UnauthorizedRecovery>,
    session_telemetry: &SessionTelemetry,
) -> Result<UnauthorizedRecoveryExecution> {
    let debug = extract_response_debug_context(&transport);
    if let Some(recovery) = auth_recovery
        && recovery.has_next()
    {
        let mode = recovery.mode_name();
        let phase = recovery.step_name();
        return match recovery.next().await {
            Ok(step_result) => {
                session_telemetry.record_auth_recovery(
                    mode,
                    phase,
                    "recovery_succeeded",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                    /*recovery_reason*/ None,
                    step_result.auth_state_changed(),
                );
                emit_feedback_auth_recovery_tags(
                    mode,
                    phase,
                    "recovery_succeeded",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                );
                Ok(UnauthorizedRecoveryExecution { mode, phase })
            }
            Err(RefreshTokenError::Permanent(failed)) => {
                session_telemetry.record_auth_recovery(
                    mode,
                    phase,
                    "recovery_failed_permanent",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                    /*recovery_reason*/ None,
                    /*auth_state_changed*/ None,
                );
                emit_feedback_auth_recovery_tags(
                    mode,
                    phase,
                    "recovery_failed_permanent",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                );
                Err(CodexErr::RefreshTokenFailed(failed))
            }
            Err(RefreshTokenError::Transient(other)) => {
                session_telemetry.record_auth_recovery(
                    mode,
                    phase,
                    "recovery_failed_transient",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                    /*recovery_reason*/ None,
                    /*auth_state_changed*/ None,
                );
                emit_feedback_auth_recovery_tags(
                    mode,
                    phase,
                    "recovery_failed_transient",
                    debug.request_id.as_deref(),
                    debug.cf_ray.as_deref(),
                    debug.auth_error.as_deref(),
                    debug.auth_error_code.as_deref(),
                );
                Err(CodexErr::Io(other))
            }
        };
    }

    let (mode, phase, recovery_reason) = match auth_recovery.as_ref() {
        Some(recovery) => (
            recovery.mode_name(),
            recovery.step_name(),
            Some(recovery.unavailable_reason()),
        ),
        None => ("none", "none", Some("auth_manager_missing")),
    };
    session_telemetry.record_auth_recovery(
        mode,
        phase,
        "recovery_not_run",
        debug.request_id.as_deref(),
        debug.cf_ray.as_deref(),
        debug.auth_error.as_deref(),
        debug.auth_error_code.as_deref(),
        recovery_reason,
        /*auth_state_changed*/ None,
    );
    emit_feedback_auth_recovery_tags(
        mode,
        phase,
        "recovery_not_run",
        debug.request_id.as_deref(),
        debug.cf_ray.as_deref(),
        debug.auth_error.as_deref(),
        debug.auth_error_code.as_deref(),
    );

    Err(map_api_error(ApiError::Transport(transport)))
}

fn api_error_http_status(error: &ApiError) -> Option<u16> {
    match error {
        ApiError::Transport(TransportError::Http { status, .. }) => Some(status.as_u16()),
        _ => None,
    }
}

struct ApiTelemetry {
    session_telemetry: SessionTelemetry,
    auth_context: AuthRequestTelemetryContext,
    request_route_telemetry: RequestRouteTelemetry,
    auth_env_telemetry: AuthEnvTelemetry,
}

impl ApiTelemetry {
    fn new(
        session_telemetry: SessionTelemetry,
        auth_context: AuthRequestTelemetryContext,
        request_route_telemetry: RequestRouteTelemetry,
        auth_env_telemetry: AuthEnvTelemetry,
    ) -> Self {
        Self {
            session_telemetry,
            auth_context,
            request_route_telemetry,
            auth_env_telemetry,
        }
    }
}

impl RequestTelemetry for ApiTelemetry {
    fn on_request(
        &self,
        attempt: u64,
        status: Option<HttpStatusCode>,
        error: Option<&TransportError>,
        duration: Duration,
    ) {
        let error_message = error.map(telemetry_transport_error_message);
        let status = status.map(|s| s.as_u16());
        let debug = error
            .map(extract_response_debug_context)
            .unwrap_or_default();
        self.session_telemetry.record_api_request(
            attempt,
            status,
            error_message.as_deref(),
            duration,
            self.auth_context.auth_header_attached,
            self.auth_context.auth_header_name,
            self.auth_context.retry_after_unauthorized,
            self.auth_context.recovery_mode,
            self.auth_context.recovery_phase,
            self.request_route_telemetry.endpoint,
            debug.request_id.as_deref(),
            debug.cf_ray.as_deref(),
            debug.auth_error.as_deref(),
            debug.auth_error_code.as_deref(),
        );
        emit_feedback_request_tags_with_auth_env(
            &FeedbackRequestTags {
                endpoint: self.request_route_telemetry.endpoint,
                auth_header_attached: self.auth_context.auth_header_attached,
                auth_header_name: self.auth_context.auth_header_name,
                auth_mode: self.auth_context.auth_mode,
                auth_retry_after_unauthorized: Some(self.auth_context.retry_after_unauthorized),
                auth_recovery_mode: self.auth_context.recovery_mode,
                auth_recovery_phase: self.auth_context.recovery_phase,
                auth_connection_reused: None,
                auth_request_id: debug.request_id.as_deref(),
                auth_cf_ray: debug.cf_ray.as_deref(),
                auth_error: debug.auth_error.as_deref(),
                auth_error_code: debug.auth_error_code.as_deref(),
                auth_recovery_followup_success: self
                    .auth_context
                    .retry_after_unauthorized
                    .then_some(error.is_none()),
                auth_recovery_followup_status: self
                    .auth_context
                    .retry_after_unauthorized
                    .then_some(status)
                    .flatten(),
            },
            &self.auth_env_telemetry,
        );
    }
}

impl SseTelemetry for ApiTelemetry {
    fn on_sse_poll(
        &self,
        result: &std::result::Result<
            Option<std::result::Result<Event, EventStreamError<TransportError>>>,
            tokio::time::error::Elapsed,
        >,
        duration: Duration,
    ) {
        self.session_telemetry.log_sse_event(result, duration);
    }
}

impl WebsocketTelemetry for ApiTelemetry {
    fn on_ws_request(&self, duration: Duration, error: Option<&ApiError>, connection_reused: bool) {
        let error_message = error.map(telemetry_api_error_message);
        let status = error.and_then(api_error_http_status);
        let debug = error
            .map(extract_response_debug_context_from_api_error)
            .unwrap_or_default();
        self.session_telemetry.record_websocket_request(
            duration,
            error_message.as_deref(),
            connection_reused,
        );
        emit_feedback_request_tags_with_auth_env(
            &FeedbackRequestTags {
                endpoint: self.request_route_telemetry.endpoint,
                auth_header_attached: self.auth_context.auth_header_attached,
                auth_header_name: self.auth_context.auth_header_name,
                auth_mode: self.auth_context.auth_mode,
                auth_retry_after_unauthorized: Some(self.auth_context.retry_after_unauthorized),
                auth_recovery_mode: self.auth_context.recovery_mode,
                auth_recovery_phase: self.auth_context.recovery_phase,
                auth_connection_reused: Some(connection_reused),
                auth_request_id: debug.request_id.as_deref(),
                auth_cf_ray: debug.cf_ray.as_deref(),
                auth_error: debug.auth_error.as_deref(),
                auth_error_code: debug.auth_error_code.as_deref(),
                auth_recovery_followup_success: self
                    .auth_context
                    .retry_after_unauthorized
                    .then_some(error.is_none()),
                auth_recovery_followup_status: self
                    .auth_context
                    .retry_after_unauthorized
                    .then_some(status)
                    .flatten(),
            },
            &self.auth_env_telemetry,
        );
    }

    fn on_ws_event(
        &self,
        result: &std::result::Result<Option<std::result::Result<Message, Error>>, ApiError>,
        duration: Duration,
    ) {
        self.session_telemetry
            .record_websocket_event(result, duration);
    }
}

#[derive(Debug, Clone, Serialize)]
struct ChatCompletionsRequest {
    model: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    max_tokens: Option<u32>,
    messages: Vec<ChatCompletionsMessage>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    tools: Vec<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    tool_choice: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    parallel_tool_calls: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    reasoning_effort: Option<ReasoningEffortConfig>,
    stream: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ChatCompletionsMessage {
    role: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    content: Option<JsonValue>,
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    tool_call_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    tool_calls: Option<Vec<ChatCompletionToolCall>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    reasoning: Option<JsonValue>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    reasoning_details: Option<JsonValue>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    reasoning_content: Option<JsonValue>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    thinking: Option<JsonValue>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    thoughts: Option<JsonValue>,
}

impl ChatCompletionsMessage {
    fn text(role: impl Into<String>, text: String) -> Self {
        Self {
            role: role.into(),
            content: Some(JsonValue::String(text)),
            name: None,
            tool_call_id: None,
            tool_calls: None,
            reasoning: None,
            reasoning_details: None,
            reasoning_content: None,
            thinking: None,
            thoughts: None,
        }
    }

    fn assistant_tool_call(
        name: String,
        call_id: String,
        arguments: String,
        extra_content: Option<ChatCompletionToolCallExtraContent>,
    ) -> Self {
        Self {
            role: "assistant".to_string(),
            content: Some(JsonValue::String(String::new())),
            name: None,
            tool_call_id: None,
            tool_calls: Some(vec![ChatCompletionToolCall {
                id: Some(call_id),
                kind: "function".to_string(),
                function: ChatCompletionCalledFunction { name, arguments },
                extra_content,
            }]),
            reasoning: None,
            reasoning_details: None,
            reasoning_content: None,
            thinking: None,
            thoughts: None,
        }
    }

    fn tool(name: Option<String>, call_id: String, content: String) -> Self {
        Self {
            role: "tool".to_string(),
            content: Some(JsonValue::String(content)),
            name,
            tool_call_id: Some(call_id),
            tool_calls: None,
            reasoning: None,
            reasoning_details: None,
            reasoning_content: None,
            thinking: None,
            thoughts: None,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ChatCompletionToolCall {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    id: Option<String>,
    #[serde(default = "default_chat_completion_tool_call_kind", rename = "type")]
    kind: String,
    function: ChatCompletionCalledFunction,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    extra_content: Option<ChatCompletionToolCallExtraContent>,
}

fn default_chat_completion_tool_call_kind() -> String {
    "function".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ChatCompletionCalledFunction {
    name: String,
    arguments: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct ChatCompletionToolCallExtraContent {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    google: Option<ChatCompletionGoogleExtraContent>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
struct ChatCompletionGoogleExtraContent {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    thought_signature: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionsResponse {
    #[serde(default)]
    id: Option<String>,
    #[serde(default)]
    model: Option<String>,
    #[serde(default)]
    choices: Vec<ChatCompletionChoice>,
    #[serde(default)]
    usage: Option<ChatCompletionsUsage>,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionChoice {
    message: ChatCompletionsMessage,
}

#[derive(Debug, Clone, Deserialize)]
struct ChatCompletionsUsage {
    #[serde(default)]
    prompt_tokens: Option<i64>,
    #[serde(default)]
    completion_tokens: Option<i64>,
    #[serde(default)]
    total_tokens: Option<i64>,
}

fn create_tools_json_for_chat_completions(
    tools: &[ToolSpec],
) -> std::result::Result<Vec<JsonValue>, serde_json::Error> {
    let mut values = Vec::new();
    for tool in tools {
        if let Some(value) = chat_completions_tool_value(tool)? {
            values.push(value);
        }
    }
    Ok(values)
}

fn serializable_chat_completions_tool_names(
    tools: &[ToolSpec],
) -> std::result::Result<HashSet<String>, serde_json::Error> {
    let mut names = HashSet::new();
    for tool in tools {
        if chat_completions_tool_value(tool)?.is_some() {
            names.insert(tool.name().to_string());
        }
    }
    Ok(names)
}

fn chat_completions_tool_value(
    tool: &ToolSpec,
) -> std::result::Result<Option<JsonValue>, serde_json::Error> {
    match tool {
        ToolSpec::Function(tool) => {
            let parameters = serde_json::to_value(&tool.parameters)?;
            Ok(Some(json!({
                "type": "function",
                "function": {
                    "name": tool.name,
                    "description": tool.description,
                    "parameters": parameters,
                }
            })))
        }
        ToolSpec::ToolSearch {
            description,
            parameters,
            ..
        } => {
            let parameters = serde_json::to_value(parameters)?;
            Ok(Some(json!({
                "type": "function",
                "function": {
                    "name": "tool_search",
                    "description": description,
                    "parameters": parameters,
                }
            })))
        }
        ToolSpec::LocalShell {} => Ok(Some(json!({
            "type": "function",
            "function": {
                "name": "local_shell",
                "description": "Run a shell command on the user's machine using an argv array.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "command": {
                            "type": "array",
                            "items": { "type": "string" },
                            "description": "Command and argv segments to execute."
                        },
                        "workdir": {
                            "type": "string",
                            "description": "Optional working directory."
                        },
                        "timeout_ms": {
                            "type": "integer",
                            "description": "Maximum runtime in milliseconds."
                        },
                        "sandbox_permissions": {
                            "type": "string",
                            "enum": ["use_default", "require_escalated", "with_additional_permissions"],
                            "description": "Optional sandbox override mode."
                        },
                        "prefix_rule": {
                            "type": "array",
                            "items": { "type": "string" },
                            "description": "Optional reusable safe command prefix."
                        },
                        "justification": {
                            "type": "string",
                            "description": "Optional short justification when elevated execution is needed."
                        }
                    },
                    "required": ["command"],
                    "additionalProperties": false
                }
            }
        }))),
        ToolSpec::Freeform(tool) => Ok(Some(json!({
            "type": "function",
            "function": {
                "name": tool.name,
                "description": format!(
                    "{}\n\nProvide the raw tool input in the `input` field exactly as plain text. Format type: {}. Syntax: {}. Definition: {}",
                    tool.description,
                    tool.format.r#type,
                    tool.format.syntax,
                    tool.format.definition,
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "input": {
                            "type": "string",
                            "description": "Raw tool input text."
                        }
                    },
                    "required": ["input"],
                    "additionalProperties": false
                }
            }
        }))),
        ToolSpec::ImageGeneration { .. } | ToolSpec::WebSearch { .. } => Ok(None),
    }
}

fn build_chat_completions_messages(
    provider: &ModelProviderInfo,
    instructions: &str,
    input: &[ResponseItem],
) -> Result<Vec<ChatCompletionsMessage>> {
    build_chat_completions_messages_with_tools(provider, instructions, input, None)
}

fn build_chat_completions_messages_with_tools(
    provider: &ModelProviderInfo,
    instructions: &str,
    input: &[ResponseItem],
    available_tool_names: Option<&HashSet<String>>,
) -> Result<Vec<ChatCompletionsMessage>> {
    let mut messages = Vec::new();
    let instructions_role = chat_completions_instructions_role(provider);
    let mut instruction_segments = Vec::new();
    if !instructions.trim().is_empty() {
        instruction_segments.push(instructions.to_string());
    }

    let mut tool_names_by_call_id: HashMap<String, String> = HashMap::new();
    let mut pending_tool_calls: Vec<ChatCompletionToolCall> = Vec::new();
    let mut synthetic_tool_call_counter = 0usize;
    for item in input {
        match item {
            ResponseItem::Message { role, content, .. } => {
                let text = content_items_to_chat_text(content);
                if !text.is_empty() {
                    let is_instruction_message = role.eq_ignore_ascii_case("developer")
                        || role.eq_ignore_ascii_case("system");
                    let is_contextual_instruction_message = role.eq_ignore_ascii_case("user")
                        && content_items_are_chat_completions_contextual_instruction(content);
                    if is_instruction_message || is_contextual_instruction_message {
                        instruction_segments.push(text);
                    } else {
                        let normalized_role = normalize_chat_completions_role(provider, role);
                        flush_chat_completions_pending_tool_calls(
                            &mut messages,
                            &mut pending_tool_calls,
                        );
                        messages.push(ChatCompletionsMessage::text(normalized_role, text));
                    }
                }
            }
            ResponseItem::FunctionCall {
                id,
                name,
                namespace,
                arguments,
                call_id,
                ..
            } => {
                let tool_name = chat_completion_tool_name(name, namespace.as_deref());
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                tool_names_by_call_id.insert(provider_call_id.clone(), tool_name.clone());
                let extra_content = if provider_uses_gemini_api(provider) {
                    decode_google_thought_signature(id.as_ref()).map(|thought_signature| {
                        ChatCompletionToolCallExtraContent {
                            google: Some(ChatCompletionGoogleExtraContent {
                                thought_signature: Some(thought_signature.to_string()),
                            }),
                        }
                    })
                } else {
                    None
                };
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: tool_name,
                        arguments: arguments.clone(),
                    },
                    extra_content,
                });
            }
            ResponseItem::CustomToolCall {
                name,
                input,
                call_id,
                ..
            } => {
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                tool_names_by_call_id.insert(provider_call_id.clone(), name.clone());
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: name.clone(),
                        arguments: serde_json::to_string(&json!({ "input": input }))?,
                    },
                    extra_content: None,
                });
            }
            ResponseItem::ToolSearchCall {
                call_id: Some(call_id),
                execution,
                arguments,
                ..
            } if execution == "client" => {
                let tool_name = "tool_search".to_string();
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                tool_names_by_call_id.insert(provider_call_id.clone(), tool_name.clone());
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: tool_name,
                        arguments: serde_json::to_string(arguments)?,
                    },
                    extra_content: None,
                });
            }
            ResponseItem::LocalShellCall {
                id,
                call_id,
                action,
                ..
            } => {
                let Some(call_id) = call_id.clone().or_else(|| id.clone()) else {
                    continue;
                };
                let tool_name = "local_shell".to_string();
                let arguments =
                    serde_json::to_string(&shell_tool_params_from_local_shell_action(action))?;
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                tool_names_by_call_id.insert(provider_call_id.clone(), tool_name.clone());
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: tool_name,
                        arguments,
                    },
                    extra_content: None,
                });
            }
            ResponseItem::FunctionCallOutput { call_id, output } => {
                flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                messages.push(ChatCompletionsMessage::tool(
                    tool_names_by_call_id.get(&provider_call_id).cloned(),
                    provider_call_id,
                    tool_output_to_chat_text(output),
                ));
            }
            ResponseItem::CustomToolCallOutput {
                call_id,
                name,
                output,
            } => {
                flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                messages.push(ChatCompletionsMessage::tool(
                    name.clone()
                        .or_else(|| tool_names_by_call_id.get(&provider_call_id).cloned()),
                    provider_call_id,
                    tool_output_to_chat_text(output),
                ));
            }
            ResponseItem::ToolSearchOutput {
                call_id: Some(call_id),
                tools,
                ..
            } => {
                if available_tool_names.is_some_and(|names| !names.contains("tool_search")) {
                    continue;
                }
                flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, call_id.as_str());
                messages.push(ChatCompletionsMessage::tool(
                    tool_names_by_call_id
                        .get(&provider_call_id)
                        .cloned()
                        .or_else(|| Some("tool_search".to_string())),
                    provider_call_id,
                    serde_json::to_string(tools)
                        .unwrap_or_else(|_| JsonValue::Array(tools.clone()).to_string()),
                ));
            }
            ResponseItem::WebSearchCall { id, status, action } => {
                if available_tool_names.is_some_and(|names| !names.contains("web_search")) {
                    continue;
                }
                synthetic_tool_call_counter += 1;
                let raw_call_id = id
                    .clone()
                    .filter(|value| !value.trim().is_empty())
                    .unwrap_or_else(|| format!("web-search-history-{synthetic_tool_call_counter}"));
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, raw_call_id.as_str());
                let tool_name = "web_search".to_string();
                tool_names_by_call_id.insert(provider_call_id.clone(), tool_name.clone());
                let arguments = action
                    .as_ref()
                    .map(serde_json::to_string)
                    .transpose()?
                    .unwrap_or_else(|| "{}".to_string());
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id.clone()),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: tool_name.clone(),
                        arguments,
                    },
                    extra_content: None,
                });
                flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);
                let output = json!({
                    "status": status,
                    "action": action,
                });
                messages.push(ChatCompletionsMessage::tool(
                    Some(tool_name),
                    provider_call_id,
                    serde_json::to_string(&output).unwrap_or_else(|_| output.to_string()),
                ));
            }
            ResponseItem::ImageGenerationCall {
                id,
                status,
                revised_prompt,
                ..
            } => {
                if available_tool_names.is_some_and(|names| !names.contains("image_generation")) {
                    continue;
                }
                synthetic_tool_call_counter += 1;
                let raw_call_id = if id.trim().is_empty() {
                    format!("image-generation-history-{synthetic_tool_call_counter}")
                } else {
                    id.clone()
                };
                let provider_call_id =
                    normalize_chat_completions_tool_call_id(provider, raw_call_id.as_str());
                let tool_name = "image_generation".to_string();
                tool_names_by_call_id.insert(provider_call_id.clone(), tool_name.clone());
                let arguments = serde_json::to_string(&json!({
                    "prompt": revised_prompt,
                }))?;
                pending_tool_calls.push(ChatCompletionToolCall {
                    id: Some(provider_call_id.clone()),
                    kind: default_chat_completion_tool_call_kind(),
                    function: ChatCompletionCalledFunction {
                        name: tool_name.clone(),
                        arguments,
                    },
                    extra_content: None,
                });
                flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);
                let output = json!({
                    "status": status,
                    "revised_prompt": revised_prompt,
                    "result": "[image generation result omitted during provider translation]",
                });
                messages.push(ChatCompletionsMessage::tool(
                    Some(tool_name),
                    provider_call_id,
                    serde_json::to_string(&output).unwrap_or_else(|_| output.to_string()),
                ));
            }
            ResponseItem::Reasoning { .. }
            | ResponseItem::ToolSearchCall { .. }
            | ResponseItem::ToolSearchOutput { .. }
            | ResponseItem::GhostSnapshot { .. }
            | ResponseItem::Compaction { .. }
            | ResponseItem::Other => {}
        }
    }
    flush_chat_completions_pending_tool_calls(&mut messages, &mut pending_tool_calls);

    if !instruction_segments.is_empty() {
        messages.insert(
            0,
            ChatCompletionsMessage::text(instructions_role, instruction_segments.join("\n\n")),
        );
    }

    Ok(messages)
}

fn trim_chat_completions_input_to_prompt_cap(
    input: Vec<ResponseItem>,
    base_instructions: &codex_protocol::models::BaseInstructions,
    prompt_tokens_cap: Option<u32>,
) -> Vec<ResponseItem> {
    let Some(prompt_tokens_cap) = prompt_tokens_cap else {
        return input;
    };
    let prompt_tokens_cap = i64::from(prompt_tokens_cap);
    if prompt_tokens_cap <= 0 {
        return input;
    }

    let mut history = ContextManager::new();
    history.replace(input);

    while history
        .estimate_token_count_with_base_instructions(base_instructions)
        .is_some_and(|estimated_tokens| estimated_tokens > prompt_tokens_cap)
    {
        let can_trim_old_history = history
            .raw_items()
            .iter()
            .rposition(is_user_turn_boundary)
            .is_some_and(|last_user_index| last_user_index > 0);
        if !can_trim_old_history {
            break;
        }
        history.remove_first_item();
    }

    history.raw_items().to_vec()
}

fn is_chat_completions_contextual_instruction_text(text: &str) -> bool {
    AGENTS_MD_FRAGMENT.matches_text(text)
        || ENVIRONMENT_CONTEXT_FRAGMENT.matches_text(text)
        || SKILL_FRAGMENT.matches_text(text)
}

fn content_items_are_chat_completions_contextual_instruction(content: &[ContentItem]) -> bool {
    !content.is_empty()
        && content.iter().all(|item| {
            matches!(
                item,
                ContentItem::InputText { text }
                    if is_chat_completions_contextual_instruction_text(text)
            )
        })
}

fn flush_chat_completions_pending_tool_calls(
    messages: &mut Vec<ChatCompletionsMessage>,
    pending_tool_calls: &mut Vec<ChatCompletionToolCall>,
) {
    if pending_tool_calls.is_empty() {
        return;
    }

    messages.push(ChatCompletionsMessage {
        role: "assistant".to_string(),
        content: None,
        name: None,
        tool_call_id: None,
        tool_calls: Some(std::mem::take(pending_tool_calls)),
        reasoning: None,
        reasoning_details: None,
        reasoning_content: None,
        thinking: None,
        thoughts: None,
    });
}

fn content_items_to_chat_text(content: &[ContentItem]) -> String {
    let mut segments = Vec::new();
    for item in content {
        match item {
            ContentItem::InputText { text } | ContentItem::OutputText { text } => {
                let text = strip_hidden_reasoning_tags(text);
                if !text.is_empty() {
                    segments.push(text);
                }
            }
            ContentItem::InputImage { .. } => {
                segments.push("[image attachment omitted during provider translation]".to_string())
            }
        }
    }
    segments.join("\n")
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ChatCompletionsContentChannel {
    Text,
    ReasoningSummary,
    ReasoningContent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ChatCompletionsExtractedSegment {
    channel: ChatCompletionsContentChannel,
    text: String,
}

fn classify_chat_completions_content_channel(
    part_type: &str,
    default_channel: ChatCompletionsContentChannel,
) -> ChatCompletionsContentChannel {
    let normalized = part_type.to_ascii_lowercase();
    if normalized == "image_url" {
        ChatCompletionsContentChannel::Text
    } else if normalized.contains("reasoning")
        || normalized.contains("thinking")
        || normalized.contains("thought")
    {
        if normalized.contains("content") || normalized.contains("encrypted") {
            ChatCompletionsContentChannel::ReasoningContent
        } else {
            ChatCompletionsContentChannel::ReasoningSummary
        }
    } else {
        default_channel
    }
}

fn push_chat_completions_segment(
    segments: &mut Vec<ChatCompletionsExtractedSegment>,
    channel: ChatCompletionsContentChannel,
    text: impl Into<String>,
) {
    let text = strip_hidden_reasoning_tags(&text.into());
    if !text.is_empty() {
        segments.push(ChatCompletionsExtractedSegment { channel, text });
    }
}

fn collect_chat_completions_segments(
    value: &JsonValue,
    default_channel: ChatCompletionsContentChannel,
    segments: &mut Vec<ChatCompletionsExtractedSegment>,
) {
    match value {
        JsonValue::Null => {}
        JsonValue::String(text) => {
            push_chat_completions_segment(segments, default_channel, text);
        }
        JsonValue::Array(items) => {
            for item in items {
                collect_chat_completions_segments(item, default_channel, segments);
            }
        }
        JsonValue::Object(object) => {
            let resolved_channel = object
                .get("type")
                .and_then(JsonValue::as_str)
                .map(|part_type| {
                    classify_chat_completions_content_channel(part_type, default_channel)
                })
                .unwrap_or(default_channel);

            if object.get("type").and_then(JsonValue::as_str) == Some("image_url") {
                push_chat_completions_segment(
                    segments,
                    ChatCompletionsContentChannel::Text,
                    "[image attachment omitted during provider translation]",
                );
                return;
            }

            if let Some(text) = object.get("delta").and_then(JsonValue::as_str) {
                push_chat_completions_segment(segments, resolved_channel, text);
            }
            if let Some(text) = object.get("text").and_then(JsonValue::as_str) {
                push_chat_completions_segment(segments, resolved_channel, text);
            }
            if let Some(text) = object
                .get("text")
                .and_then(|text| text.get("value"))
                .and_then(JsonValue::as_str)
            {
                push_chat_completions_segment(segments, resolved_channel, text);
            }
            if let Some(summary) = object.get("summary") {
                collect_chat_completions_segments(
                    summary,
                    ChatCompletionsContentChannel::ReasoningSummary,
                    segments,
                );
            }
            if let Some(content) = object.get("content") {
                collect_chat_completions_segments(content, resolved_channel, segments);
            }
            if let Some(parts) = object.get("parts") {
                collect_chat_completions_segments(parts, resolved_channel, segments);
            }
            if let Some(reasoning) = object.get("reasoning") {
                collect_chat_completions_segments(
                    reasoning,
                    ChatCompletionsContentChannel::ReasoningSummary,
                    segments,
                );
            }
            if let Some(reasoning_details) = object.get("reasoning_details") {
                collect_chat_completions_segments(
                    reasoning_details,
                    ChatCompletionsContentChannel::ReasoningSummary,
                    segments,
                );
            }
            if let Some(thinking) = object.get("thinking") {
                collect_chat_completions_segments(
                    thinking,
                    ChatCompletionsContentChannel::ReasoningSummary,
                    segments,
                );
            }
            if let Some(thoughts) = object.get("thoughts") {
                collect_chat_completions_segments(
                    thoughts,
                    ChatCompletionsContentChannel::ReasoningSummary,
                    segments,
                );
            }
            if let Some(reasoning_content) = object.get("reasoning_content") {
                collect_chat_completions_segments(
                    reasoning_content,
                    ChatCompletionsContentChannel::ReasoningContent,
                    segments,
                );
            }
        }
        other => {
            push_chat_completions_segment(segments, default_channel, other.to_string());
        }
    }
}

fn extract_chat_completions_message_segments(
    message: &ChatCompletionsMessage,
) -> Vec<ChatCompletionsExtractedSegment> {
    let mut segments = Vec::new();
    if let Some(content) = message.content.as_ref() {
        collect_chat_completions_segments(
            content,
            ChatCompletionsContentChannel::Text,
            &mut segments,
        );
    }
    if let Some(reasoning) = message.reasoning.as_ref() {
        collect_chat_completions_segments(
            reasoning,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(reasoning_details) = message.reasoning_details.as_ref() {
        collect_chat_completions_segments(
            reasoning_details,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(thinking) = message.thinking.as_ref() {
        collect_chat_completions_segments(
            thinking,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(thoughts) = message.thoughts.as_ref() {
        collect_chat_completions_segments(
            thoughts,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(reasoning_content) = message.reasoning_content.as_ref() {
        collect_chat_completions_segments(
            reasoning_content,
            ChatCompletionsContentChannel::ReasoningContent,
            &mut segments,
        );
    }
    segments
}

fn extract_chat_completions_delta_segments(
    delta: &ChatCompletionsStreamDelta,
) -> Vec<ChatCompletionsExtractedSegment> {
    let mut segments = Vec::new();
    if let Some(content) = delta.content.as_ref() {
        collect_chat_completions_segments(
            content,
            ChatCompletionsContentChannel::Text,
            &mut segments,
        );
    }
    if let Some(reasoning) = delta.reasoning.as_ref() {
        collect_chat_completions_segments(
            reasoning,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(reasoning_details) = delta.reasoning_details.as_ref() {
        collect_chat_completions_segments(
            reasoning_details,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(thinking) = delta.thinking.as_ref() {
        collect_chat_completions_segments(
            thinking,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(thoughts) = delta.thoughts.as_ref() {
        collect_chat_completions_segments(
            thoughts,
            ChatCompletionsContentChannel::ReasoningSummary,
            &mut segments,
        );
    }
    if let Some(reasoning_content) = delta.reasoning_content.as_ref() {
        collect_chat_completions_segments(
            reasoning_content,
            ChatCompletionsContentChannel::ReasoningContent,
            &mut segments,
        );
    }
    segments
}

fn chat_completions_message_text_from_segments(
    segments: &[ChatCompletionsExtractedSegment],
) -> String {
    segments
        .iter()
        .filter(|segment| segment.channel == ChatCompletionsContentChannel::Text)
        .map(|segment| segment.text.as_str())
        .collect::<Vec<_>>()
        .join("\n")
}

fn assistant_chat_completions_response_item(role: String, text: String) -> ResponseItem {
    ResponseItem::Message {
        id: None,
        role,
        content: vec![ContentItem::OutputText { text }],
        end_turn: None,
        phase: None,
    }
}

async fn ensure_chat_completions_message_started<E>(
    tx_event: &mpsc::Sender<std::result::Result<ResponseEvent, E>>,
    emitted_message: &mut bool,
) -> bool {
    if *emitted_message {
        return true;
    }

    *emitted_message = true;
    tx_event
        .send(Ok(ResponseEvent::OutputItemAdded(
            assistant_chat_completions_response_item("assistant".to_string(), String::new()),
        )))
        .await
        .is_ok()
}

fn shell_tool_params_from_local_shell_action(action: &LocalShellAction) -> JsonValue {
    match action {
        LocalShellAction::Exec(exec) => {
            let mut params = serde_json::Map::new();
            params.insert("command".to_string(), json!(exec.command));
            if let Some(workdir) = exec.working_directory.clone() {
                params.insert("workdir".to_string(), json!(workdir));
            }
            if let Some(timeout_ms) = exec.timeout_ms {
                params.insert("timeout_ms".to_string(), json!(timeout_ms));
            }
            JsonValue::Object(params)
        }
    }
}

fn chat_completion_tool_name(name: &str, namespace: Option<&str>) -> String {
    match namespace {
        Some(namespace) if !name.starts_with(namespace) => format!("{namespace}{name}"),
        _ => name.to_string(),
    }
}

fn tool_output_to_chat_text(output: &FunctionCallOutputPayload) -> String {
    output
        .body
        .to_text()
        .unwrap_or_else(|| serde_json::to_string(output).unwrap_or_default())
}

fn chat_completions_custom_tool_names(tools: &[ToolSpec]) -> HashSet<String> {
    tools
        .iter()
        .filter_map(|tool| match tool {
            ToolSpec::Freeform(tool) => Some(tool.name.clone()),
            _ => None,
        })
        .collect()
}

fn decode_chat_completions_custom_tool_input(arguments: &str) -> Option<String> {
    let parsed = serde_json::from_str::<JsonValue>(arguments).ok()?;
    let input = parsed.get("input")?;
    input
        .as_str()
        .map(ToOwned::to_owned)
        .or_else(|| Some(input.to_string()))
}

fn chat_completions_tool_call_response_item(
    tool_call: ChatCompletionToolCall,
    index: usize,
    custom_tool_names: &HashSet<String>,
) -> ResponseItem {
    let thought_signature = tool_call
        .extra_content
        .as_ref()
        .and_then(|extra| extra.google.as_ref())
        .and_then(|google| google.thought_signature.as_deref())
        .map(encode_google_thought_signature);
    let name = tool_call.function.name;
    let arguments = tool_call.function.arguments;
    let call_id = tool_call
        .id
        .unwrap_or_else(|| format!("chatcmpl-tool-{index}"));

    if custom_tool_names.contains(name.as_str())
        && let Some(input) = decode_chat_completions_custom_tool_input(arguments.as_str())
    {
        return ResponseItem::CustomToolCall {
            id: thought_signature,
            status: None,
            call_id,
            name,
            input,
        };
    }

    ResponseItem::FunctionCall {
        id: thought_signature,
        name,
        namespace: None,
        arguments,
        call_id,
    }
}

async fn execute_chat_completions_streaming_request(
    provider: &codex_api::Provider,
    auth: &CoreAuthProvider,
    request: &ChatCompletionsRequest,
    extra_headers: ApiHeaderMap,
    custom_tool_names: HashSet<String>,
    session_telemetry: &SessionTelemetry,
    idle_timeout: Duration,
) -> std::result::Result<ResponseStream, TransportError> {
    let request_body =
        serde_json::to_value(request).map_err(|err| TransportError::Build(err.to_string()))?;
    let auth_token = auth.token.clone();
    let account_id = auth.account_id.clone();
    let make_request = || {
        let mut http_request = provider.build_request(Method::POST, "chat/completions");
        http_request.headers.extend(extra_headers.clone());
        if let Some(token) = auth_token.as_ref()
            && let Ok(header) = HeaderValue::from_str(&format!("Bearer {token}"))
        {
            http_request
                .headers
                .insert(http::header::AUTHORIZATION, header);
        }
        if let Some(account_id) = account_id.as_ref()
            && let Ok(header) = HeaderValue::from_str(account_id)
        {
            http_request.headers.insert("ChatGPT-Account-ID", header);
        }
        http_request.body = Some(request_body.clone());
        http_request
    };

    let stream_response = run_with_retry(
        provider.retry.to_policy(),
        make_request,
        |req, _attempt| async move {
            let transport = ReqwestTransport::new(build_reqwest_client());
            transport.stream(req).await
        },
    )
    .await?;
    let (tx, mut rx) = mpsc::channel(128);
    sse_stream(stream_response.bytes, idle_timeout, tx);

    let (tx_event, rx_event) = mpsc::channel(128);
    let session_telemetry = session_telemetry.clone();
    tokio::spawn(async move {
        let mut emitted_created = false;
        let mut emitted_message = false;
        let mut emitted_reasoning_included = false;
        let mut emitted_reasoning_summary_part = false;
        let mut text_accumulator = String::new();
        let mut pending_tool_calls: HashMap<usize, ChatCompletionToolCall> = HashMap::new();
        let mut response_id = String::new();
        let mut response_model: Option<String> = None;
        let mut usage: Option<ChatCompletionsUsage> = None;

        while let Some(frame) = rx.recv().await {
            let raw = match frame {
                Ok(raw) => raw,
                Err(err) => {
                    let _ = tx_event
                        .send(Err(map_api_error(ApiError::Stream(err.to_string()))))
                        .await;
                    return;
                }
            };

            let trimmed = raw.trim();
            if trimmed.is_empty() {
                continue;
            }
            if trimmed == "[DONE]" {
                break;
            }

            let chunk: ChatCompletionsStreamChunk = match serde_json::from_str(trimmed) {
                Ok(chunk) => chunk,
                Err(err) => {
                    let _ = tx_event
                        .send(Err(map_api_error(ApiError::Stream(format!(
                            "failed to decode chat completions stream chunk: {err}"
                        )))))
                        .await;
                    return;
                }
            };

            if !emitted_created {
                emitted_created = true;
                if tx_event.send(Ok(ResponseEvent::Created)).await.is_err() {
                    return;
                }
            }

            if let Some(id) = chunk.id.clone() {
                response_id = id;
            }
            if let Some(model) = chunk.model.clone() {
                if response_model.as_deref() != Some(model.as_str()) {
                    response_model = Some(model.clone());
                    if tx_event
                        .send(Ok(ResponseEvent::ServerModel(model)))
                        .await
                        .is_err()
                    {
                        return;
                    }
                }
            }
            if chunk.usage.is_some() {
                usage = chunk.usage.clone();
            }

            for choice in chunk.choices {
                let delta = choice.delta;
                let segments = extract_chat_completions_delta_segments(&delta);
                for segment in segments {
                    match segment.channel {
                        ChatCompletionsContentChannel::Text => {
                            text_accumulator.push_str(&segment.text);
                            if !ensure_chat_completions_message_started(
                                &tx_event,
                                &mut emitted_message,
                            )
                            .await
                            {
                                return;
                            }
                            if tx_event
                                .send(Ok(ResponseEvent::OutputTextDelta(segment.text)))
                                .await
                                .is_err()
                            {
                                return;
                            }
                        }
                        ChatCompletionsContentChannel::ReasoningSummary => {
                            if !ensure_chat_completions_message_started(
                                &tx_event,
                                &mut emitted_message,
                            )
                            .await
                            {
                                return;
                            }
                            if !emitted_reasoning_included {
                                emitted_reasoning_included = true;
                                if tx_event
                                    .send(Ok(ResponseEvent::ServerReasoningIncluded(true)))
                                    .await
                                    .is_err()
                                {
                                    return;
                                }
                            }
                            if !emitted_reasoning_summary_part {
                                emitted_reasoning_summary_part = true;
                                if tx_event
                                    .send(Ok(ResponseEvent::ReasoningSummaryPartAdded {
                                        summary_index: 0,
                                    }))
                                    .await
                                    .is_err()
                                {
                                    return;
                                }
                            }
                            if tx_event
                                .send(Ok(ResponseEvent::ReasoningSummaryDelta {
                                    delta: segment.text,
                                    summary_index: 0,
                                }))
                                .await
                                .is_err()
                            {
                                return;
                            }
                        }
                        ChatCompletionsContentChannel::ReasoningContent => {
                            if !ensure_chat_completions_message_started(
                                &tx_event,
                                &mut emitted_message,
                            )
                            .await
                            {
                                return;
                            }
                            if !emitted_reasoning_included {
                                emitted_reasoning_included = true;
                                if tx_event
                                    .send(Ok(ResponseEvent::ServerReasoningIncluded(true)))
                                    .await
                                    .is_err()
                                {
                                    return;
                                }
                            }
                            if tx_event
                                .send(Ok(ResponseEvent::ReasoningContentDelta {
                                    delta: segment.text,
                                    content_index: 0,
                                }))
                                .await
                                .is_err()
                            {
                                return;
                            }
                        }
                    }
                }

                for tool_call_delta in delta.tool_calls {
                    let entry = pending_tool_calls
                        .entry(tool_call_delta.index)
                        .or_insert_with(|| ChatCompletionToolCall {
                            id: tool_call_delta.id.clone(),
                            kind: tool_call_delta
                                .kind
                                .clone()
                                .unwrap_or_else(default_chat_completion_tool_call_kind),
                            function: ChatCompletionCalledFunction {
                                name: String::new(),
                                arguments: String::new(),
                            },
                            extra_content: tool_call_delta.extra_content.clone(),
                        });
                    if entry.id.is_none() {
                        entry.id = tool_call_delta.id.clone();
                    }
                    if let Some(kind) = tool_call_delta.kind.clone() {
                        entry.kind = kind;
                    }
                    if let Some(function) = tool_call_delta.function {
                        if let Some(name) = function.name
                            && entry.function.name.is_empty()
                        {
                            entry.function.name = name;
                        }
                        if let Some(arguments) = function.arguments {
                            entry.function.arguments.push_str(&arguments);
                        }
                    }
                    if let Some(extra_content) = tool_call_delta.extra_content {
                        entry.extra_content = Some(extra_content);
                    }
                }
            }
        }

        if emitted_message {
            let item =
                assistant_chat_completions_response_item("assistant".to_string(), text_accumulator);
            if tx_event
                .send(Ok(ResponseEvent::OutputItemDone(item)))
                .await
                .is_err()
            {
                return;
            }
        }

        let mut ordered_tool_calls = pending_tool_calls.into_iter().collect::<Vec<_>>();
        ordered_tool_calls.sort_by_key(|(index, _)| *index);
        for (index, tool_call) in ordered_tool_calls {
            let item =
                chat_completions_tool_call_response_item(tool_call, index, &custom_tool_names);
            if tx_event
                .send(Ok(ResponseEvent::OutputItemDone(item)))
                .await
                .is_err()
            {
                return;
            }
        }

        let _ = tx_event
            .send(Ok(ResponseEvent::Completed {
                response_id,
                token_usage: usage.map(chat_completions_usage_to_token_usage),
            }))
            .await;
        drop(session_telemetry);
    });

    Ok(ResponseStream { rx_event })
}

#[derive(Debug, Clone, Deserialize)]
struct ChatCompletionsStreamChunk {
    #[serde(default)]
    id: Option<String>,
    #[serde(default)]
    model: Option<String>,
    #[serde(default)]
    choices: Vec<ChatCompletionsStreamChoice>,
    #[serde(default)]
    usage: Option<ChatCompletionsUsage>,
}

#[derive(Debug, Clone, Deserialize)]
struct ChatCompletionsStreamChoice {
    #[serde(default)]
    delta: ChatCompletionsStreamDelta,
}

#[derive(Debug, Clone, Default, Deserialize)]
struct ChatCompletionsStreamDelta {
    #[serde(default)]
    content: Option<JsonValue>,
    #[serde(default)]
    reasoning: Option<JsonValue>,
    #[serde(default)]
    reasoning_details: Option<JsonValue>,
    #[serde(default)]
    reasoning_content: Option<JsonValue>,
    #[serde(default)]
    thinking: Option<JsonValue>,
    #[serde(default)]
    thoughts: Option<JsonValue>,
    #[serde(default)]
    tool_calls: Vec<ChatCompletionsStreamToolCallDelta>,
}

#[derive(Debug, Clone, Default, Deserialize)]
struct ChatCompletionsStreamToolCallDelta {
    #[serde(default)]
    index: usize,
    #[serde(default)]
    id: Option<String>,
    #[serde(default, rename = "type")]
    kind: Option<String>,
    #[serde(default)]
    function: Option<ChatCompletionsStreamFunctionDelta>,
    #[serde(default)]
    extra_content: Option<ChatCompletionToolCallExtraContent>,
}

#[derive(Debug, Clone, Default, Deserialize)]
struct ChatCompletionsStreamFunctionDelta {
    #[serde(default)]
    name: Option<String>,
    #[serde(default)]
    arguments: Option<String>,
}

async fn execute_chat_completions_request(
    provider: &codex_api::Provider,
    auth: &CoreAuthProvider,
    request: &ChatCompletionsRequest,
) -> std::result::Result<ChatCompletionsResponse, TransportError> {
    let transport = ReqwestTransport::new(build_reqwest_client());
    let mut http_request = provider.build_request(Method::POST, "chat/completions");
    if let Some(token) = auth.token.as_ref()
        && let Ok(header) = HeaderValue::from_str(&format!("Bearer {token}"))
    {
        http_request
            .headers
            .insert(http::header::AUTHORIZATION, header);
    }
    if let Some(account_id) = auth.account_id.as_ref()
        && let Ok(header) = HeaderValue::from_str(account_id)
    {
        http_request.headers.insert("ChatGPT-Account-ID", header);
    }
    http_request.body =
        Some(serde_json::to_value(request).map_err(|err| TransportError::Build(err.to_string()))?);

    let response = transport.execute(http_request).await?;
    serde_json::from_slice::<ChatCompletionsResponse>(&response.body).map_err(|err| {
        TransportError::Build(format!("failed to decode chat completions response: {err}"))
    })
}

fn chat_completions_response_to_events(
    response: ChatCompletionsResponse,
) -> Result<Vec<ResponseEvent>> {
    let ChatCompletionsResponse {
        id,
        model,
        mut choices,
        usage,
    } = response;
    let Some(choice) = choices.pop() else {
        return Err(CodexErr::Stream(
            "chat completions response did not include any choices".to_string(),
            None,
        ));
    };

    let mut events = vec![ResponseEvent::Created];
    if let Some(model) = model {
        events.push(ResponseEvent::ServerModel(model));
    }

    let message = choice.message;
    let role = message.role.clone();
    let tool_calls = message.tool_calls.clone();
    let segments = extract_chat_completions_message_segments(&message);
    if !segments.is_empty() {
        let item = assistant_chat_completions_response_item(
            role,
            chat_completions_message_text_from_segments(&segments),
        );
        events.push(ResponseEvent::OutputItemAdded(item.clone()));
        let mut emitted_reasoning_included = false;
        let mut emitted_reasoning_summary_part = false;
        for segment in segments {
            match segment.channel {
                ChatCompletionsContentChannel::Text => {}
                ChatCompletionsContentChannel::ReasoningSummary => {
                    if !emitted_reasoning_included {
                        emitted_reasoning_included = true;
                        events.push(ResponseEvent::ServerReasoningIncluded(true));
                    }
                    if !emitted_reasoning_summary_part {
                        emitted_reasoning_summary_part = true;
                        events.push(ResponseEvent::ReasoningSummaryPartAdded { summary_index: 0 });
                    }
                    events.push(ResponseEvent::ReasoningSummaryDelta {
                        delta: segment.text,
                        summary_index: 0,
                    });
                }
                ChatCompletionsContentChannel::ReasoningContent => {
                    if !emitted_reasoning_included {
                        emitted_reasoning_included = true;
                        events.push(ResponseEvent::ServerReasoningIncluded(true));
                    }
                    events.push(ResponseEvent::ReasoningContentDelta {
                        delta: segment.text,
                        content_index: 0,
                    });
                }
            }
        }
        events.push(ResponseEvent::OutputItemDone(item));
    }

    for (index, tool_call) in tool_calls.unwrap_or_default().into_iter().enumerate() {
        let item = ResponseItem::FunctionCall {
            id: tool_call
                .extra_content
                .as_ref()
                .and_then(|extra| extra.google.as_ref())
                .and_then(|google| google.thought_signature.as_deref())
                .map(encode_google_thought_signature),
            name: tool_call.function.name,
            namespace: None,
            arguments: tool_call.function.arguments,
            call_id: tool_call
                .id
                .unwrap_or_else(|| format!("chatcmpl-tool-{index}")),
        };
        events.push(ResponseEvent::OutputItemDone(item));
    }

    events.push(ResponseEvent::Completed {
        response_id: id.unwrap_or_default(),
        token_usage: usage.map(chat_completions_usage_to_token_usage),
    });
    Ok(events)
}

fn fallback_memory_summarize_output(raw_memory: ApiRawMemory) -> ApiMemorySummarizeOutput {
    let raw_memory_text = serde_json::to_string_pretty(&raw_memory.items)
        .or_else(|_| serde_json::to_string(&raw_memory.items))
        .unwrap_or_default();
    let memory_summary = raw_memory
        .items
        .iter()
        .find_map(extract_memory_summary_text)
        .filter(|text| !text.trim().is_empty())
        .unwrap_or_else(|| format!("Trace from {}", raw_memory.metadata.source_path));

    ApiMemorySummarizeOutput {
        raw_memory: raw_memory_text,
        memory_summary,
    }
}

fn extract_memory_summary_text(value: &JsonValue) -> Option<String> {
    match value {
        JsonValue::String(text) => Some(strip_hidden_reasoning_tags(text)),
        JsonValue::Array(values) => values.iter().find_map(extract_memory_summary_text),
        JsonValue::Object(object) => object
            .get("text")
            .and_then(JsonValue::as_str)
            .map(strip_hidden_reasoning_tags)
            .or_else(|| {
                object
                    .get("summary")
                    .and_then(JsonValue::as_str)
                    .map(strip_hidden_reasoning_tags)
            })
            .or_else(|| object.get("content").and_then(extract_memory_summary_text)),
        _ => None,
    }
}

fn chat_completions_usage_to_token_usage(usage: ChatCompletionsUsage) -> TokenUsage {
    let input_tokens = usage.prompt_tokens.unwrap_or_default();
    let output_tokens = usage.completion_tokens.unwrap_or_default();
    TokenUsage {
        input_tokens,
        cached_input_tokens: 0,
        output_tokens,
        reasoning_output_tokens: 0,
        total_tokens: usage
            .total_tokens
            .unwrap_or(input_tokens.saturating_add(output_tokens)),
    }
}

#[cfg(test)]
#[path = "client_tests.rs"]
mod tests;
