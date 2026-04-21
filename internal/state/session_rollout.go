package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

var rolloutSessionIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

type sessionFormatProbe struct {
	Type string `json:"type"`
}

type rolloutEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutPayloadKind struct {
	Type string `json:"type"`
}

type rolloutSessionMetaPayload struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	CWD           string `json:"cwd"`
	Model         string `json:"model"`
	ModelProvider string `json:"model_provider"`
	Provider      string `json:"provider"`
	Profile       string `json:"profile"`
	Reasoning     string `json:"reasoning"`
	Effort        string `json:"effort"`
	Branch        string `json:"branch"`
	Git           struct {
		Branch string `json:"branch"`
	} `json:"git"`
	GitInfo struct {
		Branch string `json:"branch"`
	} `json:"git_info"`
}

type rolloutResponseMessagePayload struct {
	Type       string                       `json:"type"`
	Role       string                       `json:"role"`
	Name       string                       `json:"name"`
	ToolCallID string                       `json:"tool_call_id"`
	Refusal    string                       `json:"refusal"`
	Content    []rolloutResponseContentItem `json:"content"`
}

type rolloutResponseContentItem struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	URL         string `json:"url"`
	ImageURL    string `json:"image_url"`
	Detail      string `json:"detail"`
	AudioData   string `json:"audio_data"`
	AudioFormat string `json:"audio_format"`
	MIMEType    string `json:"mime_type"`
}

type rolloutFunctionCallPayload struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	CallID    string          `json:"call_id"`
}

type rolloutFunctionCallOutputPayload struct {
	Type   string          `json:"type"`
	CallID string          `json:"call_id"`
	Output json.RawMessage `json:"output"`
}

type rolloutEventMessagePayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Phase   string `json:"phase"`
}

type sessionAccumulator struct {
	path            string
	includeMessages bool
	targetSessionID string
	meta            SessionMeta
	messages        []runtime.Message
	firstTimestamp  time.Time
	lastTimestamp   time.Time
	previewPriority int
}

const (
	sessionPreviewPriorityAssistant = 1
	sessionPreviewPriorityUser      = 2
	sessionPreviewPriorityEventUser = 3
	sessionPreviewPriorityMeta      = 4
)

func loadSessionFile(path string, includeMessages bool) (SessionMeta, []runtime.Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	defer file.Close()

	accumulator := newSessionAccumulator(path, includeMessages)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := accumulator.consumeLine(line); err != nil {
			return SessionMeta{}, nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return SessionMeta{}, nil, err
	}
	meta, messages := accumulator.finalize()
	return meta, messages, nil
}

func newSessionAccumulator(path string, includeMessages bool) *sessionAccumulator {
	return &sessionAccumulator{
		path:            path,
		includeMessages: includeMessages,
		targetSessionID: rolloutSessionIDPattern.FindString(filepath.Base(path)),
		messages:        make([]runtime.Message, 0, 16),
	}
}

func (a *sessionAccumulator) consumeLine(line string) error {
	var probe sessionFormatProbe
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return fmt.Errorf("decode session line: %w", err)
	}

	switch probe.Type {
	case "meta", "message":
		return a.consumeNativeLine([]byte(line))
	case "session_meta", "event_msg", "response_item", "turn_context":
		return a.consumeRolloutLine([]byte(line))
	default:
		return nil
	}
}

func (a *sessionAccumulator) consumeNativeLine(line []byte) error {
	var current sessionLine
	if err := json.Unmarshal(line, &current); err != nil {
		return fmt.Errorf("decode native session line: %w", err)
	}

	switch current.Type {
	case "meta":
		a.mergeMeta(sessionMetaFromLine(current))
	case "message":
		a.appendMessage(runtimeMessageFromLine(current), false)
	}
	return nil
}

func (a *sessionAccumulator) consumeRolloutLine(line []byte) error {
	var envelope rolloutEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode rollout session line: %w", err)
	}

	a.observeTimestamp(parseSessionTimestamp(envelope.Timestamp))
	switch envelope.Type {
	case "session_meta":
		return a.consumeRolloutSessionMeta(envelope)
	case "turn_context":
		return a.consumeRolloutTurnContext(envelope)
	case "response_item":
		return a.consumeRolloutResponseItem(envelope)
	case "event_msg":
		return a.consumeRolloutEvent(envelope)
	default:
		return nil
	}
}

func (a *sessionAccumulator) consumeRolloutSessionMeta(envelope rolloutEnvelope) error {
	var payload rolloutSessionMetaPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode rollout session meta: %w", err)
	}

	payloadID := strings.TrimSpace(payload.ID)
	if payloadID != "" {
		if a.targetSessionID == "" {
			a.targetSessionID = payloadID
		}
		if !strings.EqualFold(a.targetSessionID, payloadID) {
			return nil
		}
	}

	payloadMap := unmarshalJSONObject(envelope.Payload)
	createdAt := firstNonZeroTime(
		parseSessionTimestamp(payload.Timestamp),
		parseSessionTimestamp(envelope.Timestamp),
	)

	a.mergeMeta(SessionMeta{
		SessionID: firstNonEmpty(payloadID, a.targetSessionID),
		Model: firstNonEmpty(
			strings.TrimSpace(payload.Model),
			firstJSONString(payloadMap, "model", "model_slug"),
		),
		Provider: firstNonEmpty(
			strings.TrimSpace(payload.ModelProvider),
			strings.TrimSpace(payload.Provider),
			firstJSONString(payloadMap, "provider", "provider_id"),
		),
		Profile: firstJSONString(payloadMap, "profile", "profile_name", "active_profile"),
		Reasoning: firstNonEmpty(
			strings.TrimSpace(payload.Reasoning),
			strings.TrimSpace(payload.Effort),
			firstJSONString(payloadMap, "reasoning", "reasoning_effort", "effort"),
		),
		CWD: firstNonEmpty(
			strings.TrimSpace(payload.CWD),
			firstJSONString(payloadMap, "cwd"),
		),
		Branch: firstNonEmpty(
			strings.TrimSpace(payload.Git.Branch),
			strings.TrimSpace(payload.GitInfo.Branch),
			strings.TrimSpace(payload.Branch),
			firstJSONString(payloadMap, "git.branch", "git_info.branch", "branch"),
		),
		CreatedAt: createdAt,
		UpdatedAt: firstNonZeroTime(parseSessionTimestamp(envelope.Timestamp), createdAt),
	})
	return nil
}

func (a *sessionAccumulator) consumeRolloutTurnContext(envelope rolloutEnvelope) error {
	payloadMap := unmarshalJSONObject(envelope.Payload)
	a.mergeMeta(SessionMeta{
		Model: firstJSONString(
			payloadMap,
			"model",
			"model_slug",
			"collaboration_mode.settings.model",
		),
		Provider: firstJSONString(
			payloadMap,
			"provider",
			"provider_id",
			"model_provider",
		),
		Profile: firstJSONString(
			payloadMap,
			"profile",
			"profile_name",
			"active_profile",
		),
		Reasoning: firstJSONString(
			payloadMap,
			"reasoning",
			"reasoning_effort",
			"effort",
			"collaboration_mode.settings.reasoning_effort",
		),
		CWD:       firstJSONString(payloadMap, "cwd"),
		UpdatedAt: parseSessionTimestamp(envelope.Timestamp),
	})
	return nil
}

func (a *sessionAccumulator) consumeRolloutResponseItem(envelope rolloutEnvelope) error {
	var kind rolloutPayloadKind
	if err := json.Unmarshal(envelope.Payload, &kind); err != nil {
		return fmt.Errorf("decode rollout response item kind: %w", err)
	}

	switch kind.Type {
	case "message":
		var payload rolloutResponseMessagePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return fmt.Errorf("decode rollout response message: %w", err)
		}
		message, ok := runtimeMessageFromRolloutPayload(payload)
		if ok {
			a.appendMessage(message, false)
		}
	case "function_call":
		var payload rolloutFunctionCallPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return fmt.Errorf("decode rollout function call: %w", err)
		}
		callID := strings.TrimSpace(payload.CallID)
		if callID == "" {
			return nil
		}
		a.appendMessage(runtime.Message{
			Role: runtime.RoleAssistant,
			ToolCalls: []runtime.ToolCall{{
				ID:   callID,
				Type: runtime.ToolTypeFunction,
				Function: runtime.FunctionCall{
					Name:      strings.TrimSpace(payload.Name),
					Arguments: normalizeArgumentsRaw(argumentString(payload.Arguments)),
				},
			}},
		}, false)
	case "function_call_output":
		var payload rolloutFunctionCallOutputPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return fmt.Errorf("decode rollout function call output: %w", err)
		}
		callID := strings.TrimSpace(payload.CallID)
		if callID == "" {
			return nil
		}
		output := rawJSONText(payload.Output)
		if strings.TrimSpace(output) == "" {
			return nil
		}
		a.appendMessage(runtime.Message{
			Role:       runtime.RoleTool,
			ToolCallID: callID,
			Content:    []runtime.ContentPart{runtime.TextPart(output)},
		}, false)
	}
	return nil
}

func (a *sessionAccumulator) consumeRolloutEvent(envelope rolloutEnvelope) error {
	var payload rolloutEventMessagePayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode rollout event message: %w", err)
	}

	text := strings.TrimSpace(payload.Message)
	if text == "" {
		return nil
	}
	switch payload.Type {
	case "user_message":
		a.setPreview(text, sessionPreviewPriorityEventUser)
		a.appendMessage(runtime.TextMessage(runtime.RoleUser, text), true)
	case "agent_message":
		if strings.EqualFold(strings.TrimSpace(payload.Phase), "commentary") {
			return nil
		}
		a.appendMessage(runtime.TextMessage(runtime.RoleAssistant, text), true)
	}
	return nil
}

func (a *sessionAccumulator) mergeMeta(next SessionMeta) {
	a.meta = mergeSessionMeta(a.meta, next)
	if strings.TrimSpace(next.Preview) != "" {
		a.setPreview(next.Preview, sessionPreviewPriorityMeta)
	}
}

func (a *sessionAccumulator) observeTimestamp(ts time.Time) {
	if ts.IsZero() {
		return
	}
	if a.firstTimestamp.IsZero() || ts.Before(a.firstTimestamp) {
		a.firstTimestamp = ts
	}
	if a.lastTimestamp.IsZero() || ts.After(a.lastTimestamp) {
		a.lastTimestamp = ts
	}
}

func (a *sessionAccumulator) appendMessage(message runtime.Message, dedupe bool) {
	if !messageHasPayload(message) {
		return
	}

	switch message.Role {
	case runtime.RoleUser:
		if text := message.Text(); !looksLikeInjectedUserContext(text) {
			a.setPreview(text, sessionPreviewPriorityUser)
		}
	case runtime.RoleAssistant:
		a.setPreview(message.Text(), sessionPreviewPriorityAssistant)
	}

	if !a.includeMessages {
		return
	}
	if dedupe && a.hasRecentEquivalentMessage(message, 6) {
		return
	}
	a.messages = append(a.messages, message)
}

func (a *sessionAccumulator) hasRecentEquivalentMessage(candidate runtime.Message, window int) bool {
	if window <= 0 {
		window = 1
	}
	start := len(a.messages) - window
	if start < 0 {
		start = 0
	}
	for index := len(a.messages) - 1; index >= start; index-- {
		if sessionMessageEqual(a.messages[index], candidate) {
			return true
		}
	}
	return false
}

func (a *sessionAccumulator) setPreview(text string, priority int) {
	text = normalizeSessionPreview(text)
	if text == "" || priority < a.previewPriority {
		return
	}
	if priority > a.previewPriority || strings.TrimSpace(a.meta.Preview) == "" {
		a.meta.Preview = text
		a.previewPriority = priority
	}
}

func (a *sessionAccumulator) finalize() (SessionMeta, []runtime.Message) {
	if strings.TrimSpace(a.meta.SessionID) == "" {
		a.meta.SessionID = firstNonEmpty(a.targetSessionID, fallbackSessionIDFromPath(a.path))
	}
	if strings.TrimSpace(a.meta.Preview) == "" {
		a.meta.Preview = previewFromRuntimeMessages(a.messages)
	}
	if a.meta.CreatedAt.IsZero() {
		a.meta.CreatedAt = a.firstTimestamp
	}
	if a.meta.UpdatedAt.IsZero() {
		a.meta.UpdatedAt = a.lastTimestamp
	}
	a.meta = normalizeSessionMeta(a.meta)

	if !a.includeMessages {
		return a.meta, nil
	}
	result := make([]runtime.Message, len(a.messages))
	copy(result, a.messages)
	return a.meta, result
}

func runtimeMessageFromRolloutPayload(payload rolloutResponseMessagePayload) (runtime.Message, bool) {
	role, ok := normalizeRolloutRole(payload.Role)
	if !ok {
		return runtime.Message{}, false
	}

	message := runtime.Message{
		Role:       role,
		Name:       strings.TrimSpace(payload.Name),
		ToolCallID: strings.TrimSpace(payload.ToolCallID),
		Refusal:    strings.TrimSpace(payload.Refusal),
	}
	for _, item := range payload.Content {
		switch strings.TrimSpace(item.Type) {
		case "input_text", "output_text", "text":
			if text := strings.TrimSpace(item.Text); text != "" {
				message.Content = append(message.Content, runtime.TextPart(text))
			}
		case "refusal":
			if message.Refusal == "" {
				message.Refusal = strings.TrimSpace(item.Text)
			}
		case "input_image", "output_image", "image_url":
			url := firstNonEmpty(strings.TrimSpace(item.URL), strings.TrimSpace(item.ImageURL))
			if url != "" {
				message.Content = append(message.Content, runtime.ContentPart{
					Type:   runtime.ContentPartTypeImageURL,
					URL:    url,
					Detail: strings.TrimSpace(item.Detail),
				})
			}
		case "input_audio":
			if strings.TrimSpace(item.AudioData) != "" {
				message.Content = append(message.Content, runtime.ContentPart{
					Type:        runtime.ContentPartTypeInputAudio,
					AudioData:   item.AudioData,
					AudioFormat: item.AudioFormat,
					MIMEType:    item.MIMEType,
				})
			}
		}
	}

	if message.Role == runtime.RoleTool && strings.TrimSpace(message.ToolCallID) == "" {
		return runtime.Message{}, false
	}
	if !messageHasPayload(message) {
		return runtime.Message{}, false
	}
	return message, true
}

func normalizeRolloutRole(role string) (runtime.Role, bool) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return runtime.RoleSystem, true
	case "user":
		return runtime.RoleUser, true
	case "assistant":
		return runtime.RoleAssistant, true
	case "tool":
		return runtime.RoleTool, true
	default:
		return "", false
	}
}

func messageHasPayload(message runtime.Message) bool {
	return len(message.Content) > 0 ||
		len(message.ToolCalls) > 0 ||
		strings.TrimSpace(message.Refusal) != ""
}

func parseSessionTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func unmarshalJSONObject(raw json.RawMessage) map[string]any {
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}

func firstJSONString(root map[string]any, paths ...string) string {
	for _, path := range paths {
		if value := jsonPathString(root, path); value != "" {
			return value
		}
	}
	return ""
}

func jsonPathString(root map[string]any, path string) string {
	if len(root) == 0 {
		return ""
	}
	var current any = root
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := object[segment]
		if !ok {
			return ""
		}
		current = next
	}
	if value, ok := current.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func argumentString(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(string(raw))
}

func normalizeArgumentsRaw(text string) json.RawMessage {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	quoted, err := json.Marshal(text)
	if err != nil {
		return nil
	}
	return quoted
}

func rawJSONText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func normalizeSessionPreview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	const maxRunes = 120
	runes := []rune(text)
	if len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
	}
	return text
}

func previewFromRuntimeMessages(messages []runtime.Message) string {
	for _, message := range messages {
		if message.Role == runtime.RoleUser {
			if text := strings.TrimSpace(message.Text()); text != "" && !looksLikeInjectedUserContext(text) {
				return normalizeSessionPreview(text)
			}
		}
	}
	for _, message := range messages {
		if message.Role == runtime.RoleAssistant {
			if text := strings.TrimSpace(message.Text()); text != "" {
				return normalizeSessionPreview(text)
			}
		}
	}
	return ""
}

func looksLikeInjectedUserContext(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	return strings.HasPrefix(text, "# AGENTS.md instructions") ||
		strings.HasPrefix(text, "<environment_context>") ||
		strings.HasPrefix(text, "## JavaScript REPL (Node)") ||
		strings.Contains(text, "# AGENTS.md instructions for ")
}

func fallbackSessionIDFromPath(path string) string {
	base := filepath.Base(path)
	if matched := rolloutSessionIDPattern.FindString(base); matched != "" {
		return matched
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.TrimSpace(base)
}
