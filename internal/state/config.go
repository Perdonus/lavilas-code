package state

import (
	"bufio"
	"os"
	"strings"
)

type ConfigSummary struct {
	Model          string
	Reasoning      string
	Profiles       []string
	ModelProviders []string
}

func LoadConfigSummary(path string) (ConfigSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return ConfigSummary{}, err
	}
	defer file.Close()

	var result ConfigSummary
	currentSection := ""
	profilesSeen := map[string]struct{}{}
	providersSeen := map[string]struct{}{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if name, ok := sectionSuffix(currentSection, "profiles."); ok {
				if _, seen := profilesSeen[name]; !seen {
					profilesSeen[name] = struct{}{}
					result.Profiles = append(result.Profiles, name)
				}
			}
			if name, ok := sectionSuffix(currentSection, "model_providers."); ok {
				if _, seen := providersSeen[name]; !seen {
					providersSeen[name] = struct{}{}
					result.ModelProviders = append(result.ModelProviders, name)
				}
			}
			continue
		}

		if currentSection != "" {
			continue
		}

		key, value, ok := parseKeyValue(line)
		if !ok {
			continue
		}

		switch key {
		case "model":
			result.Model = value
		case "model_reasoning_effort":
			result.Reasoning = value
		}
	}

	if err := scanner.Err(); err != nil {
		return ConfigSummary{}, err
	}

	return result, nil
}

func sectionSuffix(section string, prefix string) (string, bool) {
	if !strings.HasPrefix(section, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(section, prefix)
	name = strings.Trim(name, `"`)
	return name, name != ""
}

func parseKeyValue(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"`)
	return key, value, key != ""
}
