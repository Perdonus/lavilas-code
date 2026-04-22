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

	want := []string{"list_directory", "read_file", "request_permissions", "spawn_worker", "list_workers", "wait_worker", "cancel_worker"}
	if len(names) != len(want) {
		t.Fatalf("definition count = %d, want %d (%v)", len(names), len(want), names)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("definition[%d] = %q, want %q", index, names[index], name)
		}
	}
}

func TestDefinitionsExposeStrictSchemasForOpenAIProviders(t *testing.T) {
	definitions := Definitions()

	findDefinition := func(name string) toolruntime.ToolDefinition {
		t.Helper()
		for _, definition := range definitions {
			if definition.Function.Name == name {
				return definition
			}
		}
		t.Fatalf("definition %q not found", name)
		return toolruntime.ToolDefinition{}
	}

	runShell := findDefinition("run_shell_command")
	root := runShell.Function.Parameters
	if got, ok := root["additionalProperties"].(bool); !ok || got {
		t.Fatalf("run_shell_command additionalProperties = %#v, want false", root["additionalProperties"])
	}
	required, ok := root["required"].([]string)
	if !ok {
		t.Fatalf("run_shell_command required = %#v, want []string", root["required"])
	}
	wantRoot := []string{"cmd", "cwd", "timeout_seconds", "process_id", "yield_time_ms"}
	if len(required) != len(wantRoot) {
		t.Fatalf("run_shell_command required len = %d, want %d (%v)", len(required), len(wantRoot), required)
	}
	for index, value := range wantRoot {
		if required[index] != value {
			t.Fatalf("run_shell_command required[%d] = %q, want %q", index, required[index], value)
		}
	}

	requestPermissions := findDefinition("request_permissions")
	root = requestPermissions.Function.Parameters
	if got, ok := root["additionalProperties"].(bool); !ok || got {
		t.Fatalf("request_permissions additionalProperties = %#v, want false", root["additionalProperties"])
	}
	required, ok = root["required"].([]string)
	if !ok {
		t.Fatalf("request_permissions required = %#v, want []string", root["required"])
	}
	wantRoot = []string{"permissions", "reason"}
	if len(required) != len(wantRoot) {
		t.Fatalf("request_permissions required len = %d, want %d (%v)", len(required), len(wantRoot), required)
	}
	for index, value := range wantRoot {
		if required[index] != value {
			t.Fatalf("request_permissions required[%d] = %q, want %q", index, required[index], value)
		}
	}
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("request_permissions properties missing: %+v", root)
	}
	permissions, ok := properties["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions schema missing: %+v", properties)
	}
	if got, ok := permissions["additionalProperties"].(bool); !ok || got {
		t.Fatalf("permissions additionalProperties = %#v, want false", permissions["additionalProperties"])
	}
	nestedRequired, ok := permissions["required"].([]string)
	if !ok {
		t.Fatalf("permissions required = %#v, want []string", permissions["required"])
	}
	if len(nestedRequired) != 1 || nestedRequired[0] != "writable_roots" {
		t.Fatalf("permissions required = %#v, want [writable_roots]", nestedRequired)
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
