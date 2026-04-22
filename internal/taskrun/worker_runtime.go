package taskrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type workerRuntimeOptions struct {
	SystemPrompt string
	Model        string
	Provider     string
	Profile      string
	Reasoning    string
	CWD          string
	ToolPolicy   tooling.ToolPolicy
	History      []runtime.Message
}

type activeWorker struct {
	id         string
	prompt     string
	model      string
	provider   string
	profile    string
	reasoning  string
	cwd        string
	startedAt  time.Time
	finishedAt time.Time

	mu     sync.Mutex
	status string
	output string
	err    string
	cancel context.CancelFunc
	done   chan struct{}
}

type workerRegistry struct {
	mu      sync.Mutex
	workers map[string]*activeWorker
}

type workerToolRuntime struct {
	base workerRuntimeOptions
}

var globalWorkers = &workerRegistry{workers: map[string]*activeWorker{}}

func newWorkerToolRuntime(options workerRuntimeOptions) tooling.WorkerRuntime {
	return &workerToolRuntime{base: options}
}

func (r *workerToolRuntime) Spawn(_ context.Context, args tooling.WorkerSpawnArgs) (tooling.WorkerSummary, error) {
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return tooling.WorkerSummary{}, fmt.Errorf("worker prompt is required")
	}
	workerID := newWorkerID()
	ctx, cancel := context.WithCancel(context.Background())
	worker := &activeWorker{
		id:        workerID,
		prompt:    prompt,
		model:     firstNonEmpty(strings.TrimSpace(args.Model), strings.TrimSpace(r.base.Model)),
		provider:  firstNonEmpty(strings.TrimSpace(args.Provider), strings.TrimSpace(r.base.Provider)),
		profile:   firstNonEmpty(strings.TrimSpace(args.Profile), strings.TrimSpace(r.base.Profile)),
		reasoning: firstNonEmpty(strings.TrimSpace(args.Reasoning), strings.TrimSpace(r.base.Reasoning)),
		cwd:       firstNonEmpty(strings.TrimSpace(args.Cwd), strings.TrimSpace(r.base.CWD)),
		startedAt: time.Now().UTC(),
		status:    "running",
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	globalWorkers.store(worker)
	go r.runWorker(ctx, worker)
	return worker.summary(), nil
}

func (r *workerToolRuntime) runWorker(ctx context.Context, worker *activeWorker) {
	defer close(worker.done)
	result, err := Run(ctx, Options{
		Prompt:          worker.prompt,
		SystemPrompt:    r.base.SystemPrompt,
		Model:           worker.model,
		Provider:        worker.provider,
		Profile:         worker.profile,
		ReasoningEffort: worker.reasoning,
		CWD:             worker.cwd,
		ToolPolicy:      r.base.ToolPolicy,
		History:         cloneMessages(r.base.History),
	})

	worker.mu.Lock()
	defer worker.mu.Unlock()
	worker.finishedAt = time.Now().UTC()
	if err != nil {
		if ctx.Err() != nil {
			worker.status = "cancelled"
			worker.err = ctx.Err().Error()
		} else {
			worker.status = "failed"
			worker.err = err.Error()
		}
		return
	}
	worker.status = "completed"
	worker.output = strings.TrimSpace(result.Text)
	if worker.output == "" && result.Response != nil && len(result.Response.Choices) > 0 {
		worker.output = strings.TrimSpace(result.Response.Choices[0].Message.Text())
	}
}

func (r *workerToolRuntime) List(_ context.Context, args tooling.WorkerListArgs) ([]tooling.WorkerSummary, error) {
	return globalWorkers.list(args.IncludeFinished), nil
}

func (r *workerToolRuntime) Wait(ctx context.Context, args tooling.WorkerWaitArgs) (tooling.WorkerSummary, error) {
	worker, ok := globalWorkers.lookup(strings.TrimSpace(args.WorkerID))
	if !ok {
		return tooling.WorkerSummary{}, fmt.Errorf("worker %q not found", args.WorkerID)
	}
	wait := 0 * time.Second
	if args.TimeoutSeconds > 0 {
		wait = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-worker.done:
		case <-timer.C:
		case <-ctx.Done():
			return tooling.WorkerSummary{}, ctx.Err()
		}
	} else {
		select {
		case <-worker.done:
		default:
		}
	}
	summary := worker.summary()
	if summary.Status != "running" {
		globalWorkers.prune(summary.ID)
	}
	return summary, nil
}

func (r *workerToolRuntime) Cancel(_ context.Context, args tooling.WorkerCancelArgs) (tooling.WorkerSummary, error) {
	worker, ok := globalWorkers.lookup(strings.TrimSpace(args.WorkerID))
	if !ok {
		return tooling.WorkerSummary{}, fmt.Errorf("worker %q not found", args.WorkerID)
	}
	if worker.cancel != nil {
		worker.cancel()
	}
	return worker.summary(), nil
}

func (r *workerRegistry) store(worker *activeWorker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[worker.id] = worker
}

func (r *workerRegistry) lookup(id string) (*activeWorker, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	worker, ok := r.workers[id]
	return worker, ok
}

func (r *workerRegistry) list(includeFinished bool) []tooling.WorkerSummary {
	r.mu.Lock()
	workers := make([]*activeWorker, 0, len(r.workers))
	for _, worker := range r.workers {
		workers = append(workers, worker)
	}
	r.mu.Unlock()

	summaries := make([]tooling.WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		summary := worker.summary()
		if !includeFinished && summary.Status != "running" {
			continue
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func (r *workerRegistry) prune(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workers, id)
}

func (w *activeWorker) summary() tooling.WorkerSummary {
	w.mu.Lock()
	defer w.mu.Unlock()
	return tooling.WorkerSummary{
		ID:         w.id,
		Status:     firstNonEmpty(w.status, "running"),
		Prompt:     w.prompt,
		Model:      w.model,
		Provider:   w.provider,
		Profile:    w.profile,
		Reasoning:  w.reasoning,
		CWD:        w.cwd,
		Output:     w.output,
		Error:      w.err,
		StartedAt:  w.startedAt,
		FinishedAt: w.finishedAt,
	}
}

func newWorkerID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	return "worker-" + hex.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
