//! Registry of model providers supported by Codex.
//!
//! Providers can be defined in two places:
//!   1. Built-in defaults compiled into the binary so Codex works out-of-the-box.
//!   2. User-defined entries inside `~/.codex/config.toml` under the `model_providers`
//!      key. These override or extend the defaults at runtime.

use codex_api::Provider as ApiProvider;
use codex_api::provider::RetryConfig as ApiRetryConfig;
use codex_app_server_protocol::AuthMode;
use codex_protocol::config_types::ModelProviderAuthInfo;
use codex_protocol::error::CodexErr;
use codex_protocol::error::EnvVarError;
use codex_protocol::error::Result as CodexResult;
use http::HeaderMap;
use http::Uri;
use http::header::HeaderName;
use http::header::HeaderValue;
use schemars::JsonSchema;
use serde::Deserialize;
use serde::Serialize;
use std::collections::HashMap;
use std::fmt;
use std::time::Duration;

const DEFAULT_STREAM_IDLE_TIMEOUT_MS: u64 = 300_000;
const DEFAULT_STREAM_MAX_RETRIES: u64 = 5;
const DEFAULT_REQUEST_MAX_RETRIES: u64 = 4;
pub const DEFAULT_WEBSOCKET_CONNECT_TIMEOUT_MS: u64 = 15_000;
/// Hard cap for user-configured `stream_max_retries`.
const MAX_STREAM_MAX_RETRIES: u64 = 100;
/// Hard cap for user-configured `request_max_retries`.
const MAX_REQUEST_MAX_RETRIES: u64 = 100;

const OPENAI_PROVIDER_NAME: &str = "OpenAI";
pub const OPENAI_PROVIDER_ID: &str = "openai";
const GEMINI_API_HOST: &str = "generativelanguage.googleapis.com";
const GEMINI_PROVIDER_NAME: &str = "gemini";
const MISTRAL_API_HOST: &str = "api.mistral.ai";
const MISTRAL_VIBE_CLI_MODEL: &str = "mistral-vibe-cli";
const MISTRAL_VIBE_CLI_LATEST_MODEL: &str = "mistral-vibe-cli-latest";
const MISTRAL_VIBE_CLI_TOOLS_MODEL: &str = "mistral-vibe-cli-with-tools";
const MISTRAL_VIBE_CLI_FAST_MODEL: &str = "mistral-vibe-cli-fast";
const MISTRAL_CANONICAL_MEDIUM_MODEL: &str = "mistral-medium-latest";
const MISTRAL_CANONICAL_FAST_MODEL: &str = "mistral-small-latest";
const PROVIDER_MODEL_COMPATIBILITY_SUFFIXES: [&str; 4] =
    ["-with-tools", "-tools", "-latest", "-fast"];
const CHAT_WIRE_API_REMOVED_ERROR: &str = "`wire_api = \"chat\"` is no longer supported.\nHow to fix: set `wire_api = \"responses\"` in your provider config.\nMore info: https://github.com/openai/codex/discussions/7782";
pub const LEGACY_OLLAMA_CHAT_PROVIDER_ID: &str = "ollama-chat";
pub const OLLAMA_CHAT_PROVIDER_REMOVED_ERROR: &str = "`ollama-chat` is no longer supported.\nHow to fix: replace `ollama-chat` with `ollama` in `model_provider`, `oss_provider`, or `--local-provider`.\nMore info: https://github.com/openai/codex/discussions/7782";

pub fn canonicalize_provider_model_slug(slug: &str) -> Option<String> {
    let (prefix, terminal_segment) = match slug.rsplit_once('/') {
        Some((prefix, terminal_segment)) => (Some(prefix), terminal_segment),
        None => (None, slug),
    };

    let canonical_terminal = if terminal_segment.eq_ignore_ascii_case(MISTRAL_VIBE_CLI_FAST_MODEL) {
        Some(MISTRAL_CANONICAL_FAST_MODEL)
    } else if terminal_segment.eq_ignore_ascii_case(MISTRAL_VIBE_CLI_MODEL)
        || terminal_segment.eq_ignore_ascii_case(MISTRAL_VIBE_CLI_LATEST_MODEL)
        || terminal_segment.eq_ignore_ascii_case(MISTRAL_VIBE_CLI_TOOLS_MODEL)
    {
        Some(MISTRAL_CANONICAL_MEDIUM_MODEL)
    } else {
        PROVIDER_MODEL_COMPATIBILITY_SUFFIXES
            .iter()
            .find_map(|suffix| {
                let base = terminal_segment.strip_suffix(suffix)?;
                if !base.eq_ignore_ascii_case(MISTRAL_VIBE_CLI_MODEL) {
                    return None;
                }

                if suffix.eq_ignore_ascii_case("-fast") {
                    Some(MISTRAL_CANONICAL_FAST_MODEL)
                } else {
                    Some(MISTRAL_CANONICAL_MEDIUM_MODEL)
                }
            })
    }?;

    Some(match prefix {
        Some(prefix) => format!("{prefix}/{canonical_terminal}"),
        None => canonical_terminal.to_string(),
    })
}

/// Wire protocol that the provider speaks.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, JsonSchema)]
pub enum WireApi {
    /// The Responses API exposed by OpenAI at `/v1/responses`.
    #[default]
    #[serde(rename = "responses")]
    Responses,
    /// OpenAI-compatible Chat Completions exposed at `/v1/chat/completions`.
    #[serde(rename = "chat_completions")]
    ChatCompletions,
}

impl fmt::Display for WireApi {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let value = match self {
            Self::Responses => "responses",
            Self::ChatCompletions => "chat_completions",
        };
        f.write_str(value)
    }
}

impl<'de> Deserialize<'de> for WireApi {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let value = String::deserialize(deserializer)?;
        match value.as_str() {
            "responses" => Ok(Self::Responses),
            "chat_completions" => Ok(Self::ChatCompletions),
            "chat" => Err(serde::de::Error::custom(CHAT_WIRE_API_REMOVED_ERROR)),
            _ => Err(serde::de::Error::unknown_variant(
                &value,
                &["responses", "chat_completions"],
            )),
        }
    }
}

/// Serializable representation of a provider definition.
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, JsonSchema)]
#[schemars(deny_unknown_fields)]
pub struct ModelProviderInfo {
    /// Friendly display name.
    pub name: String,
    /// Base URL for the provider's OpenAI-compatible API.
    pub base_url: Option<String>,
    /// Environment variable that stores the user's API key for this provider.
    pub env_key: Option<String>,

    /// Optional instructions to help the user get a valid value for the
    /// variable and set it.
    pub env_key_instructions: Option<String>,
    /// Value to use with `Authorization: Bearer <token>` header. Use of this
    /// config is discouraged in favor of `env_key` for security reasons, but
    /// this may be necessary when using this programmatically.
    pub experimental_bearer_token: Option<String>,
    /// Command-backed bearer-token configuration for this provider.
    pub auth: Option<ModelProviderAuthInfo>,
    /// Which wire protocol this provider expects.
    #[serde(default)]
    pub wire_api: WireApi,
    /// Optional query parameters to append to the base URL.
    pub query_params: Option<HashMap<String, String>>,
    /// Additional HTTP headers to include in requests to this provider where
    /// the (key, value) pairs are the header name and value.
    pub http_headers: Option<HashMap<String, String>>,
    /// Optional HTTP headers to include in requests to this provider where the
    /// (key, value) pairs are the header name and _environment variable_ whose
    /// value should be used. If the environment variable is not set, or the
    /// value is empty, the header will not be included in the request.
    pub env_http_headers: Option<HashMap<String, String>>,
    /// Maximum number of times to retry a failed HTTP request to this provider.
    pub request_max_retries: Option<u64>,
    /// Number of times to retry reconnecting a dropped streaming response before failing.
    pub stream_max_retries: Option<u64>,
    /// Idle timeout (in milliseconds) to wait for activity on a streaming response before treating
    /// the connection as lost.
    pub stream_idle_timeout_ms: Option<u64>,
    /// Maximum time (in milliseconds) to wait for a websocket connection attempt before treating
    /// it as failed.
    pub websocket_connect_timeout_ms: Option<u64>,
    /// Does this provider require an OpenAI API Key or ChatGPT login token? If true,
    /// user is presented with login screen on first run, and login preference and token/key
    /// are stored in auth.json. If false (which is the default), login screen is skipped,
    /// and API key (if needed) comes from the "env_key" environment variable.
    #[serde(default)]
    pub requires_openai_auth: bool,
    /// Whether this provider supports the Responses API WebSocket transport.
    #[serde(default)]
    pub supports_websockets: bool,
}

impl ModelProviderInfo {
    fn base_url_host_matches(&self, expected_host: &str) -> bool {
        self.base_url.as_deref().is_some_and(|base_url| {
            Uri::try_from(base_url)
                .ok()
                .and_then(|uri| uri.host().map(str::to_ascii_lowercase))
                .is_some_and(|host| {
                    host == expected_host
                        || host
                            .strip_prefix("www.")
                            .is_some_and(|host| host == expected_host)
                })
        })
    }

    pub fn validate(&self) -> std::result::Result<(), String> {
        let Some(auth) = self.auth.as_ref() else {
            return Ok(());
        };

        if auth.command.trim().is_empty() {
            return Err("provider auth.command must not be empty".to_string());
        }

        let mut conflicts = Vec::new();
        if self.env_key.is_some() {
            conflicts.push("env_key");
        }
        if self.experimental_bearer_token.is_some() {
            conflicts.push("experimental_bearer_token");
        }
        if self.requires_openai_auth {
            conflicts.push("requires_openai_auth");
        }

        if conflicts.is_empty() {
            Ok(())
        } else {
            Err(format!(
                "provider auth cannot be combined with {}",
                conflicts.join(", ")
            ))
        }
    }

    fn build_header_map(&self) -> CodexResult<HeaderMap> {
        let capacity = self.http_headers.as_ref().map_or(0, HashMap::len)
            + self.env_http_headers.as_ref().map_or(0, HashMap::len);
        let mut headers = HeaderMap::with_capacity(capacity);
        if let Some(extra) = &self.http_headers {
            for (k, v) in extra {
                if let (Ok(name), Ok(value)) = (HeaderName::try_from(k), HeaderValue::try_from(v)) {
                    headers.insert(name, value);
                }
            }
        }

        if let Some(env_headers) = &self.env_http_headers {
            for (header, env_var) in env_headers {
                if let Ok(val) = std::env::var(env_var)
                    && !val.trim().is_empty()
                    && let (Ok(name), Ok(value)) =
                        (HeaderName::try_from(header), HeaderValue::try_from(val))
                {
                    headers.insert(name, value);
                }
            }
        }

        Ok(headers)
    }

    pub fn to_api_provider(&self, auth_mode: Option<AuthMode>) -> CodexResult<ApiProvider> {
        let default_base_url = if matches!(auth_mode, Some(AuthMode::Chatgpt)) {
            "https://chatgpt.com/backend-api/codex"
        } else {
            "https://api.openai.com/v1"
        };
        let base_url = self
            .base_url
            .clone()
            .unwrap_or_else(|| default_base_url.to_string());

        let headers = self.build_header_map()?;
        let retry = ApiRetryConfig {
            max_attempts: self.request_max_retries(),
            base_delay: Duration::from_millis(200),
            retry_429: !self.requires_openai_auth,
            retry_5xx: true,
            retry_transport: true,
        };

        Ok(ApiProvider {
            name: self.name.clone(),
            base_url,
            query_params: self.query_params.clone(),
            headers,
            retry,
            stream_idle_timeout: self.stream_idle_timeout(),
        })
    }

    /// If `env_key` is Some, returns the API key for this provider if present
    /// (and non-empty) in the environment. If `env_key` is required but
    /// cannot be found, returns an error.
    pub fn api_key(&self) -> CodexResult<Option<String>> {
        match &self.env_key {
            Some(env_key) => {
                let api_key = std::env::var(env_key)
                    .ok()
                    .filter(|v| !v.trim().is_empty())
                    .ok_or_else(|| {
                        CodexErr::EnvVar(EnvVarError {
                            var: env_key.clone(),
                            instructions: self.env_key_instructions.clone(),
                        })
                    })?;
                Ok(Some(api_key))
            }
            None => Ok(None),
        }
    }

    /// Effective maximum number of request retries for this provider.
    pub fn request_max_retries(&self) -> u64 {
        self.request_max_retries
            .unwrap_or(DEFAULT_REQUEST_MAX_RETRIES)
            .min(MAX_REQUEST_MAX_RETRIES)
    }

    /// Effective maximum number of stream reconnection attempts for this provider.
    pub fn stream_max_retries(&self) -> u64 {
        self.stream_max_retries
            .unwrap_or(DEFAULT_STREAM_MAX_RETRIES)
            .min(MAX_STREAM_MAX_RETRIES)
    }

    /// Effective idle timeout for streaming responses.
    pub fn stream_idle_timeout(&self) -> Duration {
        self.stream_idle_timeout_ms
            .map(Duration::from_millis)
            .unwrap_or(Duration::from_millis(DEFAULT_STREAM_IDLE_TIMEOUT_MS))
    }

    /// Effective timeout for websocket connect attempts.
    pub fn websocket_connect_timeout(&self) -> Duration {
        self.websocket_connect_timeout_ms
            .map(Duration::from_millis)
            .unwrap_or(Duration::from_millis(DEFAULT_WEBSOCKET_CONNECT_TIMEOUT_MS))
    }

    pub fn create_openai_provider(base_url: Option<String>) -> ModelProviderInfo {
        ModelProviderInfo {
            name: OPENAI_PROVIDER_NAME.into(),
            base_url,
            env_key: None,
            env_key_instructions: None,
            experimental_bearer_token: None,
            auth: None,
            wire_api: WireApi::Responses,
            query_params: None,
            http_headers: Some(
                [("version".to_string(), env!("CARGO_PKG_VERSION").to_string())]
                    .into_iter()
                    .collect(),
            ),
            env_http_headers: Some(
                [
                    (
                        "OpenAI-Organization".to_string(),
                        "OPENAI_ORGANIZATION".to_string(),
                    ),
                    ("OpenAI-Project".to_string(), "OPENAI_PROJECT".to_string()),
                ]
                .into_iter()
                .collect(),
            ),
            // Use global defaults for retry/timeout unless overridden in config.toml.
            request_max_retries: None,
            stream_max_retries: None,
            stream_idle_timeout_ms: None,
            websocket_connect_timeout_ms: None,
            requires_openai_auth: true,
            supports_websockets: true,
        }
    }

    pub fn is_openai(&self) -> bool {
        self.name == OPENAI_PROVIDER_NAME
    }

    pub fn has_command_auth(&self) -> bool {
        self.auth.is_some()
    }

    pub fn uses_mistral_api(&self) -> bool {
        self.name.eq_ignore_ascii_case("mistral") || self.base_url_host_matches(MISTRAL_API_HOST)
    }

    pub fn uses_gemini_api(&self) -> bool {
        self.name.eq_ignore_ascii_case(GEMINI_PROVIDER_NAME)
            || self.base_url_host_matches(GEMINI_API_HOST)
    }

    pub fn uses_openai_responses_api(&self) -> bool {
        if self.uses_mistral_api() {
            return false;
        }

        self.is_openai()
            || self.requires_openai_auth
            || self.base_url.as_deref().is_some_and(|base_url| {
                base_url.contains("api.openai.com")
                    || codex_api::is_azure_responses_wire_base_url(&self.name, Some(base_url))
            })
    }

    pub fn supports_reasoning_controls(&self) -> bool {
        self.uses_gemini_api() || self.uses_mistral_api()
    }

    pub fn supports_chat_completions_reasoning_effort(&self) -> bool {
        self.uses_gemini_api() || self.uses_mistral_api()
    }

    pub fn model_supports_chat_completions_reasoning_effort(&self, model: &str) -> bool {
        if self.uses_gemini_api() {
            return false;
        }

        if self.uses_mistral_api() {
            let tail = provider_model_tail(model);
            return tail.starts_with("mistral-small")
                || tail == "mistral-vibe-cli-fast"
                || tail.starts_with("magistral-")
                || tail.starts_with("labs-leanstral-");
        }

        self.supports_chat_completions_reasoning_effort()
    }

    pub fn model_supports_parallel_tool_calls(&self, model: &str) -> bool {
        let tail = provider_model_tail(model);

        if self.uses_gemini_api() {
            return tail.starts_with("gemini-");
        }

        if self.uses_mistral_api() {
            return mistral_model_family_supports_tool_use(tail.as_str());
        }

        false
    }

    pub fn model_supports_search_tool(&self, model: &str) -> bool {
        self.model_supports_parallel_tool_calls(model)
    }

    pub fn effective_wire_api(&self) -> WireApi {
        match self.wire_api {
            WireApi::ChatCompletions => WireApi::ChatCompletions,
            WireApi::Responses => {
                if self.uses_openai_responses_api() {
                    WireApi::Responses
                } else {
                    WireApi::ChatCompletions
                }
            }
        }
    }

    pub fn repair_legacy_compatibility(&mut self) -> bool {
        if !self.uses_mistral_api() {
            return false;
        }

        let mut changed = false;
        if self.wire_api != WireApi::ChatCompletions {
            self.wire_api = WireApi::ChatCompletions;
            changed = true;
        }
        if self.requires_openai_auth {
            self.requires_openai_auth = false;
            changed = true;
        }
        if self.supports_websockets {
            self.supports_websockets = false;
            changed = true;
        }

        changed
    }
}

fn provider_model_tail(model: &str) -> String {
    canonicalize_provider_model_slug(model)
        .unwrap_or_else(|| model.trim().to_string())
        .rsplit('/')
        .next()
        .unwrap_or(model.trim())
        .to_ascii_lowercase()
}

fn mistral_model_family_supports_tool_use(tail: &str) -> bool {
    [
        "codestral-",
        "devstral-",
        "magistral-",
        "mamba-codestral-",
        "ministral-",
        "mistral-",
        "mixtral-",
        "open-codestral-",
        "open-mistral-",
        "pixtral-",
        "voxtral-",
    ]
    .iter()
    .any(|prefix| tail.starts_with(prefix))
}

pub const DEFAULT_LMSTUDIO_PORT: u16 = 1234;
pub const DEFAULT_OLLAMA_PORT: u16 = 11434;

pub const LMSTUDIO_OSS_PROVIDER_ID: &str = "lmstudio";
pub const OLLAMA_OSS_PROVIDER_ID: &str = "ollama";

/// Built-in default provider list.
pub fn built_in_model_providers(
    openai_base_url: Option<String>,
) -> HashMap<String, ModelProviderInfo> {
    use ModelProviderInfo as P;
    let openai_provider = P::create_openai_provider(openai_base_url);

    // We do not want to be in the business of adjucating which third-party
    // providers are bundled with Codex CLI, so we only include the OpenAI and
    // open source ("oss") providers by default. Users are encouraged to add to
    // `model_providers` in config.toml to add their own providers.
    [
        (OPENAI_PROVIDER_ID, openai_provider),
        (
            OLLAMA_OSS_PROVIDER_ID,
            create_oss_provider(DEFAULT_OLLAMA_PORT, WireApi::Responses),
        ),
        (
            LMSTUDIO_OSS_PROVIDER_ID,
            create_oss_provider(DEFAULT_LMSTUDIO_PORT, WireApi::Responses),
        ),
    ]
    .into_iter()
    .map(|(k, v)| (k.to_string(), v))
    .collect()
}

pub fn create_oss_provider(default_provider_port: u16, wire_api: WireApi) -> ModelProviderInfo {
    // These CODEX_OSS_ environment variables are experimental: we may
    // switch to reading values from config.toml instead.
    let default_codex_oss_base_url = format!(
        "http://localhost:{codex_oss_port}/v1",
        codex_oss_port = std::env::var("CODEX_OSS_PORT")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .and_then(|value| value.parse::<u16>().ok())
            .unwrap_or(default_provider_port)
    );

    let codex_oss_base_url = std::env::var("CODEX_OSS_BASE_URL")
        .ok()
        .filter(|v| !v.trim().is_empty())
        .unwrap_or(default_codex_oss_base_url);
    create_oss_provider_with_base_url(&codex_oss_base_url, wire_api)
}

pub fn create_oss_provider_with_base_url(base_url: &str, wire_api: WireApi) -> ModelProviderInfo {
    ModelProviderInfo {
        name: "gpt-oss".into(),
        base_url: Some(base_url.into()),
        env_key: None,
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api,
        query_params: None,
        http_headers: None,
        env_http_headers: None,
        request_max_retries: None,
        stream_max_retries: None,
        stream_idle_timeout_ms: None,
        websocket_connect_timeout_ms: None,
        requires_openai_auth: false,
        supports_websockets: false,
    }
}

#[cfg(test)]
#[path = "model_provider_info_tests.rs"]
mod tests;
