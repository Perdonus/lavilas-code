package apphome

import (
	"os"
	"path/filepath"
)

func HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "."
}

func CodexHome() string {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), ".codex")
}

func ConfigPath() string {
	return filepath.Join(CodexHome(), "config.toml")
}

func ProfilesDir() string {
	return filepath.Join(CodexHome(), "Profiles")
}

func SettingsPath() string {
	return filepath.Join(ProfilesDir(), "settings.json")
}

func SessionsDir() string {
	return filepath.Join(CodexHome(), "sessions")
}
