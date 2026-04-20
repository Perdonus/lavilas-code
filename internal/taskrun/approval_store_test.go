package taskrun

import (
	"encoding/json"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

func TestApprovalSessionStoreTurnGrantOnlyLastsForCurrentTurn(t *testing.T) {
	store := NewApprovalSessionStore()
	store.beginTurn()

	requestCall := plannedCall("perm-turn", "request_permissions", map[string]any{
		"reason": "need scratch output",
		"permissions": map[string]any{
			"writable_roots": []string{"./scratch"},
		},
	})
	writeCall := plannedCall("write-turn", "write_file", map[string]any{
		"path":    "./scratch/out.txt",
		"content": "hello",
	})

	store.rememberDecision(requestCall, ApprovalDecisionApprove)

	match := store.match(writeCall)
	if !match.allowed {
		t.Fatal("expected turn grant to allow write in current turn")
	}
	if match.scope != tooling.PermissionGrantScopeTurn {
		t.Fatalf("match scope = %s, want %s", match.scope, tooling.PermissionGrantScopeTurn)
	}

	store.beginTurn()

	if match := store.match(writeCall); match.allowed {
		t.Fatalf("turn grant should not survive beginTurn: %+v", match)
	}
}

func TestApprovalSessionStoreSessionGrantPersistsAcrossTurns(t *testing.T) {
	store := NewApprovalSessionStore()
	store.beginTurn()

	requestCall := plannedCall("perm-session", "request_permissions", map[string]any{
		"reason": "need scratch output",
		"permissions": map[string]any{
			"writable_roots": []string{"./scratch"},
		},
	})
	writeCall := plannedCall("write-session", "write_file", map[string]any{
		"path":    "./scratch/out.txt",
		"content": "hello",
	})

	store.rememberDecision(requestCall, ApprovalDecisionApproveForSession)
	store.beginTurn()

	match := store.match(writeCall)
	if !match.allowed {
		t.Fatal("expected session grant to survive beginTurn")
	}
	if match.scope != tooling.PermissionGrantScopeSession {
		t.Fatalf("match scope = %s, want %s", match.scope, tooling.PermissionGrantScopeSession)
	}
}

func plannedCall(id string, name string, arguments map[string]any) tooling.ToolCallPlan {
	data, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	plan := tooling.BuildExecutionPlanWithToolPolicy([]runtime.ToolCall{{
		ID:   id,
		Type: runtime.ToolTypeFunction,
		Function: runtime.FunctionCall{
			Name:      name,
			Arguments: json.RawMessage(data),
		},
	}}, tooling.ToolPolicy{ApprovalMode: tooling.ToolApprovalModeRequire})
	return plan.Batches[0].Calls[0]
}
