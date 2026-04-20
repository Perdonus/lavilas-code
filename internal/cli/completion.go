package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func runCompletion(args []string) int {
	shell := "bash"
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "bash", "zsh", "fish", "powershell", "pwsh":
			shell = args[index]
		case "--shell":
			value, next, err := takeFlagValue(args, index, "--shell")
			if err != nil {
				fmt.Fprintf(os.Stderr, "completion: %v\n", err)
				return 2
			}
			shell = value
			index = next
		default:
			fmt.Fprintf(os.Stderr, "completion: unknown flag %q\n", args[index])
			return 2
		}
	}

	commands := registry()
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	switch shell {
	case "bash":
		fmt.Print(renderBashCompletion(commands))
	case "zsh":
		fmt.Print(renderZshCompletion(commands))
	case "fish":
		fmt.Print(renderFishCompletion(commands))
	case "powershell", "pwsh":
		fmt.Print(renderPowerShellCompletion(commands))
	default:
		fmt.Fprintf(os.Stderr, "completion: unsupported shell %q\n", shell)
		return 2
	}
	return 0
}

func renderBashCompletion(commands []Command) string {
	items := append([]string{"help", "version", "--help", "--version", "-h", "-v"}, commandAndAliasNames(commands)...)
	return fmt.Sprintf(`_lvls_complete() {
    local cur
    cur="${COMP_WORDS[COMP_CWORD]}"
    COMPREPLY=( $(compgen -W %q -- "$cur") )
}
complete -F _lvls_complete lvls
`, strings.Join(items, " "))
}

func renderZshCompletion(commands []Command) string {
	entries := make([]string, 0, len(commands)+2)
	entries = append(entries, "'help:Show help'", "'version:Show version'")
	for _, command := range commands {
		entries = append(entries, fmt.Sprintf("'%s:%s'", command.Name, escapeZshDescription(command.Description)))
		for _, alias := range command.Aliases {
			entries = append(entries, fmt.Sprintf("'%s:%s'", alias, escapeZshDescription(command.Description)))
		}
	}
	return fmt.Sprintf(`#compdef lvls

_lvls() {
  local -a commands
  commands=(
    %s
  )
  _describe 'command' commands
}

compdef _lvls lvls
`, strings.Join(entries, "\n    "))
}

func renderFishCompletion(commands []Command) string {
	var builder strings.Builder
	builder.WriteString("complete -c lvls -f\n")
	builder.WriteString("complete -c lvls -n '__fish_use_subcommand' -a help -d 'Show help'\n")
	builder.WriteString("complete -c lvls -n '__fish_use_subcommand' -a version -d 'Show version'\n")
	for _, command := range commands {
		builder.WriteString(fmt.Sprintf("complete -c lvls -n '__fish_use_subcommand' -a %s -d %q\n", command.Name, command.Description))
		for _, alias := range command.Aliases {
			builder.WriteString(fmt.Sprintf("complete -c lvls -n '__fish_use_subcommand' -a %s -d %q\n", alias, command.Description))
		}
	}
	return builder.String()
}

func renderPowerShellCompletion(commands []Command) string {
	items := append([]string{"help", "version"}, commandAndAliasNames(commands)...)
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("'%s'", strings.ReplaceAll(item, "'", "''")))
	}
	return fmt.Sprintf(`Register-ArgumentCompleter -CommandName lvls -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $commands = @(%s)
    foreach ($cmd in $commands) {
        if ($cmd -like "$wordToComplete*") {
            [System.Management.Automation.CompletionResult]::new($cmd, $cmd, 'ParameterValue', $cmd)
        }
    }
}
`, strings.Join(quoted, ", "))
}

func commandAndAliasNames(commands []Command) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(commands)*2)
	for _, command := range commands {
		if _, ok := seen[command.Name]; !ok {
			seen[command.Name] = struct{}{}
			result = append(result, command.Name)
		}
		for _, alias := range command.Aliases {
			if _, ok := seen[alias]; ok {
				continue
			}
			seen[alias] = struct{}{}
			result = append(result, alias)
		}
	}
	sort.Strings(result)
	return result
}

func escapeZshDescription(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
