package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type AuthFile struct {
	AuthMode     string `json:"auth_mode,omitempty"`
	OpenAIAPIKey string `json:"OPENAI_API_KEY,omitempty"`
}

func LoadOpenAIAPIKey(authPath string) (string, error) {
	authPath = strings.TrimSpace(authPath)
	if authPath == "" {
		return "", nil
	}
	data, err := os.ReadFile(authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var payload AuthFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.OpenAIAPIKey), nil
}
