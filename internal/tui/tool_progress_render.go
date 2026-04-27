package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type shellToolOutput struct {
	Tool            string `json:"tool"`
	Shell           string `json:"shell"`
	Cmd             string `json:"cmd"`
	Cwd             string `json:"cwd"`
	Output          string `json:"output"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        *int   `json:"exit_code"`
	ProcessID       string `json:"process_id"`
	Status          string `json:"status"`
	Running         bool   `json:"running"`
	OK              bool   `json:"ok"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	Error           string `json:"error"`
}

type workerToolOutput struct {
	Tool    string                `json:"tool"`
	OK      bool                  `json:"ok"`
	Error   string                `json:"error"`
	Worker  *tooling.WorkerSummary `json:"worker"`
	Workers []tooling.WorkerSummary `json:"workers"`
}

type updatePlanToolOutput struct {
	Tool        string                   `json:"tool"`
	OK          bool                     `json:"ok"`
	Explanation string                   `json:"explanation"`
	Plan        []tooling.UpdatePlanStep `json:"plan"`
}

type listDirectoryToolOutput struct {
	Tool      string `json:"tool"`
	OK        bool   `json:"ok"`
	Path      string `json:"path"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

type readFileToolOutput struct {
	Tool      string `json:"tool"`
	OK        bool   `json:"ok"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

type searchTextToolOutput struct {
	Tool      string `json:"tool"`
	OK        bool   `json:"ok"`
	Path      string `json:"path"`
	Query     string `json:"query"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

type writeFileToolOutput struct {
	Tool  string `json:"tool"`
	OK    bool   `json:"ok"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Error string `json:"error"`
}

type patchToolOutput struct {
	Tool           string   `json:"tool"`
	OK             bool     `json:"ok"`
	CheckOnly      bool     `json:"check_only"`
	Paths          []string `json:"paths"`
	Patch          string   `json:"patch"`
	PatchTruncated bool     `json:"patch_truncated"`
	Stdout         string   `json:"stdout"`
	Stderr         string   `json:"stderr"`
	Error          string   `json:"error"`
}

func renderToolPlanEntry(language commandcatalog.CatalogLanguage, plan *tooling.ExecutionPlan) TranscriptEntry {
	if plan == nil {
		return TranscriptEntry{}
	}
	lines := []string{
		localizedTextTUI(language, "Updated plan", "Обновлённый план"),
		localizedTextTUI(
			language,
			"└ %d batches • %d calls",
			"└ %d пакетов • %d вызовов",
			plan.Summary.BatchCount,
			plan.Summary.CallCount,
		),
	}
	for _, batch := range plan.Batches {
		modeLabel := localizedTextTUI(language, "sequential", "последовательно")
		if batch.Mode == tooling.ExecutionModeParallel {
			modeLabel = localizedTextTUI(language, "parallel", "параллельно")
		}
		lines = append(lines, localizedTextTUI(language, "  Batch %d · %s", "  Пакет %d · %s", batch.Index+1, modeLabel))
		for _, call := range batch.Calls {
			summary, details := formatToolCallPreview(language, call.Name, call.Arguments, call.Metadata.WorkingDirectory)
			line := "    □ " + summary
			if details != "" {
				line += "  " + details
			}
			lines = append(lines, line)
		}
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func RenderToolPlanText(language commandcatalog.CatalogLanguage, plan *tooling.ExecutionPlan) string {
	return renderToolPlanEntry(language, plan).Body
}

func renderApprovalEntry(language commandcatalog.CatalogLanguage, request *tooling.ApprovalRequest) TranscriptEntry {
	if request == nil {
		return TranscriptEntry{}
	}
	lines := []string{
		localizedTextTUI(language, "Approval required", "Нужно подтверждение"),
		localizedTextTUI(language, "└ tool: %s", "└ инструмент: %s", request.Name),
	}
	if summary := strings.TrimSpace(request.Summary); summary != "" {
		lines = append(lines, localizedTextTUI(language, "  summary: %s", "  сводка: %s", summary))
	}
	if details := strings.TrimSpace(request.Details); details != "" {
		lines = append(lines, localizedTextTUI(language, "  details: %s", "  детали: %s", details))
	}
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		lines = append(lines, localizedTextTUI(language, "  reason: %s", "  причина: %s", reason))
	}
	if cwd := strings.TrimSpace(request.Metadata.WorkingDirectory); cwd != "" {
		lines = append(lines, "  cwd: "+cwd)
	}
	if len(request.Metadata.RequestedWritableRoots) > 0 {
		lines = append(lines, localizedTextTUI(
			language,
			"  write access: %s",
			"  доступ на запись: %s",
			strings.Join(request.Metadata.RequestedWritableRoots, ", "),
		))
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func renderToolResultEntry(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) TranscriptEntry {
	if result == nil {
		return TranscriptEntry{}
	}
	if entry := renderPlanUpdateResultEntry(language, result); strings.TrimSpace(entry.Body) != "" {
		return entry
	}
	if entry := renderShellResultEntry(language, result); strings.TrimSpace(entry.Body) != "" {
		return entry
	}
	if entry := renderExploreResultEntry(language, result); strings.TrimSpace(entry.Body) != "" {
		return entry
	}
	if entry := renderEditResultEntry(language, result); strings.TrimSpace(entry.Body) != "" {
		return entry
	}
	lines := []string{
		localizedTextTUI(language, "%s · %s", "%s · %s", result.Name, localizedToolStatusTUI(language, string(result.Status))),
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		lines = append(lines, localizedTextTUI(language, "  summary: %s", "  сводка: %s", summary))
	}
	if details := strings.TrimSpace(result.Details); details != "" {
		lines = append(lines, localizedTextTUI(language, "  details: %s", "  детали: %s", details))
	}
	if result.Duration > 0 {
		lines = append(lines, localizedTextTUI(language, "  duration: %s", "  длительность: %s", result.Duration.Round(time.Millisecond)))
	}
	if preview := renderToolOutputPreview(language, result.Name, result.OutputText); preview != "" {
		lines = append(lines, preview)
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func RenderToolResultText(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) string {
	return renderToolResultEntry(language, result).Body
}

func renderShellResultEntry(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) TranscriptEntry {
	if result == nil || !strings.EqualFold(strings.TrimSpace(result.Name), "run_shell_command") {
		return TranscriptEntry{}
	}
	var payload shellToolOutput
	if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
		return TranscriptEntry{}
	}
	cmd := strings.TrimSpace(payload.Cmd)
	if cmd == "" {
		cmd = strings.TrimSpace(payload.ProcessID)
	}
	if cmd == "" {
		cmd = "run_shell_command"
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	running := payload.Running || status == "running"
	commandLines := compactCommandLines(cmd, 120)
	title := "Ran " + firstNonEmpty(firstString(commandLines), cmd)
	if running {
		title = localizedTextTUI(language, "Waiting for background terminal", "Ожидание фонового терминала")
		if payload.ProcessID != "" {
			title += " " + payload.ProcessID
		}
	}
	lines := []string{title}
	if strings.TrimSpace(payload.Shell) != "" {
		lines = append(lines, "│ shell: "+payload.Shell)
	}
	commandStartIndex := 1
	if running {
		commandStartIndex = 0
	}
	if commandStartIndex > len(commandLines) {
		commandStartIndex = len(commandLines)
	}
	for _, line := range commandLines[commandStartIndex:] {
		lines = append(lines, "│ "+line)
	}
	if cwd := strings.TrimSpace(payload.Cwd); cwd != "" {
		lines = append(lines, "│ cwd: "+cwd)
	}
	output := strings.TrimSpace(payload.Output)
	if output == "" {
		output = strings.TrimSpace(joinNonEmpty(payload.Stdout, payload.Stderr))
	}
	if output != "" {
		lines = append(lines, compactToolOutputBlock(truncateToolPreview(output, 4000))...)
	} else if !running && payload.ExitCode != nil {
		lines = append(lines, localizedTextTUI(language, "└ exit code: %d", "└ код выхода: %d", *payload.ExitCode))
	}
	if err := strings.TrimSpace(payload.Error); err != "" {
		lines = append(lines, localizedTextTUI(language, "└ error: %s", "└ ошибка: %s", err))
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func compactCommandLines(command string, limit int) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	rawLines := strings.Split(command, "\n")
	lines := make([]string, 0, minInt(len(rawLines), 18))
	for _, line := range rawLines {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, truncateToolPreview(line, limit))
		if len(lines) >= 18 {
			lines = append(lines, "...<truncated>")
			break
		}
	}
	return lines
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func renderExploreResultEntry(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) TranscriptEntry {
	action := renderExploreAction(language, result)
	if strings.TrimSpace(action) == "" {
		return TranscriptEntry{}
	}
	return renderExploreActionsEntry(language, []string{action})
}

func renderExploreAction(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) string {
	if result == nil {
		return ""
	}
	var action string
	switch strings.TrimSpace(result.Name) {
	case "read_file":
		var payload readFileToolOutput
		if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
			return ""
		}
		action = localizedTextTUI(language, "Read %s", "Read %s", compactToolPath(payload.Path))
	case "search_text":
		var payload searchTextToolOutput
		if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
			return ""
		}
		target := compactToolPath(payload.Path)
		if target == "" || target == "." {
			target = localizedTextTUI(language, ".", ".")
		}
		action = localizedTextTUI(language, "Search %s in %s", "Search %s in %s", strings.TrimSpace(payload.Query), target)
	case "list_directory":
		var payload listDirectoryToolOutput
		if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
			return ""
		}
		action = localizedTextTUI(language, "List %s", "List %s", compactToolPath(payload.Path))
	default:
		return ""
	}
	return strings.TrimSpace(action)
}

func renderExploreActionsEntry(language commandcatalog.CatalogLanguage, actions []string) TranscriptEntry {
	cleaned := make([]string, 0, len(actions))
	for _, action := range actions {
		if action = strings.TrimSpace(action); action != "" {
			cleaned = append(cleaned, action)
		}
	}
	if len(cleaned) == 0 {
		return TranscriptEntry{}
	}
	lines := []string{localizedTextTUI(language, "Explored", "Explored"), "└ " + cleaned[0]}
	for _, action := range cleaned[1:] {
		lines = append(lines, "  "+action)
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func renderEditResultEntry(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) TranscriptEntry {
	if result == nil {
		return TranscriptEntry{}
	}
	switch strings.TrimSpace(result.Name) {
	case "write_file":
		var payload writeFileToolOutput
		if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
			return TranscriptEntry{}
		}
		if !payload.OK {
			return TranscriptEntry{Role: "tool", Body: localizedTextTUI(language, "Edit failed %s", "Ошибка правки %s", compactToolPath(payload.Path))}
		}
		title := localizedTextTUI(language, "Edited %s", "Edited %s", compactToolPath(payload.Path))
		if payload.Bytes > 0 {
			title += fmt.Sprintf("\n└ bytes=%d", payload.Bytes)
		}
		return TranscriptEntry{Role: "tool", Body: title}
	case "apply_patch":
		var payload patchToolOutput
		if json.Unmarshal([]byte(strings.TrimSpace(result.OutputText)), &payload) != nil {
			return TranscriptEntry{}
		}
		path := localizedTextTUI(language, "workspace", "workspace")
		if len(payload.Paths) > 0 {
			path = strings.Join(compactToolPaths(payload.Paths), ", ")
		}
		added, removed := countPatchLines(payload.Patch)
		title := localizedTextTUI(language, "Edited %s", "Edited %s", path)
		if added > 0 || removed > 0 {
			title += fmt.Sprintf(" (+%d -%d)", added, removed)
		}
		lines := []string{title}
		if patch := renderPatchPreview(payload.Patch); patch != "" {
			lines = append(lines, patch)
		}
		if out := strings.TrimSpace(joinNonEmpty(payload.Stdout, payload.Stderr)); out != "" {
			lines = append(lines, compactToolOutputBlock(truncateToolPreview(out, 1200))...)
		}
		if err := strings.TrimSpace(payload.Error); err != "" {
			lines = append(lines, localizedTextTUI(language, "└ error: %s", "└ ошибка: %s", err))
		}
		return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
	default:
		return TranscriptEntry{}
	}
}

func renderPlanUpdateResultEntry(language commandcatalog.CatalogLanguage, result *tooling.ToolResultEnvelope) TranscriptEntry {
	if result == nil || !strings.EqualFold(strings.TrimSpace(result.Name), "update_plan") {
		return TranscriptEntry{}
	}
	body := renderPlanUpdateBody(language, result.OutputText)
	if strings.TrimSpace(body) == "" {
		return TranscriptEntry{}
	}
	return TranscriptEntry{Role: "tool", Body: body}
}

func renderPlanUpdateBody(language commandcatalog.CatalogLanguage, raw string) string {
	var payload updatePlanToolOutput
	raw = strings.TrimSpace(raw)
	if raw == "" || json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	payload.Plan = normalizeRenderedPlan(payload.Plan)
	lines := []string{localizedTextTUI(language, "Updated plan", "Обновлённый план")}
	sub := make([]string, 0, len(payload.Plan)+1)
	if explanation := strings.TrimSpace(payload.Explanation); explanation != "" {
		sub = append(sub, explanation)
	}
	if len(payload.Plan) == 0 {
		sub = append(sub, localizedTextTUI(language, "(no steps provided)", "(шаги не переданы)"))
	} else {
		for _, step := range payload.Plan {
			sub = append(sub, renderPlanStepLine(language, step))
		}
	}
	for index, line := range sub {
		prefix := "    "
		if index == 0 {
			prefix = "  └ "
		}
		lines = append(lines, prefix+line)
	}
	return strings.Join(lines, "\n")
}

func normalizeRenderedPlan(plan []tooling.UpdatePlanStep) []tooling.UpdatePlanStep {
	if len(plan) == 0 {
		return nil
	}
	out := make([]tooling.UpdatePlanStep, 0, len(plan))
	for _, step := range plan {
		text := strings.TrimSpace(step.Step)
		if text == "" {
			continue
		}
		out = append(out, tooling.UpdatePlanStep{
			Step:   text,
			Status: tooling.UpdatePlanStatus(strings.TrimSpace(string(step.Status))),
		})
	}
	return out
}

func renderPlanStepLine(_ commandcatalog.CatalogLanguage, step tooling.UpdatePlanStep) string {
	status := tooling.UpdatePlanStatus(strings.ToLower(strings.TrimSpace(string(step.Status))))
	switch status {
	case tooling.UpdatePlanStatusCompleted:
		return "✔ " + strings.TrimSpace(step.Step)
	case tooling.UpdatePlanStatusInProgress:
		return "□ " + strings.TrimSpace(step.Step)
	default:
		return "□ " + strings.TrimSpace(step.Step)
	}
}

func renderRetryEntry(language commandcatalog.CatalogLanguage, retryAfter time.Duration, err error) TranscriptEntry {
	lines := []string{localizedTextTUI(language, "Retry scheduled", "Запланирован повтор")}
	if retryAfter > 0 {
		lines = append(lines, localizedTextTUI(language, "└ waiting %s", "└ ожидание %s", retryAfter))
	} else {
		lines = append(lines, localizedTextTUI(language, "└ retrying now", "└ повтор сейчас"))
	}
	if err != nil {
		lines = append(lines, "  "+strings.TrimSpace(err.Error()))
	}
	return TranscriptEntry{Role: "tool", Body: strings.Join(lines, "\n")}
}

func formatToolCallPreview(language commandcatalog.CatalogLanguage, name string, raw json.RawMessage, cwd string) (string, string) {
	name = strings.TrimSpace(name)
	switch name {
	case "run_shell_command":
		var args struct {
			Cmd       string `json:"cmd"`
			Cwd       string `json:"cwd"`
			ProcessID string `json:"process_id"`
		}
		if json.Unmarshal(raw, &args) == nil {
			if strings.TrimSpace(args.ProcessID) != "" {
				return localizedTextTUI(language, "poll process %s", "опрос процесса %s", strings.TrimSpace(args.ProcessID)), ""
			}
			summary := strings.TrimSpace(args.Cmd)
			if summary == "" {
				summary = name
			}
			cwd = firstNonEmpty(strings.TrimSpace(args.Cwd), strings.TrimSpace(cwd))
			if cwd != "" {
				return summary, "cwd=" + cwd
			}
			return summary, ""
		}
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "read %s", "читать %s", strings.TrimSpace(args.Path)), ""
		}
	case "list_directory":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "list %s", "список %s", firstNonEmpty(strings.TrimSpace(args.Path), ".")), ""
		}
	case "search_text":
		var args struct {
			Query string `json:"query"`
			Path  string `json:"path"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "search %q", "поиск %q", strings.TrimSpace(args.Query)), "path=" + firstNonEmpty(strings.TrimSpace(args.Path), ".")
		}
	case "write_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "write %s", "запись %s", strings.TrimSpace(args.Path)), ""
		}
	case "apply_patch":
		return localizedTextTUI(language, "apply patch", "применить патч"), ""
	case "request_permissions":
		return localizedTextTUI(language, "request permissions", "запросить права"), ""
	case "spawn_worker":
		var args struct {
			Prompt    string `json:"prompt"`
			Cwd       string `json:"cwd"`
			Model     string `json:"model"`
			Provider  string `json:"provider"`
			Profile   string `json:"profile"`
			Reasoning string `json:"reasoning"`
		}
		if json.Unmarshal(raw, &args) == nil {
			summary := truncateToolPreview(strings.TrimSpace(args.Prompt), 96)
			if summary == "" {
				summary = localizedTextTUI(language, "spawn worker", "запустить воркер")
			}
			details := make([]string, 0, 4)
			if cwd := strings.TrimSpace(args.Cwd); cwd != "" {
				details = append(details, "cwd="+cwd)
			}
			if model := strings.TrimSpace(args.Model); model != "" {
				details = append(details, "model="+model)
			}
			if provider := strings.TrimSpace(args.Provider); provider != "" {
				details = append(details, "provider="+provider)
			}
			return localizedTextTUI(language, "worker: %s", "воркер: %s", summary), strings.Join(details, " ")
		}
	case "list_workers":
		var args struct {
			IncludeFinished bool `json:"include_finished"`
		}
		if json.Unmarshal(raw, &args) == nil {
			if args.IncludeFinished {
				return localizedTextTUI(language, "list workers", "список воркеров"), localizedTextTUI(language, "include finished", "включая завершённые")
			}
			return localizedTextTUI(language, "list workers", "список воркеров"), ""
		}
	case "wait_worker":
		var args struct {
			WorkerID       string `json:"worker_id"`
			TimeoutSeconds int    `json:"timeout_seconds"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "wait worker %s", "ожидать воркер %s", strings.TrimSpace(args.WorkerID)), localizedTextTUI(language, "timeout=%ds", "таймаут=%ds", args.TimeoutSeconds)
		}
	case "cancel_worker":
		var args struct {
			WorkerID string `json:"worker_id"`
		}
		if json.Unmarshal(raw, &args) == nil {
			return localizedTextTUI(language, "cancel worker %s", "остановить воркер %s", strings.TrimSpace(args.WorkerID)), ""
		}
	}
	arguments := strings.TrimSpace(string(raw))
	if arguments == "" {
		return name, ""
	}
	return name, truncateToolPreview(arguments, 160)
}

func renderToolOutputPreview(language commandcatalog.CatalogLanguage, name string, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	switch strings.TrimSpace(name) {
	case "run_shell_command":
		var payload shellToolOutput
		if json.Unmarshal([]byte(output), &payload) == nil {
			lines := make([]string, 0, 8)
			if payload.Cmd != "" {
				lines = append(lines, localizedTextTUI(language, "  cmd: %s", "  команда: %s", payload.Cmd))
			}
			if payload.Cwd != "" {
				lines = append(lines, "  cwd: "+payload.Cwd)
			}
			if strings.TrimSpace(payload.ProcessID) != "" {
				lines = append(lines, localizedTextTUI(language, "  process: %s", "  процесс: %s", payload.ProcessID))
			}
			if status := strings.TrimSpace(payload.Status); status != "" {
				lines = append(lines, localizedTextTUI(language, "  status: %s", "  статус: %s", status))
			}
			if payload.ExitCode != nil {
				lines = append(lines, localizedTextTUI(language, "  exit code: %d", "  код выхода: %d", *payload.ExitCode))
			}
			if payload.Output != "" {
				lines = append(lines, localizedTextTUI(language, "  output:\n%s", "  вывод:\n%s", indentBlock(truncateToolPreview(payload.Output, 4000), "    ")))
			} else {
				if payload.Stdout != "" {
					lines = append(lines, localizedTextTUI(language, "  stdout:\n%s", "  stdout:\n%s", indentBlock(truncateToolPreview(payload.Stdout, 4000), "    ")))
				}
				if payload.Stderr != "" {
					lines = append(lines, localizedTextTUI(language, "  stderr:\n%s", "  stderr:\n%s", indentBlock(truncateToolPreview(payload.Stderr, 2000), "    ")))
				}
			}
			if payload.Error != "" {
				lines = append(lines, localizedTextTUI(language, "  error: %s", "  ошибка: %s", payload.Error))
			}
			return strings.Join(lines, "\n")
		}
	case "spawn_worker", "wait_worker", "cancel_worker", "list_workers":
		var payload workerToolOutput
		if json.Unmarshal([]byte(output), &payload) == nil {
			lines := make([]string, 0, 12)
			if payload.Worker != nil {
				lines = append(lines, renderSingleWorkerPreview(language, *payload.Worker)...)
			} else if len(payload.Workers) > 0 {
				lines = append(lines, localizedTextTUI(language, "  workers:", "  воркеры:"))
				for _, worker := range payload.Workers {
					summary := worker.ID
					if prompt := strings.TrimSpace(worker.Prompt); prompt != "" {
						summary += "  " + truncateToolPreview(prompt, 96)
					}
					lines = append(lines, localizedTextTUI(language, "    - %s · %s", "    - %s · %s", summary, firstNonEmpty(strings.TrimSpace(worker.Status), localizedTextTUI(language, "unknown", "неизвестно"))))
				}
			}
			if payload.Error != "" {
				lines = append(lines, localizedTextTUI(language, "  error: %s", "  ошибка: %s", payload.Error))
			}
			if len(lines) > 0 {
				return strings.Join(lines, "\n")
			}
		}
	}

	var pretty bytes.Buffer
	if json.Valid([]byte(output)) && json.Indent(&pretty, []byte(output), "  ", "  ") == nil {
		return localizedTextTUI(language, "  output:\n%s", "  вывод:\n%s", indentBlock(truncateToolPreview(pretty.String(), 4000), "    "))
	}
	return localizedTextTUI(language, "  output:\n%s", "  вывод:\n%s", indentBlock(truncateToolPreview(output, 4000), "    "))
}

func truncateToolPreview(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 16 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-14]) + "\n...<truncated>"
}

func joinNonEmpty(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n")
}

func compactToolOutputBlock(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if index == 0 {
			lines[index] = "└ " + line
			continue
		}
		lines[index] = "  " + line
	}
	return lines
}

func compactToolPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	path = strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(path, "/")
	if len(parts) <= 3 {
		return strings.TrimSpace(path)
	}
	return strings.Join(parts[len(parts)-3:], "/")
}

func compactToolPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if compact := compactToolPath(path); compact != "" {
			out = append(out, compact)
		}
	}
	return out
}

func countPatchLines(patch string) (int, int) {
	added := 0
	removed := 0
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func renderPatchPreview(patch string) string {
	patch = strings.TrimSpace(patch)
	if patch == "" {
		return ""
	}
	lines := strings.Split(patch, "\n")
	rendered := make([]string, 0, minInt(len(lines), 24))
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		rendered = append(rendered, "    "+line)
		if len(rendered) >= 24 {
			rendered = append(rendered, "    ⋮")
			break
		}
	}
	return strings.Join(rendered, "\n")
}

func indentBlock(value string, indent string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lines[index] = indent + line
	}
	return strings.Join(lines, "\n")
}

func renderSingleWorkerPreview(language commandcatalog.CatalogLanguage, worker tooling.WorkerSummary) []string {
	lines := make([]string, 0, 8)
	if id := strings.TrimSpace(worker.ID); id != "" {
		lines = append(lines, localizedTextTUI(language, "  worker: %s", "  воркер: %s", id))
	}
	if status := strings.TrimSpace(worker.Status); status != "" {
		lines = append(lines, localizedTextTUI(language, "  status: %s", "  статус: %s", status))
	}
	if cwd := strings.TrimSpace(worker.CWD); cwd != "" {
		lines = append(lines, "  cwd: "+cwd)
	}
	if model := strings.TrimSpace(worker.Model); model != "" {
		lines = append(lines, localizedTextTUI(language, "  model: %s", "  модель: %s", model))
	}
	if provider := strings.TrimSpace(worker.Provider); provider != "" {
		lines = append(lines, localizedTextTUI(language, "  provider: %s", "  провайдер: %s", provider))
	}
	if prompt := strings.TrimSpace(worker.Prompt); prompt != "" {
		lines = append(lines, localizedTextTUI(language, "  prompt: %s", "  запрос: %s", truncateToolPreview(prompt, 160)))
	}
	if output := strings.TrimSpace(worker.Output); output != "" {
		lines = append(lines, localizedTextTUI(language, "  output:\n%s", "  вывод:\n%s", indentBlock(truncateToolPreview(output, 3000), "    ")))
	}
	if errText := strings.TrimSpace(worker.Error); errText != "" {
		lines = append(lines, localizedTextTUI(language, "  error: %s", "  ошибка: %s", errText))
	}
	return lines
}
