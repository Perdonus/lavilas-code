package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestClientCreate(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var payload ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "gpt-test" {
			t.Fatalf("unexpected model: %s", payload.Model)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "lookup_weather" {
			t.Fatalf("unexpected tools: %+v", payload.Tools)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_123",
			"created":1713640000,
			"model":"gpt-test",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Need weather data",
					"tool_calls":[{
						"id":"call_1",
						"type":"function",
						"function":{"name":"lookup_weather","arguments":"{\"city\":\"Moscow\"}"}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}
		}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Create(context.Background(), runtime.Request{
		Model:    "gpt-test",
		Messages: []runtime.Message{runtime.TextMessage(runtime.RoleUser, "What is the weather?")},
		Tools: []runtime.ToolDefinition{{
			Function: runtime.FunctionDefinition{Name: "lookup_weather"},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if response.ID != "resp_123" {
		t.Fatalf("unexpected response id: %s", response.ID)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("unexpected choices: %d", len(response.Choices))
	}
	choice := response.Choices[0]
	if choice.FinishReason != runtime.FinishReasonToolCalls {
		t.Fatalf("unexpected finish reason: %s", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("unexpected tool calls: %+v", choice.Message.ToolCalls)
	}
	if got := choice.Message.ToolCalls[0].Function.ArgumentsString(); got != `{"city":"Moscow"}` {
		t.Fatalf("unexpected arguments: %s", got)
	}
}

func TestClientStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); accept != "text/event-stream" {
			t.Fatalf("unexpected accept header: %s", accept)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"id":"resp_123","created":1713640000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Need "}}]}`,
			`data: {"id":"resp_123","created":1713640000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":"}}]}}]}`,
			`data: {"id":"resp_123","created":1713640000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Moscow\"}"}}]}},{"index":0,"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
			`data: [DONE]`,
		}
		_, _ = io.WriteString(w, strings.Join(chunks, "\n\n")+"\n\n")
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "secret", DefaultModel: "gpt-test"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	stream, err := client.Stream(context.Background(), runtime.Request{
		Messages: []runtime.Message{runtime.TextMessage(runtime.RoleUser, "What is the weather?")},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()

	var events []runtime.StreamEvent
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		events = append(events, event)
		if event.Type == runtime.StreamEventTypeDone {
			break
		}
	}

	if len(events) != 6 {
		t.Fatalf("unexpected event count: %d", len(events))
	}
	if events[0].Type != runtime.StreamEventTypeDelta || events[0].Delta.Text() != "Need " {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != runtime.StreamEventTypeDelta || len(events[1].Delta.ToolCalls) != 1 {
		t.Fatalf("unexpected second event: %+v", events[1])
	}
	if events[2].Type != runtime.StreamEventTypeDelta || events[2].Delta.ToolCalls[0].ArgumentsDelta != `"Moscow"}` {
		t.Fatalf("unexpected third event: %+v", events[2])
	}
	if events[3].Type != runtime.StreamEventTypeChoiceDone || events[3].FinishReason != runtime.FinishReasonToolCalls {
		t.Fatalf("unexpected fourth event: %+v", events[3])
	}
	if events[4].Type != runtime.StreamEventTypeUsage || events[4].Usage == nil || events[4].Usage.TotalTokens != 19 {
		t.Fatalf("unexpected fifth event: %+v", events[4])
	}
	if events[5].Type != runtime.StreamEventTypeDone {
		t.Fatalf("unexpected sixth event: %+v", events[5])
	}
}
