package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	Content    []sessionContent  `json:"content,omitempty"`
	ToolCalls  []sessionToolCall `json:"tool_calls,omitempty"`
	Refusal    string            `json:"refusal,omitempty"`
}

type sessionToolCall struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type sessionContent struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
	Detail      string `json:"detail,omitempty"`
	AudioData   string `json:"audio_data,omitempty"`
	AudioFormat string `json:"audio_format,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

func CreateSession(root string, meta SessionMeta, messages []runtime.Message) (SessionEntry, error) {
	meta = normalizeSessionMeta(meta)
	path := sessionPath(root, meta)
	if err := persistSession(path, meta, messages); err != nil {
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

	meta, existing, err := LoadSession(path)
	if err != nil {
		return err
	}
	meta.UpdatedAt = time.Now().UTC()
	return persistSession(path, meta, append(existing, messages...))
}

func AppendSessionHistory(path string, meta SessionMeta, history []runtime.Message) error {
	currentMeta, existing, err := LoadSession(path)
	if err != nil {
		return err
	}
	if len(history) < len(existing) {
		return fmt.Errorf("session history shorter than persisted history: have %d want at least %d", len(history), len(existing))
	}
	for idx := range existing {
		if !sessionMessageEqual(existing[idx], history[idx]) {
			return fmt.Errorf("session history prefix mismatch at message %d", idx)
		}
	}
	merged := mergeSessionMeta(currentMeta, meta)
	if meta.UpdatedAt.IsZero() {
		merged.UpdatedAt = time.Now().UTC()
	}
	return persistSession(path, merged, history)
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
			Content:    sessionContentFromRuntime(message.Content),
			ToolCalls:  sessionToolCallsFromRuntime(message.ToolCalls),
			Refusal:    message.Refusal,
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
		Refusal:    line.Refusal,
	}
	if len(line.Content) > 0 {
		message.Content = runtimeContentFromSession(line.Content)
	} else if strings.TrimSpace(line.Text) != "" {
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

func sessionContentFromRuntime(parts []runtime.ContentPart) []sessionContent {
	if len(parts) == 0 {
		return nil
	}
	result := make([]sessionContent, 0, len(parts))
	for _, part := range parts {
		result = append(result, sessionContent{
			Type:        string(part.Type),
			Text:        part.Text,
			URL:         part.URL,
			Detail:      part.Detail,
			AudioData:   part.AudioData,
			AudioFormat: part.AudioFormat,
			MIMEType:    part.MIMEType,
		})
	}
	return result
}

func runtimeContentFromSession(parts []sessionContent) []runtime.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	result := make([]runtime.ContentPart, 0, len(parts))
	for _, part := range parts {
		result = append(result, runtime.ContentPart{
			Type:        runtime.ContentPartType(part.Type),
			Text:        part.Text,
			URL:         part.URL,
			Detail:      part.Detail,
			AudioData:   part.AudioData,
			AudioFormat: part.AudioFormat,
			MIMEType:    part.MIMEType,
		})
	}
	return result
}

func mergeSessionMeta(current SessionMeta, next SessionMeta) SessionMeta {
	merged := current
	if strings.TrimSpace(merged.SessionID) == "" {
		merged.SessionID = next.SessionID
	}
	if strings.TrimSpace(next.Model) != "" {
		merged.Model = next.Model
	}
	if strings.TrimSpace(next.Provider) != "" {
		merged.Provider = next.Provider
	}
	if strings.TrimSpace(next.Profile) != "" {
		merged.Profile = next.Profile
	}
	if strings.TrimSpace(next.Reasoning) != "" {
		merged.Reasoning = next.Reasoning
	}
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = next.CreatedAt
	}
	if !next.UpdatedAt.IsZero() {
		merged.UpdatedAt = next.UpdatedAt
	}
	return normalizeSessionMeta(merged)
}

func sessionMessageEqual(left runtime.Message, right runtime.Message) bool {
	return left.Role == right.Role &&
		left.Name == right.Name &&
		left.ToolCallID == right.ToolCallID &&
		left.Refusal == right.Refusal &&
		reflect.DeepEqual(left.Content, right.Content) &&
		reflect.DeepEqual(left.ToolCalls, right.ToolCalls)
}

func persistSession(path string, meta SessionMeta, messages []runtime.Message) (err error) {
	meta = normalizeSessionMeta(meta)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".session-*.jsonl")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if chmodErr := file.Chmod(0o644); chmodErr != nil {
		_ = file.Close()
		return chmodErr
	}

	writer := bufio.NewWriter(file)
	if err := writeSessionMeta(writer, meta); err != nil {
		_ = file.Close()
		return err
	}
	if err := writeSessionMessages(writer, messages); err != nil {
		_ = file.Close()
		return err
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
