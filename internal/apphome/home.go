package apphome

import (
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	HomeDir   string
	CodexHome string
	Config    string
	Profiles  string
	Settings  string
	Sessions  string
}

type Layout struct {
	codexHome string
}

func HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "."
}

func ResolveCodexHome(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if strings.TrimSpace(home) == "" {
		home = filepath.Join(HomeDir(), ".codex")
	}
	return filepath.Clean(home)
}

func NewLayout(codexHome string) Layout {
	return Layout{codexHome: ResolveCodexHome(codexHome)}
}

func DefaultLayout() Layout {
	return NewLayout("")
}

func (l Layout) CodexHome() string {
	return ResolveCodexHome(l.codexHome)
}

func (l Layout) ConfigPath() string {
	return filepath.Join(l.CodexHome(), "config.toml")
}

func (l Layout) ProfilesDir() string {
	return filepath.Join(l.CodexHome(), "Profiles")
}

func (l Layout) SettingsPath() string {
	return filepath.Join(l.ProfilesDir(), "settings.json")
}

func (l Layout) SessionsDir() string {
	return filepath.Join(l.CodexHome(), "sessions")
}

func (l Layout) Paths() Paths {
	return Paths{
		HomeDir:   HomeDir(),
		CodexHome: l.CodexHome(),
		Config:    l.ConfigPath(),
		Profiles:  l.ProfilesDir(),
		Settings:  l.SettingsPath(),
		Sessions:  l.SessionsDir(),
	}
}

func CodexHome() string {
	return DefaultLayout().CodexHome()
}

func ConfigPath() string {
	return DefaultLayout().ConfigPath()
}

func ProfilesDir() string {
	return DefaultLayout().ProfilesDir()
}

func SettingsPath() string {
	return DefaultLayout().SettingsPath()
}

func SessionsDir() string {
	return DefaultLayout().SessionsDir()
}
