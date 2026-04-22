package taskrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestIsContextOverflowError_MatchesProviderPayload(t *testing.T) {
	err := &provider.Error{
		Provider:   "OpenAI",
		StatusCode: 400,
		Code:       "context_length_exceeded",
		Message:    "Maximum context length exceeded for this model.",
	}
	if !isContextOverflowError(err) {
		t.Fatalf("expected context overflow match for %v", err)
	}
}

func TestRunSingleTurn_RetriesContextOverflowWithCompactedHistory(t *testing.T) {
	attempts := 0
	seenMessageCounts := make([]int, 0, 4)
	client := fakeProviderClient{
		name: "overflow-retry",
		createFn: func(_ context.Context, request runtime.Request) (*runtime.Response, error) {
			attempts++
			seenMessageCounts = append(seenMessageCounts, len(request.Messages))
			if len(request.Messages) > 13 {
				return nil, &provider.Error{
					Provider:   "OpenAI",
					StatusCode: 400,
					Message:    "too many input tokens for this request",
				}
			}
			return &runtime.Response{
				Model: "alpha-model",
				Choices: []runtime.Choice{{
					Index:   0,
					Message: runtime.TextMessage(runtime.RoleAssistant, "compacted ok"),
				}},
			}, nil
		},
	}

	request := runtime.Request{
		Model: "alpha-model",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "keep system"),
			runtime.TextMessage(runtime.RoleUser, "u1"),
			runtime.TextMessage(runtime.RoleAssistant, "a1"),
			runtime.TextMessage(runtime.RoleUser, "u2"),
			runtime.TextMessage(runtime.RoleAssistant, "a2"),
			runtime.TextMessage(runtime.RoleUser, "u3"),
			runtime.TextMessage(runtime.RoleAssistant, "a3"),
			runtime.TextMessage(runtime.RoleUser, "u4"),
			runtime.TextMessage(runtime.RoleAssistant, "a4"),
			runtime.TextMessage(runtime.RoleUser, "u5"),
			runtime.TextMessage(runtime.RoleAssistant, "a5"),
			runtime.TextMessage(runtime.RoleUser, "u6"),
			runtime.TextMessage(runtime.RoleAssistant, "a6"),
			runtime.TextMessage(runtime.RoleUser, "u7"),
			runtime.TextMessage(runtime.RoleAssistant, "a7"),
			runtime.TextMessage(runtime.RoleUser, "fresh"),
		},
	}

	var updates []ProgressUpdate
	response, _, assistant, err := runSingleTurn(context.Background(), client, &request, false, 1, progressReporter{
		fn: func(update ProgressUpdate) {
			updates = append(updates, update)
		},
	})
	if err != nil {
		t.Fatalf("runSingleTurn: %v", err)
	}
	if response == nil || assistant.Text() != "compacted ok" {
		t.Fatalf("unexpected response after compaction retry: %+v %+v", response, assistant)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want at least 2", attempts)
	}
	if len(seenMessageCounts) < 2 || seenMessageCounts[1] >= seenMessageCounts[0] {
		t.Fatalf("message counts did not shrink across retry: %+v", seenMessageCounts)
	}
	if len(request.Messages) >= 16 {
		t.Fatalf("request messages were not compacted: %d", len(request.Messages))
	}

	sawRetryNote := false
	for _, update := range updates {
		if update.Kind != ProgressKindRetryScheduled || update.Err == nil {
			continue
		}
		if strings.Contains(strings.ToLower(update.Err.Error()), "compact history") {
			sawRetryNote = true
			break
		}
	}
	if !sawRetryNote {
		t.Fatalf("expected compaction retry progress note, got %+v", updates)
	}
}

func TestIsContextOverflowError_DoesNotTreatGenericBadRequestAsOverflow(t *testing.T) {
	err := &provider.Error{
		Provider:   "OpenAI",
		StatusCode: 400,
		Code:       "invalid_request_error",
		Message:    "temperature must be between 0 and 2",
	}
	if isContextOverflowError(err) {
		t.Fatalf("unexpected context overflow match for %v", err)
	}
	if isContextOverflowError(errors.New("plain failure")) {
		t.Fatal("unexpected generic error match")
	}
}
