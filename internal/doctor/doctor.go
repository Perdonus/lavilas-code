package doctor

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/version"
)

func Run() int {
	fmt.Printf("Go Lavilas %s (%s)\n", version.Version, version.Channel)
	fmt.Printf("commit: %s\n", version.Commit)
	fmt.Printf("goos/goarch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("home: %s\n", apphome.HomeDir())
	fmt.Printf("codex_home: %s\n", apphome.CodexHome())
	fmt.Printf("config: %s\n", apphome.ConfigPath())
	fmt.Printf("settings: %s\n", apphome.SettingsPath())
	fmt.Printf("sessions: %s\n", apphome.SessionsDir())
	fmt.Println()

	reportFile("config", apphome.ConfigPath())
	reportFile("settings", apphome.SettingsPath())

	if sessions, err := state.LoadSessions(apphome.SessionsDir(), 1); err == nil {
		fmt.Printf("sessions_found: %d+\n", len(sessions))
		if len(sessions) > 0 {
			fmt.Printf("latest_session: %s\n", sessions[0].Path)
		}
	} else {
		fmt.Printf("sessions_error: %v\n", err)
	}

	fmt.Printf("shell: %s\n", getenv("SHELL"))
	fmt.Printf("term: %s\n", getenv("TERM"))
	fmt.Printf("colorterm: %s\n", getenv("COLORTERM"))
	return 0
}

func reportFile(label string, path string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Printf("%s_exists: no (%v)\n", label, err)
		return
	}
	fmt.Printf("%s_exists: yes (%d bytes)\n", label, info.Size())
}

func getenv(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return "<empty>"
}
