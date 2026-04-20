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
	language := currentCatalogLanguage()
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			fmt.Fprintf(os.Stderr, "%s: %s %q\n", localizedText(language, "features", "фичи"), localizedText(language, "unknown flag", "неизвестный флаг"), arg)
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
		Features: alphaFeatures(language),
	}

	if jsonOutput {
		return printJSON(payload)
	}

	fmt.Printf("Go Lavilas %s (%s)\n", payload.Version, payload.Channel)
	fmt.Println(localizedText(language, "Alpha feature matrix:", "Матрица alpha-функций:"))
	fmt.Printf("%-20s %-10s %-28s %s\n",
		localizedText(language, "feature", "функция"),
		localizedText(language, "status", "статус"),
		localizedText(language, "command", "команда"),
		localizedText(language, "summary", "сводка"),
	)
	for _, feature := range payload.Features {
		fmt.Printf("%-20s %-10s %-28s %s\n", feature.Name, feature.Status, feature.Command, feature.Summary)
	}
	return 0
}

func alphaFeatures(language CatalogLanguage) []alphaFeature {
	available := localizedText(language, "available", "доступно")
	return []alphaFeature{
		{
			Name:    "interactive_chat",
			Status:  available,
			Command: "chat",
			Summary: localizedText(language, "Bubble Tea TUI chat session with palette and session loading.", "TUI-чат на Bubble Tea с палитрой команд и загрузкой сессий."),
		},
		{
			Name:    "one_shot_tasks",
			Status:  available,
			Command: "run",
			Summary: localizedText(language, "Single prompt execution without entering chat.", "Разовый запуск запроса без входа в чат."),
		},
		{
			Name:    "session_resume",
			Status:  available,
			Command: "resume, fork",
			Summary: localizedText(language, "Resume or branch from stored sessions.", "Продолжение и ответвление сохранённых сессий."),
		},
		{
			Name:    "review_apply",
			Status:  available,
			Command: "review, apply",
			Summary: localizedText(language, "Review diffs and apply patches from stdin or file.", "Ревью diff и применение патчей из stdin или файла."),
		},
		{
			Name:    "tool_runtime",
			Status:  available,
			Command: "chat, run, resume, fork",
			Summary: localizedText(language, "Built-in shell, file, search, write, and patch tools.", "Встроенные инструменты shell, чтения, поиска, записи и патчей."),
		},
		{
			Name:    "account_state",
			Status:  available,
			Command: "login, logout, status",
			Summary: localizedText(language, "Manage provider credentials and inspect active state.", "Управление токенами провайдера и просмотр активного состояния."),
		},
		{
			Name:    "config_profiles",
			Status:  available,
			Command: "model, profiles, providers, settings",
			Summary: localizedText(language, "Manage model defaults, profiles, providers, and UI settings.", "Управление моделями, профилями, провайдерами и UI-настройками."),
		},
		{
			Name:    "runtime_checks",
			Status:  available,
			Command: "doctor",
			Summary: localizedText(language, "Inspect local environment and runtime health.", "Проверка окружения и состояния рантайма."),
		},
		{
			Name:    "shell_completion",
			Status:  available,
			Command: "completion",
			Summary: localizedText(language, "Generate bash, zsh, fish, and PowerShell scripts.", "Генерация скриптов автодополнения для bash, zsh, fish и PowerShell."),
		},
		{
			Name:    "feature_matrix",
			Status:  available,
			Command: "features",
			Summary: localizedText(language, "Show this alpha capability summary in text or JSON.", "Показ этой alpha-сводки в тексте или JSON."),
		},
	}
}
