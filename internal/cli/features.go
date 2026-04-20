package cli

import (
	"fmt"
	"os"

	"github.com/Perdonus/lavilas-code/internal/version"
)

type alphaFeature struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Command string `json:"command"`
	Summary string `json:"summary"`
}

func runFeatures(args []string) int {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			fmt.Fprintf(os.Stderr, "features: unknown flag %q\n", arg)
			return 2
		}
	}

	payload := struct {
		Version  string         `json:"version"`
		Channel  string         `json:"channel"`
		Features []alphaFeature `json:"features"`
	}{
		Version:  version.Version,
		Channel:  version.Channel,
		Features: alphaFeatures(),
	}

	if jsonOutput {
		return printJSON(payload)
	}

	fmt.Printf("Go Lavilas %s (%s)\n", payload.Version, payload.Channel)
	fmt.Println("Alpha feature matrix:")
	fmt.Printf("%-20s %-10s %-28s %s\n", "feature", "status", "command", "summary")
	for _, feature := range payload.Features {
		fmt.Printf("%-20s %-10s %-28s %s\n", feature.Name, feature.Status, feature.Command, feature.Summary)
	}
	return 0
}

func alphaFeatures() []alphaFeature {
	return []alphaFeature{
		{
			Name:    "interactive_chat",
			Status:  "available",
			Command: "chat",
			Summary: "Interactive terminal chat session.",
		},
		{
			Name:    "one_shot_tasks",
			Status:  "available",
			Command: "run",
			Summary: "Single prompt execution without entering chat.",
		},
		{
			Name:    "session_resume",
			Status:  "available",
			Command: "resume, fork",
			Summary: "Resume or branch from stored sessions.",
		},
		{
			Name:    "review_apply",
			Status:  "available",
			Command: "review, apply",
			Summary: "Review diffs and apply patches from stdin or file.",
		},
		{
			Name:    "account_state",
			Status:  "available",
			Command: "login, logout, status",
			Summary: "Manage provider credentials and inspect active state.",
		},
		{
			Name:    "config_profiles",
			Status:  "available",
			Command: "model, profiles, providers, settings",
			Summary: "Manage model defaults, profiles, providers, and UI settings.",
		},
		{
			Name:    "runtime_checks",
			Status:  "available",
			Command: "doctor",
			Summary: "Inspect local environment and runtime health.",
		},
		{
			Name:    "shell_completion",
			Status:  "available",
			Command: "completion",
			Summary: "Generate bash, zsh, fish, and PowerShell scripts.",
		},
		{
			Name:    "feature_matrix",
			Status:  "available",
			Command: "features",
			Summary: "Show this alpha capability summary in text or JSON.",
		},
	}
}
