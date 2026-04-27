package tooling

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	maxBackgroundOutputChars   = maxToolOutputChars * 4
	maxBackgroundYield         = 30 * time.Second
	backgroundProcessRetention = 10 * time.Minute
)

type backgroundShellRegistry struct {
	mu        sync.Mutex
	processes map[string]*backgroundShellProcess
}

type backgroundShellProcess struct {
	id        string
	cmdText   string
	cwd       string
	timeout   time.Duration
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time

	mu              sync.Mutex
	stdout          cappedOutputBuffer
	stderr          cappedOutputBuffer
	finished        bool
	finishedAt      time.Time
	exitCode        int
	runError        string
	timedOut        bool
}

type cappedOutputBuffer struct {
	mu        sync.Mutex
	maxBytes  int
	truncated bool
	builder   strings.Builder
}

type shellCommandSnapshot struct {
	ProcessID       string
	Cmd             string
	Cwd             string
	Running         bool
	ExitCode        any
	OK              bool
	TimedOut        bool
	Error           string
	Stdout          string
	Stderr          string
	Output          string
	StdoutTruncated bool
	StderrTruncated bool
	StartedAt       time.Time
	FinishedAt      time.Time
}

var activeBackgroundShells = backgroundShellRegistry{
	processes: make(map[string]*backgroundShellProcess),
}

func ActiveBackgroundShellCount() int {
	activeBackgroundShells.mu.Lock()
	defer activeBackgroundShells.mu.Unlock()
	activeBackgroundShells.cleanupLocked(time.Now().UTC())
	count := 0
	for _, process := range activeBackgroundShells.processes {
		if process != nil && !process.isFinished() {
			count++
		}
	}
	return count
}

func BackgroundShellSnapshots() []shellCommandSnapshot {
	activeBackgroundShells.mu.Lock()
	activeBackgroundShells.cleanupLocked(time.Now().UTC())
	processes := make([]*backgroundShellProcess, 0, len(activeBackgroundShells.processes))
	for _, process := range activeBackgroundShells.processes {
		processes = append(processes, process)
	}
	activeBackgroundShells.mu.Unlock()

	snapshots := make([]shellCommandSnapshot, 0, len(processes))
	for _, process := range processes {
		if process == nil {
			continue
		}
		snapshots = append(snapshots, process.snapshot())
	}
	return snapshots
}

func StopAllBackgroundShells() int {
	activeBackgroundShells.mu.Lock()
	activeBackgroundShells.cleanupLocked(time.Now().UTC())
	processes := make([]*backgroundShellProcess, 0, len(activeBackgroundShells.processes))
	for _, process := range activeBackgroundShells.processes {
		processes = append(processes, process)
	}
	activeBackgroundShells.mu.Unlock()

	stopped := 0
	for _, process := range processes {
		if process == nil || process.isFinished() {
			continue
		}
		process.cancel()
		stopped++
	}
	return stopped
}

func startBackgroundShellCommand(ctx context.Context, commandText string, cwd string, timeout time.Duration, yieldTimeMs int) string {
	select {
	case <-ctx.Done():
		return marshalResult(map[string]any{"ok": false, "tool": "run_shell_command", "error": ctx.Err().Error()})
	default:
	}

	process, err := spawnBackgroundShell(commandText, cwd, timeout)
	if err != nil {
		return marshalResult(map[string]any{
			"ok":               false,
			"tool":             "run_shell_command",
			"cmd":              commandText,
			"cwd":              cwd,
			"exit_code":        nil,
			"output":           "",
			"stdout":           "",
			"stderr":           "",
			"timed_out":        false,
			"stdout_truncated": false,
			"stderr_truncated": false,
			"error":            err.Error(),
		})
	}

	activeBackgroundShells.store(process)
	waitForBackgroundProcess(ctx, process, normalizeYieldTime(yieldTimeMs))
	snapshot := process.snapshot()
	if !snapshot.Running {
		activeBackgroundShells.delete(process.id)
	}
	return marshalShellSnapshot(snapshot)
}

func pollBackgroundShellCommand(ctx context.Context, args shellArgs) string {
	processID := strings.TrimSpace(args.ProcessID)
	process, ok := activeBackgroundShells.lookup(processID)
	if !ok {
		return marshalResult(map[string]any{
			"ok":               false,
			"tool":             "run_shell_command",
			"process_id":       processID,
			"exit_code":        nil,
			"output":           "",
			"stdout":           "",
			"stderr":           "",
			"timed_out":        false,
			"stdout_truncated": false,
			"stderr_truncated": false,
			"error":            "process not found",
		})
	}
	waitForBackgroundProcess(ctx, process, normalizeYieldTime(args.YieldTimeMs))
	snapshot := process.snapshot()
	if !snapshot.Running {
		activeBackgroundShells.delete(processID)
	}
	return marshalShellSnapshot(snapshot)
}

func spawnBackgroundShell(commandText string, cwd string, timeout time.Duration) (*backgroundShellProcess, error) {
	commandCtx, cancel := context.WithTimeout(context.Background(), timeout)
	process := &backgroundShellProcess{
		id:        newBackgroundProcessID(),
		cmdText:   commandText,
		cwd:       cwd,
		timeout:   timeout,
		cancel:    cancel,
		done:      make(chan struct{}),
		startedAt: time.Now().UTC(),
		stdout: cappedOutputBuffer{
			maxBytes: maxBackgroundOutputChars,
		},
		stderr: cappedOutputBuffer{
			maxBytes: maxBackgroundOutputChars,
		},
	}

	cmd, _ := shellCommand(commandCtx, commandText)
	cmd.Dir = cwd

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	var copyWG sync.WaitGroup
	copyWG.Add(2)
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(&process.stdout, stdoutPipe)
	}()
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(&process.stderr, stderrPipe)
	}()
	go func() {
		waitErr := cmd.Wait()
		copyWG.Wait()
		process.markFinished(waitErr, commandCtx.Err())
	}()
	return process, nil
}

func waitForBackgroundProcess(ctx context.Context, process *backgroundShellProcess, wait time.Duration) {
	if wait < 0 {
		wait = 0
	}
	if process.isFinished() || wait == 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-process.done:
	case <-timer.C:
	case <-ctx.Done():
	}
}

func normalizeYieldTime(yieldTimeMs int) time.Duration {
	if yieldTimeMs <= 0 {
		return 0
	}
	wait := time.Duration(yieldTimeMs) * time.Millisecond
	if wait > maxBackgroundYield {
		return maxBackgroundYield
	}
	return wait
}

func marshalShellSnapshot(snapshot shellCommandSnapshot) string {
	payload := map[string]any{
		"ok":               snapshot.OK,
		"tool":             "run_shell_command",
		"cmd":              snapshot.Cmd,
		"cwd":              snapshot.Cwd,
		"running":          snapshot.Running,
		"exit_code":        snapshot.ExitCode,
		"output":           snapshot.Output,
		"stdout":           snapshot.Stdout,
		"stderr":           snapshot.Stderr,
		"timed_out":        snapshot.TimedOut,
		"stdout_truncated": snapshot.StdoutTruncated,
		"stderr_truncated": snapshot.StderrTruncated,
		"started_at":       snapshot.StartedAt,
	}
	if snapshot.Running {
		payload["status"] = "running"
		payload["process_id"] = snapshot.ProcessID
	} else if snapshot.TimedOut {
		payload["status"] = "timed_out"
		payload["finished_at"] = snapshot.FinishedAt
	} else if snapshot.OK {
		payload["status"] = "completed"
		payload["finished_at"] = snapshot.FinishedAt
	} else {
		payload["status"] = "failed"
		payload["finished_at"] = snapshot.FinishedAt
	}
	if strings.TrimSpace(snapshot.Error) != "" {
		payload["error"] = snapshot.Error
	}
	return marshalResult(payload)
}

func (r *backgroundShellRegistry) store(process *backgroundShellProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(time.Now().UTC())
	r.processes[process.id] = process
}

func (r *backgroundShellRegistry) lookup(processID string) (*backgroundShellProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(time.Now().UTC())
	process, ok := r.processes[processID]
	return process, ok
}

func (r *backgroundShellRegistry) delete(processID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.processes, processID)
}

func (r *backgroundShellRegistry) cleanupLocked(now time.Time) {
	for id, process := range r.processes {
		if process.expired(now) {
			delete(r.processes, id)
		}
	}
}

func (p *backgroundShellProcess) isFinished() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finished
}

func (p *backgroundShellProcess) markFinished(waitErr error, contextErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer close(p.done)
	defer p.cancel()

	p.finished = true
	p.finishedAt = time.Now().UTC()
	p.timedOut = contextErr == context.DeadlineExceeded
	p.runError = ""

	switch {
	case waitErr == nil:
		p.exitCode = 0
	case p.timedOut:
		p.exitCode = -1
		p.runError = fmt.Sprintf("command timed out after %s", p.timeout)
	default:
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
		} else {
			p.exitCode = -1
		}
		p.runError = waitErr.Error()
	}
}

func (p *backgroundShellProcess) snapshot() shellCommandSnapshot {
	p.mu.Lock()
	running := !p.finished
	finishedAt := p.finishedAt
	exitCode := p.exitCode
	runError := p.runError
	timedOut := p.timedOut
	p.mu.Unlock()

	stdoutText, stdoutTruncated := p.stdout.Snapshot(maxToolOutputChars)
	stderrText, stderrTruncated := p.stderr.Snapshot(maxToolOutputChars)
	snapshot := shellCommandSnapshot{
		ProcessID:       p.id,
		Cmd:             p.cmdText,
		Cwd:             p.cwd,
		Running:         running,
		ExitCode:        nil,
		OK:              running,
		TimedOut:        timedOut,
		Error:           runError,
		Stdout:          stdoutText,
		Stderr:          stderrText,
		Output:          joinCommandOutput(stdoutText, stderrText),
		StdoutTruncated: stdoutTruncated,
		StderrTruncated: stderrTruncated,
		StartedAt:       p.startedAt,
		FinishedAt:      finishedAt,
	}
	if running {
		return snapshot
	}
	snapshot.ExitCode = exitCode
	snapshot.OK = !timedOut && exitCode == 0
	return snapshot
}

func (p *backgroundShellProcess) expired(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.finished {
		return false
	}
	return now.Sub(p.finishedAt) > backgroundProcessRetention
}

func (b *cappedOutputBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxBytes <= 0 {
		b.maxBytes = maxBackgroundOutputChars
	}
	remaining := b.maxBytes - b.builder.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.builder.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	_, _ = b.builder.Write(data)
	return len(data), nil
}

func (b *cappedOutputBuffer) Snapshot(limit int) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, truncated := clampString(b.builder.String(), limit)
	return value, b.truncated || truncated
}

func newBackgroundProcessID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "proc_" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("proc_%d", time.Now().UnixNano())
}
