package tooling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	toolruntime "github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestDefinitionsWithPolicyFiltersDeniedTools(t *testing.T) {
	policy := DefaultToolPolicy()
	policy.BlockShellCommands = true
	policy.BlockMutatingTools = true
	policy.BlockedTools = []string{"search_text"}

	definitions := DefinitionsWithPolicy(policy)
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}

	want := []string{"list_directory", "read_file", "request_permissions"}
	if len(names) != len(want) {
		t.Fatalf("definition count = %d, want %d (%v)", len(names), len(want), names)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("definition[%d] = %q, want %q", index, names[index], name)
		}
	}
}

func TestExecutePlanRequireApprovalSkipsMutatingTool(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "blocked.txt")
	call := toolCall("write-blocked", "write_file", jsonArgs(map[string]any{
		"path":    targetPath,
		"content": "denied",
	}))

	policy := DefaultToolPolicy()
	policy.ApprovalMode = ToolApprovalModeRequire
	report := ExecutePlan(context.Background(), BuildExecutionPlanWithToolPolicy([]toolruntime.ToolCall{call}, policy))

	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("write_file should not execute under require policy, stat err = %v", err)
	}
	if got, want := report.Summary.ApprovalRequiredCount, 1; got != want {
		t.Fatalf("approval_required_count = %d, want %d", got, want)
	}
	if len(report.ApprovalRequests) != 1 {
		t.Fatalf("approval request count = %d, want 1", len(report.ApprovalRequests))
	}
	if report.Results[0].Status != ResultStatusApprovalRequired {
		t.Fatalf("result status = %s, want %s", report.Results[0].Status, ResultStatusApprovalRequired)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(report.Results[0].OutputText), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if payload["status"] != string(ResultStatusApprovalRequired) {
		t.Fatalf("payload status = %v, want %s", payload["status"], ResultStatusApprovalRequired)
	}
}

func TestExecutePlanDefaultPolicyAutoApprovesMutatingTool(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "written.txt")
	call := toolCall("write-ok", "write_file", jsonArgs(map[string]any{
		"path":    targetPath,
		"content": "hello",
	}))

	report := ExecutePlan(context.Background(), BuildExecutionPlanWithToolPolicy([]toolruntime.ToolCall{call}, DefaultToolPolicy()))

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("written content = %q, want hello", string(content))
	}
	if got, want := report.Summary.SucceededCount, 1; got != want {
		t.Fatalf("succeeded_count = %d, want %d", got, want)
	}
	if report.Results[0].Status != ResultStatusSucceeded {
		t.Fatalf("result status = %s, want %s", report.Results[0].Status, ResultStatusSucceeded)
	}
}

func TestExecutePlanRequestPermissionsStillRequiresApprovalUnderAutoMode(t *testing.T) {
	call := toolCall("perm-request", "request_permissions", jsonArgs(map[string]any{
		"reason": "need write access",
		"permissions": map[string]any{
			"writable_roots": []string{"./sandbox"},
		},
	}))

	report := ExecutePlan(context.Background(), BuildExecutionPlanWithToolPolicy([]toolruntime.ToolCall{call}, DefaultToolPolicy()))

	if got, want := report.Summary.ApprovalRequiredCount, 1; got != want {
		t.Fatalf("approval_required_count = %d, want %d", got, want)
	}
	if report.Results[0].Status != ResultStatusApprovalRequired {
		t.Fatalf("result status = %s, want %s", report.Results[0].Status, ResultStatusApprovalRequired)
	}
	if report.Results[0].Metadata.Permission != ToolPermissionApprovalRequired {
		t.Fatalf("permission = %s, want %s", report.Results[0].Metadata.Permission, ToolPermissionApprovalRequired)
	}
	if got := report.Results[0].Metadata.RequestedWritableRoots; len(got) != 1 {
		t.Fatalf("requested writable roots = %v, want one normalized root", got)
	}
}
