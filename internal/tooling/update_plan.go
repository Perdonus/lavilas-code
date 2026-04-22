package tooling

import "strings"

type UpdatePlanStatus string

const (
	UpdatePlanStatusPending    UpdatePlanStatus = "pending"
	UpdatePlanStatusInProgress UpdatePlanStatus = "in_progress"
	UpdatePlanStatusCompleted  UpdatePlanStatus = "completed"
)

type UpdatePlanStep struct {
	Step   string           `json:"step"`
	Status UpdatePlanStatus `json:"status"`
}

type UpdatePlanArgs struct {
	Explanation string           `json:"explanation,omitempty"`
	Plan        []UpdatePlanStep `json:"plan"`
}

func normalizeUpdatePlanArgs(args UpdatePlanArgs) UpdatePlanArgs {
	args.Explanation = strings.TrimSpace(args.Explanation)
	if len(args.Plan) == 0 {
		return args
	}
	normalized := make([]UpdatePlanStep, 0, len(args.Plan))
	for _, step := range args.Plan {
		text := strings.TrimSpace(step.Step)
		if text == "" {
			continue
		}
		status := normalizeUpdatePlanStatus(step.Status)
		normalized = append(normalized, UpdatePlanStep{
			Step:   text,
			Status: status,
		})
	}
	args.Plan = normalized
	return args
}

func normalizeUpdatePlanStatus(status UpdatePlanStatus) UpdatePlanStatus {
	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(UpdatePlanStatusCompleted):
		return UpdatePlanStatusCompleted
	case string(UpdatePlanStatusInProgress):
		return UpdatePlanStatusInProgress
	default:
		return UpdatePlanStatusPending
	}
}

func marshalUpdatePlanResult(args UpdatePlanArgs) string {
	args = normalizeUpdatePlanArgs(args)
	return marshalResult(map[string]any{
		"ok":          true,
		"tool":        "update_plan",
		"explanation": args.Explanation,
		"plan":        args.Plan,
		"message":     "Plan updated",
	})
}
