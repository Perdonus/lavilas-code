package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
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
	lookup := buildLookup(commands)

	if len(argv) == 0 {
		if isInteractiveTerminal() {
			return runChat(nil)
		}
		printBanner()
		printCommands(commands)
		return 0
	}

	switch argv[0] {
	case "--version", "-v", "version", "версия":
		fmt.Printf("Go Lavilas %s (%s)\n", version.Version, version.Channel)
		return 0
	case "help", "помощь", "--help", "-h":
		printBanner()
		printCommands(commands)
		return 0
	}

	return runCommand(commands, lookup, argv, true)
}

func buildLookup(commands []Command) map[string]Command {
	lookup := map[string]Command{}
	for _, cmd := range commands {
		lookup[cmd.Name] = cmd
		for _, alias := range cmd.Aliases {
			lookup[alias] = cmd
		}
	}
	return lookup
}

func runCommand(commands []Command, lookup map[string]Command, argv []string, allowPromptFallback bool) int {
	cmd, ok := lookup[argv[0]]
	if !ok {
		if allowPromptFallback && !strings.HasPrefix(argv[0], "-") {
			return runTask(argv)
		}
		language := currentCatalogLanguage()
		fmt.Fprintf(os.Stderr, "%s\n\n", localizedText(language, fmt.Sprintf("Unknown command: %s", argv[0]), fmt.Sprintf("Неизвестная команда: %s", argv[0])))
		printCommands(commands)
		return 2
	}
	return cmd.Run(argv[1:])
}

func printBanner() {
	language := currentCatalogLanguage()
	fmt.Printf("Go Lavilas %s (%s)\n", version.Version, version.Channel)
	fmt.Println(localizedText(language, "Standalone Go runtime for NV alpha.", "Независимый go-контур для NV alpha."))
	fmt.Println()
}

func printCommands(commands []Command) {
	_ = commands
	language := currentCatalogLanguage()
	categoryOrder := []string{
		"interactive",
		"account",
		"config",
		"automation",
		"runtime",
		"debug",
	}
	byCategory := map[string][]CatalogListItem{}
	for _, item := range Catalog().List(CatalogListOptions{Language: language}) {
		byCategory[item.Category] = append(byCategory[item.Category], item)
	}

	fmt.Println(localizedText(language, "Commands:", "Команды:"))
	for _, category := range categoryOrder {
		items := byCategory[category]
		if len(items) == 0 {
			continue
		}
		fmt.Printf("  %s:\n", CatalogCategoryLabel(category, language))
		for _, item := range items {
			aliases := ""
			if len(item.Aliases) > 0 {
				aliases = fmt.Sprintf(" [%s]", strings.Join(item.Aliases, ", "))
			}
			fmt.Printf("    %-14s%s %s\n", item.Name, aliases, item.Description)
		}
	}
}

func currentCatalogLanguage() CatalogLanguage {
	settings, err := loadSettingsOptional(apphome.SettingsPath())
	if err != nil {
		return CatalogLanguageEnglish
	}
	switch strings.ToLower(strings.TrimSpace(settings.Language)) {
	case string(commandcatalog.CatalogLanguageRussian):
		return CatalogLanguageRussian
	case string(commandcatalog.CatalogLanguageEnglish):
		return CatalogLanguageEnglish
	default:
		return CatalogLanguageEnglish
	}
}

func valueCatalogLanguage(value string) CatalogLanguage {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(commandcatalog.CatalogLanguageRussian), "русский":
		return CatalogLanguageRussian
	default:
		return CatalogLanguageEnglish
	}
}

func localizedText(language CatalogLanguage, english string, russian string) string {
	if language == CatalogLanguageRussian {
		return russian
	}
	return english
}

func localizedUnset(language CatalogLanguage) string {
	return localizedText(language, "<unset>", "<не задано>")
}

func currentCommandPrefix() string {
	settings, err := loadSettingsOptional(apphome.SettingsPath())
	if err != nil {
		return "/"
	}
	prefix := strings.TrimSpace(settings.CommandPrefix)
	if prefix == "" {
		return "/"
	}
	return prefix
}

func isInteractiveTerminal() bool {
	inputInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	outputInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (inputInfo.Mode()&os.ModeCharDevice) != 0 && (outputInfo.Mode()&os.ModeCharDevice) != 0
}
