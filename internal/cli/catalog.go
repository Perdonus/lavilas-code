package cli

import (
	"fmt"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
)

type CatalogLanguage = commandcatalog.CatalogLanguage

const (
	CatalogLanguageAuto    = commandcatalog.CatalogLanguageAuto
	CatalogLanguageEnglish = commandcatalog.CatalogLanguageEnglish
	CatalogLanguageRussian = commandcatalog.CatalogLanguageRussian
	CatalogLanguageUnknown = commandcatalog.CatalogLanguageUnknown
)

type CatalogLocale = commandcatalog.CatalogLocale
type CatalogEntry = commandcatalog.CatalogEntry
type CatalogMatch = commandcatalog.CatalogMatch
type CatalogListOptions = commandcatalog.CatalogListOptions
type CatalogListItem = commandcatalog.CatalogListItem
type CommandCatalog = commandcatalog.CommandCatalog
type CatalogMatchResult = commandcatalog.CatalogMatchResult

func Catalog() *CommandCatalog {
	return commandcatalog.Catalog()
}

func DetectCatalogLanguage(value string) CatalogLanguage {
	return commandcatalog.DetectCatalogLanguage(value)
}

func CatalogCategoryLabel(category string, language CatalogLanguage) string {
	return commandcatalog.CatalogCategoryLabel(category, language)
}

func newCommandCatalog(entries []CatalogEntry) *CommandCatalog {
	return commandcatalog.NewCommandCatalog(entries)
}

func registry() []Command {
	descriptors := Catalog().Commands()
	commands := make([]Command, 0, len(descriptors))
	for _, descriptor := range descriptors {
		commands = append(commands, commandFromCatalogDescriptor(descriptor))
	}
	return commands
}

func commandFromCatalogDescriptor(descriptor commandcatalog.Command) Command {
	run, ok := catalogCommandRun(descriptor.Name)
	if !ok {
		panic(fmt.Sprintf("cli registry: missing run handler for %q", descriptor.Name))
	}
	return Command{
		Name:        descriptor.Name,
		Aliases:     append([]string(nil), descriptor.Aliases...),
		Description: descriptor.Description,
		Category:    descriptor.Category,
		Run:         run,
	}
}

func catalogCommandRun(name string) (func(args []string) int, bool) {
	switch name {
	case "chat":
		return runChat, true
	case "resume":
		return runResume, true
	case "fork":
		return runFork, true
	case "run":
		return runTask, true
	case "review":
		return runReview, true
	case "apply":
		return runApply, true
	case "login":
		return runLogin, true
	case "logout":
		return runLogout, true
	case "status":
		return runStatus, true
	case "profiles":
		return runProfiles, true
	case "providers":
		return runProviders, true
	case "model":
		return runModel, true
	case "settings":
		return runSettings, true
	case "completion":
		return runCompletion, true
	case "features":
		return runFeatures, true
	case "doctor":
		return runDoctor, true
	default:
		return nil, false
	}
}

func catalogDisplayNames(items []CatalogListItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.DisplayName) != "" {
			names = append(names, item.DisplayName)
			continue
		}
		names = append(names, item.Name)
	}
	return names
}

func catalogLookupAliases(item CatalogListItem) []string {
	aliases := append([]string{}, item.Aliases...)
	aliases = append(aliases, item.MirrorAliases...)
	if value := strings.TrimSpace(item.InsertName); value != "" {
		aliases = append(aliases, value)
	}
	if value := strings.TrimSpace(item.MirrorName); value != "" {
		aliases = append(aliases, value)
	}
	return uniqueCatalogAliases(aliases)
}

func uniqueCatalogAliases(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
