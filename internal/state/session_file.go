package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

type SessionMeta struct {
	SessionID string    `json:"session_id"`
	Model     string    `json:"model,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Profile   string    `json:"profile,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sessionLine struct {
	Type       string            `json:"type"`
	SessionID  string            `json:"session_id,omitempty"`
	Model      string            `json:"model,omitempty"`
	Provider   string            `json:"provider,omitempty"`
	Profile    string            `json:"profile,omitempty"`
	Reasoning  string            `json:"reasoning,omitempty"`
	CreatedAt  string            `json:"created_at,omitempty"`
	UpdatedAt  string            `json:"updated_at,omitempty"`
	Role       string            `json:"role,omitempty"`
	Name       string            `json:"name,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Text       string            `json:"text,omitempty"`
	ToolCalls  []sessionToolCall `json:"tool_calls,omitempty"`
}

type sessionToolCall struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func CreateSession(root string, meta SessionMeta, messages []runtime.Message) (SessionEntry, error) {
	meta = normalizeSessionMeta(meta)
	path := sessionPath(root, meta)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return SessionEntry{}, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return SessionEntry{}, err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if err := writeSessionMeta(writer, meta); err != nil {
		return SessionEntry{}, err
	}
	if err := writeSessionMessages(writer, messages); err != nil {
		return SessionEntry{}, err
	}
	if err := writer.Flush(); err != nil {
		return SessionEntry{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return SessionEntry{}, err
	}
	return buildSessionEntry(root, path, info), nil
}

func AppendSession(path string, messages ...runtime.Message) error {
	if len(messages) == 0 {
		return nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if err := writeSessionMessages(writer, messages); err != nil {
		return err
	}
	return writer.Flush()
}

func LoadSession(path string) (SessionMeta, []runtime.Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	defer file.Close()

	var meta SessionMeta
	messages := make([]runtime.Message, 0, 8)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var current sessionLine
		if err := json.Unmarshal([]byte(line), &current); err != nil {
			return SessionMeta{}, nil, fmt.Errorf("decode session line: %w", err)
		}
		switch current.Type {
		case "meta":
			meta = sessionMetaFromLine(current)
		case "message":
			messages = append(messages, runtimeMessageFromLine(current))
		}
	}
	if err := scanner.Err(); err != nil {
		return SessionMeta{}, nil, err
	}
	return meta, messages, nil
}

func normalizeSessionMeta(meta SessionMeta) SessionMeta {
	now := time.Now().UTC()
	if strings.TrimSpace(meta.SessionID) == "" {
		meta.SessionID = fmt.Sprintf("%x", now.UnixNano())
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}
	return meta
}

func sessionPath(root string, meta SessionMeta) string {
	created := meta.CreatedAt.UTC()
	name := fmt.Sprintf(
		"rollout-%s-%s.jsonl",
		created.Format("2006-01-02T15-04-05"),
		meta.SessionID,
	)
	return filepath.Join(
		root,
		created.Format("2006"),
		created.Format("01"),
		created.Format("02"),
		name,
	)
}

func writeSessionMeta(writer *bufio.Writer, meta SessionMeta) error {
	line := sessionLine{
		Type:      "meta",
		SessionID: meta.SessionID,
		Model:     meta.Model,
		Provider:  meta.Provider,
		Profile:   meta.Profile,
		Reasoning: meta.Reasoning,
		CreatedAt: meta.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: meta.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	return writeSessionLine(writer, line)
}

func writeSessionMessages(writer *bufio.Writer, messages []runtime.Message) error {
	for _, message := range messages {
		line := sessionLine{
			Type:       "message",
			Role:       string(message.Role),
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
			Text:       message.Text(),
			ToolCalls:  sessionToolCallsFromRuntime(message.ToolCalls),
		}
		if err := writeSessionLine(writer, line); err != nil {
			return err
		}
	}
	return nil
}

func writeSessionLine(writer *bufio.Writer, line sessionLine) error {
	payload, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	if err := writer.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func sessionMetaFromLine(line sessionLine) SessionMeta {
	meta := SessionMeta{
		SessionID: line.SessionID,
		Model:     line.Model,
		Provider:  line.Provider,
		Profile:   line.Profile,
		Reasoning: line.Reasoning,
	}
	if parsed, err := time.Parse(time.RFC3339Nano, line.CreatedAt); err == nil {
		meta.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339Nano, line.UpdatedAt); err == nil {
		meta.UpdatedAt = parsed
	}
	return normalizeSessionMeta(meta)
}

func runtimeMessageFromLine(line sessionLine) runtime.Message {
	message := runtime.Message{
		Role:       runtime.Role(line.Role),
		Name:       line.Name,
		ToolCallID: line.ToolCallID,
		ToolCalls:  runtimeToolCallsFromSession(line.ToolCalls),
	}
	if strings.TrimSpace(line.Text) != "" {
		message.Content = []runtime.ContentPart{runtime.TextPart(line.Text)}
	}
	return message
}

func sessionToolCallsFromRuntime(calls []runtime.ToolCall) []sessionToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]sessionToolCall, 0, len(calls))
	for _, call := range calls {
		result = append(result, sessionToolCall{
			ID:        call.ID,
			Type:      string(call.Type),
			Name:      call.Function.Name,
			Arguments: call.Function.ArgumentsString(),
		})
	}
	return result
}

func runtimeToolCallsFromSession(calls []sessionToolCall) []runtime.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]runtime.ToolCall, 0, len(calls))
	for _, call := range calls {
		toolType := runtime.ToolType(call.Type)
		if toolType == "" {
			toolType = runtime.ToolTypeFunction
		}
		result = append(result, runtime.ToolCall{
			ID:   call.ID,
			Type: toolType,
			Function: runtime.FunctionCall{
				Name:      call.Name,
				Arguments: []byte(call.Arguments),
			},
		})
	}
	return result
}
