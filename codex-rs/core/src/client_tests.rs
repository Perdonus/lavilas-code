use super::AuthRequestTelemetryContext;
use super::ModelClient;
use super::PendingUnauthorizedRetry;
use super::UnauthorizedRecoveryExecution;
use super::X_CODEX_PARENT_THREAD_ID_HEADER;
use super::X_CODEX_TURN_METADATA_HEADER;
use super::X_CODEX_WINDOW_ID_HEADER;
use super::X_OPENAI_SUBAGENT_HEADER;
use super::build_chat_completions_messages;
use super::effective_wire_api;
use super::normalize_request_model_for_provider;
use codex_api::api_bridge::CoreAuthProvider;
use codex_app_server_protocol::AuthMode;
use codex_model_provider_info::WireApi;
use codex_model_provider_info::create_oss_provider_with_base_url;
use codex_otel::SessionTelemetry;
use codex_protocol::ThreadId;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::protocol::SessionSource;
use codex_protocol::protocol::SubAgentSource;
use pretty_assertions::assert_eq;
use serde_json::json;
use std::sync::atomic::Ordering;

fn test_model_client(session_source: SessionSource) -> ModelClient {
    let provider = create_oss_provider_with_base_url("https://example.com/v1", WireApi::Responses);
    ModelClient::new(
        /*auth_manager*/ None,
        ThreadId::new(),
        provider,
        session_source,
        /*model_verbosity*/ None,
        /*enable_request_compression*/ false,
        /*include_timing_metrics*/ false,
        /*beta_features_header*/ None,
    )
}

fn test_model_info() -> ModelInfo {
    serde_json::from_value(json!({
        "slug": "gpt-test",
        "display_name": "gpt-test",
        "description": "desc",
        "default_reasoning_level": "medium",
        "supported_reasoning_levels": [
            {"effort": "medium", "description": "medium"}
        ],
        "shell_type": "shell_command",
        "visibility": "list",
        "supported_in_api": true,
        "priority": 1,
        "upgrade": null,
        "base_instructions": "base instructions",
        "model_messages": null,
        "supports_reasoning_summaries": false,
        "support_verbosity": false,
        "default_verbosity": null,
        "apply_patch_tool_type": null,
        "truncation_policy": {"mode": "bytes", "limit": 10000},
        "supports_parallel_tool_calls": false,
        "supports_image_detail_original": false,
        "context_window": 272000,
        "auto_compact_token_limit": null,
        "experimental_supported_tools": []
    }))
    .expect("deserialize test model info")
}

fn test_session_telemetry() -> SessionTelemetry {
    SessionTelemetry::new(
        ThreadId::new(),
        "gpt-test",
        "gpt-test",
        /*account_id*/ None,
        /*account_email*/ None,
        /*auth_mode*/ None,
        "test-originator".to_string(),
        /*log_user_prompts*/ false,
        "test-terminal".to_string(),
        SessionSource::Cli,
    )
}

#[test]
fn build_subagent_headers_sets_other_subagent_label() {
    let client = test_model_client(SessionSource::SubAgent(SubAgentSource::Other(
        "memory_consolidation".to_string(),
    )));
    let headers = client.build_subagent_headers();
    let value = headers
        .get(X_OPENAI_SUBAGENT_HEADER)
        .and_then(|value| value.to_str().ok());
    assert_eq!(value, Some("memory_consolidation"));
}

#[test]
fn build_ws_client_metadata_includes_window_lineage_and_turn_metadata() {
    let parent_thread_id = ThreadId::new();
    let client = test_model_client(SessionSource::SubAgent(SubAgentSource::ThreadSpawn {
        parent_thread_id,
        depth: 2,
        agent_path: None,
        agent_nickname: None,
        agent_role: None,
    }));

    client.advance_window_generation();

    let client_metadata = client.build_ws_client_metadata(Some(r#"{"turn_id":"turn-123"}"#));
    let conversation_id = client.state.conversation_id;
    assert_eq!(
        client_metadata,
        std::collections::HashMap::from([
            (
                X_CODEX_WINDOW_ID_HEADER.to_string(),
                format!("{conversation_id}:1"),
            ),
            (
                X_OPENAI_SUBAGENT_HEADER.to_string(),
                "collab_spawn".to_string(),
            ),
            (
                X_CODEX_PARENT_THREAD_ID_HEADER.to_string(),
                parent_thread_id.to_string(),
            ),
            (
                X_CODEX_TURN_METADATA_HEADER.to_string(),
                r#"{"turn_id":"turn-123"}"#.to_string(),
            ),
        ])
    );
}

#[tokio::test]
async fn summarize_memories_returns_empty_for_empty_input() {
    let client = test_model_client(SessionSource::Cli);
    let model_info = test_model_info();
    let session_telemetry = test_session_telemetry();

    let output = client
        .summarize_memories(
            Vec::new(),
            &model_info,
            /*effort*/ None,
            &session_telemetry,
        )
        .await
        .expect("empty summarize request should succeed");
    assert_eq!(output.len(), 0);
}

#[test]
fn auth_request_telemetry_context_tracks_attached_auth_and_retry_phase() {
    let auth_context = AuthRequestTelemetryContext::new(
        Some(AuthMode::Chatgpt),
        &CoreAuthProvider::for_test(Some("access-token"), Some("workspace-123")),
        PendingUnauthorizedRetry::from_recovery(UnauthorizedRecoveryExecution {
            mode: "managed",
            phase: "refresh_token",
        }),
    );

    assert_eq!(auth_context.auth_mode, Some("Chatgpt"));
    assert!(auth_context.auth_header_attached);
    assert_eq!(auth_context.auth_header_name, Some("authorization"));
    assert!(auth_context.retry_after_unauthorized);
    assert_eq!(auth_context.recovery_mode, Some("managed"));
    assert_eq!(auth_context.recovery_phase, Some("refresh_token"));
}

#[test]
fn effective_wire_api_keeps_openai_responses_provider() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::Responses);

    assert_eq!(effective_wire_api(&provider), WireApi::Responses);
}

#[test]
fn effective_wire_api_upgrades_mistral_responses_provider_to_chat_completions() {
    let provider =
        create_oss_provider_with_base_url("https://api.mistral.ai/v1", WireApi::Responses);

    assert_eq!(effective_wire_api(&provider), WireApi::ChatCompletions);
}

#[test]
fn normalize_request_model_maps_legacy_mistral_tool_alias() {
    let provider =
        create_oss_provider_with_base_url("https://api.mistral.ai/v1", WireApi::Responses);

    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli-with-tools").as_ref(),
        "mistral-large-latest"
    );
    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli-fast").as_ref(),
        "mistral-large-latest"
    );
}

#[test]
fn build_chat_completions_messages_maps_developer_role_to_system() {
    let messages = build_chat_completions_messages(
        "system prompt",
        &[ResponseItem::Message {
            id: None,
            role: "developer".to_string(),
            content: vec![ContentItem::InputText {
                text: "developer prompt".to_string(),
            }],
            end_turn: None,
            phase: None,
        }],
    )
    .expect("chat completions messages");

    assert_eq!(messages.len(), 2);
    assert_eq!(messages[0].role, "system");
    assert_eq!(messages[1].role, "system");
}

#[test]
fn reconfigure_updates_runtime_provider_and_resets_transport_fallback_state() {
    let client = test_model_client(SessionSource::Cli);
    client
        .state
        .disable_websockets
        .store(true, Ordering::Relaxed);

    let mistral_provider =
        create_oss_provider_with_base_url("https://api.mistral.ai/v1", WireApi::Responses);
    client.reconfigure(
        /*auth_manager*/ None,
        mistral_provider,
        SessionSource::SubAgent(SubAgentSource::Other("runtime-reload".to_string())),
        /*model_verbosity*/ None,
        /*enable_request_compression*/ false,
        /*include_timing_metrics*/ false,
        /*beta_features_header*/ None,
    );

    let runtime_config = client.runtime_config();
    assert_eq!(
        effective_wire_api(&runtime_config.provider),
        WireApi::ChatCompletions
    );
    let headers = client.build_subagent_headers();
    assert_eq!(
        headers
            .get(X_OPENAI_SUBAGENT_HEADER)
            .and_then(|value| value.to_str().ok()),
        Some("runtime-reload")
    );
    assert!(!client.state.disable_websockets.load(Ordering::Relaxed));
}
