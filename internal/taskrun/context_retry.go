package taskrun

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
)

type contextCompactionStage struct {
	TailMessages int
	Label        string
}

type contextCompactionCandidate struct {
	Messages []runtime.Message
	Label    string
}

var contextCompactionStages = []contextCompactionStage{
	{TailMessages: 24, Label: "keep last 24 messages"},
	{TailMessages: 12, Label: "keep last 12 messages"},
	{TailMessages: 6, Label: "keep last 6 messages"},
	{TailMessages: 3, Label: "keep last 3 messages"},
}

func isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *provider.Error
	statusCode := 0
	var parts []string
	if errors.As(err, &providerErr) {
		statusCode = providerErr.StatusCode
		parts = append(parts, providerErr.Code, providerErr.Message)
	}
	parts = append(parts, err.Error())
	text := strings.ToLower(strings.Join(parts, " "))
	if text == "" {
		return false
	}

	if statusCode != 0 && statusCode != 400 && statusCode != 413 && statusCode != 422 {
		return false
	}

	markers := []string{
		"exceeded context",
		"maximum context",
		"too many input tokens",
		"context length exceeded",
		"maximum context length",
		"context overflow",
		"context_window_exceeded",
		"context length",
		"context_length_exceeded",
		"max context",
		"max input tokens",
		"too many tokens",
		"input token limit",
		"prompt tokens limit exceeded",
		"input is too long",
		"prompt is too long",
		"maximum prompt length",
		"maximum input length",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func buildContextCompactionCandidates(messages []runtime.Message) []contextCompactionCandidate {
	systemMessages := make([]runtime.Message, 0, len(messages))
	nonSystemMessages := make([]runtime.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == runtime.RoleSystem {
			systemMessages = append(systemMessages, message)
			continue
		}
		nonSystemMessages = append(nonSystemMessages, message)
	}
	if len(nonSystemMessages) <= 1 {
		return nil
	}

	candidates := make([]contextCompactionCandidate, 0, len(contextCompactionStages))
	last := cloneMessages(messages)
	for _, stage := range contextCompactionStages {
		if stage.TailMessages <= 0 || len(nonSystemMessages) <= stage.TailMessages {
			continue
		}
		start := len(nonSystemMessages) - stage.TailMessages
		compacted := make([]runtime.Message, 0, len(systemMessages)+stage.TailMessages)
		compacted = append(compacted, cloneMessages(systemMessages)...)
		compacted = append(compacted, cloneMessages(nonSystemMessages[start:])...)
		if messagesEquivalent(last, compacted) {
			continue
		}
		candidates = append(candidates, contextCompactionCandidate{
			Messages: compacted,
			Label:    stage.Label,
		})
		last = compacted
	}
	return candidates
}

func emitContextCompactionRetry(reporter progressReporter, round int, providerName string, model string, label string, err error) {
	message := "provider context limit exceeded"
	if strings.TrimSpace(label) != "" {
		message = fmt.Sprintf("%s; retrying with compact history (%s)", message, label)
	}
	if err != nil {
		message = fmt.Sprintf("%s: %s", message, err.Error())
	}
	reporter.Emit(ProgressUpdate{
		Kind:         ProgressKindRetryScheduled,
		Round:        round,
		ProviderName: providerName,
		Model:        model,
		RetryAfter:   0,
		Err:          errors.New(message),
	})
}

func messagesEquivalent(left []runtime.Message, right []runtime.Message) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].Role != right[idx].Role {
			return false
		}
		if left[idx].Name != right[idx].Name {
			return false
		}
		if left[idx].ToolCallID != right[idx].ToolCallID {
			return false
		}
		if left[idx].Refusal != right[idx].Refusal {
			return false
		}
		if !contentPartsEquivalent(left[idx].Content, right[idx].Content) {
			return false
		}
		if !toolCallsEquivalent(left[idx].ToolCalls, right[idx].ToolCalls) {
			return false
		}
	}
	return true
}

func contentPartsEquivalent(left []runtime.ContentPart, right []runtime.ContentPart) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].Type != right[idx].Type {
			return false
		}
		if left[idx].Text != right[idx].Text {
			return false
		}
		if left[idx].URL != right[idx].URL {
			return false
		}
		if left[idx].Detail != right[idx].Detail {
			return false
		}
		if left[idx].AudioData != right[idx].AudioData {
			return false
		}
		if left[idx].AudioFormat != right[idx].AudioFormat {
			return false
		}
		if left[idx].MIMEType != right[idx].MIMEType {
			return false
		}
	}
	return true
}

func toolCallsEquivalent(left []runtime.ToolCall, right []runtime.ToolCall) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].ID != right[idx].ID {
			return false
		}
		if left[idx].Type != right[idx].Type {
			return false
		}
		if left[idx].Function.Name != right[idx].Function.Name {
			return false
		}
		if strings.TrimSpace(left[idx].Function.ArgumentsString()) != strings.TrimSpace(right[idx].Function.ArgumentsString()) {
			return false
		}
	}
	return true
}
