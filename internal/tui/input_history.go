package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
)

const inputHistoryFilename = "history.jsonl"

type inputHistoryRecord struct {
	SessionID string `json:"session_id"`
	TS        uint64 `json:"ts"`
	Text      string `json:"text"`
}

type inputHistory struct {
	path         string
	persistent   []string
	local        []string
	cursor       int
	lastRecalled string
}

func newInputHistory(layout apphome.Layout) *inputHistory {
	history := &inputHistory{
		path:   filepath.Join(layout.CodexHome(), inputHistoryFilename),
		cursor: -1,
	}
	history.load()
	return history
}

func (h *inputHistory) load() {
	if h == nil || strings.TrimSpace(h.path) == "" {
		return
	}
	file, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry inputHistoryRecord
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		h.persistent = appendHistoryText(h.persistent, entry.Text)
	}
}

func (h *inputHistory) shouldHandleNavigation(text string, cursor int) bool {
	if h == nil || h.totalEntries() == 0 {
		return false
	}
	if strings.TrimSpace(text) == "" {
		return true
	}
	runeCount := len([]rune(text))
	if cursor != 0 && cursor != runeCount {
		return false
	}
	return h.lastRecalled == text
}

func (h *inputHistory) navigateUp() (string, bool) {
	if h == nil {
		return "", false
	}
	total := h.totalEntries()
	if total == 0 {
		return "", false
	}
	switch {
	case h.cursor < 0:
		h.cursor = total - 1
	case h.cursor == 0:
		return "", false
	default:
		h.cursor--
	}
	entry, ok := h.entryAt(h.cursor)
	if !ok {
		return "", false
	}
	h.lastRecalled = entry
	return entry, true
}

func (h *inputHistory) navigateDown() (string, bool) {
	if h == nil {
		return "", false
	}
	total := h.totalEntries()
	if total == 0 || h.cursor < 0 {
		return "", false
	}
	if h.cursor+1 >= total {
		h.cursor = -1
		h.lastRecalled = ""
		return "", true
	}
	h.cursor++
	entry, ok := h.entryAt(h.cursor)
	if !ok {
		return "", false
	}
	h.lastRecalled = entry
	return entry, true
}

func (h *inputHistory) resetNavigation() {
	if h == nil {
		return
	}
	h.cursor = -1
	h.lastRecalled = ""
}

func (h *inputHistory) syncDraft(draft string) {
	if h == nil {
		return
	}
	if h.lastRecalled == "" {
		return
	}
	if draft != h.lastRecalled {
		h.resetNavigation()
	}
}

func (h *inputHistory) record(text string, sessionID string) error {
	if h == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if last, ok := h.lastEntry(); ok && last == text {
		h.resetNavigation()
		return nil
	}
	h.local = appendHistoryText(h.local, text)
	h.resetNavigation()
	return h.appendToDisk(text, sessionID)
}

func (h *inputHistory) appendToDisk(text string, sessionID string) error {
	if strings.TrimSpace(h.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	record := inputHistoryRecord{
		SessionID: firstNonEmpty(strings.TrimSpace(sessionID), "standalone"),
		TS:        uint64(time.Now().Unix()),
		Text:      text,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = file.Write(line)
	return err
}

func (h *inputHistory) totalEntries() int {
	if h == nil {
		return 0
	}
	return len(h.persistent) + len(h.local)
}

func (h *inputHistory) entryAt(index int) (string, bool) {
	if h == nil || index < 0 || index >= h.totalEntries() {
		return "", false
	}
	if index < len(h.persistent) {
		return h.persistent[index], true
	}
	localIndex := index - len(h.persistent)
	if localIndex < 0 || localIndex >= len(h.local) {
		return "", false
	}
	return h.local[localIndex], true
}

func (h *inputHistory) lastEntry() (string, bool) {
	if h == nil {
		return "", false
	}
	if len(h.local) > 0 {
		return h.local[len(h.local)-1], true
	}
	if len(h.persistent) > 0 {
		return h.persistent[len(h.persistent)-1], true
	}
	return "", false
}

func appendHistoryText(entries []string, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return entries
	}
	if len(entries) > 0 && entries[len(entries)-1] == text {
		return entries
	}
	return append(entries, text)
}
