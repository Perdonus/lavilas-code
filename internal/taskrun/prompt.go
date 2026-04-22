package taskrun

import (
	"fmt"
	"os"
	goRuntime "runtime"
	"path/filepath"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

const (
	maxAgentsDocumentBytes  = 16 * 1024
	maxAgentsAggregateBytes = 64 * 1024
)

const defaultSystemPrompt = `You are Go Lavilas, based on the current Lavilas/Codex client. You are running as a coding agent in a terminal on the user's computer.

General:
- Prefer concrete, verifiable actions over generic advice.
- When tools are available, inspect the repository, current directory, configuration, operating system, and relevant files before making strong claims.
- Prefer using rg or rg --files for search when those tools exist.
- Keep moving until the requested implementation or investigation is actually carried through, not merely described.

Editing constraints:
- Default to ASCII when editing or creating files unless the file already requires Unicode.
- Add brief comments only when they materially help explain non-obvious code.
- Prefer focused patches over broad rewrites unless the task clearly requires a rewrite.
- You may be working inside a dirty git tree. Never revert or discard user changes you did not make.
- Never use destructive git commands such as git reset --hard or git checkout -- unless the user explicitly asks for them.
- If you notice unexpected changes that you did not make, stop and surface that conflict clearly.

Working style:
- Before editing code, inspect the exact target files and understand the relevant project structure.
- After a meaningful edit, re-read the affected files and verify the result with the most direct command or test available.
- Re-check assumptions before proposing edits or commands.
- Do not invent tool output, file contents, environment facts, test results, or API behavior.
- If a capability is unavailable, say that clearly and continue with the best precise fallback.
- For long-running shell work, prefer background execution through run_shell_command with yield_time_ms and then poll the same process_id instead of blocking the turn.
- For parallel sub-tasks, use spawn_worker, then collect or manage results with list_workers, wait_worker, and cancel_worker.
- If a change needs extra write access, request it explicitly with request_permissions instead of guessing.

Frontend tasks:
- Avoid generic, average-looking UI output.
- Keep the existing design language when the project already has one.
- Aim for intentional layouts, strong typography, and restrained but meaningful motion.

Response style:
- Keep answers concise, technical, and execution-focused.
- Surface risks, regressions, and missing verification steps explicitly.
- Do not stop at guesses when the repository or environment can be inspected first.`

const agentsHierarchyPromptPrelude = `AGENTS.md policy:
- Files named AGENTS.md can appear anywhere in the container.
- Each AGENTS.md governs the directory that contains it and every child directory beneath it.
- When multiple AGENTS.md files apply, the deeper file overrides the higher file.
- System, developer, and user instructions override AGENTS.md instructions.`

type agentsDocument struct {
	Path string
	Body string
}

func resolveSystemPrompt(value string, cwd string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultSystemPrompt
	}
	contextBlock := buildRuntimePromptContext(cwd)
	if contextBlock == "" {
		return value
	}
	return strings.TrimSpace(value) + "\n\n" + contextBlock
}

func systemPromptFromMessages(messages []runtime.Message) string {
	if len(messages) == 0 {
		return ""
	}
	blocks := make([]string, 0, 2)
	for _, message := range messages {
		if message.Role != runtime.RoleSystem {
			continue
		}
		if body := strings.TrimSpace(message.Text()); body != "" {
			blocks = append(blocks, body)
		}
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n"))
}

func buildRuntimePromptContext(cwd string) string {
	cwd = normalizePromptPath(cwd)
	lines := []string{"Runtime context:"}
	if cwd != "" {
		lines = append(lines, "- cwd: "+cwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = normalizePromptPath(home)
		if home != "" {
			lines = append(lines, "- home: "+home)
		}
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		lines = append(lines, "- shell: "+shell)
	}
	lines = append(lines, fmt.Sprintf("- platform: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))

	agentsBlock := renderAgentsDocuments(cwd)
	if agentsBlock != "" {
		lines = append(lines, "", agentsHierarchyPromptPrelude, "", agentsBlock)
	}
	return strings.Join(lines, "\n")
}

func renderAgentsDocuments(cwd string) string {
	documents := collectAgentsDocuments(cwd)
	if len(documents) == 0 {
		return ""
	}
	var builder strings.Builder
	for index, document := range documents {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("# AGENTS.md instructions for ")
		builder.WriteString(document.Path)
		builder.WriteString("\n\n")
		builder.WriteString(document.Body)
	}
	return strings.TrimSpace(builder.String())
}

func collectAgentsDocuments(cwd string) []agentsDocument {
	cwd = normalizePromptPath(cwd)
	if cwd == "" {
		return nil
	}
	dirs := ancestorPromptDirs(cwd)
	if len(dirs) == 0 {
		return nil
	}
	documents := make([]agentsDocument, 0, len(dirs))
	total := 0
	for _, dir := range dirs {
		path := filepath.Join(dir, "AGENTS.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body, used := truncatePromptText(string(data), maxAgentsDocumentBytes)
		if body == "" {
			continue
		}
		if total+used > maxAgentsAggregateBytes {
			remaining := maxAgentsAggregateBytes - total
			if remaining <= 0 {
				break
			}
			body, used = truncatePromptText(body, remaining)
			if body == "" {
				break
			}
		}
		documents = append(documents, agentsDocument{
			Path: normalizePromptPath(path),
			Body: body,
		})
		total += used
		if total >= maxAgentsAggregateBytes {
			break
		}
	}
	return documents
}

func ancestorPromptDirs(path string) []string {
	path = normalizePromptPath(path)
	if path == "" {
		return nil
	}
	result := []string{path}
	for {
		parent := normalizePromptPath(filepath.Dir(path))
		if parent == "" || parent == path {
			break
		}
		path = parent
		result = append(result, path)
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func normalizePromptPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func truncatePromptText(value string, limit int) (string, int) {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return "", 0
	}
	if len(value) <= limit {
		return value, len(value)
	}
	if limit <= 32 {
		return value[:limit], limit
	}
	suffix := "\n\n[truncated]"
	limitWithoutSuffix := limit - len(suffix)
	if limitWithoutSuffix <= 0 {
		return value[:limit], limit
	}
	return strings.TrimRight(value[:limitWithoutSuffix], "\n\r\t ") + suffix, limit
}
