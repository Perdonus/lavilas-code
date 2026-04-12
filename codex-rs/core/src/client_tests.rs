use super::AuthRequestTelemetryContext;
use super::ChatCompletionCalledFunction;
use super::ChatCompletionGoogleExtraContent;
use super::ChatCompletionToolCall;
use super::ChatCompletionToolCallExtraContent;
use super::ChatCompletionsMessage;
use super::ChatCompletionsResponse;
use super::ModelClient;
use super::PendingUnauthorizedRetry;
use super::UnauthorizedRecoveryExecution;
use super::X_CODEX_PARENT_THREAD_ID_HEADER;
use super::X_CODEX_TURN_METADATA_HEADER;
use super::X_CODEX_WINDOW_ID_HEADER;
use super::X_OPENAI_SUBAGENT_HEADER;
use super::build_chat_completions_messages;
use super::chat_completions_response_to_events;
use super::chat_completions_tool_call_response_item;
use super::effective_wire_api;
use super::encode_google_thought_signature;
use super::normalize_request_model_for_provider;
use codex_api::api_bridge::CoreAuthProvider;
use codex_app_server_protocol::AuthMode;
use codex_model_provider_info::ModelProviderInfo;
use codex_model_provider_info::WireApi;
use codex_model_provider_info::create_oss_provider_with_base_url;
use codex_otel::SessionTelemetry;
use codex_protocol::ThreadId;
use codex_protocol::models::BaseInstructions;
use codex_protocol::models::ContentItem;
use codex_protocol::models::ResponseItem;
use codex_protocol::openai_models::ModelInfo;
use codex_protocol::openai_models::ReasoningEffortPreset;
use codex_protocol::protocol::SessionSource;
use codex_protocol::protocol::SubAgentSource;
use pretty_assertions::assert_eq;
use serde_json::Value;
use serde_json::json;
use std::collections::HashSet;
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

fn test_model_client_with_provider(provider: ModelProviderInfo) -> ModelClient {
    ModelClient::new(
        /*auth_manager*/ None,
        ThreadId::new(),
        provider,
        SessionSource::Cli,
        /*model_verbosity*/ None,
        /*enable_request_compression*/ false,
        /*include_timing_metrics*/ false,
        /*beta_features_header*/ None,
    )
}

fn gemini_provider() -> ModelProviderInfo {
    ModelProviderInfo {
        name: "Gemini".to_string(),
        base_url: Some("https://generativelanguage.googleapis.com/v1beta/openai".to_string()),
        env_key: None,
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api: WireApi::ChatCompletions,
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

fn mistral_provider() -> ModelProviderInfo {
    ModelProviderInfo {
        name: "Mistral".to_string(),
        base_url: Some("https://api.mistral.ai/v1".to_string()),
        env_key: None,
        env_key_instructions: None,
        experimental_bearer_token: None,
        auth: None,
        wire_api: WireApi::ChatCompletions,
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
fn effective_wire_api_prioritizes_mistral_base_url_over_legacy_openai_flags() {
    let provider = codex_model_provider_info::ModelProviderInfo::create_openai_provider(Some(
        "https://api.mistral.ai/v1".to_string(),
    ));

    assert_eq!(effective_wire_api(&provider), WireApi::ChatCompletions);
}

#[test]
fn normalize_request_model_repairs_mistral_aliases() {
    let provider =
        create_oss_provider_with_base_url("https://api.mistral.ai/v1", WireApi::Responses);

    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli").as_ref(),
        "mistral-medium-latest"
    );
    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli-with-tools").as_ref(),
        "mistral-medium-latest"
    );
    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli-fast").as_ref(),
        "mistral-small-latest"
    );
    assert_eq!(
        normalize_request_model_for_provider(&provider, "mistral-vibe-cli-latest").as_ref(),
        "mistral-medium-latest"
    );
}

#[test]
fn normalize_request_model_strips_gemini_models_prefix() {
    let provider = gemini_provider();

    assert_eq!(
        normalize_request_model_for_provider(&provider, "models/gemini-2.5-flash").as_ref(),
        "gemini-2.5-flash"
    );
    assert_eq!(
        normalize_request_model_for_provider(&provider, "gemini-flash-latest").as_ref(),
        "gemini-2.5-flash"
    );
    assert!(provider.supports_chat_completions_reasoning_effort());
    assert!(provider.supports_reasoning_controls());
}

#[test]
fn build_chat_completions_request_includes_reasoning_effort_for_gemini() {
    let client = test_model_client_with_provider(gemini_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![super::ResponseItem::Message {
            id: None,
            role: "user".to_string(),
            content: vec![super::ContentItem::InputText {
                text: "hello <thought>hidden</thought> world".to_string(),
            }],
            end_turn: None,
            phase: None,
        }],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let model_info = test_model_info();
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::High),
        )
        .expect("chat completions request");

    assert_eq!(
        request.reasoning_effort,
        Some(codex_protocol::openai_models::ReasoningEffort::High)
    );
    assert_eq!(request.messages.len(), 1);
    assert_eq!(
        request.messages[0].content,
        Some(Value::String("hello  world".to_string()))
    );
}

#[test]
fn build_chat_completions_request_omits_reasoning_effort_for_gemini_without_metadata() {
    let client = test_model_client_with_provider(gemini_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![super::ResponseItem::Message {
            id: None,
            role: "user".to_string(),
            content: vec![super::ContentItem::InputText {
                text: "hello".to_string(),
            }],
            end_turn: None,
            phase: None,
        }],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let mut model_info = test_model_info();
    model_info.default_reasoning_level = None;
    model_info.supported_reasoning_levels.clear();
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::High),
        )
        .expect("chat completions request");

    assert_eq!(request.reasoning_effort, None);
}

#[test]
fn build_chat_completions_request_maps_reasoning_effort_for_mistral() {
    let client = test_model_client_with_provider(mistral_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let mut model_info = test_model_info();
    model_info.default_reasoning_level = Some(codex_protocol::openai_models::ReasoningEffort::High);
    model_info.supported_reasoning_levels = vec![ReasoningEffortPreset {
        effort: codex_protocol::openai_models::ReasoningEffort::High,
        description: "high".to_string(),
    }];
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::XHigh),
        )
        .expect("chat completions request");

    assert_eq!(
        request.reasoning_effort,
        Some(codex_protocol::openai_models::ReasoningEffort::High)
    );
}

#[test]
fn build_chat_completions_request_omits_reasoning_effort_for_mistral_medium_with_stale_metadata() {
    let client = test_model_client_with_provider(mistral_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let mut model_info = test_model_info();
    model_info.slug = "mistral-medium-latest".to_string();
    model_info.default_reasoning_level = Some(codex_protocol::openai_models::ReasoningEffort::High);
    model_info.supported_reasoning_levels = vec![ReasoningEffortPreset {
        effort: codex_protocol::openai_models::ReasoningEffort::High,
        description: "high".to_string(),
    }];
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::High),
        )
        .expect("chat completions request");

    assert_eq!(request.reasoning_effort, None);
}

#[test]
fn build_chat_completions_request_keeps_reasoning_effort_for_mistral_small_models() {
    let client = test_model_client_with_provider(mistral_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let mut model_info = test_model_info();
    model_info.slug = "mistral-small-latest".to_string();
    model_info.default_reasoning_level = Some(codex_protocol::openai_models::ReasoningEffort::None);
    model_info.supported_reasoning_levels = vec![
        ReasoningEffortPreset {
            effort: codex_protocol::openai_models::ReasoningEffort::None,
            description: "none".to_string(),
        },
        ReasoningEffortPreset {
            effort: codex_protocol::openai_models::ReasoningEffort::High,
            description: "high".to_string(),
        },
    ];
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::High),
        )
        .expect("chat completions request");

    assert_eq!(
        request.reasoning_effort,
        Some(codex_protocol::openai_models::ReasoningEffort::High)
    );
}

#[test]
fn build_chat_completions_request_omits_reasoning_effort_for_mistral_without_metadata() {
    let client = test_model_client_with_provider(mistral_provider());
    let prompt = super::Prompt {
        base_instructions: BaseInstructions {
            text: "".to_string(),
        },
        input: vec![],
        tools: vec![],
        parallel_tool_calls: false,
        personality: None,
        output_schema: None,
    };
    let model_info = test_model_info();
    let request = client
        .new_session()
        .build_chat_completions_request(
            &prompt,
            &model_info,
            Some(codex_protocol::openai_models::ReasoningEffort::High),
        )
        .expect("chat completions request");

    assert_eq!(request.reasoning_effort, None);
}

#[test]
fn chat_completions_response_strips_hidden_reasoning_tags() {
    let response = ChatCompletionsResponse {
        id: Some("resp-1".to_string()),
        model: Some("models/gemini-2.5-flash".to_string()),
        choices: vec![super::ChatCompletionChoice {
            message: ChatCompletionsMessage::text(
                "assistant",
                "A<thought>hidden</thought>B".to_string(),
            ),
        }],
        usage: None,
    };

    let events = chat_completions_response_to_events(response).expect("events");
    let text = events
        .into_iter()
        .find_map(|event| match event {
            super::ResponseEvent::OutputItemDone(super::ResponseItem::Message {
                content, ..
            }) => content.into_iter().find_map(|content| match content {
                super::ContentItem::OutputText { text } => Some(text),
                _ => None,
            }),
            _ => None,
        })
        .expect("assistant text");

    assert_eq!(text, "AB");
}

#[test]
fn build_chat_completions_messages_strips_hidden_reasoning_tags_from_replay() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let messages = build_chat_completions_messages(
        &provider,
        "system prompt",
        &[super::ResponseItem::Message {
            id: None,
            role: "assistant".to_string(),
            content: vec![super::ContentItem::InputText {
                text: "before<thought>hidden</thought>after".to_string(),
            }],
            end_turn: None,
            phase: None,
        }],
    )
    .expect("chat completions messages");

    assert_eq!(messages.len(), 2);
    assert_eq!(
        messages[1].content,
        Some(Value::String("beforeafter".to_string()))
    );
}

#[test]
fn build_chat_completions_messages_maps_developer_role_to_system() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let messages = build_chat_completions_messages(
        &provider,
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

    assert_eq!(messages.len(), 1);
    assert_eq!(messages[0].role, "system");
    assert_eq!(
        messages[0].content,
        Some(json!("system prompt\n\ndeveloper prompt"))
    );
}

#[test]
fn build_chat_completions_messages_maps_developer_role_to_system_for_mistral() {
    let provider = mistral_provider();
    let messages = build_chat_completions_messages(
        &provider,
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

    assert_eq!(messages.len(), 1);
    assert_eq!(messages[0].role, "system");
    assert_eq!(
        messages[0].content,
        Some(json!("system prompt\n\ndeveloper prompt"))
    );
}

#[test]
fn build_chat_completions_messages_hoists_mistral_tool_history_instructions_to_leading_system() {
    let provider = mistral_provider();
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "run it".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::FunctionCall {
                id: None,
                name: "shell".to_string(),
                namespace: None,
                arguments: "{}".to_string(),
                call_id: "call-1".to_string(),
            },
            ResponseItem::FunctionCallOutput {
                call_id: "call-1".to_string(),
                output: codex_protocol::models::FunctionCallOutputPayload::from_text(
                    "ok".to_string(),
                ),
            },
            ResponseItem::Message {
                id: None,
                role: "developer".to_string(),
                content: vec![ContentItem::InputText {
                    text: "keep it terse".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
        ],
    )
    .expect("chat completions messages");

    assert_eq!(messages[0].role, "system");
    assert_eq!(messages[0].content, Some(json!("keep it terse")));
    assert_eq!(messages[1].role, "user");
    assert_eq!(messages[2].role, "assistant");
    assert_eq!(messages[3].role, "tool");
}

#[test]
fn build_chat_completions_messages_preserves_mistral_user_turn_boundaries() {
    let provider = mistral_provider();
    let messages = build_chat_completions_messages(
        &provider,
        "system prompt",
        &[
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "first ask".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "assistant".to_string(),
                content: vec![ContentItem::OutputText {
                    text: "first answer".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "second ask".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "assistant".to_string(),
                content: vec![ContentItem::OutputText {
                    text: "second answer".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "third ask".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
        ],
    )
    .expect("chat completions messages");

    let user_messages = messages
        .iter()
        .filter(|message| message.role == "user")
        .map(|message| message.content.clone().expect("user content"))
        .collect::<Vec<_>>();

    assert_eq!(
        user_messages,
        vec![json!("first ask"), json!("second ask"), json!("third ask"),]
    );
    assert_eq!(
        messages
            .iter()
            .map(|message| message.role.as_str())
            .collect::<Vec<_>>(),
        vec!["system", "user", "assistant", "user", "assistant", "user"]
    );
    assert_eq!(messages[0].content, Some(json!("system prompt")));
}

#[test]
fn build_chat_completions_messages_hoists_trailing_mistral_instructions_to_leading_system() {
    let provider = mistral_provider();
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "run it".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "assistant".to_string(),
                content: vec![ContentItem::InputText {
                    text: "done".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::Message {
                id: None,
                role: "developer".to_string(),
                content: vec![ContentItem::InputText {
                    text: "keep it terse".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
        ],
    )
    .expect("chat completions messages");

    assert_eq!(messages.len(), 3);
    assert_eq!(messages[0].role, "system");
    assert_eq!(messages[0].content, Some(json!("keep it terse")));
    assert_eq!(messages[1].role, "user");
    assert_eq!(messages[2].role, "assistant");
}

#[test]
fn build_chat_completions_messages_sanitizes_mistral_tool_call_ids() {
    let provider = mistral_provider();
    let original_call_id = "call_Smc1D0Q5cb1b6Gh1KASesy74".to_string();
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::FunctionCall {
                id: None,
                name: "shell".to_string(),
                namespace: None,
                arguments: "{}".to_string(),
                call_id: original_call_id.clone(),
            },
            ResponseItem::FunctionCallOutput {
                call_id: original_call_id,
                output: codex_protocol::models::FunctionCallOutputPayload::from_text(
                    "ok".to_string(),
                ),
            },
        ],
    )
    .expect("chat completions messages");

    let tool_call_id = messages[0]
        .tool_calls
        .as_ref()
        .and_then(|tool_calls| tool_calls.first())
        .and_then(|tool_call| tool_call.id.as_deref())
        .expect("assistant tool call id");

    assert_eq!(tool_call_id.len(), 9);
    assert!(tool_call_id.chars().all(|ch| ch.is_ascii_alphanumeric()));
    assert_eq!(messages[1].tool_call_id.as_deref(), Some(tool_call_id));
}

#[test]
fn build_chat_completions_messages_never_emits_system_after_tool() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::Message {
                id: None,
                role: "user".to_string(),
                content: vec![ContentItem::InputText {
                    text: "run it".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
            ResponseItem::FunctionCall {
                id: None,
                name: "shell".to_string(),
                namespace: None,
                arguments: "{}".to_string(),
                call_id: "call-1".to_string(),
            },
            ResponseItem::FunctionCallOutput {
                call_id: "call-1".to_string(),
                output: codex_protocol::models::FunctionCallOutputPayload::from_text(
                    "ok".to_string(),
                ),
            },
            ResponseItem::Message {
                id: None,
                role: "developer".to_string(),
                content: vec![ContentItem::InputText {
                    text: "keep it terse".to_string(),
                }],
                end_turn: None,
                phase: None,
            },
        ],
    )
    .expect("chat completions messages");

    assert_eq!(messages[0].role, "system");
    assert_eq!(messages[1].role, "user");
    assert_eq!(messages[2].role, "assistant");
    assert_eq!(messages[3].role, "tool");
    assert!(
        messages
            .iter()
            .skip(1)
            .all(|message| message.role != "system"),
        "system message must stay ahead of tool messages: {messages:?}"
    );
}

#[test]
fn build_chat_completions_messages_preserves_gemini_thought_signature_for_tool_calls() {
    let provider = gemini_provider();
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[ResponseItem::FunctionCall {
            id: Some(encode_google_thought_signature("sig-123")),
            name: "shell".to_string(),
            namespace: None,
            arguments: "{}".to_string(),
            call_id: "call-1".to_string(),
        }],
    )
    .expect("chat completions messages");

    let tool_call = messages[0]
        .tool_calls
        .as_ref()
        .and_then(|calls| calls.first())
        .expect("tool call");
    assert_eq!(
        tool_call
            .extra_content
            .as_ref()
            .and_then(|extra| extra.google.as_ref())
            .and_then(|google| google.thought_signature.as_deref()),
        Some("sig-123")
    );
}

#[test]
fn build_chat_completions_messages_replays_builtin_provider_tools_as_tool_history() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let action: codex_protocol::models::WebSearchAction = serde_json::from_value(json!({
        "type": "search",
        "query": "weather in kudrovo",
    }))
    .expect("web search action");
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::WebSearchCall {
                id: Some("ws_1".to_string()),
                status: Some("completed".to_string()),
                action: Some(action),
            },
            ResponseItem::ImageGenerationCall {
                id: "ig_1".to_string(),
                status: "completed".to_string(),
                revised_prompt: Some("a landing page hero image".to_string()),
                result: "Zm9v".to_string(),
            },
        ],
    )
    .expect("chat completions messages");

    assert_eq!(messages.len(), 4);
    assert_eq!(
        messages[0]
            .tool_calls
            .as_ref()
            .and_then(|calls| calls.first())
            .map(|call| call.function.name.as_str()),
        Some("web_search")
    );
    assert_eq!(messages[1].role, "tool");
    assert_eq!(messages[1].name.as_deref(), Some("web_search"));
    assert_eq!(
        messages[2]
            .tool_calls
            .as_ref()
            .and_then(|calls| calls.first())
            .map(|call| call.function.name.as_str()),
        Some("image_generation")
    );
    assert_eq!(messages[3].role, "tool");
    assert_eq!(messages[3].name.as_deref(), Some("image_generation"));
    assert!(
        messages[3]
            .content
            .as_ref()
            .and_then(Value::as_str)
            .is_some_and(|text| text.contains("[image generation result omitted during provider translation]"))
    );
}

#[test]
fn build_chat_completions_messages_skips_unavailable_builtin_history_tools_when_known() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let action: codex_protocol::models::WebSearchAction = serde_json::from_value(json!({
        "type": "search",
        "query": "weather in kudrovo",
    }))
    .expect("web search action");
    let available_tool_names = HashSet::from(["local_shell".to_string()]);

    let messages = build_chat_completions_messages_with_tools(
        &provider,
        "",
        &[
            ResponseItem::WebSearchCall {
                id: Some("ws_1".to_string()),
                status: Some("completed".to_string()),
                action: Some(action),
            },
            ResponseItem::ImageGenerationCall {
                id: "ig_1".to_string(),
                status: "completed".to_string(),
                revised_prompt: Some("a landing page hero image".to_string()),
                result: "Zm9v".to_string(),
            },
        ],
        Some(&available_tool_names),
    )
    .expect("chat completions messages");

    assert!(
        messages.is_empty(),
        "unsupported built-in history tools should be omitted for chat completions providers"
    );
}

#[test]
fn build_chat_completions_messages_batches_consecutive_tool_calls_into_one_assistant_turn() {
    let provider =
        create_oss_provider_with_base_url("https://api.openai.com/v1", WireApi::ChatCompletions);
    let messages = build_chat_completions_messages(
        &provider,
        "",
        &[
            ResponseItem::FunctionCall {
                id: None,
                name: "shell".to_string(),
                namespace: None,
                arguments: "{\"command\":\"pwd\"}".to_string(),
                call_id: "call-1".to_string(),
            },
            ResponseItem::CustomToolCall {
                id: None,
                status: None,
                call_id: "call-2".to_string(),
                name: "apply_patch".to_string(),
                input: "*** Begin Patch\n*** End Patch\n".to_string(),
            },
            ResponseItem::FunctionCallOutput {
                call_id: "call-1".to_string(),
                output: codex_protocol::models::FunctionCallOutputPayload::from_text(
                    "/root".to_string(),
                ),
            },
            ResponseItem::CustomToolCallOutput {
                call_id: "call-2".to_string(),
                name: Some("apply_patch".to_string()),
                output: codex_protocol::models::FunctionCallOutputPayload::from_text(
                    "ok".to_string(),
                ),
            },
        ],
    )
    .expect("chat completions messages");

    assert_eq!(messages.len(), 3);
    assert_eq!(messages[0].role, "assistant");
    assert_eq!(
        messages[0]
            .tool_calls
            .as_ref()
            .map(std::vec::Vec::len),
        Some(2)
    );
    assert_eq!(messages[1].role, "tool");
    assert_eq!(messages[2].role, "tool");
}

#[test]
fn chat_completions_tool_call_response_item_restores_custom_tool_calls() {
    let item = chat_completions_tool_call_response_item(
        ChatCompletionToolCall {
            id: Some("call-1".to_string()),
            kind: "function".to_string(),
            function: ChatCompletionCalledFunction {
                name: "apply_patch".to_string(),
                arguments: "{\"input\":\"*** Begin Patch\\n*** End Patch\\n\"}".to_string(),
            },
            extra_content: None,
        },
        0,
        &HashSet::from(["apply_patch".to_string()]),
    );

    assert_eq!(
        item,
        ResponseItem::CustomToolCall {
            id: None,
            status: None,
            call_id: "call-1".to_string(),
            name: "apply_patch".to_string(),
            input: "*** Begin Patch\n*** End Patch\n".to_string(),
        }
    );
}

#[test]
fn chat_completions_response_preserves_gemini_thought_signature_on_tool_calls() {
    let response = ChatCompletionsResponse {
        id: Some("resp-1".to_string()),
        model: Some("gemini-2.5-flash".to_string()),
        choices: vec![super::ChatCompletionChoice {
            message: ChatCompletionsMessage {
                role: "assistant".to_string(),
                content: Some(json!("")),
                name: None,
                tool_call_id: None,
                tool_calls: Some(vec![ChatCompletionToolCall {
                    id: Some("call-1".to_string()),
                    kind: "function".to_string(),
                    function: ChatCompletionCalledFunction {
                        name: "shell".to_string(),
                        arguments: "{}".to_string(),
                    },
                    extra_content: Some(ChatCompletionToolCallExtraContent {
                        google: Some(ChatCompletionGoogleExtraContent {
                            thought_signature: Some("sig-123".to_string()),
                        }),
                    }),
                }]),
                reasoning: None,
                reasoning_details: None,
                reasoning_content: None,
                thinking: None,
                thoughts: None,
            },
        }],
        usage: None,
    };

    let events = chat_completions_response_to_events(response).expect("events");
    let function_call = events
        .into_iter()
        .find_map(|event| match event {
            super::ResponseEvent::OutputItemDone(ResponseItem::FunctionCall { id, .. }) => id,
            _ => None,
        })
        .expect("function call id");

    assert_eq!(function_call, encode_google_thought_signature("sig-123"));
}

#[test]
fn chat_completions_response_emits_reasoning_events_from_structured_content() {
    let response = ChatCompletionsResponse {
        id: Some("resp-1".to_string()),
        model: Some("gemini-2.5-flash".to_string()),
        choices: vec![super::ChatCompletionChoice {
            message: ChatCompletionsMessage {
                role: "assistant".to_string(),
                content: Some(json!([
                    {
                        "type": "thinking",
                        "text": "plan first"
                    },
                    {
                        "type": "text",
                        "text": "final answer"
                    }
                ])),
                name: None,
                tool_call_id: None,
                tool_calls: None,
                reasoning: None,
                reasoning_details: None,
                reasoning_content: Some(json!("raw trace")),
                thinking: None,
                thoughts: None,
            },
        }],
        usage: None,
    };

    let events = chat_completions_response_to_events(response).expect("events");

    assert!(events.iter().any(|event| matches!(
        event,
        super::ResponseEvent::ServerReasoningIncluded(true)
    )));
    assert!(events.iter().any(|event| matches!(
        event,
        super::ResponseEvent::ReasoningSummaryPartAdded { summary_index: 0 }
    )));
    assert!(events.iter().any(|event| matches!(
        event,
        super::ResponseEvent::ReasoningSummaryDelta { delta, summary_index: 0 }
            if delta == "plan first"
    )));
    assert!(events.iter().any(|event| matches!(
        event,
        super::ResponseEvent::ReasoningContentDelta { delta, content_index: 0 }
            if delta == "raw trace"
    )));
    assert!(events.iter().any(|event| matches!(
        event,
        super::ResponseEvent::OutputItemDone(ResponseItem::Message { content, .. })
            if matches!(content.as_slice(), [ContentItem::OutputText { text }] if text == "final answer")
    )));
}

#[test]
fn chat_completions_response_allows_missing_tool_call_type() {
    let response: ChatCompletionsResponse = serde_json::from_value(json!({
        "id": "resp-1",
        "model": "mistral-small-latest",
        "choices": [{
            "message": {
                "role": "assistant",
                "content": "",
                "tool_calls": [{
                    "id": "call-1",
                    "function": {
                        "name": "exec_command",
                        "arguments": "{\"cmd\":\"pwd\"}"
                    }
                }]
            }
        }]
    }))
    .expect("chat completions response");

    let events = chat_completions_response_to_events(response).expect("events");
    let function_call = events.into_iter().find_map(|event| match event {
        super::ResponseEvent::OutputItemDone(ResponseItem::FunctionCall {
            name,
            arguments,
            call_id,
            ..
        }) => Some((name, arguments, call_id)),
        _ => None,
    });

    assert_eq!(
        function_call,
        Some((
            "exec_command".to_string(),
            "{\"cmd\":\"pwd\"}".to_string(),
            "call-1".to_string(),
        ))
    );
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
