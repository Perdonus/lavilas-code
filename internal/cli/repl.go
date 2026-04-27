package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
	"github.com/Perdonus/lavilas-code/internal/tooling"
	"github.com/Perdonus/lavilas-code/internal/tui"
	"github.com/Perdonus/lavilas-code/internal/version"
)

const (
	replPrompt        = "› "
	replExitRequested = -1
)

var replAllowedCommands = map[string]struct{}{
	"status":      {},
	"model":       {},
	"presets":     {},
	"profiles":    {},
	"providers":   {},
	"settings":    {},
	"setlang":     {},
	"permissions": {},
}

type chatSession struct {
	options       taskrun.Options
	approvalStore *taskrun.ApprovalSessionStore
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
	if proceed, code := tui.RunUpdateGate(); !proceed {
		return code
	}

	session := chatSession{
		options:       options,
		approvalStore: taskrun.NewApprovalSessionStore(),
		language:      language,
		commandPrefix: currentCommandPrefix(),
		commands:      replCommands(),
	}
	session.lookup = buildReplLookup(session.commands, session.language)

	session.printHeader()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for {
		fmt.Println()
		fmt.Println()
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
		return true, nil
	}
	usePlain := true
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--plain", "--no-tui":
			usePlain = true
		case "--tui":
			usePlain = false
		default:
			filtered = append(filtered, arg)
		}
	}
	return usePlain, filtered
}

func (s *chatSession) printHeader() {
	config, _ := loadConfigOptional(apphome.ConfigPath())
	model := firstNonEmpty(strings.TrimSpace(s.options.Model), config.EffectiveModel(), localizedUnset(s.language))
	reasoning := firstNonEmpty(strings.TrimSpace(s.options.ReasoningEffort), config.EffectiveReasoningEffort())
	if reasoning != "" {
		model += " " + plainReasoningLabel(s.language, reasoning)
	}
	cwd := strings.TrimSpace(s.options.CWD)
	if cwd == "" {
		if current, err := os.Getwd(); err == nil {
			cwd = current
		}
	}
	cwd = compactPlainPath(cwd)
	changeHint := fmt.Sprintf("%s%s %s", s.commandPrefix, localizedText(s.language, "model", "модель"), localizedText(s.language, "to change", "для смены"))
	width := 57
	rows := []string{
		fmt.Sprintf(">_ Go Lavilas (v%s)", version.Version),
		"",
		fmt.Sprintf("%-14s %s", localizedText(s.language, "model:", "модель:"), joinPlainHeaderValue(model, changeHint, width-18)),
		fmt.Sprintf("%-14s %s", localizedText(s.language, "directory:", "каталог:"), cwd),
	}
	printPlainBox(rows, width)
	fmt.Println()
	fmt.Println(localizedText(s.language,
		"  Tip: Go Lavilas prints the chat directly into terminal scrollback.",
		"  Подсказка: Go Lavilas печатает чат прямо в прокрутку терминала.",
	))
}

func joinPlainHeaderValue(value string, hint string, limit int) string {
	value = strings.TrimSpace(value)
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return value
	}
	joined := value + "   " + hint
	if len([]rune(joined)) <= limit {
		return joined
	}
	return value
}

func plainReasoningLabel(language CatalogLanguage, reasoning string) string {
	switch strings.ToLower(strings.TrimSpace(reasoning)) {
	case "xhigh", "extra-high":
		return localizedText(language, "xhigh", "максимальный")
	case "high":
		return localizedText(language, "high", "высокий")
	case "medium":
		return localizedText(language, "medium", "средний")
	case "low":
		return localizedText(language, "low", "низкий")
	case "none", "off":
		return localizedText(language, "no reasoning", "без размышлений")
	default:
		return strings.TrimSpace(reasoning)
	}
}

func compactPlainPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "~"
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.EqualFold(path, home) {
		return "~"
	}
	if home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func printPlainBox(rows []string, width int) {
	if width < 32 {
		width = 32
	}
	fmt.Println("╭" + strings.Repeat("─", width-2) + "╮")
	for _, row := range rows {
		runes := []rune(row)
		if len(runes) > width-4 {
			runes = runes[:width-5]
			row = string(runes) + "…"
		}
		padding := width - 4 - len([]rune(row))
		if padding < 0 {
			padding = 0
		}
		fmt.Println("│ " + row + strings.Repeat(" ", padding) + " │")
	}
	fmt.Println("╰" + strings.Repeat("─", width-2) + "╯")
}

func parseChatOptions(args []string) (taskrun.Options, error) {
	var options taskrun.Options

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if next, handled, err := consumeCommonTaskFlag(&options, args, index, arg, false); handled {
			if err != nil {
				return taskrun.Options{}, err
			}
			index = next
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return taskrun.Options{}, fmt.Errorf(localizedText(currentCatalogLanguage(), "unknown flag %q", "неизвестный флаг %q"), arg)
		}
		return taskrun.Options{}, fmt.Errorf(localizedText(currentCatalogLanguage(), "unexpected argument %q", "неожиданный аргумент %q"), arg)
	}

	applySettingsToolPolicy(&options)
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
	options.ApprovalStore = s.approvalStore
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
		fmt.Fprintln(os.Stdout, cliApprovalTitle(language, request))
		fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "tool", "инструмент"), request.Name)
		if hint := cliApprovalHint(language, request); hint != "" {
			fmt.Fprintf(os.Stdout, "  %s\n", hint)
		}
		if strings.TrimSpace(request.Summary) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "summary", "сводка"), request.Summary)
		}
		if strings.TrimSpace(request.Details) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "details", "детали"), request.Details)
		}
		if strings.TrimSpace(request.Reason) != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "reason", "причина"), request.Reason)
		}
		if cwd := strings.TrimSpace(request.Metadata.WorkingDirectory); cwd != "" {
			fmt.Fprintf(os.Stdout, "  cwd: %s\n", cwd)
		}
		if targets := cliApprovalTargets(request); targets != "" {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", localizedText(language, "targets", "цели"), targets)
		}
		allowOnce, allowSession, denyLabel := cliApprovalLabels(language, request)
		fmt.Fprintf(os.Stdout, localizedText(language, "Decision: [y] %s / [a] %s / [n] %s: ", "Решение: [y] %s / [a] %s / [n] %s: "), allowOnce, allowSession, denyLabel)
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

func cliApprovalTitle(language CatalogLanguage, request tooling.ApprovalRequest) string {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return localizedText(language, "[approval] additional permissions requested", "[подтверждение] запрошены дополнительные разрешения")
	case tooling.ApprovalKindShellCommand:
		return localizedText(language, "[approval] shell command requires confirmation", "[подтверждение] команда shell требует подтверждения")
	case tooling.ApprovalKindApplyPatch:
		return localizedText(language, "[approval] patch requires confirmation", "[подтверждение] патч требует подтверждения")
	case tooling.ApprovalKindWorkspaceWrite:
		return localizedText(language, "[approval] write requires confirmation", "[подтверждение] запись требует подтверждения")
	default:
		return localizedText(language, "[approval] tool call requires confirmation", "[подтверждение] вызов инструмента требует подтверждения")
	}
}

func cliApprovalHint(language CatalogLanguage, request tooling.ApprovalRequest) string {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return localizedText(language, "  The model needs extra write access before it can continue.", "  Модели нужен дополнительный доступ на запись перед продолжением.")
	case tooling.ApprovalKindShellCommand:
		return localizedText(language, "  This will run a subprocess and may change the workspace.", "  Это запустит подпроцесс и может изменить рабочую папку.")
	case tooling.ApprovalKindApplyPatch:
		return localizedText(language, "  This will edit files through an inline patch.", "  Это изменит файлы через встроенный патч.")
	case tooling.ApprovalKindWorkspaceWrite:
		return localizedText(language, "  This tool writes directly into the workspace.", "  Этот инструмент пишет прямо в рабочую папку.")
	default:
		return ""
	}
}

func cliApprovalLabels(language CatalogLanguage, request tooling.ApprovalRequest) (string, string, string) {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return localizedText(language, "grant this turn", "дать доступ на этот ход"), localizedText(language, "grant this session", "дать доступ на всю сессию"), localizedText(language, "deny", "запретить")
	case tooling.ApprovalKindShellCommand:
		return localizedText(language, "run once", "запустить один раз"), localizedText(language, "allow shell for session", "разрешить shell на сессию"), localizedText(language, "deny", "запретить")
	case tooling.ApprovalKindApplyPatch:
		return localizedText(language, "apply once", "применить один раз"), localizedText(language, "allow patches for session", "разрешить патчи на сессию"), localizedText(language, "deny", "запретить")
	case tooling.ApprovalKindWorkspaceWrite:
		return localizedText(language, "write once", "записать один раз"), localizedText(language, "allow writes for session", "разрешить запись на сессию"), localizedText(language, "deny", "запретить")
	default:
		return localizedText(language, "approve once", "разрешить один раз"), localizedText(language, "approve for session", "разрешить на сессию"), localizedText(language, "deny", "запретить")
	}
}

func cliApprovalTargets(request tooling.ApprovalRequest) string {
	if len(request.Metadata.RequestedWritableRoots) > 0 {
		return strings.Join(request.Metadata.RequestedWritableRoots, ", ")
	}
	if len(request.Metadata.ResourceKeys) == 0 {
		return ""
	}
	preview := make([]string, 0, 3)
	for _, value := range request.Metadata.ResourceKeys {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, prefix := range []string{"file:", "dir:", "tree:", "cwd:", "tool:", "writable_root:"} {
			value = strings.TrimPrefix(value, prefix)
		}
		if value == "" {
			continue
		}
		preview = append(preview, value)
		if len(preview) == 3 {
			break
		}
	}
	return strings.Join(preview, ", ")
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
		// Internal execution batches are intentionally quiet. User-facing plans
		// are printed from the update_plan tool result below.
	case taskrun.ProgressKindApprovalRequired:
		if update.ApprovalRequest != nil {
			p.printBlock(localizedText(p.language,
				fmt.Sprintf("[approval] %s -> %s", update.ApprovalRequest.Name, localizedToolStatusCLI(p.language, string(update.ApprovalRequest.Status))),
				fmt.Sprintf("[подтверждение] %s -> %s", update.ApprovalRequest.Name, localizedToolStatusCLI(p.language, string(update.ApprovalRequest.Status))),
			))
		}
	case taskrun.ProgressKindToolResult:
		if update.ToolResult != nil {
			if body := strings.TrimSpace(tui.RenderToolResultText(p.language, update.ToolResult)); body != "" {
				p.printBlock(body)
			} else {
				p.printBlock(localizedText(p.language,
					fmt.Sprintf("%s -> %s", update.ToolResult.Name, localizedToolStatusCLI(p.language, string(update.ToolResult.Status))),
					fmt.Sprintf("%s -> %s", update.ToolResult.Name, localizedToolStatusCLI(p.language, string(update.ToolResult.Status))),
				))
			}
		}
	case taskrun.ProgressKindRetryScheduled:
		if update.RetryAfter > 0 {
			p.printBlock(localizedText(p.language,
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

func (p *streamPrinter) printBlock(body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	if p.lineOpen {
		fmt.Println()
	}
	fmt.Println()
	for index, line := range strings.Split(body, "\n") {
		if index == 0 {
			fmt.Println("• " + line)
			continue
		}
		fmt.Println("  " + line)
	}
	fmt.Println()
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
