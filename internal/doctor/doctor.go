package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/version"
)

type Payload struct {
	Version       string `json:"version"`
	Channel       string `json:"channel"`
	Commit        string `json:"commit"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	Home          string `json:"home"`
	CodexHome     string `json:"codex_home"`
	ConfigPath    string `json:"config_path"`
	SettingsPath  string `json:"settings_path"`
	SessionsPath  string `json:"sessions_path"`
	ConfigExists  bool   `json:"config_exists"`
	ConfigSize    int64  `json:"config_size"`
	SettingsExists bool  `json:"settings_exists"`
	SettingsSize  int64  `json:"settings_size"`
	SessionsFound int    `json:"sessions_found"`
	LatestSession string `json:"latest_session,omitempty"`
	SessionsError string `json:"sessions_error,omitempty"`
	Shell         string `json:"shell"`
	Term          string `json:"term"`
	ColorTerm     string `json:"colorterm"`
}

func Collect() Payload {
	payload := Payload{
		Version:       version.Version,
		Channel:       version.Channel,
		Commit:        version.Commit,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Home:          apphome.HomeDir(),
		CodexHome:     apphome.CodexHome(),
		ConfigPath:    apphome.ConfigPath(),
		SettingsPath:  apphome.SettingsPath(),
		SessionsPath:  apphome.SessionsDir(),
		Shell:         getenv("SHELL"),
		Term:          getenv("TERM"),
		ColorTerm:     getenv("COLORTERM"),
	}

	payload.ConfigExists, payload.ConfigSize = statFile(apphome.ConfigPath())
	payload.SettingsExists, payload.SettingsSize = statFile(apphome.SettingsPath())

	if sessions, err := state.LoadSessions(apphome.SessionsDir(), 1); err == nil {
		payload.SessionsFound = len(sessions)
		if len(sessions) > 0 {
			payload.LatestSession = sessions[0].Path
		}
	} else {
		payload.SessionsError = err.Error()
	}

	return payload
}

func Run(jsonOutput bool) int {
	payload := Collect()
	if jsonOutput {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode json: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
		return 0
	}

	fmt.Printf("Go Lavilas %s (%s)\n", payload.Version, payload.Channel)
	fmt.Printf("commit: %s\n", payload.Commit)
	fmt.Printf("goos/goarch: %s/%s\n", payload.GOOS, payload.GOARCH)
	fmt.Printf("home: %s\n", payload.Home)
	fmt.Printf("codex_home: %s\n", payload.CodexHome)
	fmt.Printf("config: %s\n", payload.ConfigPath)
	fmt.Printf("settings: %s\n", payload.SettingsPath)
	fmt.Printf("sessions: %s\n", payload.SessionsPath)
	fmt.Println()

	reportFile("config", payload.ConfigExists, payload.ConfigSize)
	reportFile("settings", payload.SettingsExists, payload.SettingsSize)

	if payload.SessionsError != "" {
		fmt.Printf("sessions_error: %s\n", payload.SessionsError)
	} else {
		fmt.Printf("sessions_found: %d+\n", payload.SessionsFound)
		if payload.LatestSession != "" {
			fmt.Printf("latest_session: %s\n", payload.LatestSession)
		}
	}

	fmt.Printf("shell: %s\n", payload.Shell)
	fmt.Printf("term: %s\n", payload.Term)
	fmt.Printf("colorterm: %s\n", payload.ColorTerm)
	return 0
}

func statFile(path string) (bool, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}
	return true, info.Size()
}

func reportFile(label string, exists bool, size int64) {
	if !exists {
		fmt.Printf("%s_exists: no\n", label)
		return
	}
	fmt.Printf("%s_exists: yes (%d bytes)\n", label, size)
}

func getenv(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return "<empty>"
}
