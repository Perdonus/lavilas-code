package tui

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var terminalRendererConfigOnce sync.Once

func ensureTerminalRendererConfigured() {
	terminalRendererConfigOnce.Do(func() {
		profile := detectTerminalColorProfile()
		lipgloss.SetColorProfile(profile)
		if dark, ok := detectTerminalDarkBackground(); ok {
			lipgloss.SetHasDarkBackground(dark)
		}
	})
}

func detectTerminalColorProfile() termenv.Profile {
	if noColorRequested() || termLooksDumb() {
		return termenv.Ascii
	}
	if hasTrueColorEnvHint() {
		return termenv.TrueColor
	}
	if has256ColorEnvHint() {
		return termenv.ANSI256
	}

	detected := lipgloss.ColorProfile()
	switch detected {
	case termenv.TrueColor, termenv.ANSI256, termenv.ANSI:
		return detected
	case termenv.Ascii:
		if colorEnvLooksInteractive() {
			return termenv.TrueColor
		}
		return termenv.Ascii
	default:
		return termenv.TrueColor
	}
}

func detectTerminalDarkBackground() (bool, bool) {
	if value := strings.TrimSpace(os.Getenv("TERMINAL_BACKGROUND")); value != "" {
		switch strings.ToLower(value) {
		case "dark", "night", "black":
			return true, true
		case "light", "day", "white":
			return false, true
		}
	}
	if value := strings.TrimSpace(os.Getenv("COLORFGBG")); value != "" {
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == ';' || r == ':' || r == ','
		})
		if len(parts) > 0 {
			if idx, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
				switch {
				case idx >= 0 && idx <= 6:
					return true, true
				case idx == 7 || idx >= 15:
					return false, true
				}
			}
		}
	}
	return false, false
}

func noColorRequested() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

func termLooksDumb() bool {
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	return term == "dumb"
}

func colorEnvLooksInteractive() bool {
	for _, name := range []string{"COLORTERM", "TERM", "TERM_PROGRAM", "WT_SESSION", "VTE_VERSION"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

func hasTrueColorEnvHint() bool {
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	return envVarContains("COLORTERM", "truecolor", "24bit") ||
		envVarContains("TERM", "truecolor", "24bit", "direct", "kitty", "wezterm", "ghostty", "alacritty") ||
		envVarContains("TERM_PROGRAM", "wezterm", "warp", "ghostty", "vscode", "iterm", "apple_terminal")
}

func has256ColorEnvHint() bool {
	return envVarContains("TERM", "256color") || envVarContains("COLORTERM", "256")
}

func envVarContains(name string, needles ...string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return false
	}
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
