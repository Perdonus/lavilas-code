package taskrun

import (
	"context"
	"io"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
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

	response, _, assistant, err := collectStreamTurn(context.Background(), client, runtime.Request{Model: "alpha-model"})
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

	history, requestMessages, _, assistant, _, err := runWithToolLoop(context.Background(), client, request, true)
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
