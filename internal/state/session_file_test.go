package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func TestCreateAndLoadSessionRoundTripsRichMessages(t *testing.T) {
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
		{
			Role: runtime.RoleUser,
			Content: []runtime.ContentPart{
				runtime.TextPart("hello"),
				{
					Type:   runtime.ContentPartTypeImageURL,
					URL:    "https://example.com/image.png",
					Detail: "high",
				},
				runtime.TextPart("world"),
			},
		},
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
		{
			Role:    runtime.RoleAssistant,
			Refusal: "cannot comply",
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
	if len(messages) != 4 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if got := messages[1].Text(); got != "hello\nworld" {
		t.Fatalf("unexpected user text: %q", got)
	}
	if len(messages[1].Content) != 3 {
		t.Fatalf("unexpected content parts: %+v", messages[1].Content)
	}
	if messages[1].Content[1].Type != runtime.ContentPartTypeImageURL || messages[1].Content[1].URL != "https://example.com/image.png" {
		t.Fatalf("unexpected image part: %+v", messages[1].Content[1])
	}
	if len(messages[2].ToolCalls) != 1 || messages[2].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("unexpected assistant tool calls: %+v", messages[2].ToolCalls)
	}
	if messages[3].Refusal != "cannot comply" {
		t.Fatalf("unexpected refusal: %+v", messages[3])
	}
}

func TestCreateSessionDefaultsWorkingDirectoryIntoMetaAndIndex(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	withWorkingDirectory(t, workspace)

	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	entry, err := CreateSession(root, SessionMeta{
		SessionID: "cwd123",
		Model:     "gpt-test",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if entry.CWD != workspace {
		t.Fatalf("unexpected entry cwd: %q", entry.CWD)
	}

	meta, err := LoadSessionMeta(entry.Path)
	if err != nil {
		t.Fatalf("LoadSessionMeta: %v", err)
	}
	if meta.CWD != workspace {
		t.Fatalf("unexpected meta cwd: %q", meta.CWD)
	}

	entries, err := LoadSessions(root, 1)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(entries) != 1 || entries[0].CWD != workspace {
		t.Fatalf("unexpected indexed sessions: %+v", entries)
	}
}

func TestAppendSessionUpdatesUpdatedAtAndAppendsMessages(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	entry, err := CreateSession(root, SessionMeta{
		SessionID: "append123",
		Model:     "gpt-test",
		Provider:  "openai",
		Profile:   "default",
		Reasoning: "medium",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	beforeAppend := time.Now().UTC()
	err = AppendSession(entry.Path,
		runtime.Message{
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
		runtime.Message{
			Role:       runtime.RoleTool,
			ToolCallID: "call_1",
			Content: []runtime.ContentPart{
				runtime.TextPart(`{"weather":"sunny"}`),
			},
		},
		runtime.Message{
			Role:    runtime.RoleAssistant,
			Refusal: "still refusing",
		},
	)
	if err != nil {
		t.Fatalf("AppendSession: %v", err)
	}

	meta, messages, err := LoadSession(entry.Path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !meta.UpdatedAt.After(createdAt) {
		t.Fatalf("updated_at was not updated: created=%s updated=%s", createdAt, meta.UpdatedAt)
	}
	if meta.UpdatedAt.Before(beforeAppend) {
		t.Fatalf("updated_at was not moved to append time: before=%s updated=%s", beforeAppend, meta.UpdatedAt)
	}
	if len(messages) != 4 {
		t.Fatalf("unexpected message count after append: %d", len(messages))
	}
	if messages[2].Role != runtime.RoleTool || messages[2].ToolCallID != "call_1" || messages[2].Text() != `{"weather":"sunny"}` {
		t.Fatalf("unexpected tool trace after append: %+v", messages[2])
	}
	if messages[3].Refusal != "still refusing" {
		t.Fatalf("unexpected refusal after append: %+v", messages[3])
	}
}

func TestAppendSessionHistoryRewritesWholeHistoryWithoutDuplication(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	entry, err := CreateSession(root, SessionMeta{
		SessionID: "history123",
		Model:     "gpt-test",
		Provider:  "openai",
		Profile:   "default",
		Reasoning: "medium",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	fullHistory := []runtime.Message{
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
		{
			Role:       runtime.RoleTool,
			ToolCallID: "call_1",
			Content: []runtime.ContentPart{
				runtime.TextPart(`{"weather":"sunny"}`),
			},
		},
		runtime.TextMessage(runtime.RoleAssistant, "done"),
	}

	firstUpdate := createdAt.Add(5 * time.Minute)
	if err := AppendSessionHistory(entry.Path, SessionMeta{
		Model:     "gpt-next",
		UpdatedAt: firstUpdate,
	}, fullHistory); err != nil {
		t.Fatalf("AppendSessionHistory first pass: %v", err)
	}

	secondUpdate := createdAt.Add(10 * time.Minute)
	if err := AppendSessionHistory(entry.Path, SessionMeta{
		UpdatedAt: secondUpdate,
	}, fullHistory); err != nil {
		t.Fatalf("AppendSessionHistory second pass: %v", err)
	}

	meta, messages, err := LoadSession(entry.Path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if meta.Model != "gpt-next" {
		t.Fatalf("expected updated model, got %+v", meta)
	}
	if !meta.UpdatedAt.Equal(secondUpdate) {
		t.Fatalf("unexpected updated_at after history rewrite: %s", meta.UpdatedAt)
	}
	if len(messages) != len(fullHistory) {
		t.Fatalf("history duplicated or truncated: got %d want %d", len(messages), len(fullHistory))
	}
	for idx := range fullHistory {
		if !sessionMessageEqual(messages[idx], fullHistory[idx]) {
			t.Fatalf("message %d mismatch after history rewrite: got %+v want %+v", idx, messages[idx], fullHistory[idx])
		}
	}
}

func TestLoadSessionReadsRustRolloutJSONL(t *testing.T) {
	root := t.TempDir()
	sessionID := "019db095-251f-78a0-9fcb-40892a47a780"
	parentID := "019d5ef3-4516-7e80-93f2-e386ed2e289a"
	path := filepath.Join(root, "2026", "04", "21", "rollout-2026-04-21T18-07-38-"+sessionID+".jsonl")

	writeJSONLLines(t, path,
		map[string]any{
			"timestamp": "2026-04-21T15:07:38.843Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id":             sessionID,
				"timestamp":      "2026-04-21T15:07:38.160Z",
				"cwd":            "/root",
				"model_provider": "openai",
				"git":            map[string]any{"branch": "main"},
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:38.900Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id":             parentID,
				"timestamp":      "2026-04-05T18:41:34.495Z",
				"cwd":            "/root/old",
				"model_provider": "mistral",
				"git":            map[string]any{"branch": "legacy"},
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:38.910Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "developer",
				"content": []map[string]any{
					{"type": "input_text", "text": "system instructions"},
				},
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:38.920Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "# AGENTS.md instructions for /root"},
				},
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.000Z",
			"type":      "turn_context",
			"payload": map[string]any{
				"cwd":   "/root/project",
				"model": "gpt-5.4",
				"effort": "high",
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.010Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Implement the plan."},
				},
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.015Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "Implement the plan.",
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.030Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "exec_command",
				"call_id":   "call_123",
				"arguments": "{\"cmd\":\"pwd\"}",
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.050Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  "done",
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.120Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "Finished."},
				},
			},
		},
	)

	meta, messages, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if meta.SessionID != sessionID {
		t.Fatalf("meta.SessionID = %q", meta.SessionID)
	}
	if meta.Provider != "openai" {
		t.Fatalf("meta.Provider = %q", meta.Provider)
	}
	if meta.Model != "gpt-5.4" {
		t.Fatalf("meta.Model = %q", meta.Model)
	}
	if meta.Reasoning != "high" {
		t.Fatalf("meta.Reasoning = %q", meta.Reasoning)
	}
	if meta.CWD != "/root/project" {
		t.Fatalf("meta.CWD = %q", meta.CWD)
	}
	if meta.Branch != "main" {
		t.Fatalf("meta.Branch = %q", meta.Branch)
	}
	if meta.Preview != "Implement the plan." {
		t.Fatalf("meta.Preview = %q", meta.Preview)
	}
	if meta.CreatedAt.IsZero() || meta.UpdatedAt.IsZero() {
		t.Fatalf("meta timestamps were not restored: %+v", meta)
	}
	if len(messages) != 6 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if messages[0].Role != runtime.RoleSystem || messages[0].Text() != "system instructions" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].Role != runtime.RoleUser || messages[1].Text() != "# AGENTS.md instructions for /root" {
		t.Fatalf("unexpected injected user message: %+v", messages[1])
	}
	if messages[2].Role != runtime.RoleUser || messages[2].Text() != "Implement the plan." {
		t.Fatalf("unexpected user message: %+v", messages[2])
	}
	if len(messages[3].ToolCalls) != 1 || messages[3].ToolCalls[0].ID != "call_123" {
		t.Fatalf("unexpected tool call message: %+v", messages[3])
	}
	if messages[4].Role != runtime.RoleTool || messages[4].ToolCallID != "call_123" || messages[4].Text() != "done" {
		t.Fatalf("unexpected tool output message: %+v", messages[4])
	}
	if messages[5].Role != runtime.RoleAssistant || messages[5].Text() != "Finished." {
		t.Fatalf("unexpected final assistant message: %+v", messages[5])
	}
}

func TestLoadSessionSupportsMixedRolloutAndNativeJSONL(t *testing.T) {
	root := t.TempDir()
	sessionID := "019db095-251f-78a0-9fcb-40892a47a780"
	path := filepath.Join(root, "2026", "04", "21", "rollout-2026-04-21T18-07-38-"+sessionID+".jsonl")

	writeJSONLLines(t, path,
		map[string]any{
			"timestamp": "2026-04-21T15:07:38.843Z",
			"type":      "session_meta",
			"payload": map[string]any{
				"id":             sessionID,
				"timestamp":      "2026-04-21T15:07:38.160Z",
				"cwd":            "/root/project",
				"model_provider": "openai",
			},
		},
		map[string]any{
			"timestamp": "2026-04-21T15:07:39.010Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "legacy prompt"},
				},
			},
		},
		sessionLine{
			Type:      "meta",
			SessionID: sessionID,
			Model:     "gpt-5.5",
			Provider:  "openrouter",
			Profile:   "openrouter-profile",
			Reasoning: "medium",
			CWD:       "/root/project",
			Branch:    "main",
			Preview:   "legacy prompt",
			CreatedAt: "2026-04-21T15:07:38.160Z",
			UpdatedAt: "2026-04-21T15:09:00.000Z",
		},
		sessionLine{
			Type: "message",
			Role: string(runtime.RoleAssistant),
			Content: []sessionContent{
				{Type: "text", Text: "native append"},
			},
		},
	)

	meta, messages, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if meta.Provider != "openrouter" {
		t.Fatalf("meta.Provider = %q", meta.Provider)
	}
	if meta.Profile != "openrouter-profile" {
		t.Fatalf("meta.Profile = %q", meta.Profile)
	}
	if meta.Model != "gpt-5.5" {
		t.Fatalf("meta.Model = %q", meta.Model)
	}
	if meta.Reasoning != "medium" {
		t.Fatalf("meta.Reasoning = %q", meta.Reasoning)
	}
	if meta.Branch != "main" {
		t.Fatalf("meta.Branch = %q", meta.Branch)
	}
	if meta.Preview != "legacy prompt" {
		t.Fatalf("meta.Preview = %q", meta.Preview)
	}
	if len(messages) != 2 {
		t.Fatalf("unexpected message count: %d", len(messages))
	}
	if messages[0].Role != runtime.RoleUser || messages[0].Text() != "legacy prompt" {
		t.Fatalf("unexpected legacy message: %+v", messages[0])
	}
	if messages[1].Role != runtime.RoleAssistant || messages[1].Text() != "native append" {
		t.Fatalf("unexpected native message: %+v", messages[1])
	}

	index, err := ScanSessions(root)
	if err != nil {
		t.Fatalf("ScanSessions: %v", err)
	}
	if len(index.Entries) != 1 {
		t.Fatalf("unexpected index entries: %+v", index.Entries)
	}
	if index.Entries[0].Branch != "main" {
		t.Fatalf("entry.Branch = %q", index.Entries[0].Branch)
	}
	if index.Entries[0].Preview != "legacy prompt" {
		t.Fatalf("entry.Preview = %q", index.Entries[0].Preview)
	}
}

func writeJSONLLines(t *testing.T, path string, lines ...any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%s): %v", path, err)
	}
	defer file.Close()
	for _, line := range lines {
		payload, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("Marshal line: %v", err)
		}
		if _, err := file.Write(payload); err != nil {
			t.Fatalf("Write payload: %v", err)
		}
		if _, err := file.WriteString("\n"); err != nil {
			t.Fatalf("Write newline: %v", err)
		}
	}
}

func TestAppendSessionHistoryRejectsMismatchedPrefix(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	entry, err := CreateSession(root, SessionMeta{
		SessionID: "mismatch123",
		Model:     "gpt-test",
		Provider:  "openai",
		Profile:   "default",
		Reasoning: "medium",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	err = AppendSessionHistory(entry.Path, SessionMeta{
		UpdatedAt: createdAt.Add(time.Minute),
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "different"),
		runtime.TextMessage(runtime.RoleAssistant, "done"),
	})
	if err == nil {
		t.Fatal("expected prefix mismatch error")
	}
	if !strings.Contains(err.Error(), "prefix mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, messages, err := LoadSession(entry.Path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !meta.UpdatedAt.Equal(createdAt) {
		t.Fatalf("session meta changed after rejected append: %+v", meta)
	}
	if len(messages) != 1 || messages[0].Text() != "hello" {
		t.Fatalf("session history changed after rejected append: %+v", messages)
	}
}

func TestLoadSessionsUsesUpdatedIndexOrdering(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	first, err := CreateSession(root, SessionMeta{
		SessionID: "first",
		Model:     "gpt-test",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, []runtime.Message{runtime.TextMessage(runtime.RoleUser, "first")})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	second, err := CreateSession(root, SessionMeta{
		SessionID: "second",
		Model:     "gpt-test",
		CreatedAt: createdAt.Add(time.Minute),
		UpdatedAt: createdAt.Add(time.Minute),
	}, []runtime.Message{runtime.TextMessage(runtime.RoleUser, "second")})
	if err != nil {
		t.Fatalf("CreateSession second: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, sessionIndexFileName)); err != nil {
		t.Fatalf("session index missing: %v", err)
	}

	entries, err := LoadSessions(root, 2)
	if err != nil {
		t.Fatalf("LoadSessions initial: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("unexpected session count before append: %+v", entries)
	}
	target := first.Path
	if entries[0].Path == first.Path {
		target = second.Path
	}

	if err := AppendSession(target, runtime.TextMessage(runtime.RoleAssistant, "bumped")); err != nil {
		t.Fatalf("AppendSession: %v", err)
	}

	entries, err = LoadSessions(root, 1)
	if err != nil {
		t.Fatalf("LoadSessions after append: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != target {
		t.Fatalf("unexpected latest session after append: %+v", entries)
	}
}

func TestLoadSessionIndexHydratesWorkingDirectoryFromLegacyIndex(t *testing.T) {
	root := t.TempDir()
	createdAt := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}

	entry, err := CreateSession(root, SessionMeta{
		SessionID: "legacyindex",
		Model:     "gpt-test",
		CWD:       workspace,
		CreatedAt: createdAt,
		UpdatedAt: createdAt.Add(2 * time.Minute),
	}, []runtime.Message{
		runtime.TextMessage(runtime.RoleUser, "hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	legacyPayload := sessionIndexFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Entries: []SessionEntry{{
			ID:      entry.ID,
			Name:    entry.Name,
			Path:    entry.Path,
			RelPath: entry.RelPath,
			ModTime: createdAt,
			Size:    entry.Size,
		}},
	}
	data, err := json.Marshal(legacyPayload)
	if err != nil {
		t.Fatalf("Marshal legacy index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, sessionIndexFileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile legacy index: %v", err)
	}

	index, err := LoadSessionIndex(root)
	if err != nil {
		t.Fatalf("LoadSessionIndex: %v", err)
	}
	if len(index.Entries) != 1 {
		t.Fatalf("unexpected hydrated index entries: %+v", index.Entries)
	}
	if index.Entries[0].CWD != workspace {
		t.Fatalf("unexpected hydrated cwd: %q", index.Entries[0].CWD)
	}
	if !index.Entries[0].ModTime.Equal(createdAt.Add(2 * time.Minute)) {
		t.Fatalf("unexpected hydrated mod time: %s", index.Entries[0].ModTime)
	}
}
