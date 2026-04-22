package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type shellToolOutput struct {
	Tool            string `json:"tool"`
	Cmd             string `json:"cmd"`
	Cwd             string `json:"cwd"`
	Output          string `json:"output"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        *int   `json:"exit_code"`
	ProcessID       string `json:"process_id"`
	Status          string `json:"status"`
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
	lines := []string{
		localizedTextTUI(language, "Tool result", "Результат инструмента"),
		localizedTextTUI(language, "└ %s · %s", "└ %s · %s", result.Name, localizedToolStatusTUI(language, string(result.Status))),
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
