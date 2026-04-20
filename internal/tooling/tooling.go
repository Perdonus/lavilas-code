package tooling

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	toolruntime "github.com/Perdonus/lavilas-code/internal/runtime"
)

const (
	maxToolOutputChars  = 32 * 1024
	maxReadBytes        = 128 * 1024
	maxListEntries      = 256
	maxSearchResults    = 128
	defaultShellTimeout = 20 * time.Second
	maxShellTimeout     = 2 * time.Minute
)

type shellArgs struct {
	Cmd            string `json:"cmd"`
	Cwd            string `json:"cwd,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type listArgs struct {
	Path string `json:"path,omitempty"`
}

type readArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type searchArgs struct {
	Path          string `json:"path,omitempty"`
	Query         string `json:"query"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	MaxResults    int    `json:"max_results,omitempty"`
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type patchArgs struct {
	Patch     string `json:"patch"`
	CheckOnly bool   `json:"check_only,omitempty"`
}

func Definitions() []toolruntime.ToolDefinition {
	return []toolruntime.ToolDefinition{
		functionTool(
			"run_shell_command",
			"Run a shell command in the current environment. Use this to inspect the repository, current directory, operating system, disk, git state, or to perform edits through standard CLI tools.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cmd":             map[string]any{"type": "string", "description": "Shell command to execute."},
					"cwd":             map[string]any{"type": "string", "description": "Optional working directory. Defaults to the current directory."},
					"timeout_seconds": map[string]any{"type": "integer", "description": "Optional timeout in seconds. Defaults to 20, max 120."},
				},
				"required": []string{"cmd"},
			},
		),
		functionTool(
			"list_directory",
			"List directory entries with type and size. Use this before making assumptions about the project layout.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Directory path. Defaults to the current directory."},
				},
			},
		),
		functionTool(
			"read_file",
			"Read a file from disk, optionally restricted to a line range.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string", "description": "File path to read."},
					"start_line": map[string]any{"type": "integer", "description": "Optional 1-based start line."},
					"end_line":   map[string]any{"type": "integer", "description": "Optional 1-based end line, inclusive."},
				},
				"required": []string{"path"},
			},
		),
		functionTool(
			"search_text",
			"Search recursively for text inside files under a directory.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":           map[string]any{"type": "string", "description": "Search root. Defaults to the current directory."},
					"query":          map[string]any{"type": "string", "description": "Text to search for."},
					"case_sensitive": map[string]any{"type": "boolean", "description": "Whether the search is case-sensitive."},
					"max_results":    map[string]any{"type": "integer", "description": "Optional max number of matches. Defaults to 128."},
				},
				"required": []string{"query"},
			},
		),
		functionTool(
			"write_file",
			"Create or replace a file with the provided content. Use this for deliberate file edits when a full rewrite is acceptable.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "File path to write."},
					"content": map[string]any{"type": "string", "description": "Full file content."},
				},
				"required": []string{"path", "content"},
			},
		),
		functionTool(
			"apply_patch",
			"Apply a git-style patch through `git apply`. Use this for smaller surgical file edits.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch":      map[string]any{"type": "string", "description": "Patch content to apply."},
					"check_only": map[string]any{"type": "boolean", "description": "If true, validate the patch without applying it."},
				},
				"required": []string{"patch"},
			},
		),
		functionTool(
			"request_permissions",
			"Ask the user to grant additional writable roots for later write operations. The approval choice decides whether the grant lasts for the current turn or the whole session.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{"type": "string", "description": "Why the extra write access is needed."},
					"permissions": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"writable_roots": map[string]any{
								"type":        "array",
								"description": "Directories or file paths that should be writable after approval.",
								"items":       map[string]any{"type": "string"},
								"minItems":    1,
							},
						},
						"required": []string{"writable_roots"},
					},
				},
				"required": []string{"permissions"},
			},
		),
	}
}

func ExecuteCalls(ctx context.Context, calls []toolruntime.ToolCall) []toolruntime.Message {
	return ExecuteCallsWithPolicy(ctx, calls, DefaultToolPolicy())
}

func ExecuteCallsWithPolicy(ctx context.Context, calls []toolruntime.ToolCall, policy ToolPolicy) []toolruntime.Message {
	if len(calls) == 0 {
		return nil
	}
	report := ExecutePlan(ctx, BuildExecutionPlanWithToolPolicy(calls, policy))
	return report.Messages()
}

func ExecuteCall(ctx context.Context, call toolruntime.ToolCall) toolruntime.Message {
	return ExecuteCallWithPolicy(ctx, call, DefaultToolPolicy())
}

func ExecuteCallWithPolicy(ctx context.Context, call toolruntime.ToolCall, policy ToolPolicy) toolruntime.Message {
	report := ExecutePlan(ctx, BuildExecutionPlanWithToolPolicy([]toolruntime.ToolCall{call}, policy))
	if len(report.Results) == 0 {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = strings.TrimSpace(call.Function.Name)
		}
		return toolruntime.Message{
			Role:       toolruntime.RoleTool,
			ToolCallID: callID,
			Content:    []toolruntime.ContentPart{toolruntime.TextPart(marshalResult(map[string]any{"ok": false, "error": "tool execution produced no result"}))},
		}
	}
	return report.Results[0].Message()
}

func functionTool(name string, description string, parameters map[string]any) toolruntime.ToolDefinition {
	return toolruntime.ToolDefinition{
		Type: toolruntime.ToolTypeFunction,
		Function: toolruntime.FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			Strict:      true,
		},
	}
}

func dispatch(ctx context.Context, name string, arguments []byte) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return marshalResult(map[string]any{"ok": false, "error": "tool name is empty"})
	}

	switch name {
	case "run_shell_command":
		var args shellArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return runShellCommand(ctx, args)
	case "list_directory":
		var args listArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return listDirectory(args)
	case "read_file":
		var args readArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return readFile(args)
	case "search_text":
		var args searchArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return searchText(args)
	case "write_file":
		var args writeArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return writeFile(args)
	case "apply_patch":
		var args patchArgs
		if err := decodeArgs(arguments, &args); err != nil {
			return marshalResult(map[string]any{"ok": false, "tool": name, "error": err.Error()})
		}
		return applyPatch(ctx, args)
	default:
		return marshalResult(map[string]any{"ok": false, "tool": name, "error": "unknown tool"})
	}
}

func decodeArgs(data []byte, target any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("tool arguments are empty")
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func runShellCommand(ctx context.Context, args shellArgs) string {
	commandText := strings.TrimSpace(args.Cmd)
	if commandText == "" {
		return marshalResult(map[string]any{"ok": false, "tool": "run_shell_command", "error": "cmd is required"})
	}

	cwd, err := resolvePath(args.Cwd, true)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "run_shell_command", "error": err.Error()})
	}

	timeout := defaultShellTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if timeout > maxShellTimeout {
		timeout = maxShellTimeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(commandCtx, "cmd", "/C", commandText)
	} else {
		cmd = exec.CommandContext(commandCtx, "sh", "-lc", commandText)
	}
	cmd.Dir = cwd

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}

	stdoutText, stdoutTruncated := clampString(stdout.String(), maxToolOutputChars)
	stderrText, stderrTruncated := clampString(stderr.String(), maxToolOutputChars)
	payload := map[string]any{
		"ok":               err == nil,
		"tool":             "run_shell_command",
		"cmd":              commandText,
		"cwd":              cwd,
		"exit_code":        exitCode,
		"stdout":           stdoutText,
		"stderr":           stderrText,
		"stdout_truncated": stdoutTruncated,
		"stderr_truncated": stderrTruncated,
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		payload["error"] = fmt.Sprintf("command timed out after %s", timeout)
	} else if err != nil && exitCode == -1 {
		payload["error"] = err.Error()
	}
	return marshalResult(payload)
}

func listDirectory(args listArgs) string {
	path, err := resolvePath(args.Path, true)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "list_directory", "error": err.Error()})
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "list_directory", "path": path, "error": err.Error()})
	}

	type entryView struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Size int64  `json:"size,omitempty"`
	}
	items := make([]entryView, 0, minInt(len(entries), maxListEntries))
	for index, entry := range entries {
		if index >= maxListEntries {
			break
		}
		item := entryView{Name: entry.Name(), Type: "file"}
		if entry.IsDir() {
			item.Type = "dir"
		} else if info, infoErr := entry.Info(); infoErr == nil {
			item.Size = info.Size()
			if info.Mode()&fs.ModeSymlink != 0 {
				item.Type = "symlink"
			}
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Type == items[j].Type {
			return items[i].Name < items[j].Name
		}
		return items[i].Type < items[j].Type
	})

	return marshalResult(map[string]any{
		"ok":        true,
		"tool":      "list_directory",
		"path":      path,
		"entries":   items,
		"truncated": len(entries) > len(items),
	})
}

func readFile(args readArgs) string {
	path, err := resolvePath(args.Path, false)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "read_file", "error": err.Error()})
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "read_file", "path": path, "error": err.Error()})
	}
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
	}

	content := string(data)
	startLine := args.StartLine
	endLine := args.EndLine
	if startLine > 0 || endLine > 0 {
		content = sliceLines(content, startLine, endLine)
	}
	content, truncated := clampString(content, maxToolOutputChars)

	return marshalResult(map[string]any{
		"ok":         true,
		"tool":       "read_file",
		"path":       path,
		"start_line": startLine,
		"end_line":   endLine,
		"content":    content,
		"truncated":  truncated,
	})
}

func searchText(args searchArgs) string {
	if strings.TrimSpace(args.Query) == "" {
		return marshalResult(map[string]any{"ok": false, "tool": "search_text", "error": "query is required"})
	}
	path, err := resolvePath(args.Path, true)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "search_text", "error": err.Error()})
	}
	maxResults := args.MaxResults
	if maxResults <= 0 || maxResults > maxSearchResults {
		maxResults = maxSearchResults
	}

	needle := args.Query
	matchNeedle := needle
	if !args.CaseSensitive {
		matchNeedle = strings.ToLower(needle)
	}

	type matchView struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}

	results := make([]matchView, 0, maxResults)
	truncated := false
	walkErr := filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == ".codex" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(results) >= maxResults {
			truncated = true
			return ioEOFStop{}
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > maxReadBytes {
			return nil
		}
		file, openErr := os.Open(current)
		if openErr != nil {
			return nil
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), maxReadBytes)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			candidate := line
			if !args.CaseSensitive {
				candidate = strings.ToLower(candidate)
			}
			if strings.Contains(candidate, matchNeedle) {
				preview, _ := clampString(line, 400)
				results = append(results, matchView{Path: current, Line: lineNo, Text: preview})
				if len(results) >= maxResults {
					truncated = true
					return ioEOFStop{}
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		if _, ok := walkErr.(ioEOFStop); !ok {
			return marshalResult(map[string]any{"ok": false, "tool": "search_text", "path": path, "error": walkErr.Error()})
		}
	}

	return marshalResult(map[string]any{
		"ok":             true,
		"tool":           "search_text",
		"path":           path,
		"query":          needle,
		"case_sensitive": args.CaseSensitive,
		"matches":        results,
		"truncated":      truncated,
	})
}

func writeFile(args writeArgs) string {
	path, err := resolvePath(args.Path, false)
	if err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "write_file", "error": err.Error()})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "write_file", "path": path, "error": err.Error()})
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return marshalResult(map[string]any{"ok": false, "tool": "write_file", "path": path, "error": err.Error()})
	}
	return marshalResult(map[string]any{
		"ok":    true,
		"tool":  "write_file",
		"path":  path,
		"bytes": len(args.Content),
	})
}

func applyPatch(ctx context.Context, args patchArgs) string {
	patchText := strings.TrimSpace(args.Patch)
	if patchText == "" {
		return marshalResult(map[string]any{"ok": false, "tool": "apply_patch", "error": "patch is required"})
	}
	commandCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()
	commandArgs := []string{"apply", "--whitespace=nowarn"}
	if args.CheckOnly {
		commandArgs = append(commandArgs, "--check")
	}
	commandArgs = append(commandArgs, "-")
	cmd := exec.CommandContext(commandCtx, "git", commandArgs...)
	cmd.Stdin = strings.NewReader(patchText)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	stdoutText, stdoutTruncated := clampString(stdout.String(), maxToolOutputChars)
	stderrText, stderrTruncated := clampString(stderr.String(), maxToolOutputChars)
	payload := map[string]any{
		"ok":               err == nil,
		"tool":             "apply_patch",
		"check_only":       args.CheckOnly,
		"stdout":           stdoutText,
		"stderr":           stderrText,
		"stdout_truncated": stdoutTruncated,
		"stderr_truncated": stderrTruncated,
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	return marshalResult(payload)
}

func patchTouchedPaths(patchText string) []string {
	lines := strings.Split(patchText, "\n")
	dedupe := make(map[string]struct{})
	paths := make([]string, 0, 4)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || value == "/dev/null" {
			return
		}
		value = strings.Trim(value, "\"")
		if strings.HasPrefix(value, "a/") || strings.HasPrefix(value, "b/") {
			value = value[2:]
		}
		value = normalizeResourcePath(value, ".")
		if _, ok := dedupe[value]; ok {
			return
		}
		dedupe[value] = struct{}{}
		paths = append(paths, value)
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
		case strings.HasPrefix(line, "rename to "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "rename to ")))
		case strings.HasPrefix(line, "*** Add File: "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: ")))
		case strings.HasPrefix(line, "*** Update File: "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: ")))
		case strings.HasPrefix(line, "*** Delete File: "):
			add(strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: ")))
		}
	}
	sort.Strings(paths)
	return paths
}

func resolvePath(path string, allowDir bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		if os.IsNotExist(statErr) && !allowDir {
			return resolved, nil
		}
		if os.IsNotExist(statErr) && allowDir {
			return "", statErr
		}
		return "", statErr
	}
	if allowDir && !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	if !allowDir && info.IsDir() {
		return "", fmt.Errorf("%s is a directory", resolved)
	}
	return resolved, nil
}

func sliceLines(content string, startLine int, endLine int) string {
	lines := strings.Split(content, "\n")
	start := 1
	if startLine > 0 {
		start = startLine
	}
	end := len(lines)
	if endLine > 0 && endLine < end {
		end = endLine
	}
	if start > end || start > len(lines) {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func clampString(value string, limit int) (string, bool) {
	if limit <= 0 || len(value) <= limit {
		return value, false
	}
	trimmed := value[:limit]
	return trimmed + "\n...<truncated>", true
}

func marshalResult(payload map[string]any) string {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return string(data)
}

type ioEOFStop struct{}

func (ioEOFStop) Error() string { return "stop" }

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
