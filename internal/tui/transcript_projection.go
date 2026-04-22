package tui

import (
	"fmt"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	runtimeapi "github.com/Perdonus/lavilas-code/internal/runtime"
)

func visibleTranscriptFromMessages(messages []runtimeapi.Message, language commandcatalog.CatalogLanguage) []TranscriptEntry {
	if len(messages) == 0 {
		return nil
	}
	entries := make([]TranscriptEntry, 0, len(messages))
	for _, message := range messages {
		if entry, ok := visibleTranscriptEntry(message, language); ok {
			entries = appendTranscriptEntryDedup(entries, entry)
		}
	}
	return entries
}

func transcriptFromMessages(messages []runtimeapi.Message, language commandcatalog.CatalogLanguage) []TranscriptEntry {
	return visibleTranscriptFromMessages(messages, language)
}

func visibleTranscriptBody(message runtimeapi.Message) string {
	switch message.Role {
	case runtimeapi.RoleSystem, runtimeapi.RoleTool:
		return ""
	}

	body := normalizeTranscriptBody(message.Text())
	if body == "" {
		body = normalizeTranscriptBody(message.Refusal)
	}
	return body
}

func visibleTranscriptEntry(message runtimeapi.Message, language commandcatalog.CatalogLanguage) (TranscriptEntry, bool) {
	if message.Role == runtimeapi.RoleTool {
		if body := visibleToolTranscriptBody(message, language); body != "" {
			return TranscriptEntry{Role: "tool", Body: body}, true
		}
		return TranscriptEntry{}, false
	}
	body := visibleTranscriptBody(message)
	if body == "" || isHiddenRuntimeTranscript(body) {
		return TranscriptEntry{}, false
	}
	return TranscriptEntry{Role: string(message.Role), Body: body}, true
}

func visibleToolTranscriptBody(message runtimeapi.Message, language commandcatalog.CatalogLanguage) string {
	raw := normalizeTranscriptBody(message.Text())
	if raw == "" {
		return ""
	}
	return renderPlanUpdateBody(language, raw)
}

func visibleToolCallBody(call runtimeapi.ToolCall, language commandcatalog.CatalogLanguage) string {
	name := strings.TrimSpace(call.Function.Name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf(
		"%s %s",
		localizedTextTUI(language, "Tool", "Инструмент"),
		name,
	)
}

func visibleLiveTurnNotes(live *LiveTurnState, language commandcatalog.CatalogLanguage) []string {
	if live == nil {
		return nil
	}
	notes := make([]string, 0, len(live.Notes))
	for _, note := range live.Notes {
		notes = appendUniqueNote(notes, note)
	}
	return notes
}

func appendTranscriptEntry(entries []TranscriptEntry, role string, body string) []TranscriptEntry {
	return appendTranscriptEntryDedup(entries, TranscriptEntry{Role: role, Body: body})
}

func appendTranscriptEntryDedup(entries []TranscriptEntry, entry TranscriptEntry) []TranscriptEntry {
	entry.Role = strings.TrimSpace(strings.ToLower(entry.Role))
	entry.Body = normalizeTranscriptBody(entry.Body)
	if entry.Body == "" {
		return entries
	}
	if len(entries) > 0 {
		last := entries[len(entries)-1]
		if strings.EqualFold(strings.TrimSpace(last.Role), entry.Role) && normalizeTranscriptBody(last.Body) == entry.Body {
			return entries
		}
	}
	return append(entries, entry)
}

func appendUniqueNote(lines []string, line string) []string {
	line = normalizeTranscriptBody(line)
	if line == "" {
		return lines
	}
	if len(lines) > 0 && normalizeTranscriptBody(lines[len(lines)-1]) == line {
		return lines
	}
	return append(lines, line)
}

func normalizeTranscriptBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimSpace(body)
}

func isHiddenRuntimeTranscript(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	if lower == "" {
		return false
	}
	markers := []string{
		"you are go lavilas, based on the current lavilas/codex client.",
		"working style:",
		"frontend tasks:",
		"response style:",
		"runtime context:",
		"# agents.md instructions for ",
		"general:\n- prefer concrete, verifiable actions",
		"editing constraints:",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
