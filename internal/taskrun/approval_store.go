package taskrun

import (
	"strings"

	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type ApprovalSessionStore struct {
	sessionApproved map[string]struct{}
	turnApproved    map[string]struct{}
	sessionGrant    tooling.PermissionGrant
	turnGrant       tooling.PermissionGrant
}

type approvalSessionStore = ApprovalSessionStore

type approvalMatch struct {
	allowed       bool
	scope         tooling.PermissionGrantScope
	writableRoots []string
}

func NewApprovalSessionStore() *ApprovalSessionStore {
	return &ApprovalSessionStore{
		sessionApproved: make(map[string]struct{}),
		turnApproved:    make(map[string]struct{}),
	}
}

func newApprovalSessionStore() *ApprovalSessionStore {
	return NewApprovalSessionStore()
}

func (s *ApprovalSessionStore) IsApprovedForSession(call tooling.ToolCallPlan) bool {
	if s == nil {
		return false
	}
	if approvalKeysMatch(call, s.sessionApproved) {
		return true
	}
	return len(s.sessionGrant.MatchingWritableRoots(tooling.WriteTargetsForCall(call))) > 0
}

func (s *ApprovalSessionStore) RememberApproved(call tooling.ToolCallPlan) {
	if s == nil {
		return
	}
	s.rememberApprovalKeys(call, tooling.PermissionGrantScopeSession)
	s.rememberPermissionGrant(tooling.PermissionGrantForApprovedCall(call), tooling.PermissionGrantScopeSession)
}

func (s *ApprovalSessionStore) beginTurn() {
	if s == nil {
		return
	}
	s.turnApproved = make(map[string]struct{})
	s.turnGrant = tooling.PermissionGrant{}
}

func (s *ApprovalSessionStore) match(call tooling.ToolCallPlan) approvalMatch {
	if s == nil {
		return approvalMatch{}
	}
	if approvalKeysMatch(call, s.turnApproved) {
		return approvalMatch{allowed: true, scope: tooling.PermissionGrantScopeTurn}
	}
	if approvalKeysMatch(call, s.sessionApproved) {
		return approvalMatch{allowed: true, scope: tooling.PermissionGrantScopeSession}
	}
	targets := tooling.WriteTargetsForCall(call)
	if roots := s.turnGrant.MatchingWritableRoots(targets); len(roots) > 0 {
		return approvalMatch{
			allowed:       true,
			scope:         tooling.PermissionGrantScopeTurn,
			writableRoots: roots,
		}
	}
	if roots := s.sessionGrant.MatchingWritableRoots(targets); len(roots) > 0 {
		return approvalMatch{
			allowed:       true,
			scope:         tooling.PermissionGrantScopeSession,
			writableRoots: roots,
		}
	}
	return approvalMatch{}
}

func (s *ApprovalSessionStore) rememberDecision(call tooling.ToolCallPlan, decision ApprovalDecision) {
	if s == nil {
		return
	}
	switch decision {
	case ApprovalDecisionApproveForSession:
		s.rememberApprovalKeys(call, tooling.PermissionGrantScopeSession)
		s.rememberPermissionGrant(tooling.PermissionGrantForApprovedCall(call), tooling.PermissionGrantScopeSession)
	case ApprovalDecisionApprove:
		if !strings.EqualFold(strings.TrimSpace(call.Name), "request_permissions") {
			return
		}
		s.rememberApprovalKeys(call, tooling.PermissionGrantScopeTurn)
		s.rememberPermissionGrant(tooling.PermissionGrantForApprovedCall(call), tooling.PermissionGrantScopeTurn)
	}
}

func (s *ApprovalSessionStore) rememberApprovalKeys(call tooling.ToolCallPlan, scope tooling.PermissionGrantScope) {
	keys := approvalStoreKeys(call)
	if len(keys) == 0 {
		return
	}
	store := s.approvedMap(scope)
	for _, key := range keys {
		store[key] = struct{}{}
	}
}

func (s *ApprovalSessionStore) rememberPermissionGrant(grant tooling.PermissionGrant, scope tooling.PermissionGrantScope) {
	if grant.IsEmpty() {
		return
	}
	switch scope {
	case tooling.PermissionGrantScopeTurn:
		s.turnGrant = s.turnGrant.Merge(grant)
	default:
		s.sessionGrant = s.sessionGrant.Merge(grant)
	}
}

func (s *ApprovalSessionStore) approvedMap(scope tooling.PermissionGrantScope) map[string]struct{} {
	switch scope {
	case tooling.PermissionGrantScopeTurn:
		if s.turnApproved == nil {
			s.turnApproved = make(map[string]struct{})
		}
		return s.turnApproved
	default:
		if s.sessionApproved == nil {
			s.sessionApproved = make(map[string]struct{})
		}
		return s.sessionApproved
	}
}

func approvalKeysMatch(call tooling.ToolCallPlan, approved map[string]struct{}) bool {
	if len(approved) == 0 {
		return false
	}
	keys := approvalStoreKeys(call)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if _, ok := approved[key]; !ok {
			return false
		}
	}
	return true
}

func approvalStoreKeys(call tooling.ToolCallPlan) []string {
	if len(call.ApprovalKeys) > 0 {
		return append([]string(nil), call.ApprovalKeys...)
	}
	if call.ApprovalID == "" {
		return nil
	}
	return []string{call.ApprovalID}
}
