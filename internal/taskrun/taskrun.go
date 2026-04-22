package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/provider/responsesapi"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type Options struct {
	Prompt           string
	SystemPrompt     string
	Model            string
	Profile          string
	Provider         string
	ReasoningEffort  string
	CWD              string
	ToolPolicy       tooling.ToolPolicy
	ApprovalStore    *ApprovalSessionStore
	OnProgress       func(ProgressUpdate)
	OnApproval       ApprovalHandler
	JSON             bool
	DisableStreaming bool
	History          []runtime.Message
}

type Result struct {
	ProviderName     string                    `json:"provider_name"`
	Model            string                    `json:"model"`
	Reasoning        string                    `json:"reasoning,omitempty"`
	Profile          string                    `json:"profile,omitempty"`
	CWD              string                    `json:"cwd,omitempty"`
	SessionPath      string                    `json:"session_path,omitempty"`
	Response         *runtime.Response         `json:"response,omitempty"`
	Events           []runtime.StreamEvent     `json:"events,omitempty"`
	ToolReports      []tooling.ExecutionReport `json:"tool_reports,omitempty"`
	Text             string                    `json:"text,omitempty"`
	History          []runtime.Message         `json:"-"`
	RequestMessages  []runtime.Message         `json:"-"`
	AssistantMessage runtime.Message           `json:"-"`
}

type ProgressKind string

const (
	ProgressKindTurnStarted       ProgressKind = "turn_started"
	ProgressKindAssistantSnapshot ProgressKind = "assistant_snapshot"
	ProgressKindToolPlanned       ProgressKind = "tool_planned"
	ProgressKindToolResult        ProgressKind = "tool_result"
	ProgressKindApprovalRequired  ProgressKind = "approval_required"
	ProgressKindRetryScheduled    ProgressKind = "retry_scheduled"
	ProgressKindTurnDone          ProgressKind = "turn_done"
	ProgressKindTurnFailed        ProgressKind = "turn_failed"
)

type PartialAssistantSnapshot struct {
	ResponseID   string               `json:"response_id,omitempty"`
	Model        string               `json:"model,omitempty"`
	Text         string               `json:"text,omitempty"`
	ToolCalls    []runtime.ToolCall   `json:"tool_calls,omitempty"`
	Usage        runtime.Usage        `json:"usage,omitempty"`
	FinishReason runtime.FinishReason `json:"finish_reason,omitempty"`
}

type ApprovalDecision string

const (
	ApprovalDecisionApprove           ApprovalDecision = "approve"
	ApprovalDecisionApproveForSession ApprovalDecision = "approve_for_session"
	ApprovalDecisionDeny              ApprovalDecision = "deny"
)

type ApprovalHandler func(context.Context, tooling.ApprovalRequest) (ApprovalDecision, error)

type ProgressUpdate struct {
	Kind            ProgressKind                `json:"kind"`
	Round           int                         `json:"round,omitempty"`
	Prompt          string                      `json:"prompt,omitempty"`
	ProviderName    string                      `json:"provider_name,omitempty"`
	Model           string                      `json:"model,omitempty"`
	Profile         string                      `json:"profile,omitempty"`
	Reasoning       string                      `json:"reasoning,omitempty"`
	Snapshot        PartialAssistantSnapshot    `json:"snapshot,omitempty"`
	ToolPlan        *tooling.ExecutionPlan      `json:"tool_plan,omitempty"`
	ToolResult      *tooling.ToolResultEnvelope `json:"tool_result,omitempty"`
	ApprovalRequest *tooling.ApprovalRequest    `json:"approval_request,omitempty"`
	RetryAfter      time.Duration               `json:"retry_after,omitempty"`
	Err             error                       `json:"-"`
}

type progressReporter struct {
	fn func(ProgressUpdate)
}

func (r progressReporter) Emit(update ProgressUpdate) {
	if r.fn == nil {
		return
	}
	r.fn(update)
}

const (
	maxProviderAttempts = 4
	baseProviderBackoff = time.Second
	maxProviderBackoff  = 30 * time.Second
)

var workingDirectoryMu sync.Mutex

func (result Result) FullHistory() []runtime.Message {
	if len(result.History) > 0 {
		return cloneMessages(result.History)
	}
	history := cloneMessages(result.RequestMessages)
	if hasPersistableMessage(result.AssistantMessage) {
		history = append(history, result.AssistantMessage)
	}
	return history
}

func Run(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.Prompt) == "" {
		return Result{}, fmt.Errorf("prompt is required")
	}

	config, err := state.LoadConfig(apphome.ConfigPath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("load config: %w", err)
		}
		config = state.Config{}
	}

	resolved, err := resolveRequest(config, options)
	if err != nil {
		return Result{}, err
	}

	request := runtime.Request{
		Model:           resolved.Model,
		ReasoningEffort: resolved.ReasoningEffort,
		Messages:        resolved.Messages,
	}
	toolPolicy := tooling.NormalizeToolPolicy(options.ToolPolicy)
	reporter := progressReporter{fn: options.OnProgress}
	if resolved.Client.Capabilities().Tools {
		request.Tools = tooling.DefinitionsWithPolicy(toolPolicy)
		if len(request.Tools) > 0 {
			request.ToolChoice = runtime.ToolChoice{Mode: runtime.ToolChoiceModeAuto}
			parallel := true
			request.ParallelToolCalls = &parallel
		}
	}

	result := Result{
		ProviderName:    resolved.ProviderName,
		Model:           resolved.Model,
		Reasoning:       resolved.ReasoningEffort,
		Profile:         resolved.ProfileName,
		CWD:             resolved.CWD,
		RequestMessages: cloneMessages(resolved.Messages),
	}
	restoreCWD, err := enterWorkingDirectory(resolved.CWD)
	if err != nil {
		return Result{}, err
	}
	defer restoreCWD()
	preferStreaming := !options.JSON && !options.DisableStreaming
	reporter.Emit(ProgressUpdate{
		Kind:         ProgressKindTurnStarted,
		Round:        1,
		Prompt:       options.Prompt,
		ProviderName: resolved.ProviderName,
		Model:        resolved.Model,
		Profile:      resolved.ProfileName,
		Reasoning:    resolved.ReasoningEffort,
	})

	if len(request.Tools) > 0 {
		workerBase := workerRuntimeOptions{
			SystemPrompt: systemPromptFromMessages(resolved.Messages),
			Model:        resolved.Model,
			Provider:     resolved.ProviderName,
			Profile:      resolved.ProfileName,
			Reasoning:    resolved.ReasoningEffort,
			CWD:          resolved.CWD,
			ToolPolicy:   toolPolicy,
			History:      cloneMessages(resolved.Messages),
		}
		history, requestMessages, response, assistantMessage, events, toolReports, err := runWithToolLoop(ctx, resolved.Client, request, preferStreaming, toolPolicy, options.ApprovalStore, options.OnApproval, reporter, workerBase)
		if err != nil {
			reporter.Emit(ProgressUpdate{Kind: ProgressKindTurnFailed, Err: err})
			return Result{}, err
		}
		result.History = cloneMessages(history)
		result.RequestMessages = cloneMessages(requestMessages)
		result.Response = response
		result.Events = append(result.Events, events...)
		result.ToolReports = append(result.ToolReports, toolReports...)
		result.Text = strings.TrimSpace(assistantMessage.Text())
		if result.Text == "" {
			result.Text = responseText(response)
		}
		result.AssistantMessage = assistantMessage
		reporter.Emit(ProgressUpdate{
			Kind:         ProgressKindTurnDone,
			Round:        1,
			ProviderName: result.ProviderName,
			Model:        result.Model,
			Profile:      result.Profile,
			Reasoning:    result.Reasoning,
			Snapshot:     snapshotFromMessage(response, assistantMessage),
		})
		return result, nil
	}

	if !preferStreaming {
		response, _, assistantMessage, err := runSingleTurn(ctx, resolved.Client, &request, false, 1, reporter)
		if err != nil {
			reporter.Emit(ProgressUpdate{Kind: ProgressKindTurnFailed, Err: err})
			return Result{}, err
		}
		result.Response = response
		result.Text = responseText(response)
		if hasPersistableMessage(assistantMessage) {
			result.AssistantMessage = assistantMessage
		} else if strings.TrimSpace(result.Text) != "" {
			result.AssistantMessage = runtime.TextMessage(runtime.RoleAssistant, result.Text)
		}
		result.History = append(cloneMessages(request.Messages), result.AssistantMessage)
		reporter.Emit(ProgressUpdate{
			Kind:         ProgressKindTurnDone,
			Round:        1,
			ProviderName: result.ProviderName,
			Model:        result.Model,
			Profile:      result.Profile,
			Reasoning:    result.Reasoning,
			Snapshot:     snapshotFromMessage(result.Response, result.AssistantMessage),
		})
		return result, nil
	}

	response, events, assistantMessage, err := runSingleTurn(ctx, resolved.Client, &request, true, 1, reporter)
	if err != nil {
		reporter.Emit(ProgressUpdate{Kind: ProgressKindTurnFailed, Err: err})
		return Result{}, err
	}
	result.Response = response
	result.Events = append(result.Events, events...)
	result.AssistantMessage = assistantMessage
	result.Text = strings.TrimSpace(assistantMessage.Text())
	if result.Text == "" {
		result.Text = responseText(response)
	}
	result.History = append(cloneMessages(request.Messages), assistantMessage)
	reporter.Emit(ProgressUpdate{
		Kind:         ProgressKindTurnDone,
		Round:        1,
		ProviderName: result.ProviderName,
		Model:        result.Model,
		Profile:      result.Profile,
		Reasoning:    result.Reasoning,
		Snapshot:     snapshotFromMessage(result.Response, result.AssistantMessage),
	})
	return result, nil
}

func runWithToolLoop(ctx context.Context, client provider.Client, request runtime.Request, preferStreaming bool, toolPolicy tooling.ToolPolicy, approvalStore *ApprovalSessionStore, approvalHandler ApprovalHandler, reporter progressReporter, workerBase workerRuntimeOptions) ([]runtime.Message, []runtime.Message, *runtime.Response, runtime.Message, []runtime.StreamEvent, []tooling.ExecutionReport, error) {
	const maxRounds = 8

	messages := cloneMessages(request.Messages)
	baseRequest := request
	if approvalStore == nil {
		approvalStore = newApprovalSessionStore()
	}
	approvalStore.beginTurn()
	var events []runtime.StreamEvent
	var toolReports []tooling.ExecutionReport

	for round := 0; round < maxRounds; round++ {
		currentRequest := baseRequest
		currentRequest.Messages = cloneMessages(messages)
		if round > 0 {
			reporter.Emit(ProgressUpdate{
				Kind:  ProgressKindTurnStarted,
				Round: round + 1,
				Model: currentRequest.Model,
			})
		}

		response, roundEvents, assistantMessage, err := runSingleTurn(ctx, client, &currentRequest, preferStreaming, round+1, reporter)
		if err != nil {
			return nil, nil, nil, runtime.Message{}, nil, nil, err
		}
		events = append(events, roundEvents...)
		if len(assistantMessage.ToolCalls) == 0 {
			fullHistory := cloneMessages(messages)
			if hasPersistableMessage(assistantMessage) {
				fullHistory = append(fullHistory, assistantMessage)
			}
			return fullHistory, currentRequest.Messages, response, assistantMessage, events, toolReports, nil
		}

		messages = append(messages, assistantMessage)
		plan := tooling.BuildExecutionPlanWithToolPolicy(assistantMessage.ToolCalls, toolPolicy)
		reporter.Emit(ProgressUpdate{
			Kind:         ProgressKindToolPlanned,
			Round:        round + 1,
			ProviderName: client.Name(),
			Model:        currentRequest.Model,
			ToolPlan:     &plan,
		})
		resolvedPlan, err := resolveToolApprovals(ctx, plan, approvalStore, approvalHandler, reporter, client.Name(), currentRequest.Model, round+1)
		if err != nil {
			return nil, nil, nil, runtime.Message{}, nil, nil, err
		}
		workerOptions := workerBase
		workerOptions.Model = currentRequest.Model
		workerOptions.Reasoning = currentRequest.ReasoningEffort
		workerOptions.History = cloneMessages(currentRequest.Messages)
		executionCtx := tooling.WithWorkerRuntime(ctx, newWorkerToolRuntime(workerOptions))
		report := tooling.ExecutePlan(executionCtx, resolvedPlan)
		toolReports = append(toolReports, report)
		if approvalHandler == nil {
			for _, approval := range report.ApprovalRequests {
				copy := approval
				reporter.Emit(ProgressUpdate{
					Kind:            ProgressKindApprovalRequired,
					Round:           round + 1,
					ProviderName:    client.Name(),
					Model:           currentRequest.Model,
					ApprovalRequest: &copy,
				})
			}
		}
		for _, toolResult := range report.Results {
			copy := toolResult
			reporter.Emit(ProgressUpdate{
				Kind:         ProgressKindToolResult,
				Round:        round + 1,
				ProviderName: client.Name(),
				Model:        currentRequest.Model,
				ToolResult:   &copy,
			})
		}
		messages = append(messages, report.Messages()...)
	}

	return nil, nil, nil, runtime.Message{}, nil, nil, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
}

func resolveToolApprovals(ctx context.Context, plan tooling.ExecutionPlan, approvalStore *approvalSessionStore, approvalHandler ApprovalHandler, reporter progressReporter, providerName string, model string, round int) (tooling.ExecutionPlan, error) {
	if approvalStore == nil && approvalHandler == nil {
		return plan, nil
	}
	resolved := plan
	for batchIndex := range resolved.Batches {
		for callIndex := range resolved.Batches[batchIndex].Calls {
			call := resolved.Batches[batchIndex].Calls[callIndex]
			if call.Metadata.Permission != tooling.ToolPermissionApprovalRequired {
				continue
			}
			if approvalStore != nil {
				match := approvalStore.match(call)
				if match.allowed {
					decision := ApprovalDecisionApprove
					if match.scope == tooling.PermissionGrantScopeSession {
						decision = ApprovalDecisionApproveForSession
					}
					applyApprovalDecision(&resolved.Batches[batchIndex].Calls[callIndex], decision)
					if len(match.writableRoots) > 0 {
						resolved.Batches[batchIndex].Calls[callIndex].Metadata.GrantedWritableRoots = append([]string(nil), match.writableRoots...)
						resolved.Batches[batchIndex].Calls[callIndex].Metadata.PermissionGrantScope = match.scope
					}
					continue
				}
			}
			if approvalHandler == nil {
				continue
			}
			request := tooling.ApprovalRequestForCall(resolved.Batches[batchIndex].Index, call)
			reporter.Emit(ProgressUpdate{
				Kind:            ProgressKindApprovalRequired,
				Round:           round,
				ProviderName:    providerName,
				Model:           model,
				ApprovalRequest: &request,
			})
			decision, err := approvalHandler(ctx, request)
			if err != nil {
				return plan, err
			}
			applyApprovalDecision(&resolved.Batches[batchIndex].Calls[callIndex], decision)
			if approvalStore != nil {
				approvalStore.rememberDecision(resolved.Batches[batchIndex].Calls[callIndex], decision)
			}
		}
	}
	return resolved, nil
}

func applyApprovalDecision(call *tooling.ToolCallPlan, decision ApprovalDecision) {
	call.Metadata.GrantedWritableRoots = nil
	call.Metadata.PermissionGrantScope = ""
	switch decision {
	case ApprovalDecisionApproveForSession:
		call.Metadata.Permission = tooling.ToolPermissionAllowed
		call.Metadata.ToolEnabled = true
		call.Metadata.ApprovalState = tooling.ToolApprovalStateSessionApproved
		call.Metadata.PolicyReason = ""
	case ApprovalDecisionApprove:
		call.Metadata.Permission = tooling.ToolPermissionAllowed
		call.Metadata.ToolEnabled = true
		call.Metadata.ApprovalState = tooling.ToolApprovalStateUserApproved
		call.Metadata.PolicyReason = ""
	default:
		call.Metadata.Permission = tooling.ToolPermissionDenied
		call.Metadata.ToolEnabled = false
		call.Metadata.ApprovalState = tooling.ToolApprovalStateDenied
		call.Metadata.PolicyReason = "tool call denied by user"
		return
	}

	grant := tooling.PermissionGrantForApprovedCall(*call)
	if grant.IsEmpty() {
		return
	}
	call.Metadata.GrantedWritableRoots = append([]string(nil), grant.WritableRoots...)
	if decision == ApprovalDecisionApproveForSession {
		call.Metadata.PermissionGrantScope = tooling.PermissionGrantScopeSession
		return
	}
	call.Metadata.PermissionGrantScope = tooling.PermissionGrantScopeTurn
}

func runSingleTurn(ctx context.Context, client provider.Client, request *runtime.Request, preferStreaming bool, round int, reporter progressReporter) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	if request == nil {
		return nil, nil, runtime.Message{}, fmt.Errorf("request is required")
	}

	originalMessages := cloneMessages(request.Messages)
	candidates := buildContextCompactionCandidates(originalMessages)
	currentRequest := *request
	currentRequest.Messages = cloneMessages(originalMessages)

	for index := -1; ; index++ {
		var (
			response         *runtime.Response
			events           []runtime.StreamEvent
			assistantMessage runtime.Message
			err              error
		)

		if preferStreaming && client.Capabilities().Streaming {
			response, events, assistantMessage, err = collectStreamTurn(ctx, client, currentRequest, round, reporter)
		} else {
			response, err = createWithRetry(ctx, client, currentRequest, round, reporter)
			if err == nil {
				if response == nil || len(response.Choices) == 0 {
					err = fmt.Errorf("provider returned no choices")
				} else {
					assistantMessage = response.Choices[0].Message
				}
			}
		}
		if err == nil {
			request.Messages = cloneMessages(currentRequest.Messages)
			return response, events, assistantMessage, nil
		}
		if !isContextOverflowError(err) || index+1 >= len(candidates) {
			return nil, nil, runtime.Message{}, err
		}

		next := candidates[index+1]
		emitContextCompactionRetry(reporter, round, client.Name(), currentRequest.Model, next.Label, err)
		currentRequest = *request
		currentRequest.Messages = cloneMessages(next.Messages)
	}
}

func collectStreamTurn(ctx context.Context, client provider.Client, request runtime.Request, round int, reporter progressReporter) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	stream, err := streamWithRetry(ctx, client, request, round, reporter)
	if err != nil {
		return nil, nil, runtime.Message{}, err
	}
	defer stream.Close()
	return collectStreamResponse(stream, client.Name(), request.Model, round, reporter)
}

func collectStreamResponse(stream runtime.Stream, providerName string, fallbackModel string, round int, reporter progressReporter) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	events := make([]runtime.StreamEvent, 0, 16)
	accumulator := newTurnAccumulator(providerName, fallbackModel)
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, runtime.Message{}, err
		}
		events = append(events, event)
		accumulator.Apply(event)
		if event.Type != runtime.StreamEventTypeDone {
			reporter.Emit(ProgressUpdate{
				Kind:         ProgressKindAssistantSnapshot,
				Round:        round,
				ProviderName: providerName,
				Model:        fallbackModel,
				Snapshot:     accumulator.Snapshot(),
			})
		}
		if event.Type == runtime.StreamEventTypeDone {
			break
		}
	}

	response := accumulator.Response()
	if response == nil || len(response.Choices) == 0 {
		return nil, nil, runtime.Message{}, fmt.Errorf("provider returned no streamed choices")
	}
	return response, events, response.Choices[0].Message, nil
}

func createWithRetry(ctx context.Context, client provider.Client, request runtime.Request, round int, reporter progressReporter) (*runtime.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxProviderAttempts; attempt++ {
		response, err := client.Create(ctx, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		delay, ok := providerRetryDelay(err, attempt)
		if !ok {
			return nil, err
		}
		reporter.Emit(ProgressUpdate{
			Kind:         ProgressKindRetryScheduled,
			Round:        round,
			ProviderName: client.Name(),
			Model:        request.Model,
			RetryAfter:   delay,
			Err:          err,
		})
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func streamWithRetry(ctx context.Context, client provider.Client, request runtime.Request, round int, reporter progressReporter) (runtime.Stream, error) {
	var lastErr error
	for attempt := 0; attempt < maxProviderAttempts; attempt++ {
		stream, err := client.Stream(ctx, request)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		delay, ok := providerRetryDelay(err, attempt)
		if !ok {
			return nil, err
		}
		reporter.Emit(ProgressUpdate{
			Kind:         ProgressKindRetryScheduled,
			Round:        round,
			ProviderName: client.Name(),
			Model:        request.Model,
			RetryAfter:   delay,
			Err:          err,
		})
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func providerRetryDelay(err error, attempt int) (time.Duration, bool) {
	if attempt >= maxProviderAttempts-1 {
		return 0, false
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || !providerErr.Retryable {
		return 0, false
	}
	if providerErr.RetryAfter > 0 {
		return providerErr.RetryAfter, true
	}
	delay := baseProviderBackoff * time.Duration(1<<attempt)
	if delay > maxProviderBackoff {
		delay = maxProviderBackoff
	}
	return delay, true
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type turnAccumulator struct {
	provider      string
	fallbackModel string
	responseID    string
	model         string
	createdAt     time.Time
	message       runtime.Message
	finishReason  runtime.FinishReason
	usage         runtime.Usage
	toolOrder     []string
	toolCalls     map[string]*runtime.ToolCall
}

func newTurnAccumulator(providerName string, fallbackModel string) *turnAccumulator {
	return &turnAccumulator{
		provider:      providerName,
		fallbackModel: strings.TrimSpace(fallbackModel),
		message:       runtime.Message{Role: runtime.RoleAssistant},
		toolCalls:     map[string]*runtime.ToolCall{},
	}
}

func (a *turnAccumulator) Apply(event runtime.StreamEvent) {
	if strings.TrimSpace(event.ResponseID) != "" {
		a.responseID = event.ResponseID
	}
	if strings.TrimSpace(event.Model) != "" {
		a.model = event.Model
	}
	if !event.CreatedAt.IsZero() {
		a.createdAt = event.CreatedAt
	}

	switch event.Type {
	case runtime.StreamEventTypeDelta:
		if event.Delta.Role != "" {
			a.message.Role = event.Delta.Role
		}
		for _, part := range event.Delta.Content {
			if part.Type == runtime.ContentPartTypeText && part.Text != "" {
				a.appendText(part.Text)
			}
		}
		for _, toolCall := range event.Delta.ToolCalls {
			a.applyToolCallDelta(toolCall)
		}
	case runtime.StreamEventTypeChoiceDone:
		if event.FinishReason != "" {
			a.finishReason = event.FinishReason
		}
	case runtime.StreamEventTypeUsage:
		if event.Usage != nil {
			a.usage = *event.Usage
		}
	}
}

func (a *turnAccumulator) Response() *runtime.Response {
	if a.message.Role == "" {
		a.message.Role = runtime.RoleAssistant
	}
	if len(a.toolOrder) > 0 {
		a.message.ToolCalls = make([]runtime.ToolCall, 0, len(a.toolOrder))
		for _, key := range a.toolOrder {
			if current, ok := a.toolCalls[key]; ok {
				a.message.ToolCalls = append(a.message.ToolCalls, *current)
			}
		}
	}

	finishReason := a.finishReason
	if finishReason == "" {
		if len(a.message.ToolCalls) > 0 {
			finishReason = runtime.FinishReasonToolCalls
		} else {
			finishReason = runtime.FinishReasonStop
		}
	}

	model := a.model
	if strings.TrimSpace(model) == "" {
		model = a.fallbackModel
	}
	createdAt := a.createdAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return &runtime.Response{
		ID:        a.responseID,
		Model:     model,
		Provider:  a.provider,
		CreatedAt: createdAt,
		Choices: []runtime.Choice{{
			Index:        0,
			Message:      a.message,
			FinishReason: finishReason,
		}},
		Usage: a.usage,
	}
}

func (a *turnAccumulator) Snapshot() PartialAssistantSnapshot {
	message := runtime.Message{
		Role:    runtime.RoleAssistant,
		Content: cloneContentParts(a.message.Content),
	}
	if len(a.toolOrder) > 0 {
		message.ToolCalls = make([]runtime.ToolCall, 0, len(a.toolOrder))
		for _, key := range a.toolOrder {
			if current, ok := a.toolCalls[key]; ok {
				message.ToolCalls = append(message.ToolCalls, cloneToolCall(*current))
			}
		}
	}
	model := strings.TrimSpace(a.model)
	if model == "" {
		model = a.fallbackModel
	}
	return PartialAssistantSnapshot{
		ResponseID:   a.responseID,
		Model:        model,
		Text:         message.Text(),
		ToolCalls:    cloneToolCalls(message.ToolCalls),
		Usage:        a.usage,
		FinishReason: a.finishReason,
	}
}

func (a *turnAccumulator) appendText(text string) {
	if text == "" {
		return
	}
	if len(a.message.Content) > 0 {
		last := &a.message.Content[len(a.message.Content)-1]
		if last.Type == runtime.ContentPartTypeText {
			last.Text += text
			return
		}
	}
	a.message.Content = append(a.message.Content, runtime.TextPart(text))
}

func (a *turnAccumulator) applyToolCallDelta(delta runtime.ToolCallDelta) {
	key := streamToolKey(delta)
	current, ok := a.toolCalls[key]
	if !ok {
		toolType := delta.Type
		if toolType == "" {
			toolType = runtime.ToolTypeFunction
		}
		current = &runtime.ToolCall{Type: toolType}
		a.toolCalls[key] = current
		a.toolOrder = append(a.toolOrder, key)
	}
	if strings.TrimSpace(delta.ID) != "" {
		current.ID = delta.ID
	}
	if delta.Type != "" {
		current.Type = delta.Type
	}
	if delta.NameDelta != "" {
		current.Function.Name += delta.NameDelta
	}
	if delta.ArgumentsDelta != "" {
		current.Function.Arguments = append(current.Function.Arguments, []byte(delta.ArgumentsDelta)...)
	}
}

func streamToolKey(delta runtime.ToolCallDelta) string {
	if strings.TrimSpace(delta.ID) != "" {
		return "id:" + strings.TrimSpace(delta.ID)
	}
	return fmt.Sprintf("index:%d", delta.Index)
}

func hasPersistableMessage(message runtime.Message) bool {
	return strings.TrimSpace(message.Text()) != "" || len(message.ToolCalls) > 0 || strings.TrimSpace(message.Refusal) != ""
}

func Print(result Result) error {
	if result.Response != nil {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if strings.TrimSpace(result.Text) != "" {
		fmt.Println(result.Text)
		return nil
	}

	if len(result.Events) == 0 {
		fmt.Println("<no output>")
		return nil
	}

	data, err := json.MarshalIndent(result.Events, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

type resolvedRequest struct {
	ProviderName    string
	Client          provider.Client
	Model           string
	ReasoningEffort string
	ProfileName     string
	CWD             string
	Messages        []runtime.Message
}

func resolveRequest(config state.Config, options Options) (resolvedRequest, error) {
	profileName := strings.TrimSpace(options.Profile)
	if profileName == "" {
		profileName = config.ActiveProfileName()
	}

	var profile state.ProfileConfig
	if profileName != "" {
		selected, ok := config.Profile(profileName)
		if !ok {
			return resolvedRequest{}, fmt.Errorf("profile %q not found", profileName)
		}
		profile = selected
	}

	model := strings.TrimSpace(options.Model)
	if model == "" && strings.TrimSpace(profile.Model) != "" {
		model = strings.TrimSpace(profile.Model)
	}
	if model == "" {
		model = config.EffectiveModel()
	}
	if model == "" {
		return resolvedRequest{}, fmt.Errorf("model is not configured")
	}

	reasoning := strings.TrimSpace(options.ReasoningEffort)
	if reasoning == "" && strings.TrimSpace(profile.ReasoningEffort) != "" {
		reasoning = strings.TrimSpace(profile.ReasoningEffort)
	}
	if reasoning == "" {
		reasoning = config.EffectiveReasoningEffort()
	}

	providerName := strings.TrimSpace(options.Provider)
	if providerName == "" && strings.TrimSpace(profile.Provider) != "" {
		providerName = strings.TrimSpace(profile.Provider)
	}
	if providerName == "" {
		providerName = config.EffectiveProviderName()
	}

	client, resolvedProviderName, err := resolveProviderClient(config, providerName)
	if err != nil {
		return resolvedRequest{}, err
	}
	cwd := strings.TrimSpace(options.CWD)
	if cwd == "" {
		if currentCWD, cwdErr := os.Getwd(); cwdErr == nil {
			cwd = currentCWD
		}
	}

	systemPrompt := resolveSystemPrompt(options.SystemPrompt, cwd)
	messages := cloneMessages(options.History)
	switch {
	case len(messages) == 0:
		messages = append(messages, runtime.TextMessage(runtime.RoleSystem, systemPrompt))
	case messages[0].Role == runtime.RoleSystem:
		messages[0] = runtime.TextMessage(runtime.RoleSystem, systemPrompt)
	default:
		messages = append([]runtime.Message{runtime.TextMessage(runtime.RoleSystem, systemPrompt)}, messages...)
	}
	messages = append(messages, runtime.TextMessage(runtime.RoleUser, options.Prompt))

	return resolvedRequest{
		ProviderName:    resolvedProviderName,
		Client:          client,
		Model:           model,
		ReasoningEffort: reasoning,
		ProfileName:     profileName,
		CWD:             cwd,
		Messages:        messages,
	}, nil
}

func enterWorkingDirectory(target string) (func(), error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return func() {}, nil
	}
	workingDirectoryMu.Lock()
	previous, err := os.Getwd()
	if err != nil {
		workingDirectoryMu.Unlock()
		return nil, err
	}
	if err := os.Chdir(target); err != nil {
		workingDirectoryMu.Unlock()
		return nil, err
	}
	return func() {
		_ = os.Chdir(previous)
		workingDirectoryMu.Unlock()
	}, nil
}

func cloneMessages(messages []runtime.Message) []runtime.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]runtime.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

func resolveProviderClient(config state.Config, providerName string) (provider.Client, string, error) {
	if strings.TrimSpace(providerName) == "" {
		return resolveBuiltinOpenAIClient()
	}

	providerConfig, ok := config.Provider(providerName)
	if !ok {
		if builtin := normalizeBuiltinProviderName(providerName); builtin == "openai" || builtin == "codex_oauth" {
			return resolveBuiltinOpenAIClient()
		}
		return nil, "", fmt.Errorf("provider %q not found", providerName)
	}

	token := providerConfig.BearerToken()
	if token == "" && providerLooksLikeOpenAI(providerName, providerConfig) {
		authKey, err := state.LoadOpenAIAPIKey(apphome.AuthPath())
		if err != nil {
			return nil, "", err
		}
		token = strings.TrimSpace(authKey)
	}
	if token == "" {
		return nil, "", fmt.Errorf("provider %q has no bearer token or env key value", providerName)
	}

	wireAPI := strings.TrimSpace(providerConfig.WireAPI)
	switch wireAPI {
	case "", "chat_completions":
		baseURL, chatPath, err := providerEndpoint(providerConfig.BaseURL)
		if err != nil {
			return nil, "", fmt.Errorf("provider %q: %w", providerName, err)
		}
		client, err := openai.NewClient(openai.Config{
			Name:                providerConfig.DisplayName(),
			BaseURL:             baseURL,
			ChatCompletionsPath: chatPath,
			APIKey:              token,
		})
		if err != nil {
			return nil, "", err
		}
		return client, providerName, nil
	case "responses":
		baseURL, responsesPath, err := responsesEndpoint(providerConfig.BaseURL)
		if err != nil {
			return nil, "", fmt.Errorf("provider %q: %w", providerName, err)
		}
		client, err := responsesapi.NewClient(responsesapi.Config{
			Name:          providerConfig.DisplayName(),
			BaseURL:       baseURL,
			ResponsesPath: responsesPath,
			APIKey:        token,
		})
		if err != nil {
			return nil, "", err
		}
		return client, providerName, nil
	default:
		return nil, "", fmt.Errorf("provider %q uses unsupported wire_api %q", providerName, wireAPI)
	}
}

func resolveBuiltinOpenAIClient() (provider.Client, string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		authKey, err := state.LoadOpenAIAPIKey(apphome.AuthPath())
		if err != nil {
			return nil, "", err
		}
		apiKey = strings.TrimSpace(authKey)
	}
	if apiKey == "" {
		return nil, "", fmt.Errorf("provider is not configured and OPENAI_API_KEY/auth.json is empty")
	}
	client, err := responsesapi.NewClient(responsesapi.Config{
		Name:   "OpenAI",
		APIKey: apiKey,
	})
	if err != nil {
		return nil, "", err
	}
	return client, "openai", nil
}

func normalizeBuiltinProviderName(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "openai", "openai api", "openai-api", "openai_api":
		return "openai"
	case "codex_oauth", "codex-oauth", "oauth", "chatgpt", "openai-oauth", "codex":
		return "codex_oauth"
	default:
		return ""
	}
}

func providerLooksLikeOpenAI(providerName string, providerConfig state.ProviderConfig) bool {
	name := strings.ToLower(strings.TrimSpace(providerName))
	display := strings.ToLower(strings.TrimSpace(providerConfig.DisplayName()))
	baseURL := strings.ToLower(strings.TrimSpace(providerConfig.BaseURL))
	return strings.Contains(name, "openai") || strings.Contains(display, "openai") || strings.Contains(baseURL, "api.openai.com")
}

func providerEndpoint(baseURL string) (string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return openai.DefaultBaseURL, openai.DefaultChatCompletionsPath, nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("parse provider base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("provider base_url must include scheme and host")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/responses"):
		return "", "", fmt.Errorf("provider base_url points to responses endpoint")
	case strings.HasSuffix(path, "/chat/completions"):
		parsed.Path = strings.TrimSuffix(path, "/chat/completions")
		if parsed.Path == "/" {
			parsed.Path = ""
		}
		return parsed.String(), "/chat/completions", nil
	case path == "":
		return parsed.String(), "/v1/chat/completions", nil
	default:
		return parsed.String(), "/chat/completions", nil
	}
}

func responsesEndpoint(baseURL string) (string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return responsesapi.DefaultBaseURL, responsesapi.DefaultResponsesPath, nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("parse provider base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("provider base_url must include scheme and host")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/responses"):
		parsed.Path = strings.TrimSuffix(path, "/responses")
		if parsed.Path == "/" {
			parsed.Path = ""
		}
		return parsed.String(), "/responses", nil
	case path == "":
		return parsed.String(), "/v1/responses", nil
	default:
		return parsed.String(), "/responses", nil
	}
}

func responseText(response *runtime.Response) string {
	if response == nil {
		return ""
	}
	var parts []string
	for _, choice := range response.Choices {
		text := strings.TrimSpace(choice.Message.Text())
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func snapshotFromMessage(response *runtime.Response, message runtime.Message) PartialAssistantSnapshot {
	snapshot := PartialAssistantSnapshot{
		Text:      message.Text(),
		ToolCalls: cloneToolCalls(message.ToolCalls),
	}
	if response != nil {
		snapshot.ResponseID = response.ID
		snapshot.Model = response.Model
		snapshot.Usage = response.Usage
		if len(response.Choices) > 0 {
			snapshot.FinishReason = response.Choices[0].FinishReason
		}
	}
	return snapshot
}

func cloneToolCalls(calls []runtime.ToolCall) []runtime.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]runtime.ToolCall, len(calls))
	for index, call := range calls {
		cloned[index] = cloneToolCall(call)
	}
	return cloned
}

func cloneToolCall(call runtime.ToolCall) runtime.ToolCall {
	cloned := call
	cloned.Function.Arguments = cloneRawMessage(call.Function.Arguments)
	return cloned
}

func cloneContentParts(parts []runtime.ContentPart) []runtime.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	cloned := make([]runtime.ContentPart, len(parts))
	copy(cloned, parts)
	return cloned
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return json.RawMessage(cloned)
}
