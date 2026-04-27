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
	result     updater.CheckResult
	selected   int
	width      int
	height     int
	decision   updateDecision
	installing bool
	spinner    int
	installErr error
}

type updateInstallFinishedMsg struct {
	Result updater.InstallResult
	Err    error
}

type updateSpinnerMsg struct{}

var updateSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func RunUpdateGate() (bool, int) {
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
		if m.installing {
			return m, nil
		}
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
				m.installing = true
				m.installErr = nil
				return m, tea.Batch(updateSpinnerCmd(), installUpdateCmd(m.result))
			}
			m.decision = updateDecisionLater
			return m, tea.Quit
		}
	case updateSpinnerMsg:
		if !m.installing {
			return m, nil
		}
		m.spinner++
		return m, updateSpinnerCmd()
	case updateInstallFinishedMsg:
		m.installing = false
		if msg.Err != nil {
			m.installErr = msg.Err
			return m, nil
		}
		m.decision = updateDecisionInstall
		return m, tea.Quit
	}
	return m, nil
}

func (m updatePromptModel) View() string {
	boxWidth := 58
	if m.width > 0 {
		boxWidth = minInt(maxInt(42, m.width-4), 72)
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f43f5e"))
	progressStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38bdf8"))
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

	versions := oldStyle.Render(m.result.CurrentVersion) + muted.Render("  ->  ") + newStyle.Render(m.result.LatestVersion)
	if m.installing {
		frame := "⠋"
		if len(updateSpinnerFrames) > 0 {
			frame = updateSpinnerFrames[m.spinner%len(updateSpinnerFrames)]
		}
		lines := []string{
			progressStyle.Render("ОБНОВЛЕНИЕ УСТАНАВЛИВАЕТСЯ"),
			"",
			versions,
			"",
			progressStyle.Render(frame+" NV ставит новую версию. Терминал не закрывай."),
			muted.Render("После замены запусти lvls снова."),
		}
		return updatePromptBox(boxWidth).Render(strings.Join(lines, "\n")) + "\n"
	}
	lines := []string{
		titleStyle.Render("ВЫШЛО ОБНОВЛЕНИЕ!!!"),
		"",
		versions,
		"",
		updateButton,
		laterButton,
	}
	if m.installErr != nil {
		lines = append(lines, "", titleStyle.Render("Ошибка обновления: "+m.installErr.Error()))
	}
	lines = append(lines, "", muted.Render("enter — выбрать, ↑/↓ — переключить, esc — позже, ctrl+c — выйти"))
	return updatePromptBox(boxWidth).Render(strings.Join(lines, "\n")) + "\n"
}

func updatePromptBox(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#38bdf8"))
}

func updateSpinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return updateSpinnerMsg{}
	})
}

func installUpdateCmd(result updater.CheckResult) tea.Cmd {
	return func() tea.Msg {
		installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer installCancel()
		installResult, err := updater.InstallOrSchedule(installCtx, result.NVPath, result.PackageSpec)
		return updateInstallFinishedMsg{Result: installResult, Err: err}
	}
}
