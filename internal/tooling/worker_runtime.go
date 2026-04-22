package tooling

import (
	"context"
	"time"
)

type WorkerSpawnArgs struct {
	Prompt    string `json:"prompt"`
	Cwd       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type WorkerWaitArgs struct {
	WorkerID       string `json:"worker_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type WorkerCancelArgs struct {
	WorkerID string `json:"worker_id"`
}

type WorkerListArgs struct {
	IncludeFinished bool `json:"include_finished,omitempty"`
}

type WorkerSummary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Prompt     string    `json:"prompt,omitempty"`
	Model      string    `json:"model,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
	CWD        string    `json:"cwd,omitempty"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type WorkerRuntime interface {
	Spawn(context.Context, WorkerSpawnArgs) (WorkerSummary, error)
	List(context.Context, WorkerListArgs) ([]WorkerSummary, error)
	Wait(context.Context, WorkerWaitArgs) (WorkerSummary, error)
	Cancel(context.Context, WorkerCancelArgs) (WorkerSummary, error)
}

type workerRuntimeContextKey struct{}

func WithWorkerRuntime(ctx context.Context, runtime WorkerRuntime) context.Context {
	if runtime == nil {
		return ctx
	}
	return context.WithValue(ctx, workerRuntimeContextKey{}, runtime)
}

func workerRuntimeFromContext(ctx context.Context) WorkerRuntime {
	if ctx == nil {
		return nil
	}
	runtime, _ := ctx.Value(workerRuntimeContextKey{}).(WorkerRuntime)
	return runtime
}
