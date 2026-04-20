package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestCreateAndLoadSession(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	entry, err := CreateSession(root, SessionMeta{
		SessionID: "abc123",
		Model:     "gpt-test",
		Provider:  "openai",
		Profile:   "default",
		Reasoning: "medium",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleSystem, "system"),
		runtime.TextMessage(runtime.RoleUser, "hello"),
		{
			Role: runtime.RoleAssistant,
			ToolCalls: []runtime.ToolCall{{
				ID:   "call_1",
				Type: runtime.ToolTypeFunction,
				Function: runtime.FunctionCall{
					Name:      "lookup_weather",
					Arguments: []byte(`{"city":"Moscow"}`),
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	expectedPath := filepath.Join(root, "2026", "04", "20", "rollout-2026-04-20T12-00-00-abc123.jsonl")
	if entry.Path != expectedPath {
		t.Fatalf("unexpected path: %s", entry.Path)
	}

	meta, messages, err := LoadSession(entry.Path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if meta.SessionID != "abc123" || meta.Model != "gpt-test" || meta.Provider != "openai" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if len(messages) != 3 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if messages[1].Text() != "hello" {
		t.Fatalf("unexpected second message: %+v", messages[1])
	}
	if len(messages[2].ToolCalls) != 1 || messages[2].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("unexpected assistant tool calls: %+v", messages[2].ToolCalls)
	}
}
