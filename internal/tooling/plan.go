package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	toolruntime "github.com/Perdonus/lavilas-code/internal/runtime"
)

type ExecutionMode string

const (
	ExecutionModeSequential ExecutionMode = "sequential"
	ExecutionModeParallel   ExecutionMode = "parallel"
)

type SideEffectKind string

const (
	SideEffectKindReadOnly       SideEffectKind = "read_only"
	SideEffectKindWorkspaceWrite SideEffectKind = "workspace_write"
	SideEffectKindShell          SideEffectKind = "shell"
	SideEffectKindUnknown        SideEffectKind = "unknown"
)

type SandboxHint string

const (
	SandboxHintInherited      SandboxHint = "inherited"
	SandboxHintWorkspaceWrite SandboxHint = "workspace_write"
	SandboxHintDangerous      SandboxHint = "dangerous"
)

type PlanningPolicy struct {
	AllowParallel    bool
	MaxParallelCalls int
}

func DefaultPlanningPolicy() PlanningPolicy {
	return PlanningPolicy{
		AllowParallel:    true,
		MaxParallelCalls: 4,
	}
}

type ToolExecutionMetadata struct {
	SideEffectKind     SideEffectKind    `json:"side_effect_kind"`
	SandboxHint        SandboxHint       `json:"sandbox_hint"`
	ApprovalRequired   bool              `json:"approval_required"`
	Permission         ToolPermission    `json:"permission"`
	ApprovalState      ToolApprovalState `json:"approval_state"`
	ToolEnabled        bool              `json:"tool_enabled"`
	PolicyReason       string            `json:"policy_reason,omitempty"`
	SupportsParallel   bool              `json:"supports_parallel"`
	MutatesWorkspace   bool              `json:"mutates_workspace"`
	SpawnsSubprocess   bool              `json:"spawns_subprocess"`
	ResourceKeys       []string          `json:"resource_keys,omitempty"`
	WorkingDirectory   string            `json:"working_directory,omitempty"`
	ArgumentParseError string            `json:"argument_parse_error,omitempty"`
}

type ToolCallPlan struct {
	Index        int
	Call         toolruntime.ToolCall
	CallID       string
	ApprovalID   string
	ApprovalKeys []string
	Name         string
	Arguments    json.RawMessage
	Metadata     ToolExecutionMetadata
}

type ExecutionBatch struct {
	Index  int
	Mode   ExecutionMode
	Calls  []ToolCallPlan
	Reason string
}

type ExecutionPlanSummary struct {
	CallCount            int
	BatchCount           int
	ParallelBatchCount   int
	SequentialBatchCount int
	ReadOnlyCallCount    int
	MutatingCallCount    int
}

type ExecutionPlan struct {
	Policy     PlanningPolicy
	ToolPolicy ToolPolicy
	CreatedAt  time.Time
	Batches    []ExecutionBatch
	Summary    ExecutionPlanSummary
}

type ResultStatus string

const (
	ResultStatusSucceeded        ResultStatus = "succeeded"
	ResultStatusFailed           ResultStatus = "failed"
	ResultStatusApprovalRequired ResultStatus = "approval_required"
	ResultStatusDenied           ResultStatus = "denied"
)

type ToolResultEnvelope struct {
	Index      int
	BatchIndex int
	Mode       ExecutionMode
	CallID     string
	ApprovalID string
	Name       string
	Summary    string
	Details    string
	Metadata   ToolExecutionMetadata
	Status     ResultStatus
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
	OutputText string
	OutputJSON json.RawMessage
}

type BatchResultEnvelope struct {
	Index      int
	Mode       ExecutionMode
	Reason     string
	StartedAt  time.Time
	FinishedAt time.Time
	Results    []ToolResultEnvelope
}

type ExecutionReport struct {
	Plan             ExecutionPlan
	StartedAt        time.Time
	FinishedAt       time.Time
	Batches          []BatchResultEnvelope
	Results          []ToolResultEnvelope
	Summary          ExecutionReportSummary
	ApprovalRequests []ApprovalRequest
}

func BuildExecutionPlan(calls []toolruntime.ToolCall) ExecutionPlan {
	return BuildExecutionPlanWithToolPolicy(calls, DefaultToolPolicy())
}

func BuildExecutionPlanWithPolicy(calls []toolruntime.ToolCall, policy PlanningPolicy) ExecutionPlan {
	toolPolicy := DefaultToolPolicy()
	toolPolicy.Planning = policy
	return BuildExecutionPlanWithToolPolicy(calls, toolPolicy)
}

func BuildExecutionPlanWithToolPolicy(calls []toolruntime.ToolCall, policy ToolPolicy) ExecutionPlan {
	policy = NormalizeToolPolicy(policy)
	plan := ExecutionPlan{
		Policy:     policy.Planning,
		ToolPolicy: policy,
		CreatedAt:  time.Now().UTC(),
	}
	if len(calls) == 0 {
		return plan
	}

	plannedCalls := make([]ToolCallPlan, 0, len(calls))
	for index, call := range calls {
		plannedCalls = append(plannedCalls, buildCallPlan(index, call, policy))
	}

	var currentCalls []ToolCallPlan
	currentMode := ExecutionModeSequential
	flush := func() {
		if len(currentCalls) == 0 {
			return
		}
		batchMode := currentMode
		if batchMode == ExecutionModeParallel && len(currentCalls) == 1 {
			batchMode = ExecutionModeSequential
		}
		batch := ExecutionBatch{
			Index: len(plan.Batches),
			Mode:  batchMode,
			Calls: append([]ToolCallPlan(nil), currentCalls...),
		}
		batch.Reason = batchReason(batch)
		plan.Batches = append(plan.Batches, batch)
		currentCalls = nil
	}

	for _, call := range plannedCalls {
		desiredMode := desiredExecutionMode(call, policy.Planning)
		if len(currentCalls) == 0 {
			currentMode = desiredMode
			currentCalls = append(currentCalls, call)
			continue
		}
		if desiredMode != currentMode {
			flush()
			currentMode = desiredMode
			currentCalls = append(currentCalls, call)
			continue
		}
		if currentMode == ExecutionModeParallel && len(currentCalls) >= policy.Planning.MaxParallelCalls {
			flush()
			currentMode = desiredMode
		}
		currentCalls = append(currentCalls, call)
	}
	flush()

	plan.Summary = summarizePlan(plan.Batches)
	return plan
}

func ExecutePlan(ctx context.Context, plan ExecutionPlan) ExecutionReport {
	report := ExecutionReport{
		Plan:      plan,
		StartedAt: time.Now().UTC(),
	}
	if len(plan.Batches) == 0 {
		report.FinishedAt = report.StartedAt
		return report
	}

	results := make([]ToolResultEnvelope, 0, plan.Summary.CallCount)
	batches := make([]BatchResultEnvelope, 0, len(plan.Batches))
	for _, batch := range plan.Batches {
		batchReport := executeBatch(ctx, batch)
		batches = append(batches, batchReport)
		results = append(results, batchReport.Results...)
	}
	report.Batches = batches
	report.Results = results
	report.FinishedAt = time.Now().UTC()
	report.Summary = summarizeExecutionResults(results)
	report.ApprovalRequests = collectApprovalRequests(results)
	return report
}

func (r ExecutionReport) Messages() []toolruntime.Message {
	if len(r.Results) == 0 {
		return nil
	}
	messages := make([]toolruntime.Message, 0, len(r.Results))
	for _, result := range r.Results {
		messages = append(messages, result.Message())
	}
	return messages
}

func (r ToolResultEnvelope) Message() toolruntime.Message {
	callID := strings.TrimSpace(r.CallID)
	if callID == "" {
		callID = strings.TrimSpace(r.Name)
	}
	return toolruntime.Message{
		Role:       toolruntime.RoleTool,
		ToolCallID: callID,
		Content:    []toolruntime.ContentPart{toolruntime.TextPart(r.OutputText)},
	}
}

func normalizePlanningPolicy(policy PlanningPolicy) PlanningPolicy {
	if policy.MaxParallelCalls <= 0 {
		policy.MaxParallelCalls = DefaultPlanningPolicy().MaxParallelCalls
	}
	if policy.MaxParallelCalls < 1 {
		policy.MaxParallelCalls = 1
	}
	return policy
}

func buildCallPlan(index int, call toolruntime.ToolCall, policy ToolPolicy) ToolCallPlan {
	name := strings.TrimSpace(call.Function.Name)
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = name
	}
	arguments := cloneRawMessage(call.Function.Arguments)
	metadata := inspectToolCall(name, arguments)
	metadata = applyToolPolicy(name, metadata, policy)
	approvalKeys := approvalKeysForCall(name, arguments, metadata)
	return ToolCallPlan{
		Index:        index,
		Call:         call,
		CallID:       callID,
		ApprovalID:   approvalIDForKeys(approvalKeys),
		ApprovalKeys: approvalKeys,
		Name:         name,
		Arguments:    arguments,
		Metadata:     metadata,
	}
}

func desiredExecutionMode(call ToolCallPlan, policy PlanningPolicy) ExecutionMode {
	if !policy.AllowParallel {
		return ExecutionModeSequential
	}
	if call.Metadata.SupportsParallel {
		return ExecutionModeParallel
	}
	return ExecutionModeSequential
}

func batchReason(batch ExecutionBatch) string {
	if batch.Mode == ExecutionModeParallel {
		return "adjacent read-only tools can run together"
	}
	if len(batch.Calls) == 1 {
		return "serialized for safety"
	}
	return "serialized to preserve ordering and side effects"
}

func summarizePlan(batches []ExecutionBatch) ExecutionPlanSummary {
	summary := ExecutionPlanSummary{
		BatchCount: len(batches),
	}
	for _, batch := range batches {
		if batch.Mode == ExecutionModeParallel {
			summary.ParallelBatchCount++
		} else {
			summary.SequentialBatchCount++
		}
		for _, call := range batch.Calls {
			summary.CallCount++
			if call.Metadata.MutatesWorkspace || call.Metadata.SideEffectKind == SideEffectKindShell {
				summary.MutatingCallCount++
			} else {
				summary.ReadOnlyCallCount++
			}
		}
	}
	return summary
}

func executeBatch(ctx context.Context, batch ExecutionBatch) BatchResultEnvelope {
	report := BatchResultEnvelope{
		Index:     batch.Index,
		Mode:      batch.Mode,
		Reason:    batch.Reason,
		StartedAt: time.Now().UTC(),
	}
	if batch.Mode == ExecutionModeParallel && len(batch.Calls) > 1 {
		results := make([]ToolResultEnvelope, len(batch.Calls))
		var wg sync.WaitGroup
		wg.Add(len(batch.Calls))
		for index, call := range batch.Calls {
			index := index
			call := call
			go func() {
				defer wg.Done()
				results[index] = executePlannedCall(ctx, batch.Index, batch.Mode, call)
			}()
		}
		wg.Wait()
		report.Results = results
		report.FinishedAt = time.Now().UTC()
		return report
	}

	results := make([]ToolResultEnvelope, 0, len(batch.Calls))
	for _, call := range batch.Calls {
		results = append(results, executePlannedCall(ctx, batch.Index, ExecutionModeSequential, call))
	}
	report.Results = results
	report.FinishedAt = time.Now().UTC()
	return report
}

func executePlannedCall(ctx context.Context, batchIndex int, mode ExecutionMode, call ToolCallPlan) ToolResultEnvelope {
	startedAt := time.Now().UTC()
	summary, details := describeApprovalCall(call)
	if call.Metadata.Permission == ToolPermissionDenied {
		output := marshalPolicyResult(call, ResultStatusDenied)
		finishedAt := time.Now().UTC()
		return ToolResultEnvelope{
			Index:      call.Index,
			BatchIndex: batchIndex,
			Mode:       mode,
			CallID:     call.CallID,
			ApprovalID: call.ApprovalID,
			Name:       call.Name,
			Summary:    summary,
			Details:    details,
			Metadata:   call.Metadata,
			Status:     ResultStatusDenied,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Duration:   finishedAt.Sub(startedAt),
			OutputText: output,
			OutputJSON: rawJSON(output),
		}
	}
	if call.Metadata.Permission == ToolPermissionApprovalRequired {
		output := marshalPolicyResult(call, ResultStatusApprovalRequired)
		finishedAt := time.Now().UTC()
		return ToolResultEnvelope{
			Index:      call.Index,
			BatchIndex: batchIndex,
			Mode:       mode,
			CallID:     call.CallID,
			ApprovalID: call.ApprovalID,
			Name:       call.Name,
			Summary:    summary,
			Details:    details,
			Metadata:   call.Metadata,
			Status:     ResultStatusApprovalRequired,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Duration:   finishedAt.Sub(startedAt),
			OutputText: output,
			OutputJSON: rawJSON(output),
		}
	}
	output := dispatch(ctx, call.Name, call.Arguments)
	finishedAt := time.Now().UTC()
	return ToolResultEnvelope{
		Index:      call.Index,
		BatchIndex: batchIndex,
		Mode:       mode,
		CallID:     call.CallID,
		ApprovalID: call.ApprovalID,
		Name:       call.Name,
		Summary:    summary,
		Details:    details,
		Metadata:   call.Metadata,
		Status:     detectResultStatus(output),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Duration:   finishedAt.Sub(startedAt),
		OutputText: output,
		OutputJSON: rawJSON(output),
	}
}

func ApprovalRequestForCall(batch int, call ToolCallPlan) ApprovalRequest {
	summary, details := describeApprovalCall(call)
	return ApprovalRequest{
		Index:      call.Index,
		Batch:      batch,
		ApprovalID: call.ApprovalID,
		CallID:     call.CallID,
		Name:       call.Name,
		Status:     ResultStatusApprovalRequired,
		Summary:    summary,
		Details:    details,
		Reason:     call.Metadata.PolicyReason,
		Metadata:   call.Metadata,
	}
}

func inspectToolCall(name string, arguments json.RawMessage) ToolExecutionMetadata {
	metadata := baseToolMetadata(name)

	switch strings.TrimSpace(name) {
	case "run_shell_command":
		var args shellArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			return metadata
		}
		metadata.WorkingDirectory = normalizeResourcePath(args.Cwd, ".")
		metadata.ResourceKeys = []string{"cwd:" + metadata.WorkingDirectory}
		return metadata
	case "list_directory":
		var args listArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			return metadata
		}
		resource := normalizeResourcePath(args.Path, ".")
		metadata.ResourceKeys = []string{"dir:" + resource}
		return metadata
	case "read_file":
		var args readArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			return metadata
		}
		resource := normalizeResourcePath(args.Path, ".")
		metadata.ResourceKeys = []string{"file:" + resource}
		return metadata
	case "search_text":
		var args searchArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			return metadata
		}
		resource := normalizeResourcePath(args.Path, ".")
		metadata.ResourceKeys = []string{"tree:" + resource}
		return metadata
	case "write_file":
		var args writeArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			return metadata
		}
		resource := normalizeResourcePath(args.Path, ".")
		metadata.ResourceKeys = []string{"file:" + resource}
		return metadata
	case "apply_patch":
		var args patchArgs
		if err := decodeArgs(arguments, &args); err != nil {
			metadata.ArgumentParseError = err.Error()
			metadata.ResourceKeys = []string{"workspace"}
			return metadata
		}
		if paths := patchTouchedPaths(args.Patch); len(paths) > 0 {
			metadata.ResourceKeys = make([]string, 0, len(paths))
			for _, path := range paths {
				metadata.ResourceKeys = append(metadata.ResourceKeys, "file:"+path)
			}
			return metadata
		}
		metadata.ResourceKeys = []string{"workspace"}
		return metadata
	default:
		metadata.ResourceKeys = []string{"tool:" + strings.TrimSpace(name)}
		return metadata
	}
}

func marshalPolicyResult(call ToolCallPlan, status ResultStatus) string {
	errorMessage := call.Metadata.PolicyReason
	if strings.TrimSpace(errorMessage) == "" {
		errorMessage = "tool call blocked by execution policy"
	}
	return marshalResult(map[string]any{
		"ok":          false,
		"tool":        call.Name,
		"approval_id": call.ApprovalID,
		"status":      status,
		"error":       errorMessage,
		"policy": map[string]any{
			"permission":        call.Metadata.Permission,
			"approval_state":    call.Metadata.ApprovalState,
			"approval_required": call.Metadata.ApprovalRequired,
			"tool_enabled":      call.Metadata.ToolEnabled,
			"side_effect_kind":  call.Metadata.SideEffectKind,
			"sandbox_hint":      call.Metadata.SandboxHint,
			"resource_keys":     call.Metadata.ResourceKeys,
			"working_directory": call.Metadata.WorkingDirectory,
		},
	})
}

func normalizeResourcePath(path string, fallback string) string {
	candidate := strings.TrimSpace(path)
	if candidate == "" {
		candidate = fallback
	}
	if candidate == "" {
		candidate = "."
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return candidate
	}
	return resolved
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func rawJSON(value string) json.RawMessage {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return nil
	}
	return json.RawMessage([]byte(trimmed))
}

func detectResultStatus(output string) ResultStatus {
	payload := rawJSON(output)
	if len(payload) == 0 {
		return ResultStatusFailed
	}
	var parsed struct {
		OK     *bool        `json:"ok"`
		Status ResultStatus `json:"status"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return ResultStatusFailed
	}
	if parsed.Status == ResultStatusApprovalRequired || parsed.Status == ResultStatusDenied {
		return parsed.Status
	}
	if parsed.OK != nil && *parsed.OK {
		return ResultStatusSucceeded
	}
	return ResultStatusFailed
}

func summarizeExecutionResults(results []ToolResultEnvelope) ExecutionReportSummary {
	summary := ExecutionReportSummary{CallCount: len(results)}
	for _, result := range results {
		switch result.Status {
		case ResultStatusSucceeded:
			summary.ExecutedCount++
			summary.SucceededCount++
		case ResultStatusFailed:
			summary.ExecutedCount++
			summary.FailedCount++
		case ResultStatusApprovalRequired:
			summary.ApprovalRequiredCount++
		case ResultStatusDenied:
			summary.DeniedCount++
		}
	}
	return summary
}

func collectApprovalRequests(results []ToolResultEnvelope) []ApprovalRequest {
	requests := make([]ApprovalRequest, 0)
	for _, result := range results {
		if result.Status != ResultStatusApprovalRequired && result.Status != ResultStatusDenied {
			continue
		}
		requests = append(requests, ApprovalRequest{
			Index:      result.Index,
			Batch:      result.BatchIndex,
			ApprovalID: result.ApprovalID,
			CallID:     result.CallID,
			Name:       result.Name,
			Status:     result.Status,
			Summary:    result.Summary,
			Details:    result.Details,
			Reason:     result.Metadata.PolicyReason,
			Metadata:   result.Metadata,
		})
	}
	return requests
}

func describeApprovalCall(call ToolCallPlan) (string, string) {
	switch strings.TrimSpace(call.Name) {
	case "run_shell_command":
		var args shellArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			summary := strings.TrimSpace(args.Cmd)
			if summary == "" {
				summary = call.Name
			}
			if cwd := strings.TrimSpace(args.Cwd); cwd != "" {
				return summary, fmt.Sprintf("cwd=%s", cwd)
			}
			return summary, ""
		}
	case "write_file":
		var args writeArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			return fmt.Sprintf("write %s", normalizeResourcePath(args.Path, ".")), fmt.Sprintf("bytes=%d", len(args.Content))
		}
	case "apply_patch":
		var args patchArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			patch := strings.TrimSpace(args.Patch)
			if patch == "" {
				return "apply patch", ""
			}
			if paths := patchTouchedPaths(args.Patch); len(paths) > 0 {
				previewCount := minInt(len(paths), 3)
				return fmt.Sprintf("apply patch to %d file(s)", len(paths)), strings.Join(paths[:previewCount], ", ")
			}
			firstLine := strings.SplitN(patch, "\n", 2)[0]
			return "apply patch", firstLine
		}
	case "read_file":
		var args readArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			return fmt.Sprintf("read %s", normalizeResourcePath(args.Path, ".")), ""
		}
	case "list_directory":
		var args listArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			return fmt.Sprintf("list %s", normalizeResourcePath(args.Path, ".")), ""
		}
	case "search_text":
		var args searchArgs
		if err := decodeArgs(call.Arguments, &args); err == nil {
			return fmt.Sprintf("search %q", strings.TrimSpace(args.Query)), fmt.Sprintf("path=%s", normalizeResourcePath(args.Path, "."))
		}
	}
	arguments := strings.TrimSpace(string(call.Arguments))
	if arguments == "" {
		return call.Name, ""
	}
	return call.Name, arguments
}
