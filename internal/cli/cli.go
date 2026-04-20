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
		fmt.Printf("Lavilas Codex Go %s (%s)\\n", version.Version, version.Channel)
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
	fmt.Printf("Lavilas Codex Go %s (%s)\\n", version.Version, version.Channel)
	fmt.Println("Независимый go-контур для NV alpha.")
	fmt.Println()
}

func printCommands(commands []Command) {
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})
	fmt.Println("Commands:")
	for _, cmd := range commands {
		aliases := ""
		if len(cmd.Aliases) > 0 {
			aliases = fmt.Sprintf(" [%s]", strings.Join(cmd.Aliases, ", "))
		}
		fmt.Printf("  %-12s%s %s\\n", cmd.Name, aliases, cmd.Description)
	}
}
