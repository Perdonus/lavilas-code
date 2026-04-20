package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
)

const (
	replPrompt        = "lvls> "
	replExitRequested = -1
)

var replAllowedCommands = map[string]struct{}{
	"status":    {},
	"model":     {},
	"profiles":  {},
	"providers": {},
	"settings":  {},
}

type chatSession struct {
	options     taskrun.Options
	history     []runtime.Message
	sessionPath string
	commands    []Command
	lookup      map[string]Command
}

func runChat(args []string) int {
	options, err := parseChatOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
		return 2
	}

	if !isInteractiveTerminal() {
		fmt.Fprintln(os.Stderr, "chat: interactive terminal required")
		return 2
	}

	session := chatSession{
		options:  options,
		commands: replCommands(),
	}
	session.lookup = buildReplLookup(session.commands)

	fmt.Println("Interactive chat. /help for commands, /exit to quit.")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for {
		fmt.Print(replPrompt)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "chat: %v\n", err)
				return 1
			}
			fmt.Println()
			return 0
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		status := session.handleLine(line)
		if status == replExitRequested {
			return 0
		}
	}
}

func parseChatOptions(args []string) (taskrun.Options, error) {
	var options taskrun.Options

	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--model":
			value, next, err := takeFlagValue(args, index, "--model")
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Model = value
			index = next
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Provider = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				return taskrun.Options{}, err
			}
			options.ReasoningEffort = value
			index = next
		case "--system":
			value, next, err := takeFlagValue(args, index, "--system")
			if err != nil {
				return taskrun.Options{}, err
			}
			options.SystemPrompt = value
			index = next
		default:
			if strings.HasPrefix(args[index], "--") {
				return taskrun.Options{}, fmt.Errorf("unknown flag %q", args[index])
			}
			return taskrun.Options{}, fmt.Errorf("unexpected argument %q", args[index])
		}
	}

	return options, nil
}

func replCommands() []Command {
	base := registry()
	commands := make([]Command, 0, len(replAllowedCommands))
	for _, cmd := range base {
		if _, ok := replAllowedCommands[cmd.Name]; ok {
			commands = append(commands, cmd)
		}
	}
	return commands
}

func buildReplLookup(commands []Command) map[string]Command {
	lookup := buildLookup(commands)
	lookup["help"] = Command{
		Name:        "help",
		Description: "Show REPL commands",
		Category:    "interactive",
		Run: func(args []string) int {
			if len(args) > 0 {
				fmt.Fprintln(os.Stderr, "help: does not accept arguments")
				return 2
			}
			printReplHelp(commands)
			return 0
		},
	}
	lookup["exit"] = Command{
		Name:        "exit",
		Description: "Exit chat mode",
		Category:    "interactive",
		Run: func(args []string) int {
			if len(args) > 0 {
				fmt.Fprintln(os.Stderr, "exit: does not accept arguments")
				return 2
			}
			return replExitRequested
		},
	}
	lookup["quit"] = Command{
		Name:        "quit",
		Description: "Exit chat mode",
		Category:    "interactive",
		Run: func(args []string) int {
			if len(args) > 0 {
				fmt.Fprintln(os.Stderr, "quit: does not accept arguments")
				return 2
			}
			return replExitRequested
		},
	}
	return lookup
}

func printReplHelp(commands []Command) {
	fmt.Println("Slash commands:")
	fmt.Println("  /help        Show REPL help")
	for _, cmd := range commands {
		fmt.Printf("  /%-11s %s\n", cmd.Name, cmd.Description)
	}
	fmt.Println("  /exit        Exit chat")
	fmt.Println("  /quit        Exit chat")
	fmt.Println()
	fmt.Println("Any other input is sent to the active chat session.")
}

func (s *chatSession) handleLine(line string) int {
	if strings.HasPrefix(line, "/") {
		return s.dispatchCommand(line)
	}
	return s.runPrompt(line)
}

func (s *chatSession) dispatchCommand(line string) int {
	argv := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "/")))
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "chat: empty slash command")
		return 2
	}
	return runCommand(s.commands, s.lookup, argv, false)
}

func (s *chatSession) runPrompt(prompt string) int {
	options := s.options
	options.Prompt = prompt
	options.History = clonePersistedMessages(s.history)

	result, err := taskrun.Run(contextBackground(), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
		return 1
	}

	s.captureResult(result)
	s.persistTurn(prompt, result)

	if err := taskrun.Print(result); err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
		return 1
	}
	return 0
}

func (s *chatSession) captureResult(result taskrun.Result) {
	s.history = clonePersistedMessages(result.RequestMessages)
	if hasPersistableMessage(result.AssistantMessage) {
		s.history = append(s.history, result.AssistantMessage)
	}

	if strings.TrimSpace(s.options.Model) == "" {
		s.options.Model = result.Model
	}
	if strings.TrimSpace(s.options.Profile) == "" && strings.TrimSpace(result.Profile) != "" {
		s.options.Profile = result.Profile
	}
	if strings.TrimSpace(s.options.Provider) == "" && strings.TrimSpace(result.ProviderName) != "" {
		s.options.Provider = result.ProviderName
	}
	if strings.TrimSpace(s.options.ReasoningEffort) == "" && strings.TrimSpace(result.Reasoning) != "" {
		s.options.ReasoningEffort = result.Reasoning
	}
}

func (s *chatSession) persistTurn(prompt string, result taskrun.Result) {
	if strings.TrimSpace(s.sessionPath) == "" {
		entry, err := persistNewSession(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chat: warning: failed to save session: %v\n", err)
			return
		}
		s.sessionPath = entry.Path
		return
	}

	if err := appendSessionTurn(s.sessionPath, prompt, result.AssistantMessage); err != nil {
		fmt.Fprintf(os.Stderr, "chat: warning: failed to append session: %v\n", err)
	}
}
