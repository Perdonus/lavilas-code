package cli

import "testing"

func TestParseChatOptions(t *testing.T) {
	options, err := parseChatOptions([]string{
		"--model", "gpt-5",
		"--profile", "alpha",
		"--provider", "openai",
		"--reasoning", "high",
		"--system", "keep it terse",
	})
	if err != nil {
		t.Fatalf("parseChatOptions() error = %v", err)
	}

	if options.Model != "gpt-5" {
		t.Fatalf("Model = %q, want %q", options.Model, "gpt-5")
	}
	if options.Profile != "alpha" {
		t.Fatalf("Profile = %q, want %q", options.Profile, "alpha")
	}
	if options.Provider != "openai" {
		t.Fatalf("Provider = %q, want %q", options.Provider, "openai")
	}
	if options.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want %q", options.ReasoningEffort, "high")
	}
	if options.SystemPrompt != "keep it terse" {
		t.Fatalf("SystemPrompt = %q, want %q", options.SystemPrompt, "keep it terse")
	}
}

func TestParseChatOptionsRejectsUnexpectedArgs(t *testing.T) {
	if _, err := parseChatOptions([]string{"hello"}); err == nil {
		t.Fatal("parseChatOptions() error = nil, want error")
	}
	if _, err := parseChatOptions([]string{"--json"}); err == nil {
		t.Fatal("parseChatOptions() error = nil, want error")
	}
}

func TestReplCommandsStayBounded(t *testing.T) {
	commands := replCommands()
	seen := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		seen[cmd.Name] = struct{}{}
	}

	for _, name := range []string{"status", "model", "profiles", "providers", "settings"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing repl command %q", name)
		}
	}

	for _, name := range []string{"chat", "resume", "run", "login", "logout"} {
		if _, ok := seen[name]; ok {
			t.Fatalf("unexpected repl command %q", name)
		}
	}
}

func TestBuildReplLookupAcceptsBilingualAliases(t *testing.T) {
	lookup := buildReplLookup(replCommands())
	tests := map[string]string{
		"status":     "status",
		"статус":     "status",
		"model":      "model",
		"модель":     "model",
		"profiles":   "profiles",
		"профили":    "profiles",
		"providers":  "providers",
		"провайдеры": "providers",
		"settings":   "settings",
		"настройки":  "settings",
	}

	for key, want := range tests {
		command, ok := lookup[key]
		if !ok {
			t.Fatalf("lookup[%q] missing", key)
		}
		if command.Name != want {
			t.Fatalf("lookup[%q].Name = %q, want %q", key, command.Name, want)
		}
	}
}
