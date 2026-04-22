package tooling

import (
	"fmt"
	"sort"
	"strings"

	toolruntime "github.com/Perdonus/lavilas-code/internal/runtime"
)

type ToolApprovalMode string

const (
	ToolApprovalModeAuto    ToolApprovalMode = "auto"
	ToolApprovalModeRequire ToolApprovalMode = "require"
	ToolApprovalModeDeny    ToolApprovalMode = "deny"
)

type ToolPermission string

const (
	ToolPermissionAllowed          ToolPermission = "allowed"
	ToolPermissionApprovalRequired ToolPermission = "approval_required"
	ToolPermissionDenied           ToolPermission = "denied"
)

type ToolApprovalState string

const (
	ToolApprovalStateNotNeeded       ToolApprovalState = "not_needed"
	ToolApprovalStateAutoApproved    ToolApprovalState = "auto_approved"
	ToolApprovalStatePending         ToolApprovalState = "pending"
	ToolApprovalStateUserApproved    ToolApprovalState = "user_approved"
	ToolApprovalStateSessionApproved ToolApprovalState = "session_approved"
	ToolApprovalStateDenied          ToolApprovalState = "denied"
)

type ApprovalKind string

const (
	ApprovalKindUnknown           ApprovalKind = "unknown"
	ApprovalKindPermissionRequest ApprovalKind = "permission_request"
	ApprovalKindShellCommand      ApprovalKind = "shell_command"
	ApprovalKindApplyPatch        ApprovalKind = "apply_patch"
	ApprovalKindWorkspaceWrite    ApprovalKind = "workspace_write"
	ApprovalKindReadOnly          ApprovalKind = "read_only"
)

type ToolPolicy struct {
	Planning           PlanningPolicy   `json:"planning"`
	ApprovalMode       ToolApprovalMode `json:"approval_mode"`
	AllowedTools       []string         `json:"allowed_tools,omitempty"`
	BlockedTools       []string         `json:"blocked_tools,omitempty"`
	BlockMutatingTools bool             `json:"block_mutating_tools,omitempty"`
	BlockShellCommands bool             `json:"block_shell_commands,omitempty"`
}

type ApprovalRequest struct {
	Kind       ApprovalKind          `json:"kind,omitempty"`
	Index      int                   `json:"index"`
	Batch      int                   `json:"batch"`
	ApprovalID string                `json:"approval_id,omitempty"`
	CallID     string                `json:"call_id"`
	Name       string                `json:"name"`
	Status     ResultStatus          `json:"status"`
	Summary    string                `json:"summary,omitempty"`
	Details    string                `json:"details,omitempty"`
	Reason     string                `json:"reason,omitempty"`
	Metadata   ToolExecutionMetadata `json:"metadata"`
}

func ApprovalKindForRequest(request ApprovalRequest) ApprovalKind {
	if request.Kind != "" {
		return request.Kind
	}
	return ClassifyApprovalKind(request.Name, request.Metadata)
}

func ClassifyApprovalKind(name string, metadata ToolExecutionMetadata) ApprovalKind {
	switch normalizeToolName(name) {
	case "request_permissions":
		return ApprovalKindPermissionRequest
	case "run_shell_command":
		return ApprovalKindShellCommand
	case "apply_patch":
		return ApprovalKindApplyPatch
	case "write_file":
		return ApprovalKindWorkspaceWrite
	case "read_file", "list_directory", "search_text":
		return ApprovalKindReadOnly
	}
	switch metadata.SideEffectKind {
	case SideEffectKindShell:
		return ApprovalKindShellCommand
	case SideEffectKindWorkspaceWrite:
		return ApprovalKindWorkspaceWrite
	case SideEffectKindReadOnly:
		return ApprovalKindReadOnly
	default:
		return ApprovalKindUnknown
	}
}

type ExecutionReportSummary struct {
	CallCount             int `json:"call_count"`
	ExecutedCount         int `json:"executed_count"`
	SucceededCount        int `json:"succeeded_count"`
	FailedCount           int `json:"failed_count"`
	ApprovalRequiredCount int `json:"approval_required_count"`
	DeniedCount           int `json:"denied_count"`
}

func DefaultToolPolicy() ToolPolicy {
	return ToolPolicy{
		Planning:     DefaultPlanningPolicy(),
		ApprovalMode: ToolApprovalModeAuto,
	}
}

func NormalizeToolPolicy(policy ToolPolicy) ToolPolicy {
	if isZeroToolPolicy(policy) {
		return DefaultToolPolicy()
	}
	policy.Planning = normalizePlanningPolicy(policy.Planning)
	if policy.ApprovalMode == "" {
		policy.ApprovalMode = ToolApprovalModeAuto
	}
	policy.AllowedTools = normalizeToolList(policy.AllowedTools)
	policy.BlockedTools = normalizeToolList(policy.BlockedTools)
	return policy
}

func IsZeroToolPolicy(policy ToolPolicy) bool {
	return isZeroToolPolicy(policy)
}

func DefinitionsWithPolicy(policy ToolPolicy) []toolruntime.ToolDefinition {
	policy = NormalizeToolPolicy(policy)
	definitions := Definitions()
	filtered := make([]toolruntime.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		metadata := applyToolPolicy(definition.Function.Name, baseToolMetadata(definition.Function.Name), policy)
		if metadata.Permission == ToolPermissionDenied || !metadata.ToolEnabled {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

func baseToolMetadata(name string) ToolExecutionMetadata {
	metadata := ToolExecutionMetadata{
		SideEffectKind:   SideEffectKindUnknown,
		SandboxHint:      SandboxHintInherited,
		ApprovalRequired: true,
		Permission:       ToolPermissionAllowed,
		ApprovalState:    ToolApprovalStateNotNeeded,
		ToolEnabled:      true,
	}

	switch normalizeToolName(name) {
	case "run_shell_command":
		metadata.SideEffectKind = SideEffectKindShell
		metadata.SandboxHint = SandboxHintDangerous
		metadata.MutatesWorkspace = true
		metadata.SpawnsSubprocess = true
	case "spawn_worker":
		metadata.SideEffectKind = SideEffectKindUnknown
		metadata.SandboxHint = SandboxHintInherited
		metadata.ApprovalRequired = false
		metadata.SupportsParallel = true
	case "list_workers", "wait_worker", "cancel_worker":
		metadata.SideEffectKind = SideEffectKindReadOnly
		metadata.SandboxHint = SandboxHintInherited
		metadata.ApprovalRequired = false
		metadata.SupportsParallel = true
	case "list_directory", "read_file", "search_text":
		metadata.SideEffectKind = SideEffectKindReadOnly
		metadata.SandboxHint = SandboxHintInherited
		metadata.ApprovalRequired = false
		metadata.SupportsParallel = true
	case "write_file", "apply_patch":
		metadata.SideEffectKind = SideEffectKindWorkspaceWrite
		metadata.SandboxHint = SandboxHintWorkspaceWrite
		metadata.MutatesWorkspace = true
	default:
		metadata.ResourceKeys = []string{"tool:" + normalizeToolName(name)}
	}
	return metadata
}

func applyToolPolicy(name string, metadata ToolExecutionMetadata, policy ToolPolicy) ToolExecutionMetadata {
	policy = NormalizeToolPolicy(policy)
	normalizedName := normalizeToolName(name)
	metadata.Permission = ToolPermissionAllowed
	metadata.ApprovalState = ToolApprovalStateNotNeeded
	metadata.ToolEnabled = true
	metadata.PolicyReason = ""

	if len(policy.AllowedTools) > 0 && !toolListContains(policy.AllowedTools, normalizedName) {
		metadata.Permission = ToolPermissionDenied
		metadata.ToolEnabled = false
		metadata.ApprovalState = ToolApprovalStateDenied
		metadata.PolicyReason = fmt.Sprintf("tool %q is not enabled by the current allowlist", normalizedName)
		return metadata
	}
	if toolListContains(policy.BlockedTools, normalizedName) {
		metadata.Permission = ToolPermissionDenied
		metadata.ToolEnabled = false
		metadata.ApprovalState = ToolApprovalStateDenied
		metadata.PolicyReason = fmt.Sprintf("tool %q is blocked by policy", normalizedName)
		return metadata
	}
	if policy.BlockShellCommands && metadata.SideEffectKind == SideEffectKindShell {
		metadata.Permission = ToolPermissionDenied
		metadata.ToolEnabled = false
		metadata.ApprovalState = ToolApprovalStateDenied
		metadata.PolicyReason = "shell command tools are blocked by policy"
		return metadata
	}
	if policy.BlockMutatingTools && metadata.MutatesWorkspace {
		metadata.Permission = ToolPermissionDenied
		metadata.ToolEnabled = false
		metadata.ApprovalState = ToolApprovalStateDenied
		metadata.PolicyReason = "mutating tools are blocked by policy"
		return metadata
	}
	if normalizedName == "request_permissions" {
		switch policy.ApprovalMode {
		case ToolApprovalModeDeny:
			metadata.Permission = ToolPermissionDenied
			metadata.ToolEnabled = false
			metadata.ApprovalState = ToolApprovalStateDenied
			metadata.PolicyReason = "permission requests are denied by approval policy"
		default:
			metadata.Permission = ToolPermissionApprovalRequired
			metadata.ApprovalState = ToolApprovalStatePending
			metadata.PolicyReason = "permission request requires user approval"
		}
		return metadata
	}

	if metadata.ApprovalRequired {
		switch policy.ApprovalMode {
		case ToolApprovalModeRequire:
			metadata.Permission = ToolPermissionApprovalRequired
			metadata.ApprovalState = ToolApprovalStatePending
			metadata.PolicyReason = "tool call requires approval before execution"
		case ToolApprovalModeDeny:
			metadata.Permission = ToolPermissionDenied
			metadata.ToolEnabled = false
			metadata.ApprovalState = ToolApprovalStateDenied
			metadata.PolicyReason = "tool call denied by approval policy"
		default:
			metadata.Permission = ToolPermissionAllowed
			metadata.ApprovalState = ToolApprovalStateAutoApproved
		}
		return metadata
	}

	return metadata
}

func isZeroToolPolicy(policy ToolPolicy) bool {
	return policy.Planning == (PlanningPolicy{}) &&
		policy.ApprovalMode == "" &&
		len(policy.AllowedTools) == 0 &&
		len(policy.BlockedTools) == 0 &&
		!policy.BlockMutatingTools &&
		!policy.BlockShellCommands
}

func normalizeToolList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	dedupe := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		key := normalizeToolName(value)
		if key == "" {
			continue
		}
		if _, exists := dedupe[key]; exists {
			continue
		}
		dedupe[key] = struct{}{}
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized
}

func toolListContains(values []string, name string) bool {
	if len(values) == 0 {
		return false
	}
	normalized := normalizeToolName(name)
	for _, value := range values {
		if value == normalized {
			return true
		}
	}
	return false
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
