package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
	"github.com/Perdonus/lavilas-code/internal/tooling"
	"github.com/Perdonus/lavilas-code/internal/tui"
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
	options       taskrun.Options
	history       []runtime.Message
	sessionPath   string
	language      CatalogLanguage
	commandPrefix string
	commands      []Command
	lookup        map[string]Command
}

type streamPrinter struct {
	language     CatalogLanguage
	currentRound int
	printedLen   int
	lineOpen     bool
	seenProgress bool
}

func runChat(args []string) int {
	usePlain, filteredArgs := consumeChatModeFlags(args)
	language := currentCatalogLanguage()
	options, err := parseChatOptions(filteredArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", localizedText(language, "chat", "чат"), err)
		return 2
	}

	if !isInteractiveTerminal() {
		fmt.Fprintln(os.Stderr, localizedText(language, "chat: interactive terminal required", "чат: нужен интерактивный терминал"))
		return 2
	}

	if !usePlain {
		return tui.Run(tui.Options{TaskOptions: options})
	}

	session := chatSession{
		options:       options,
		language:      language,
		commandPrefix: currentCommandPrefix(),
		commands:      replCommands(),
	}
	session.lookup = buildReplLookup(session.commands, session.language)

	fmt.Println(localizedText(session.language,
		fmt.Sprintf("Interactive chat. %shelp for commands, %sexit to quit.", session.commandPrefix, session.commandPrefix),
		fmt.Sprintf("Интерактивный чат. %sпомощь покажет команды, %sвыход завершит сеанс.", session.commandPrefix, session.commandPrefix),
	))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for {
		fmt.Print(replPrompt)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", localizedText(session.language, "chat", "чат"), err)
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

func consumeChatModeFlags(args []string) (bool, []string) {
	if len(args) == 0 {
		return false, nil
	}
	usePlain := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--plain", "--no-tui":
			usePlain = true
		default:
			filtered = append(filtered, arg)
		}
	}
	return usePlain, filtered
}

func parseChatOptions(args []string) (taskrun.Options, error) {
	var options taskrun.Options

	for index := 0; index < len(args); index++ {
		if next, handled, err := consumeToolPolicyFlag(&options, args, index); handled {
			if err != nil {
				return taskrun.Options{}, err
			}
			index = next
			continue
		}
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
				return taskrun.Options{}, fmt.Errorf(localizedText(currentCatalogLanguage(), "unknown flag %q", "неизвестный флаг %q"), args[index])
			}
			return taskrun.Options{}, fmt.Errorf(localizedText(currentCatalogLanguage(), "unexpected argument %q", "неожиданный аргумент %q"), args[index])
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

func buildReplLookup(commands []Command, languages ...CatalogLanguage) map[string]Command {
	language := CatalogLanguageEnglish
	if len(languages) > 0 && languages[0] != CatalogLanguageUnknown {
		language = languages[0]
	}
	lookup := buildLookup(commands)
	helpCommand := Command{
		Name:        "help",
		Aliases:     []string{"помощь"},
		Description: localizedText(language, "Show REPL commands", "Показать команды REPL"),
		Category:    "interactive",
		Run: func(args []string) int {
			if len(args) > 0 {
				fmt.Fprintln(os.Stderr, localizedText(language, "help: does not accept arguments", "помощь: команда не принимает аргументы"))
				return 2
			}
			printReplHelp(commands, language, currentCommandPrefix())
			return 0
		},
	}
	lookup["help"] = helpCommand
	for _, alias := range helpCommand.Aliases {
		lookup[alias] = helpCommand
	}
	exitCommand := Command{
		Name:        "exit",
		Aliases:     []string{"quit", "выход", "выйти"},
		Description: localizedText(language, "Exit chat mode", "Выйти из режима чата"),
		Category:    "interactive",
		Run: func(args []string) int {
			if len(args) > 0 {
				fmt.Fprintln(os.Stderr, localizedText(language, "exit: does not accept arguments", "выход: команда не принимает аргументы"))
				return 2
			}
			return replExitRequested
		},
	}
	lookup["exit"] = exitCommand
	for _, alias := range exitCommand.Aliases {
		lookup[alias] = exitCommand
	}
	return lookup
}

func printReplHelp(commands []Command, language CatalogLanguage, prefix string) {
	_ = commands
	fmt.Println(localizedText(language, "Slash commands:", "Слэш-команды:"))
	width := 0
	items := make([]CatalogListItem, 0, len(replAllowedCommands))
	for _, item := range Catalog().List(CatalogListOptions{Language: language, Category: "account"}) {
		if _, ok := replAllowedCommands[item.Command]; ok {
			items = append(items, item)
			if len(item.Name) > width {
				width = len(item.Name)
			}
		}
	}
	for _, item := range Catalog().List(CatalogListOptions{Language: language, Category: "config"}) {
		if _, ok := replAllowedCommands[item.Command]; ok {
			items = append(items, item)
			if len(item.Name) > width {
				width = len(item.Name)
			}
		}
	}
	width = max(width, len(localizedText(language, "help", "помощь")), len(localizedText(language, "exit", "выход")))
	fmt.Printf("  %s%-*s %s\n", prefix, width, localizedText(language, "help", "помощь"), localizedText(language, "Show REPL help", "Показать помощь REPL"))
	for _, item := range items {
		fmt.Printf("  %s%-*s %s\n", prefix, width, item.Name, item.Description)
	}
	fmt.Printf("  %s%-*s %s\n", prefix, width, localizedText(language, "exit", "выход"), localizedText(language, "Exit chat", "Выйти из чата"))
	fmt.Println()
	fmt.Println(localizedText(language, "Any other input is sent to the active chat session.", "Любой другой ввод отправляется в активную чат-сессию."))
}

func (s *chatSession) handleLine(line string) int {
	if strings.HasPrefix(line, s.commandPrefix) {
		return s.dispatchCommand(line)
	}
	return s.runPrompt(line)
}

func (s *chatSession) dispatchCommand(line string) int {
	argv := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, s.commandPrefix)))
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, localizedText(s.language, "chat: empty slash command", "чат: пустая слэш-команда"))
		return 2
	}
	return runCommand(s.commands, s.lookup, argv, false)
}

func (s *chatSession) runPrompt(prompt string) int {
	options := s.options
	options.Prompt = prompt
	options.History = clonePersistedMessages(s.history)
	printer := newStreamPrinter(s.language)
	if !options.JSON && !options.DisableStreaming {
		options.OnProgress = printer.Handle
	}
	options.OnApproval = func(ctx context.Context, request tooling.ApprovalRequest) (taskrun.ApprovalDecision, error) {
		printer.Finish()
		return promptApprovalDecisionCLI(ctx, s.language, request)
	}

	result, err := taskrun.Run(contextBackground(), options)
	if err != nil {
		printer.Finish()
		fmt.Fprintf(os.Stderr, "%s: %v\n", localizedText(s.language, "chat", "чат"), err)
		return 1
	}

	s.captureResult(result)
	s.persistTurn(result)

	if printer.ShouldFallback() {
		if err := taskrun.Print(result); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", localizedText(s.language, "chat", "чат"), err)
			return 1
		}
		return 0
	}
	printer.Finish()
	return 0
}

func promptApprovalDecisionCLI(ctx context.Context, language CatalogLanguage, request tooling.ApprovalRequest) (taskrun.ApprovalDecision, error) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return taskrun.ApprovalDecisionDeny, err
	}
	defer tty.Close()

	reader := bufio.NewReader(tty)
	for {
		fmt.Fprintln(os.Stdout, localizedText(language, "[approval] tool call requires confirmation", "[подтверждение] вызов инструмента требует подтверждения"))
		fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "tool", "инструмент"), request.Name)
		if strings.TrimSpace(request.Summary) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "summary", "сводка"), request.Summary)
		}
		if strings.TrimSpace(request.Details) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "details", "детали"), request.Details)
		}
		if strings.TrimSpace(request.Reason) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "reason", "причина"), request.Reason)
		}
		fmt.Fprint(os.Stdout, localizedText(language, "Allow? [y] once / [a] session / [n] deny: ", "Разрешить? [y] один раз / [a] на сессию / [n] запретить: "))
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				errCh <- readErr
				return
			}
			lineCh <- line
		}()
		select {
		case <-ctx.Done():
			return taskrun.ApprovalDecisionDeny, ctx.Err()
		case readErr := <-errCh:
			return taskrun.ApprovalDecisionDeny, readErr
		case line := <-lineCh:
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes", "д", "да":
				return taskrun.ApprovalDecisionApprove, nil
			case "a", "always", "с", "сессия":
				return taskrun.ApprovalDecisionApproveForSession, nil
			case "n", "no", "н", "нет", "":
				return taskrun.ApprovalDecisionDeny, nil
			}
		}
	}
}

func newStreamPrinter(language CatalogLanguage) *streamPrinter {
	return &streamPrinter{
		language: language,
	}
}

func (p *streamPrinter) Handle(update taskrun.ProgressUpdate) {
	switch update.Kind {
	case taskrun.ProgressKindTurnStarted:
		p.ensureRound(update.Round)
	case taskrun.ProgressKindAssistantSnapshot:
		p.ensureRound(update.Round)
		if update.Snapshot.Text == "" {
			return
		}
		if p.printedLen > len(update.Snapshot.Text) {
			p.printedLen = 0
		}
		if p.printedLen >= len(update.Snapshot.Text) {
			return
		}
		suffix := update.Snapshot.Text[p.printedLen:]
		if suffix == "" {
			return
		}
		fmt.Print(suffix)
		p.seenProgress = true
		p.printedLen = len(update.Snapshot.Text)
		p.lineOpen = !strings.HasSuffix(suffix, "\n")
	case taskrun.ProgressKindToolPlanned:
		if update.ToolPlan != nil {
			p.printStatus(localizedText(p.language,
				fmt.Sprintf("[tools] planned %d calls in %d batches", update.ToolPlan.Summary.CallCount, update.ToolPlan.Summary.BatchCount),
				fmt.Sprintf("[инструменты] запланировано %d вызовов в %d пакетах", update.ToolPlan.Summary.CallCount, update.ToolPlan.Summary.BatchCount),
			))
		}
	case taskrun.ProgressKindApprovalRequired:
		if update.ApprovalRequest != nil {
			p.printStatus(localizedText(p.language,
				fmt.Sprintf("[approval] %s -> %s", update.ApprovalRequest.Name, localizedToolStatusCLI(p.language, string(update.ApprovalRequest.Status))),
				fmt.Sprintf("[подтверждение] %s -> %s", update.ApprovalRequest.Name, localizedToolStatusCLI(p.language, string(update.ApprovalRequest.Status))),
			))
		}
	case taskrun.ProgressKindToolResult:
		if update.ToolResult != nil {
			p.printStatus(localizedText(p.language,
				fmt.Sprintf("[tool] %s -> %s", update.ToolResult.Name, localizedToolStatusCLI(p.language, string(update.ToolResult.Status))),
				fmt.Sprintf("[инструмент] %s -> %s", update.ToolResult.Name, localizedToolStatusCLI(p.language, string(update.ToolResult.Status))),
			))
		}
	case taskrun.ProgressKindRetryScheduled:
		if update.RetryAfter > 0 {
			p.printStatus(localizedText(p.language,
				fmt.Sprintf("[retry] waiting %s", update.RetryAfter),
				fmt.Sprintf("[повтор] ждём %s", update.RetryAfter),
			))
		}
	case taskrun.ProgressKindTurnDone, taskrun.ProgressKindTurnFailed:
		p.Finish()
	}
}

func (p *streamPrinter) ensureRound(round int) {
	if round <= 0 || round == p.currentRound {
		return
	}
	if p.lineOpen {
		fmt.Println()
	}
	p.currentRound = round
	p.printedLen = 0
	p.lineOpen = false
}

func (p *streamPrinter) printStatus(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if p.lineOpen {
		fmt.Println()
	}
	fmt.Println(line)
	p.seenProgress = true
	p.lineOpen = false
}

func (p *streamPrinter) Finish() {
	if p.lineOpen {
		fmt.Println()
	}
	p.lineOpen = false
}

func (p *streamPrinter) ShouldFallback() bool {
	return !p.seenProgress
}

func localizedToolStatusCLI(language CatalogLanguage, status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded":
		return localizedText(language, "succeeded", "успешно")
	case "failed":
		return localizedText(language, "failed", "ошибка")
	case "approval_required":
		return localizedText(language, "approval required", "нужно подтверждение")
	case "denied":
		return localizedText(language, "denied", "запрещено")
	default:
		return fallback(strings.TrimSpace(status), localizedText(language, "unknown", "неизвестно"))
	}
}

func (s *chatSession) captureResult(result taskrun.Result) {
	s.history = clonePersistedMessages(result.FullHistory())

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

func (s *chatSession) persistTurn(result taskrun.Result) {
	if strings.TrimSpace(s.sessionPath) == "" {
		entry, err := persistNewSession(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", localizedText(s.language, "chat", "чат"), localizedText(s.language, "warning: failed to save session", "предупреждение: не удалось сохранить сессию"), err)
			return
		}
		s.sessionPath = entry.Path
		return
	}

	if err := appendSessionTurn(s.sessionPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s: %v\n", localizedText(s.language, "chat", "чат"), localizedText(s.language, "warning: failed to append session", "предупреждение: не удалось дописать сессию"), err)
	}
}
