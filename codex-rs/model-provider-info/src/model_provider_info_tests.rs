use super::*;
use codex_utils_absolute_path::AbsolutePathBuf;
use codex_utils_absolute_path::AbsolutePathBufGuard;
use pretty_assertions::assert_eq;
use std::num::NonZeroU64;
use tempfile::tempdir;

#[test]
fn test_deserialize_ollama_model_provider_toml() {
    let azure_provider_toml = r#"
name = "Ollama"
base_url = "http://localhost:11434/v1"
        "#;
    let expected_provider = ModelProviderInfo {
        name: "Ollama".into(),
        base_url: Some("http://localhost:11434/v1".into()),
        env_key: None,
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api: WireApi::Responses,
        query_params: None,
        http_headers: None,
        env_http_headers: None,
        request_max_retries: None,
        stream_max_retries: None,
        stream_idle_timeout_ms: None,
        websocket_connect_timeout_ms: None,
        requires_openai_auth: false,
        supports_websockets: false,
    };

    let provider: ModelProviderInfo = toml::from_str(azure_provider_toml).unwrap();
    assert_eq!(expected_provider, provider);
}

#[test]
fn test_deserialize_azure_model_provider_toml() {
    let azure_provider_toml = r#"
name = "Azure"
base_url = "https://xxxxx.openai.azure.com/openai"
env_key = "AZURE_OPENAI_API_KEY"
query_params = { api-version = "2025-04-01-preview" }
        "#;
    let expected_provider = ModelProviderInfo {
        name: "Azure".into(),
        base_url: Some("https://xxxxx.openai.azure.com/openai".into()),
        env_key: Some("AZURE_OPENAI_API_KEY".into()),
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api: WireApi::Responses,
        query_params: Some(maplit::hashmap! {
            "api-version".to_string() => "2025-04-01-preview".to_string(),
        }),
        http_headers: None,
        env_http_headers: None,
        request_max_retries: None,
        stream_max_retries: None,
        stream_idle_timeout_ms: None,
        websocket_connect_timeout_ms: None,
        requires_openai_auth: false,
        supports_websockets: false,
    };

    let provider: ModelProviderInfo = toml::from_str(azure_provider_toml).unwrap();
    assert_eq!(expected_provider, provider);
}

#[test]
fn test_deserialize_example_model_provider_toml() {
    let azure_provider_toml = r#"
name = "Example"
base_url = "https://example.com"
env_key = "API_KEY"
http_headers = { "X-Example-Header" = "example-value" }
env_http_headers = { "X-Example-Env-Header" = "EXAMPLE_ENV_VAR" }
        "#;
    let expected_provider = ModelProviderInfo {
        name: "Example".into(),
        base_url: Some("https://example.com".into()),
        env_key: Some("API_KEY".into()),
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api: WireApi::Responses,
        query_params: None,
        http_headers: Some(maplit::hashmap! {
            "X-Example-Header".to_string() => "example-value".to_string(),
        }),
        env_http_headers: Some(maplit::hashmap! {
            "X-Example-Env-Header".to_string() => "EXAMPLE_ENV_VAR".to_string(),
        }),
        request_max_retries: None,
        stream_max_retries: None,
        stream_idle_timeout_ms: None,
        websocket_connect_timeout_ms: None,
        requires_openai_auth: false,
        supports_websockets: false,
    };

    let provider: ModelProviderInfo = toml::from_str(azure_provider_toml).unwrap();
    assert_eq!(expected_provider, provider);
}

#[test]
fn test_deserialize_chat_wire_api_shows_helpful_error() {
    let provider_toml = r#"
name = "OpenAI using Chat Completions"
base_url = "https://api.openai.com/v1"
env_key = "OPENAI_API_KEY"
wire_api = "chat"
        "#;

    let err = toml::from_str::<ModelProviderInfo>(provider_toml).unwrap_err();
    assert!(err.to_string().contains(CHAT_WIRE_API_REMOVED_ERROR));
}

#[test]
fn test_deserialize_chat_completions_wire_api() {
    let provider_toml = r#"
name = "Mistral"
base_url = "https://api.mistral.ai/v1"
wire_api = "chat_completions"
        "#;

    let provider: ModelProviderInfo = toml::from_str(provider_toml).unwrap();
    assert_eq!(provider.wire_api, WireApi::ChatCompletions);
}

#[test]
fn mistral_base_url_wins_over_openai_name_for_effective_wire_api() {
    let provider =
        ModelProviderInfo::create_openai_provider(Some("https://api.mistral.ai/v1".to_string()));

    assert_eq!(provider.effective_wire_api(), WireApi::ChatCompletions);
}

#[test]
fn repair_legacy_compatibility_rewrites_stale_mistral_provider_state() {
    let mut provider =
        ModelProviderInfo::create_openai_provider(Some("https://api.mistral.ai/v1".to_string()));
    provider.supports_websockets = true;

    assert!(provider.repair_legacy_compatibility());
    assert_eq!(provider.wire_api, WireApi::ChatCompletions);
    assert!(!provider.requires_openai_auth);
    assert!(!provider.supports_websockets);
}

#[test]
fn mistral_provider_supports_reasoning_controls() {
    let provider =
        ModelProviderInfo::create_openai_provider(Some("https://api.mistral.ai/v1".to_string()));

    assert!(provider.supports_reasoning_controls());
    assert!(provider.supports_chat_completions_reasoning_effort());
}

#[test]
fn mistral_model_reasoning_effort_support_is_model_specific() {
    let provider =
        ModelProviderInfo::create_openai_provider(Some("https://api.mistral.ai/v1".to_string()));

    assert!(!provider.model_supports_chat_completions_reasoning_effort("mistral-medium-latest"));
    assert!(!provider.model_supports_chat_completions_reasoning_effort(
        "mistral-vibe-cli-with-tools"
    ));
    assert!(provider.model_supports_chat_completions_reasoning_effort("mistral-small-latest"));
    assert!(provider.model_supports_chat_completions_reasoning_effort("mistral-vibe-cli-fast"));
}

#[test]
fn mistral_model_tool_support_is_model_specific() {
    let provider =
        ModelProviderInfo::create_openai_provider(Some("https://api.mistral.ai/v1".to_string()));

    assert!(provider.model_supports_parallel_tool_calls("devstral-latest"));
    assert!(provider.model_supports_parallel_tool_calls("pixtral-large-latest"));
    assert!(provider.model_supports_search_tool("mistral-medium-latest"));
}

#[test]
fn gemini_model_reasoning_effort_support_stays_disabled() {
    let provider = ModelProviderInfo::create_openai_provider(Some(
        "https://generativelanguage.googleapis.com/v1beta/openai".to_string(),
    ));

    assert!(!provider.supports_chat_completions_reasoning_effort());
    assert!(!provider.model_supports_chat_completions_reasoning_effort("gemini-2.5-pro"));
}

#[test]
fn gemini_model_tool_support_stays_enabled() {
    let provider = ModelProviderInfo::create_openai_provider(Some(
        "https://generativelanguage.googleapis.com/v1beta/openai".to_string(),
    ));

    assert!(provider.model_supports_parallel_tool_calls("gemini-2.5-flash"));
    assert!(provider.model_supports_search_tool("gemini-2.5-pro"));
}

#[test]
fn gemini_host_detection_uses_parsed_host() {
    let provider = ModelProviderInfo::create_openai_provider(Some(
        "https://generativelanguage.googleapis.com/v1beta/openai".to_string(),
    ));

    assert!(provider.uses_gemini_api());
}

#[test]
fn canonicalize_provider_model_slug_repairs_mistral_tool_alias() {
    assert_eq!(
        canonicalize_provider_model_slug("mistral-vibe-cli-with-tools"),
        Some("mistral-medium-latest".to_string())
    );
}

#[test]
fn canonicalize_provider_model_slug_preserves_namespace() {
    assert_eq!(
        canonicalize_provider_model_slug("mistral/mistral-vibe-cli-fast"),
        Some("mistral/mistral-small-latest".to_string())
    );
}

#[test]
fn test_deserialize_websocket_connect_timeout() {
    let provider_toml = r#"
name = "OpenAI"
base_url = "https://api.openai.com/v1"
websocket_connect_timeout_ms = 15000
supports_websockets = true
        "#;

    let provider: ModelProviderInfo = toml::from_str(provider_toml).unwrap();
    assert_eq!(provider.websocket_connect_timeout_ms, Some(15_000));
}

#[test]
fn test_deserialize_provider_auth_config_defaults() {
    let base_dir = tempdir().unwrap();
    let provider_toml = r#"
name = "Corp"

[auth]
command = "./scripts/print-token"
args = ["--format=text"]
        "#;

    let provider: ModelProviderInfo = {
        let _guard = AbsolutePathBufGuard::new(base_dir.path());
        toml::from_str(provider_toml).unwrap()
    };

    assert_eq!(
        provider.auth,
        Some(ModelProviderAuthInfo {
            command: "./scripts/print-token".to_string(),
            args: vec!["--format=text".to_string()],
            timeout_ms: NonZeroU64::new(5_000).unwrap(),
            refresh_interval_ms: 300_000,
            cwd: AbsolutePathBuf::resolve_path_against_base(".", base_dir.path()).unwrap(),
        })
    );
}

#[test]
fn test_deserialize_provider_auth_config_allows_zero_refresh_interval() {
    let base_dir = tempdir().unwrap();
    let provider_toml = r#"
name = "Corp"

[auth]
command = "./scripts/print-token"
refresh_interval_ms = 0
        "#;

    let provider: ModelProviderInfo = {
        let _guard = AbsolutePathBufGuard::new(base_dir.path());
        toml::from_str(provider_toml).unwrap()
    };

    let auth = provider.auth.expect("auth config should deserialize");
    assert_eq!(auth.refresh_interval_ms, 0);
    assert_eq!(auth.refresh_interval(), None);
}
