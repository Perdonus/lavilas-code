package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/version"
)

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Category    string
	Run         func(args []string) int
}

func Run(argv []string) int {
	commands := registry()
	lookup := map[string]Command{}
	for _, cmd := range commands {
		lookup[cmd.Name] = cmd
		for _, alias := range cmd.Aliases {
			lookup[alias] = cmd
		}
	}

	if len(argv) == 0 {
		printBanner()
		printCommands(commands)
		return 0
	}

	switch argv[0] {
	case "--version", "-v", "version":
		fmt.Printf("Go Lavilas %s (%s)\\n", version.Version, version.Channel)
		return 0
	case "help", "--help", "-h":
		printBanner()
		printCommands(commands)
		return 0
	}

	cmd, ok := lookup[argv[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\\n\\n", argv[0])
		printCommands(commands)
		return 2
	}
	return cmd.Run(argv[1:])
}

func printBanner() {
	fmt.Printf("Go Lavilas %s (%s)\\n", version.Version, version.Channel)
	fmt.Println("Независимый go-контур для NV alpha.")
	fmt.Println()
}

func printCommands(commands []Command) {
	categoryOrder := []string{
		"interactive",
		"account",
		"config",
		"automation",
		"runtime",
		"debug",
	}
	labels := map[string]string{
		"interactive": "Interactive",
		"account":     "Account",
		"config":      "Config",
		"automation":  "Automation",
		"runtime":     "Runtime",
		"debug":       "Debug",
	}

	byCategory := map[string][]Command{}
	for _, cmd := range commands {
		byCategory[cmd.Category] = append(byCategory[cmd.Category], cmd)
	}

	fmt.Println("Commands:")
	for _, category := range categoryOrder {
		items := byCategory[category]
		if len(items) == 0 {
			continue
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
		fmt.Printf("  %s:\n", labels[category])
		for _, cmd := range items {
			aliases := ""
			if len(cmd.Aliases) > 0 {
				aliases = fmt.Sprintf(" [%s]", strings.Join(cmd.Aliases, ", "))
			}
			fmt.Printf("    %-14s%s %s\\n", cmd.Name, aliases, cmd.Description)
		}
	}
}
