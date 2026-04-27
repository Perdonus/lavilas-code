package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Perdonus/lavilas-code/internal/updater"
)

type updateDecision int

const (
	updateDecisionLater updateDecision = iota
	updateDecisionInstall
	updateDecisionExit
)

type updatePromptModel struct {
	result   updater.CheckResult
	selected int
	width    int
	height   int
	decision updateDecision
}

func maybeRunUpdateGate() (bool, int) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("LAVILAS_SKIP_UPDATE_CHECK")), "1") {
		return true, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	result, err := updater.Check(ctx)
	cancel()
	if err != nil || !result.Available {
		return true, 0
	}
	decision, err := runUpdatePrompt(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update prompt: %v\n", err)
		return true, 0
	}
	switch decision {
	case updateDecisionInstall:
		fmt.Printf("\nОбновляю Go Lavilas через NV: %s -> %s\n\n", result.CurrentVersion, result.LatestVersion)
		installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err := updater.Install(installCtx, result.NVPath, result.PackageSpec)
		installCancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
			return false, 1
		}
		fmt.Printf("\nОбновление установлено: %s. Запусти lvls снова.\n", result.LatestVersion)
		return false, 0
	case updateDecisionExit:
		return false, 130
	default:
		return true, 0
	}
}

func runUpdatePrompt(result updater.CheckResult) (updateDecision, error) {
	model := updatePromptModel{result: result}
	program := tea.NewProgram(model)
	finalModel, err := program.Run()
	if err != nil {
		return updateDecisionLater, err
	}
	if final, ok := finalModel.(updatePromptModel); ok {
		return final.decision, nil
	}
	return updateDecisionLater, nil
}

func (m updatePromptModel) Init() tea.Cmd { return nil }

func (m updatePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch strings.ToLower(strings.TrimSpace(msg.String())) {
		case "ctrl+c":
			m.decision = updateDecisionExit
			return m, tea.Quit
		case "esc":
			m.decision = updateDecisionLater
			return m, tea.Quit
		case "up", "k":
			m.selected = 0
			return m, nil
		case "down", "j", "pgdown":
			m.selected = 1
			return m, nil
		case "enter":
			if m.selected == 0 {
				m.decision = updateDecisionInstall
			} else {
				m.decision = updateDecisionLater
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m updatePromptModel) View() string {
	width := maxInt(42, m.width-4)
	if m.width <= 0 {
		width = 78
	}
	boxWidth := minInt(maxInt(50, width), 86)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f43f5e"))
	oldStyle := lipgloss.NewStyle().Strikethrough(true).Foreground(lipgloss.Color("#8b949e"))
	newStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38bdf8"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))
	button := lipgloss.NewStyle().Padding(0, 2)
	selectedButton := button.Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#ffffff"))

	updateButton := button.Render("обновить версию")
	laterButton := button.Render("позже")
	if m.selected == 0 {
		updateButton = selectedButton.Render("обновить версию")
	} else {
		laterButton = selectedButton.Render("позже")
	}

	versions := padBetween(oldStyle.Render(m.result.CurrentVersion), newStyle.Render(m.result.LatestVersion), boxWidth-4)
	spacerLines := maxInt(8, minInt(14, m.height/2))
	if m.height <= 0 {
		spacerLines = 10
	}
	lines := []string{
		titleStyle.Render("ВЫШЛО ОБНОВЛЕНИЕ!!!"),
		"",
		versions,
		"",
		updateButton,
		muted.Render("↓ ниже есть вариант позже"),
		strings.Repeat("\n", spacerLines),
		laterButton,
		"",
		muted.Render("enter — выбрать, ↑/↓ — переключить, esc — позже, ctrl+c — выйти"),
	}
	pane := lipgloss.NewStyle().Width(boxWidth).Padding(1, 2)
	return pane.Render(strings.Join(lines, "\n"))
}
