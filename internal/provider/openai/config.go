package openai

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL             = "https://api.openai.com"
	DefaultChatCompletionsPath = "/v1/chat/completions"
	DefaultTimeout             = 60 * time.Second
	DefaultUserAgent           = "lavilas-go/provider-openai"
)

type Config struct {
	Name                   string
	BaseURL                string
	ChatCompletionsPath    string
	APIKey                 string
	Organization           string
	Project                string
	DefaultModel           string
	DefaultReasoningEffort string
	Headers                map[string]string
	HTTPClient             *http.Client
	Timeout                time.Duration
	UserAgent              string
}

func (c Config) Validate() error {
	normalized := c.withDefaults()

	parsed, err := url.Parse(normalized.BaseURL)
	if err != nil {
		return fmt.Errorf("parse base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("base url must include scheme and host")
	}
	return nil
}

func (c Config) Endpoint() string {
	normalized := c.withDefaults()
	path := normalized.ChatCompletionsPath
	if strings.TrimSpace(path) == "" {
		return normalized.BaseURL
	}

	parsed, err := url.Parse(normalized.BaseURL)
	if err != nil {
		base := strings.TrimRight(normalized.BaseURL, "/")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return base + path
	}

	joinedBasePath := strings.TrimRight(parsed.Path, "/")
	joinedSuffix := strings.TrimLeft(path, "/")
	switch {
	case joinedBasePath == "":
		parsed.Path = "/" + joinedSuffix
	case joinedSuffix == "":
		parsed.Path = joinedBasePath
	default:
		parsed.Path = joinedBasePath + "/" + joinedSuffix
	}
	return parsed.String()
}

func (c Config) HTTPClientOrDefault() *http.Client {
	normalized := c.withDefaults()
	if normalized.HTTPClient != nil {
		return normalized.HTTPClient
	}
	return &http.Client{Timeout: normalized.Timeout}
}

func (c Config) BuildHeaders(accept string) http.Header {
	normalized := c.withDefaults()
	if strings.TrimSpace(accept) == "" {
		accept = "application/json"
	}

	headers := make(http.Header, len(normalized.Headers)+5)
	headers.Set("Accept", accept)
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", normalized.UserAgent)
	if normalized.APIKey != "" {
		headers.Set("Authorization", "Bearer "+normalized.APIKey)
	}
	if normalized.Organization != "" {
		headers.Set("OpenAI-Organization", normalized.Organization)
	}
	if normalized.Project != "" {
		headers.Set("OpenAI-Project", normalized.Project)
	}
	for key, value := range normalized.Headers {
		headers.Set(key, value)
	}
	return headers
}

func (c Config) withDefaults() Config {
	result := c
	if strings.TrimSpace(result.Name) == "" {
		result.Name = "openai-compatible"
	}
	if strings.TrimSpace(result.BaseURL) == "" {
		result.BaseURL = DefaultBaseURL
	}
	if strings.TrimSpace(result.ChatCompletionsPath) == "" {
		result.ChatCompletionsPath = DefaultChatCompletionsPath
	}
	if result.Timeout <= 0 {
		result.Timeout = DefaultTimeout
	}
	if strings.TrimSpace(result.UserAgent) == "" {
		result.UserAgent = DefaultUserAgent
	}
	if result.Headers == nil {
		result.Headers = map[string]string{}
	}
	return result
}
