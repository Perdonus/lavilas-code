package taskrun

import "testing"

func TestProviderEndpoint_DefaultRoot(t *testing.T) {
	base, path, err := providerEndpoint("https://api.openai.com")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://api.openai.com" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_VersionedBase(t *testing.T) {
	base, path, err := providerEndpoint("https://api.mistral.ai/v1")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://api.mistral.ai/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_FullChatCompletionsURL(t *testing.T) {
	base, path, err := providerEndpoint("https://example.com/custom/v1/chat/completions")
	if err != nil {
		t.Fatalf("providerEndpoint: %v", err)
	}
	if base != "https://example.com/custom/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/chat/completions" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestProviderEndpoint_RejectsResponsesEndpoint(t *testing.T) {
	_, _, err := providerEndpoint("https://api.openai.com/v1/responses")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResponsesEndpoint_DefaultRoot(t *testing.T) {
	base, path, err := responsesEndpoint("https://api.openai.com")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://api.openai.com" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/v1/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestResponsesEndpoint_VersionedBase(t *testing.T) {
	base, path, err := responsesEndpoint("https://api.openai.com/v1")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://api.openai.com/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestResponsesEndpoint_FullResponsesURL(t *testing.T) {
	base, path, err := responsesEndpoint("https://example.com/custom/v1/responses")
	if err != nil {
		t.Fatalf("responsesEndpoint: %v", err)
	}
	if base != "https://example.com/custom/v1" {
		t.Fatalf("unexpected base: %s", base)
	}
	if path != "/responses" {
		t.Fatalf("unexpected path: %s", path)
	}
}
