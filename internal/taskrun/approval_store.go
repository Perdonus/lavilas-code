package taskrun

import "github.com/Perdonus/lavilas-code/internal/tooling"

type approvalSessionStore struct {
	approved map[string]struct{}
}

func newApprovalSessionStore() *approvalSessionStore {
	return &approvalSessionStore{
		approved: make(map[string]struct{}),
	}
}

func (s *approvalSessionStore) IsApprovedForSession(call tooling.ToolCallPlan) bool {
	keys := approvalStoreKeys(call)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if _, ok := s.approved[key]; !ok {
			return false
		}
	}
	return true
}

func (s *approvalSessionStore) RememberApproved(call tooling.ToolCallPlan) {
	for _, key := range approvalStoreKeys(call) {
		s.approved[key] = struct{}{}
	}
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
