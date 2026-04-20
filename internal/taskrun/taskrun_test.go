package taskrun

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

func TestProviderEndpoint_DefaultRoot(t *testing.T) {
	base, path, err := providerEndpoint("https://api.openai.com")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://api.openai.com" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_VersionedBase(t *testing.T) {
	base, path, err := providerEndpoint("https://api.mistral.ai/v1")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://api.mistral.ai/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_FullChatCompletionsURL(t *testing.T) {
	base, path, err := providerEndpoint("https://example.com/custom/v1/chat/completions")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://example.com/custom/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_RejectsResponsesEndpoint(t *testing.T) {
	_, _, err := providerEndpoint("https://api.openai.com/v1/responses")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResponsesEndpoint_DefaultRoot(t *testing.T) {
	base, path, err := responsesEndpoint("https://api.openai.com")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://api.openai.com" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/v1/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestResponsesEndpoint_VersionedBase(t *testing.T) {
	base, path, err := responsesEndpoint("https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://api.openai.com/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestResponsesEndpoint_FullResponsesURL(t *testing.T) {
	base, path, err := responsesEndpoint("https://example.com/custom/v1/responses")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://example.com/custom/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestCollectStreamTurn_GroupsToolCallsByID(t *testing.T) {
	client := fakeProviderClient{
		name: "responses-test",
		caps: provider.Capabilities{Streaming: true},
		streamFn: func(context.Context, runtime.Request) (runtime.Stream, error) {
			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role: runtime.RoleAssistant,
						ToolCalls: []runtime.ToolCallDelta{
							{ID: "call_alpha", NameDelta: "lookup_", ArgumentsDelta: `{"city":"`},
							{ID: "call_beta", NameDelta: "search_", ArgumentsDelta: `{"query":"`},
						},
					},
				},
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						ToolCalls: []runtime.ToolCallDelta{
							{ID: "call_alpha", NameDelta: "weather", ArgumentsDelta: `Moscow"}`},
							{ID: "call_beta", NameDelta: "repo", ArgumentsDelta: `README"}`},
						},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	response, _, assistant, err := collectStreamTurn(context.Background(), client, runtime.Request{Model: "alpha-model"}, 1, progressReporter{})
	if err != nil {
		t.Fatalf("collectStreamTurn: %v", err)
	}
	if response == nil {
		t.Fatal("collectStreamTurn returned nil response")
	}
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("assistant tool call count = %d, want 2", len(assistant.ToolCalls))
	}
	if assistant.ToolCalls[0].ID != "call_alpha" || assistant.ToolCalls[0].Function.Name != "lookup_weather" || assistant.ToolCalls[0].Function.ArgumentsString() != `{"city":"Moscow"}` {
		t.Fatalf("unexpected first tool call: %+v", assistant.ToolCalls[0])
	}
	if assistant.ToolCalls[1].ID != "call_beta" || assistant.ToolCalls[1].Function.Name != "search_repo" || assistant.ToolCalls[1].Function.ArgumentsString() != `{"query":"README"}` {
		t.Fatalf("unexpected second tool call: %+v", assistant.ToolCalls[1])
	}
}

func TestRunWithToolLoop_PreservesToolTraceInHistory(t *testing.T) {
	tempDir := t.TempDir()
	client := fakeProviderClient{
		name: "stream-tool-loop",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			if len(request.Messages) == 2 {
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{
								{
									ID:             "call_list",
									Type:           runtime.ToolTypeFunction,
									NameDelta:      "list_directory",
									ArgumentsDelta: `{"path":"` + tempDir + `"}`,
								},
							},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}

			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role: runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{
							{Type: runtime.ContentPartTypeText, Text: "done"},
						},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "inspect"),
		},
		Tools: []runtime.ToolDefinition{{
			Type: runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{
				Name: "list_directory",
			},
		}},
	}

	history, requestMessages, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, tooling.DefaultToolPolicy(), nil, nil, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if assistant.Text() != "done" {
		t.Fatalf("assistant text = %q, want done", assistant.Text())
	}
	if len(requestMessages) != 4 {
		t.Fatalf("requestMessages len = %d, want 4", len(requestMessages))
	}
	if len(history) != 5 {
		t.Fatalf("history len = %d, want 5", len(history))
	}
	if len(history[2].ToolCalls) != 1 || history[2].ToolCalls[0].ID != "call_list" {
		t.Fatalf("assistant tool trace missing from history: %+v", history[2])
	}
	if history[3].Role != runtime.RoleTool || history[3].ToolCallID != "call_list" || history[3].Text() == "" {
		t.Fatalf("tool output missing from history: %+v", history[3])
	}
	if history[4].Role != runtime.RoleAssistant || history[4].Text() != "done" {
		t.Fatalf("final assistant missing from history: %+v", history[4])
	}
	if len(reports) != 1 || reports[0].Summary.SucceededCount != 1 {
		t.Fatalf("tool reports missing success summary: %+v", reports)
	}
}

func TestRunWithToolLoop_RequireApprovalBlocksMutatingTool(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "blocked.txt")
	client := fakeProviderClient{
		name: "approval-tool-loop",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			if len(request.Messages) == 2 {
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{
								{
									ID:             "call_write",
									Type:           runtime.ToolTypeFunction,
									NameDelta:      "write_file",
									ArgumentsDelta: `{"path":"` + targetPath + `","content":"denied"}`,
								},
							},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}

			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role: runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{
							{Type: runtime.ContentPartTypeText, Text: "approval noted"},
						},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "write"),
		},
		Tools: []runtime.ToolDefinition{{
			Type: runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{
				Name: "write_file",
			},
		}},
	}

	policy := tooling.DefaultToolPolicy()
	policy.ApprovalMode = tooling.ToolApprovalModeRequire
	history, _, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, policy, nil, nil, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if assistant.Text() != "approval noted" {
		t.Fatalf("assistant text = %q, want approval noted", assistant.Text())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("mutating tool should not execute, stat err = %v", err)
	}
	if len(history) != 5 || history[3].Role != runtime.RoleTool {
		t.Fatalf("unexpected history after blocked tool: %+v", history)
	}
	if got := history[3].Text(); got == "" || !strings.Contains(got, `"status": "approval_required"`) {
		t.Fatalf("tool message missing approval payload: %s", got)
	}
	if len(reports) != 1 || reports[0].Summary.ApprovalRequiredCount != 1 {
		t.Fatalf("tool reports missing approval summary: %+v", reports)
	}
}

func TestRunWithToolLoop_ApprovalHandlerExecutesApprovedTool(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "approved.txt")
	client := fakeProviderClient{
		name: "approval-handler-tool-loop",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			if len(request.Messages) == 2 {
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:             "call_write",
								Type:           runtime.ToolTypeFunction,
								NameDelta:      "write_file",
								ArgumentsDelta: `{"path":"` + targetPath + `","content":"approved"}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}

			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role:    runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{{Type: runtime.ContentPartTypeText, Text: "tool approved"}},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "write"),
		},
		Tools: []runtime.ToolDefinition{{
			Type:     runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{Name: "write_file"},
		}},
	}

	policy := tooling.DefaultToolPolicy()
	policy.ApprovalMode = tooling.ToolApprovalModeRequire
	handlerCalls := 0
	history, _, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, policy, nil, func(_ context.Context, request tooling.ApprovalRequest) (ApprovalDecision, error) {
		handlerCalls++
		if request.Name != "write_file" {
			t.Fatalf("unexpected approval request: %+v", request)
		}
		if !strings.Contains(request.Summary, targetPath) {
			t.Fatalf("approval summary = %q, want path %q", request.Summary, targetPath)
		}
		return ApprovalDecisionApprove, nil
	}, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("approval handler calls = %d, want 1", handlerCalls)
	}
	if assistant.Text() != "tool approved" {
		t.Fatalf("assistant text = %q, want tool approved", assistant.Text())
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("expected approved tool to write file: %v", err)
	}
	if string(content) != "approved" {
		t.Fatalf("written content = %q, want approved", string(content))
	}
	if len(reports) != 1 || reports[0].Summary.SucceededCount != 1 || reports[0].Summary.ApprovalRequiredCount != 0 {
		t.Fatalf("unexpected tool report after approval: %+v", reports)
	}
	if len(history) < 4 || history[3].Role != runtime.RoleTool {
		t.Fatalf("missing tool output in history: %+v", history)
	}
	if got := history[3].Text(); got == "" || !strings.Contains(got, `"ok": true`) {
		t.Fatalf("tool output missing success payload: %s", got)
	}
}

func TestRunWithToolLoop_ApprovalHandlerDeniesTool(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "denied.txt")
	client := fakeProviderClient{
		name: "approval-handler-deny",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			if len(request.Messages) == 2 {
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:             "call_write",
								Type:           runtime.ToolTypeFunction,
								NameDelta:      "write_file",
								ArgumentsDelta: `{"path":"` + targetPath + `","content":"denied"}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}

			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role:    runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{{Type: runtime.ContentPartTypeText, Text: "tool denied"}},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "write"),
		},
		Tools: []runtime.ToolDefinition{{
			Type:     runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{Name: "write_file"},
		}},
	}

	policy := tooling.DefaultToolPolicy()
	policy.ApprovalMode = tooling.ToolApprovalModeRequire
	history, _, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, policy, nil, func(_ context.Context, request tooling.ApprovalRequest) (ApprovalDecision, error) {
		if request.Name != "write_file" {
			t.Fatalf("unexpected approval request: %+v", request)
		}
		return ApprovalDecisionDeny, nil
	}, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if assistant.Text() != "tool denied" {
		t.Fatalf("assistant text = %q, want tool denied", assistant.Text())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("denied tool should not execute, stat err = %v", err)
	}
	if len(reports) != 1 || reports[0].Summary.DeniedCount != 1 || reports[0].Summary.ApprovalRequiredCount != 0 {
		t.Fatalf("unexpected tool report after denial: %+v", reports)
	}
	if len(history) < 4 || history[3].Role != runtime.RoleTool {
		t.Fatalf("missing denied tool payload in history: %+v", history)
	}
	if got := history[3].Text(); got == "" || !strings.Contains(got, `"status": "denied"`) || !strings.Contains(got, `denied by user`) {
		t.Fatalf("tool output missing denied payload: %s", got)
	}
}

func TestRunWithToolLoop_ApprovalHandlerApproveForSessionCachesEquivalentCalls(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "session-approved.txt")
	client := fakeProviderClient{
		name: "approval-handler-session-cache",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			toolMessages := 0
			for _, message := range request.Messages {
				if message.Role == runtime.RoleTool {
					toolMessages++
				}
			}
			switch toolMessages {
			case 0, 1:
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:             "call_write",
								Type:           runtime.ToolTypeFunction,
								NameDelta:      "write_file",
								ArgumentsDelta: `{"path":"` + targetPath + `","content":"approved"}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			default:
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role:    runtime.RoleAssistant,
							Content: []runtime.ContentPartDelta{{Type: runtime.ContentPartTypeText, Text: "cached approval reused"}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "write twice"),
		},
		Tools: []runtime.ToolDefinition{{
			Type:     runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{Name: "write_file"},
		}},
	}

	policy := tooling.DefaultToolPolicy()
	policy.ApprovalMode = tooling.ToolApprovalModeRequire
	handlerCalls := 0
	history, _, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, policy, nil, func(_ context.Context, request tooling.ApprovalRequest) (ApprovalDecision, error) {
		handlerCalls++
		if request.ApprovalID == "" {
			t.Fatalf("expected stable approval id: %+v", request)
		}
		return ApprovalDecisionApproveForSession, nil
	}, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("approval handler calls = %d, want 1", handlerCalls)
	}
	if assistant.Text() != "cached approval reused" {
		t.Fatalf("assistant text = %q, want cached approval reused", assistant.Text())
	}
	if len(reports) != 2 {
		t.Fatalf("tool report count = %d, want 2", len(reports))
	}
	if reports[0].Results[0].Metadata.ApprovalState != tooling.ToolApprovalStateSessionApproved {
		t.Fatalf("first call approval state = %s, want %s", reports[0].Results[0].Metadata.ApprovalState, tooling.ToolApprovalStateSessionApproved)
	}
	if reports[1].Results[0].Metadata.ApprovalState != tooling.ToolApprovalStateSessionApproved {
		t.Fatalf("cached call approval state = %s, want %s", reports[1].Results[0].Metadata.ApprovalState, tooling.ToolApprovalStateSessionApproved)
	}
	if len(history) < 6 {
		t.Fatalf("unexpected history length: %d", len(history))
	}
}

func TestRunWithToolLoop_RequestPermissionsGrantAllowsLaterWrite(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "granted", "out.txt")
	client := fakeProviderClient{
		name: "approval-handler-request-permissions",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			toolMessages := 0
			for _, message := range request.Messages {
				if message.Role == runtime.RoleTool {
					toolMessages++
				}
			}
			switch toolMessages {
			case 0:
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:        "call_permissions",
								Type:      runtime.ToolTypeFunction,
								NameDelta: "request_permissions",
								ArgumentsDelta: `{"reason":"need write access","permissions":{"writable_roots":["` +
									tempDir + `"]}}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			case 1:
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:             "call_write_granted",
								Type:           runtime.ToolTypeFunction,
								NameDelta:      "write_file",
								ArgumentsDelta: `{"path":"` + targetPath + `","content":"granted"}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			default:
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role:    runtime.RoleAssistant,
							Content: []runtime.ContentPartDelta{{Type: runtime.ContentPartTypeText, Text: "permission grant reused"}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}
		},
	}

	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "write after requesting permissions"),
		},
		Tools: []runtime.ToolDefinition{
			{
				Type:     runtime.ToolTypeFunction,
				Function: runtime.FunctionDefinition{Name: "request_permissions"},
			},
			{
				Type:     runtime.ToolTypeFunction,
				Function: runtime.FunctionDefinition{Name: "write_file"},
			},
		},
	}

	policy := tooling.DefaultToolPolicy()
	policy.ApprovalMode = tooling.ToolApprovalModeRequire
	handlerCalls := 0
	_, _, _, assistant, _, reports, err := runWithToolLoop(context.Background(), client, request, true, policy, nil, func(_ context.Context, request tooling.ApprovalRequest) (ApprovalDecision, error) {
		handlerCalls++
		if request.Name != "request_permissions" {
			t.Fatalf("unexpected approval request: %+v", request)
		}
		return ApprovalDecisionApproveForSession, nil
	}, progressReporter{})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("approval handler calls = %d, want 1", handlerCalls)
	}
	if assistant.Text() != "permission grant reused" {
		t.Fatalf("assistant text = %q, want permission grant reused", assistant.Text())
	}
	if len(reports) != 2 {
		t.Fatalf("tool report count = %d, want 2", len(reports))
	}
	if reports[0].Results[0].Name != "request_permissions" || reports[0].Results[0].Status != tooling.ResultStatusSucceeded {
		t.Fatalf("unexpected request_permissions report: %+v", reports[0].Results[0])
	}
	if reports[0].Results[0].Metadata.PermissionGrantScope != tooling.PermissionGrantScopeSession {
		t.Fatalf("request grant scope = %s, want %s", reports[0].Results[0].Metadata.PermissionGrantScope, tooling.PermissionGrantScopeSession)
	}
	if reports[1].Results[0].Name != "write_file" || reports[1].Results[0].Metadata.ApprovalState != tooling.ToolApprovalStateSessionApproved {
		t.Fatalf("write_file should reuse session grant: %+v", reports[1].Results[0])
	}
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read granted file: %v", err)
	}
	if string(content) != "granted" {
		t.Fatalf("written content = %q, want granted", string(content))
	}
}

func TestCollectStreamTurn_EmitsAssistantSnapshots(t *testing.T) {
	updates := make([]ProgressUpdate, 0, 4)
	client := fakeProviderClient{
		name: "stream-progress",
		caps: provider.Capabilities{Streaming: true},
		streamFn: func(context.Context, runtime.Request) (runtime.Stream, error) {
			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role: runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{
							{Type: runtime.ContentPartTypeText, Text: "hello"},
						},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	_, _, assistant, err := collectStreamTurn(context.Background(), client, runtime.Request{Model: "alpha-model"}, 2, progressReporter{
		fn: func(update ProgressUpdate) {
			updates = append(updates, update)
		},
	})
	if err != nil {
		t.Fatalf("collectStreamTurn: %v", err)
	}
	if assistant.Text() != "hello" {
		t.Fatalf("assistant text = %q, want hello", assistant.Text())
	}
	if len(updates) == 0 {
		t.Fatal("expected progress updates")
	}
	found := false
	for _, update := range updates {
		if update.Kind == ProgressKindAssistantSnapshot && update.Round == 2 && update.Snapshot.Text == "hello" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("assistant snapshot for round 2 not found: %+v", updates)
	}
}

func TestRunWithToolLoop_EmitsToolPlanningProgress(t *testing.T) {
	updates := make([]ProgressUpdate, 0, 8)
	client := fakeProviderClient{
		name: "tool-progress",
		caps: provider.Capabilities{Streaming: true, Tools: true},
		streamFn: func(_ context.Context, request runtime.Request) (runtime.Stream, error) {
			if len(request.Messages) == 2 {
				return &fakeStream{events: []runtime.StreamEvent{
					{
						Type: runtime.StreamEventTypeDelta,
						Delta: runtime.MessageDelta{
							Role: runtime.RoleAssistant,
							ToolCalls: []runtime.ToolCallDelta{{
								ID:             "call_read",
								Type:           runtime.ToolTypeFunction,
								NameDelta:      "read_file",
								ArgumentsDelta: `{"path":"go.mod"}`,
							}},
						},
					},
					{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonToolCalls},
					{Type: runtime.StreamEventTypeDone},
				}}, nil
			}
			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role:    runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{{Type: runtime.ContentPartTypeText, Text: "done"}},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}
	request := runtime.Request{
		Model: "tool-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "system"),
			runtime.TextMessage(runtime.RoleUser, "inspect"),
		},
		Tools: []runtime.ToolDefinition{{
			Type:     runtime.ToolTypeFunction,
			Function: runtime.FunctionDefinition{Name: "read_file"},
		}},
	}

	_, _, _, _, _, reports, err := runWithToolLoop(context.Background(), client, request, true, tooling.DefaultToolPolicy(), nil, nil, progressReporter{
		fn: func(update ProgressUpdate) {
			updates = append(updates, update)
		},
	})
	if err != nil {
		t.Fatalf("runWithToolLoop: %v", err)
	}
	if len(reports) != 1 || reports[0].Summary.CallCount != 1 {
		t.Fatalf("unexpected tool reports: %+v", reports)
	}
	var sawPlan bool
	var sawResult bool
	for _, update := range updates {
		if update.Kind == ProgressKindToolPlanned && update.ToolPlan != nil && update.ToolPlan.Summary.CallCount == 1 {
			sawPlan = true
		}
		if update.Kind == ProgressKindToolResult && update.ToolResult != nil && update.ToolResult.Name == "read_file" {
			sawResult = true
		}
	}
	if !sawPlan || !sawResult {
		t.Fatalf("missing planning progress: sawPlan=%t sawResult=%t updates=%+v", sawPlan, sawResult, updates)
	}
}

func TestRunSingleTurn_RetriesRetryableCreateErrors(t *testing.T) {
	attempts := 0
	client := fakeProviderClient{
		name: "retry-create",
		createFn: func(context.Context, runtime.Request) (*runtime.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, &provider.Error{
					Provider:   "retry-create",
					StatusCode: 429,
					Message:    "rate limited",
					Retryable:  true,
					RetryAfter: time.Millisecond,
				}
			}
			return &runtime.Response{
				Model: "alpha-model",
				Choices: []runtime.Choice{{
					Index:   0,
					Message: runtime.TextMessage(runtime.RoleAssistant, "ready"),
				}},
			}, nil
		},
	}

	response, _, assistant, err := runSingleTurn(context.Background(), client, runtime.Request{Model: "alpha-model"}, false, 1, progressReporter{})
	if err != nil {
		t.Fatalf("runSingleTurn: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("create attempts = %d, want 3", attempts)
	}
	if response == nil || assistant.Text() != "ready" {
		t.Fatalf("unexpected response after retry: %+v %+v", response, assistant)
	}
}

func TestCollectStreamTurn_RetriesRetryableStreamOpen(t *testing.T) {
	attempts := 0
	client := fakeProviderClient{
		name: "retry-stream",
		caps: provider.Capabilities{Streaming: true},
		streamFn: func(context.Context, runtime.Request) (runtime.Stream, error) {
			attempts++
			if attempts < 3 {
				return nil, &provider.Error{
					Provider:   "retry-stream",
					StatusCode: 503,
					Message:    "temporary upstream failure",
					Retryable:  true,
					RetryAfter: time.Millisecond,
				}
			}
			return &fakeStream{events: []runtime.StreamEvent{
				{
					Type: runtime.StreamEventTypeDelta,
					Delta: runtime.MessageDelta{
						Role: runtime.RoleAssistant,
						Content: []runtime.ContentPartDelta{
							{Type: runtime.ContentPartTypeText, Text: "stream ok"},
						},
					},
				},
				{Type: runtime.StreamEventTypeChoiceDone, FinishReason: runtime.FinishReasonStop},
				{Type: runtime.StreamEventTypeDone},
			}}, nil
		},
	}

	response, _, assistant, err := collectStreamTurn(context.Background(), client, runtime.Request{Model: "alpha-model"}, 1, progressReporter{})
	if err != nil {
		t.Fatalf("collectStreamTurn: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("stream attempts = %d, want 3", attempts)
	}
	if response == nil || assistant.Text() != "stream ok" {
		t.Fatalf("unexpected streamed response after retry: %+v %+v", response, assistant)
	}
}

type fakeProviderClient struct {
	name     string
	caps     provider.Capabilities
	createFn func(context.Context, runtime.Request) (*runtime.Response, error)
	streamFn func(context.Context, runtime.Request) (runtime.Stream, error)
}

func (client fakeProviderClient) Name() string {
	if client.name == "" {
		return "fake"
	}
	return client.name
}

func (client fakeProviderClient) Capabilities() provider.Capabilities {
	return client.caps
}

func (client fakeProviderClient) Create(ctx context.Context, request runtime.Request) (*runtime.Response, error) {
	if client.createFn == nil {
		return nil, io.EOF
	}
	return client.createFn(ctx, request)
}

func (client fakeProviderClient) Stream(ctx context.Context, request runtime.Request) (runtime.Stream, error) {
	if client.streamFn == nil {
		return nil, io.EOF
	}
	return client.streamFn(ctx, request)
}

type fakeStream struct {
	events []runtime.StreamEvent
	index  int
}

func (stream *fakeStream) Recv() (runtime.StreamEvent, error) {
	if stream.index >= len(stream.events) {
		return runtime.StreamEvent{}, io.EOF
	}
	event := stream.events[stream.index]
	stream.index++
	return event, nil
}

func (stream *fakeStream) Close() error {
	return nil
}
