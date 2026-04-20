package tooling

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	toolruntime "github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestBuildExecutionPlanGroupsAdjacentReadOnlyCalls(t *testing.T) {
	calls := []toolruntime.ToolCall{
		toolCall("read-a", "read_file", `{"path":"a.txt"}`),
		toolCall("search-a", "search_text", `{"path":".","query":"needle"}`),
		toolCall("write-a", "write_file", `{"path":"a.txt","content":"updated"}`),
		toolCall("list-a", "list_directory", `{"path":"."}`),
		toolCall("read-b", "read_file", `{"path":"b.txt"}`),
	}

	plan := BuildExecutionPlan(calls)
	if got, want := len(plan.Batches), 3; got != want {
		t.Fatalf("batch count = %d, want %d", got, want)
	}
	assertBatchMode(t, plan.Batches[0], ExecutionModeParallel, 2)
	assertBatchMode(t, plan.Batches[1], ExecutionModeSequential, 1)
	assertBatchMode(t, plan.Batches[2], ExecutionModeParallel, 2)
	if !plan.Batches[0].Calls[0].Metadata.SupportsParallel {
		t.Fatalf("read_file should be marked parallel-safe")
	}
	if !plan.Batches[1].Calls[0].Metadata.MutatesWorkspace {
		t.Fatalf("write_file should be marked mutating")
	}
	if got, want := plan.Summary.ParallelBatchCount, 2; got != want {
		t.Fatalf("parallel batch count = %d, want %d", got, want)
	}
}

func TestBuildExecutionPlanRespectsSequentialPolicy(t *testing.T) {
	calls := []toolruntime.ToolCall{
		toolCall("read-a", "read_file", `{"path":"a.txt"}`),
		toolCall("read-b", "read_file", `{"path":"b.txt"}`),
	}

	plan := BuildExecutionPlanWithPolicy(calls, PlanningPolicy{AllowParallel: false})
	if got, want := len(plan.Batches), 1; got != want {
		t.Fatalf("batch count = %d, want %d", got, want)
	}
	assertBatchMode(t, plan.Batches[0], ExecutionModeSequential, 2)
}

func TestExecutePlanProducesOrderedResultsAndMessages(t *testing.T) {
	tmpDir := t.TempDir()
	firstPath := filepath.Join(tmpDir, "first.txt")
	secondPath := filepath.Join(tmpDir, "second.txt")
	if err := os.WriteFile(firstPath, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("beta\n"), 0o644); err != nil {
		t.Fatalf("write second file: %v", err)
	}

	calls := []toolruntime.ToolCall{
		toolCall("read-first", "read_file", jsonArgs(map[string]any{"path": firstPath})),
		toolCall("read-second", "read_file", jsonArgs(map[string]any{"path": secondPath})),
	}

	plan := BuildExecutionPlan(calls)
	report := ExecutePlan(context.Background(), plan)
	if got, want := len(report.Batches), 1; got != want {
		t.Fatalf("batch report count = %d, want %d", got, want)
	}
	if report.Batches[0].Mode != ExecutionModeParallel {
		t.Fatalf("batch report mode = %s, want %s", report.Batches[0].Mode, ExecutionModeParallel)
	}
	if got, want := len(report.Results), 2; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	for index, result := range report.Results {
		if result.Index != index {
			t.Fatalf("result[%d].Index = %d, want %d", index, result.Index, index)
		}
		if result.Mode != ExecutionModeParallel {
			t.Fatalf("result[%d].Mode = %s, want %s", index, result.Mode, ExecutionModeParallel)
		}
		if result.Status != ResultStatusSucceeded {
			t.Fatalf("result[%d].Status = %s, want %s", index, result.Status, ResultStatusSucceeded)
		}
		if len(result.OutputJSON) == 0 {
			t.Fatalf("result[%d] missing JSON payload", index)
		}
	}
	messages := report.Messages()
	if got, want := len(messages), 2; got != want {
		t.Fatalf("message count = %d, want %d", got, want)
	}
	if messages[0].ToolCallID != "read-first" || messages[1].ToolCallID != "read-second" {
		t.Fatalf("tool call ids were not preserved: %#v", []string{messages[0].ToolCallID, messages[1].ToolCallID})
	}
	if messages[0].Text() == "" || messages[1].Text() == "" {
		t.Fatalf("tool messages should contain payload text")
	}
}

func TestBuildExecutionPlanMarksShellAsConservative(t *testing.T) {
	plan := BuildExecutionPlan([]toolruntime.ToolCall{
		toolCall("shell", "run_shell_command", `{"cmd":"pwd","cwd":"."}`),
	})
	if got, want := len(plan.Batches), 1; got != want {
		t.Fatalf("batch count = %d, want %d", got, want)
	}
	call := plan.Batches[0].Calls[0]
	if call.Metadata.SideEffectKind != SideEffectKindShell {
		t.Fatalf("side effect kind = %s, want %s", call.Metadata.SideEffectKind, SideEffectKindShell)
	}
	if !call.Metadata.ApprovalRequired {
		t.Fatalf("shell tool should require approval metadata")
	}
	if !call.Metadata.SpawnsSubprocess {
		t.Fatalf("shell tool should be marked as spawning subprocesses")
	}
	if call.Metadata.SupportsParallel {
		t.Fatalf("shell tool should not be marked parallel-safe")
	}
}

func toolCall(id string, name string, arguments string) toolruntime.ToolCall {
	return toolruntime.ToolCall{
		ID:   id,
		Type: toolruntime.ToolTypeFunction,
		Function: toolruntime.FunctionCall{
			Name:      name,
			Arguments: json.RawMessage(arguments),
		},
	}
}

func jsonArgs(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func assertBatchMode(t *testing.T, batch ExecutionBatch, wantMode ExecutionMode, wantCalls int) {
	t.Helper()
	if batch.Mode != wantMode {
		t.Fatalf("batch[%d].Mode = %s, want %s", batch.Index, batch.Mode, wantMode)
	}
	if got := len(batch.Calls); got != wantCalls {
		t.Fatalf("batch[%d] call count = %d, want %d", batch.Index, got, wantCalls)
	}
}
