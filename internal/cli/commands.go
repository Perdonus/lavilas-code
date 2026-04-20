package cli

import "fmt"

func registry() []Command {
	stub := func(name string) func([]string) int {
		return func(args []string) int {
			fmt.Printf("%s is not implemented in alpha yet.\\n", name)
			return 2
		}
	}

	return []Command{
		{Name: "resume", Aliases: []string{"r"}, Description: "Resume previous session", Run: stub("resume")},
		{Name: "run", Aliases: []string{"exec"}, Description: "Execute a one-shot task", Run: stub("run")},
		{Name: "login", Description: "Configure account access", Run: stub("login")},
		{Name: "logout", Description: "Remove saved account access", Run: stub("logout")},
		{Name: "model", Description: "Change active model", Run: stub("model")},
		{Name: "profiles", Description: "Manage saved profiles", Run: stub("profiles")},
		{Name: "settings", Description: "Open settings", Run: stub("settings")},
		{Name: "update", Description: "Check for updates", Run: stub("update")},
		{Name: "doctor", Description: "Inspect local environment", Run: stub("doctor")},
	}
}
