package state

import (
	"bufio"
	"bytes"
	"os"
	"sort"
	"strconv"
	"strings"
)

type ConfigSummary struct {
	Model          string
	Reasoning      string
	Profiles       []string
	ModelProviders []string
}

type Config struct {
	Path           string
	Fields         ConfigFields
	Model          ModelConfig
	Profiles       []ProfileConfig
	ModelProviders []ProviderConfig
	Sections       []ConfigSection

	fieldOrder []string
}

type ModelConfig struct {
	Name            string
	ReasoningEffort string
	Profile         string
	Fields          ConfigFields
	fieldOrder      []string
}

type ProfileConfig struct {
	Name            string
	Model           string
	Provider        string
	ReasoningEffort string
	Fields          ConfigFields
	fieldOrder      []string
}

type ProviderConfig struct {
	Name       string
	Type       string
	BaseURL    string
	APIKeyEnv  string
	WireAPI    string
	Fields     ConfigFields
	fieldOrder []string
}

type ConfigSection struct {
	Path       []string
	Fields     ConfigFields
	fieldOrder []string
}

type ConfigFields map[string]ConfigValue

type ConfigValue struct {
	Raw   string
	value any
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	config, err := ParseConfig(data)
	if err != nil {
		return Config{}, err
	}
	config.Path = path
	return config, nil
}

func ParseConfig(data []byte) (Config, error) {
	config := Config{
		Fields: make(ConfigFields),
		Model: ModelConfig{
			Fields: make(ConfigFields),
		},
	}

	profileIndex := make(map[string]int)
	providerIndex := make(map[string]int)
	sectionIndex := make(map[string]int)

	currentTarget := configTargetRoot
	currentIndex := -1

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = trimInlineComment(line)
		if line == "" {
			continue
		}

		if path, ok := parseSectionHeader(line); ok {
			switch {
			case len(path) == 2 && path[0] == "profiles":
				currentTarget = configTargetProfile
				currentIndex = ensureProfile(&config, profileIndex, path[1])
			case len(path) == 2 && path[0] == "model_providers":
				currentTarget = configTargetProvider
				currentIndex = ensureProvider(&config, providerIndex, path[1])
			default:
				currentTarget = configTargetSection
				currentIndex = ensureSection(&config, sectionIndex, path)
			}
			continue
		}

		key, rawValue, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		value := parseConfigValue(rawValue)

		switch currentTarget {
		case configTargetProfile:
			config.Profiles[currentIndex].setField(key, value)
		case configTargetProvider:
			config.ModelProviders[currentIndex].setField(key, value)
		case configTargetSection:
			config.Sections[currentIndex].setField(key, value)
		default:
			config.setRootField(key, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func LoadConfigSummary(path string) (ConfigSummary, error) {
	config, err := LoadConfig(path)
	if err != nil {
		return ConfigSummary{}, err
	}
	return config.Summary(), nil
}

func SaveConfig(path string, config Config) error {
	data, err := config.Encode()
	if err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o644)
}

func (c Config) Summary() ConfigSummary {
	return ConfigSummary{
		Model:          c.Model.Name,
		Reasoning:      c.Model.ReasoningEffort,
		Profiles:       c.ProfileNames(),
		ModelProviders: c.ModelProviderNames(),
	}
}

func (c Config) Clone() Config {
	profiles := make([]ProfileConfig, len(c.Profiles))
	for index := range c.Profiles {
		profiles[index] = c.Profiles[index].clone()
	}

	providers := make([]ProviderConfig, len(c.ModelProviders))
	for index := range c.ModelProviders {
		providers[index] = c.ModelProviders[index].clone()
	}

	sections := make([]ConfigSection, len(c.Sections))
	for index := range c.Sections {
		sections[index] = c.Sections[index].clone()
	}

	return Config{
		Path:           c.Path,
		Fields:         c.Fields.Clone(),
		Model:          c.Model.clone(),
		Profiles:       profiles,
		ModelProviders: providers,
		Sections:       sections,
		fieldOrder:     cloneStrings(c.fieldOrder),
	}
}

func (c Config) Profile(name string) (ProfileConfig, bool) {
	for _, profile := range c.Profiles {
		if profile.Name == name {
			return profile.clone(), true
		}
	}
	return ProfileConfig{}, false
}

func (c Config) Provider(name string) (ProviderConfig, bool) {
	for _, provider := range c.ModelProviders {
		if provider.Name == name {
			return provider.clone(), true
		}
	}
	return ProviderConfig{}, false
}

func (c Config) Section(path ...string) (ConfigSection, bool) {
	for _, section := range c.Sections {
		if samePath(section.Path, path) {
			return section.clone(), true
		}
	}
	return ConfigSection{}, false
}

func (c Config) ProfileNames() []string {
	result := make([]string, 0, len(c.Profiles))
	for _, profile := range c.Profiles {
		result = append(result, profile.Name)
	}
	return result
}

func (c Config) ProfilesForProvider(name string) []ProfileConfig {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	result := make([]ProfileConfig, 0)
	for _, profile := range c.Profiles {
		if strings.TrimSpace(profile.EffectiveProviderName()) != name {
			continue
		}
		result = append(result, profile.clone())
	}
	return result
}

func (c Config) ModelProviderNames() []string {
	result := make([]string, 0, len(c.ModelProviders))
	for _, provider := range c.ModelProviders {
		result = append(result, provider.Name)
	}
	return result
}

func (c *Config) SetActiveProfile(name string) {
	c.Model.Profile = strings.TrimSpace(name)
	c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, "profile")
}

func (c *Config) SetModel(name string) {
	c.Model.Name = strings.TrimSpace(name)
	c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, "model")
}

func (c *Config) SetModelProvider(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		c.Model.Fields.Delete("model_provider")
		c.Model.fieldOrder = removeKey(c.Model.fieldOrder, "model_provider")
		return
	}
	c.Model.Fields.Set("model_provider", StringConfigValue(name))
	c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, "model_provider")
}

func (c *Config) SetReasoningEffort(value string) {
	c.Model.ReasoningEffort = strings.TrimSpace(value)
	c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, "model_reasoning_effort")
}

func (c *Config) UpsertProfile(profile ProfileConfig) {
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		return
	}
	for index := range c.Profiles {
		if c.Profiles[index].Name == name {
			c.Profiles[index] = profile.clone()
			c.Profiles[index].Name = name
			return
		}
	}
	profile.Name = name
	if profile.Fields == nil {
		profile.Fields = make(ConfigFields)
	}
	c.Profiles = append(c.Profiles, profile)
}

func (c *Config) DeleteProfile(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for index := range c.Profiles {
		if c.Profiles[index].Name != name {
			continue
		}
		c.Profiles = append(c.Profiles[:index], c.Profiles[index+1:]...)
		if c.ActiveProfileName() == name {
			c.Model.Profile = ""
			c.Model.fieldOrder = removeKey(c.Model.fieldOrder, "profile")
		}
		return true
	}
	return false
}

func (c *Config) UpsertProvider(provider ProviderConfig) {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		return
	}
	for index := range c.ModelProviders {
		if c.ModelProviders[index].Name == name {
			c.ModelProviders[index] = provider.clone()
			c.ModelProviders[index].Name = name
			return
		}
	}
	provider.Name = name
	if provider.Fields == nil {
		provider.Fields = make(ConfigFields)
	}
	c.ModelProviders = append(c.ModelProviders, provider)
}

func (c *Config) DeleteProvider(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for index := range c.ModelProviders {
		if c.ModelProviders[index].Name != name {
			continue
		}
		c.ModelProviders = append(c.ModelProviders[:index], c.ModelProviders[index+1:]...)
		for profileIndex := range c.Profiles {
			if c.Profiles[profileIndex].Provider == name {
				c.Profiles[profileIndex].Provider = ""
				c.Profiles[profileIndex].fieldOrder = removeKey(c.Profiles[profileIndex].fieldOrder, "model_provider")
				c.Profiles[profileIndex].fieldOrder = removeKey(c.Profiles[profileIndex].fieldOrder, "provider")
			}
		}
		if c.EffectiveProviderName() == name {
			c.Model.Fields.Delete("model_provider")
			c.Model.fieldOrder = removeKey(c.Model.fieldOrder, "model_provider")
		}
		return true
	}
	return false
}

func (c Config) ActiveProfileName() string {
	return strings.TrimSpace(c.Model.Profile)
}

func (c Config) ActiveProfile() (ProfileConfig, bool) {
	name := c.ActiveProfileName()
	if name == "" {
		return ProfileConfig{}, false
	}
	return c.Profile(name)
}

func (c Config) EffectiveModel() string {
	if profile, ok := c.ActiveProfile(); ok && strings.TrimSpace(profile.Model) != "" {
		return strings.TrimSpace(profile.Model)
	}
	return strings.TrimSpace(c.Model.Name)
}

func (c Config) EffectiveReasoningEffort() string {
	if profile, ok := c.ActiveProfile(); ok && strings.TrimSpace(profile.ReasoningEffort) != "" {
		return strings.TrimSpace(profile.ReasoningEffort)
	}
	return strings.TrimSpace(c.Model.ReasoningEffort)
}

func (c Config) EffectiveProviderName() string {
	if profile, ok := c.ActiveProfile(); ok {
		if provider := strings.TrimSpace(profile.EffectiveProviderName()); provider != "" {
			return provider
		}
	}
	return strings.TrimSpace(c.Model.Fields.Text("model_provider"))
}

func (c Config) EffectiveProvider() (ProviderConfig, bool) {
	name := c.EffectiveProviderName()
	if name == "" {
		return ProviderConfig{}, false
	}
	return c.Provider(name)
}

func (p ProviderConfig) DisplayName() string {
	if value := strings.TrimSpace(p.Fields.Text("name")); value != "" {
		return value
	}
	if value := strings.TrimSpace(p.Name); value != "" {
		return value
	}
	return "provider"
}

func (p ProviderConfig) BearerToken() string {
	if value := strings.TrimSpace(p.Fields.Text("experimental_bearer_token")); value != "" {
		return value
	}
	if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
		return strings.TrimSpace(os.Getenv(env))
	}
	return ""
}

func (c Config) Encode() ([]byte, error) {
	blocks := make([]string, 0, 1+len(c.Profiles)+len(c.ModelProviders)+len(c.Sections))
	if root := c.encodeRootBlock(); root != "" {
		blocks = append(blocks, root)
	}
	for _, profile := range c.Profiles {
		blocks = append(blocks, encodeSectionBlock([]string{"profiles", profile.Name}, profile.encodeLines()))
	}
	for _, provider := range c.ModelProviders {
		blocks = append(blocks, encodeSectionBlock([]string{"model_providers", provider.Name}, provider.encodeLines()))
	}
	for _, section := range c.Sections {
		blocks = append(blocks, encodeSectionBlock(section.Path, section.encodeLines()))
	}
	return []byte(strings.Join(blocks, "\n\n")), nil
}

func (m ModelConfig) clone() ModelConfig {
	return ModelConfig{
		Name:            m.Name,
		ReasoningEffort: m.ReasoningEffort,
		Profile:         m.Profile,
		Fields:          m.Fields.Clone(),
		fieldOrder:      cloneStrings(m.fieldOrder),
	}
}

func (p ProfileConfig) clone() ProfileConfig {
	return ProfileConfig{
		Name:            p.Name,
		Model:           p.Model,
		Provider:        p.Provider,
		ReasoningEffort: p.ReasoningEffort,
		Fields:          p.Fields.Clone(),
		fieldOrder:      cloneStrings(p.fieldOrder),
	}
}

func (p ProfileConfig) EffectiveProviderName() string {
	if value := strings.TrimSpace(p.Provider); value != "" {
		return value
	}
	if value := strings.TrimSpace(p.Fields.Text("model_provider")); value != "" {
		return value
	}
	return strings.TrimSpace(p.Fields.Text("provider"))
}

func (p ProfileConfig) CatalogPath() string {
	return strings.TrimSpace(p.Fields.Text("model_catalog_json"))
}

func (p *ProfileConfig) SetModel(value string) {
	p.Model = strings.TrimSpace(value)
	p.fieldOrder = appendUnique(p.fieldOrder, "model")
}

func (p *ProfileConfig) SetProvider(value string) {
	p.Provider = strings.TrimSpace(value)
	if p.Provider == "" {
		p.Fields.Delete("model_provider")
		p.Fields.Delete("provider")
		p.fieldOrder = removeKey(p.fieldOrder, "model_provider")
		p.fieldOrder = removeKey(p.fieldOrder, "provider")
		return
	}
	key := profileProviderKey(p.fieldOrder)
	p.fieldOrder = removeKey(p.fieldOrder, alternateProfileProviderKey(key))
	p.fieldOrder = appendUnique(p.fieldOrder, key)
}

func (p *ProfileConfig) SetReasoningEffort(value string) {
	p.ReasoningEffort = strings.TrimSpace(value)
	if p.ReasoningEffort == "" {
		p.fieldOrder = removeKey(p.fieldOrder, "model_reasoning_effort")
		return
	}
	p.fieldOrder = appendUnique(p.fieldOrder, "model_reasoning_effort")
}

func (p *ProfileConfig) SetCatalogPath(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		p.Fields.Delete("model_catalog_json")
		p.fieldOrder = removeKey(p.fieldOrder, "model_catalog_json")
		return
	}
	p.Fields.Set("model_catalog_json", StringConfigValue(value))
	p.fieldOrder = appendUnique(p.fieldOrder, "model_catalog_json")
}

func (p ProviderConfig) clone() ProviderConfig {
	return ProviderConfig{
		Name:       p.Name,
		Type:       p.Type,
		BaseURL:    p.BaseURL,
		APIKeyEnv:  p.APIKeyEnv,
		WireAPI:    p.WireAPI,
		Fields:     p.Fields.Clone(),
		fieldOrder: cloneStrings(p.fieldOrder),
	}
}

func (s ConfigSection) clone() ConfigSection {
	return ConfigSection{
		Path:       cloneStrings(s.Path),
		Fields:     s.Fields.Clone(),
		fieldOrder: cloneStrings(s.fieldOrder),
	}
}

func (s ConfigSection) Name() string {
	return joinSectionPath(s.Path)
}

func (s *ConfigSection) setField(key string, value ConfigValue) {
	s.Fields.Set(key, value)
	s.fieldOrder = appendUnique(s.fieldOrder, key)
}

func (s ConfigSection) encodeLines() []string {
	lines := make([]string, 0, len(s.Fields))
	for _, key := range orderedKeys(s.fieldOrder, s.Fields) {
		lines = append(lines, encodeFieldLine(key, s.Fields[key]))
	}
	return lines
}

func (f ConfigFields) Clone() ConfigFields {
	if len(f) == 0 {
		return nil
	}
	result := make(ConfigFields, len(f))
	for key, value := range f {
		result[key] = value.Clone()
	}
	return result
}

func (f *ConfigFields) Set(key string, value ConfigValue) {
	if strings.TrimSpace(key) == "" {
		return
	}
	if *f == nil {
		*f = make(ConfigFields)
	}
	(*f)[key] = value
}

func (f ConfigFields) Delete(key string) {
	delete(f, key)
}

func (f ConfigFields) Get(key string) (ConfigValue, bool) {
	value, ok := f[key]
	if !ok {
		return ConfigValue{}, false
	}
	return value, true
}

func (f ConfigFields) Text(key string) string {
	value, ok := f[key]
	if !ok {
		return ""
	}
	text, _ := value.Text()
	return text
}

func (f ConfigFields) Bool(key string) (bool, bool) {
	value, ok := f[key]
	if !ok {
		return false, false
	}
	return value.Bool()
}

func (f ConfigFields) Int64(key string) (int64, bool) {
	value, ok := f[key]
	if !ok {
		return 0, false
	}
	return value.Int64()
}

func (f ConfigFields) Float64(key string) (float64, bool) {
	value, ok := f[key]
	if !ok {
		return 0, false
	}
	return value.Float64()
}

func (f ConfigFields) Array(key string) ([]ConfigValue, bool) {
	value, ok := f[key]
	if !ok {
		return nil, false
	}
	return value.Array()
}

func (f ConfigFields) Keys() []string {
	result := make([]string, 0, len(f))
	for key := range f {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func StringConfigValue(value string) ConfigValue {
	return ConfigValue{value: value}
}

func BoolConfigValue(value bool) ConfigValue {
	return ConfigValue{value: value}
}

func IntConfigValue(value int64) ConfigValue {
	return ConfigValue{value: value}
}

func FloatConfigValue(value float64) ConfigValue {
	return ConfigValue{value: value}
}

func ArrayConfigValue(values ...ConfigValue) ConfigValue {
	cloned := make([]ConfigValue, len(values))
	for index := range values {
		cloned[index] = values[index].Clone()
	}
	return ConfigValue{value: cloned}
}

func (v ConfigValue) Clone() ConfigValue {
	cloned := ConfigValue{Raw: v.Raw}
	switch current := v.value.(type) {
	case []ConfigValue:
		values := make([]ConfigValue, len(current))
		for index := range current {
			values[index] = current[index].Clone()
		}
		cloned.value = values
	default:
		cloned.value = current
	}
	return cloned
}

func (v ConfigValue) Interface() any {
	switch current := v.value.(type) {
	case []ConfigValue:
		values := make([]ConfigValue, len(current))
		for index := range current {
			values[index] = current[index].Clone()
		}
		return values
	default:
		return current
	}
}

func (v ConfigValue) Text() (string, bool) {
	current, ok := v.value.(string)
	return current, ok
}

func (v ConfigValue) Bool() (bool, bool) {
	current, ok := v.value.(bool)
	return current, ok
}

func (v ConfigValue) Int64() (int64, bool) {
	current, ok := v.value.(int64)
	return current, ok
}

func (v ConfigValue) Float64() (float64, bool) {
	switch current := v.value.(type) {
	case float64:
		return current, true
	case int64:
		return float64(current), true
	default:
		return 0, false
	}
}

func (v ConfigValue) Array() ([]ConfigValue, bool) {
	current, ok := v.value.([]ConfigValue)
	if !ok {
		return nil, false
	}
	values := make([]ConfigValue, len(current))
	for index := range current {
		values[index] = current[index].Clone()
	}
	return values, true
}

func (v ConfigValue) String() string {
	if text, ok := v.Text(); ok {
		return text
	}
	return v.Encode()
}

func (v ConfigValue) Encode() string {
	if strings.TrimSpace(v.Raw) != "" {
		return strings.TrimSpace(v.Raw)
	}
	return encodeAnonymousValue(v)
}

func (c *Config) setRootField(key string, value ConfigValue) {
	switch key {
	case "model":
		c.Model.Name = value.String()
		c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, key)
	case "model_reasoning_effort":
		c.Model.ReasoningEffort = value.String()
		c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, key)
	case "profile":
		c.Model.Profile = value.String()
		c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, key)
	default:
		if strings.HasPrefix(key, "model_") {
			c.Model.Fields.Set(key, value)
			c.Model.fieldOrder = appendUnique(c.Model.fieldOrder, key)
			return
		}
		c.Fields.Set(key, value)
		c.fieldOrder = appendUnique(c.fieldOrder, key)
	}
}

func (p *ProfileConfig) setField(key string, value ConfigValue) {
	switch key {
	case "model":
		p.Model = value.String()
	case "model_provider", "provider":
		p.Provider = value.String()
	case "model_reasoning_effort":
		p.ReasoningEffort = value.String()
	default:
		p.Fields.Set(key, value)
	}
	p.fieldOrder = appendUnique(p.fieldOrder, key)
}

func (p ProfileConfig) encodeLines() []string {
	known := map[string]ConfigValue{}
	if p.Model != "" || containsKey(p.fieldOrder, "model") {
		known["model"] = StringConfigValue(p.Model)
	}
	if p.Provider != "" || containsKey(p.fieldOrder, "model_provider") || containsKey(p.fieldOrder, "provider") {
		known[profileProviderKey(p.fieldOrder)] = StringConfigValue(p.Provider)
	}
	if p.ReasoningEffort != "" || containsKey(p.fieldOrder, "model_reasoning_effort") {
		known["model_reasoning_effort"] = StringConfigValue(p.ReasoningEffort)
	}
	return encodeOrderedLines(p.fieldOrder, known, p.Fields)
}

func (p *ProviderConfig) setField(key string, value ConfigValue) {
	switch key {
	case "type":
		p.Type = value.String()
	case "base_url":
		p.BaseURL = value.String()
	case "env_key":
		p.APIKeyEnv = value.String()
	case "wire_api":
		p.WireAPI = value.String()
	default:
		p.Fields.Set(key, value)
	}
	p.fieldOrder = appendUnique(p.fieldOrder, key)
}

func (p ProviderConfig) encodeLines() []string {
	known := map[string]ConfigValue{}
	if p.Type != "" || containsKey(p.fieldOrder, "type") {
		known["type"] = StringConfigValue(p.Type)
	}
	if p.BaseURL != "" || containsKey(p.fieldOrder, "base_url") {
		known["base_url"] = StringConfigValue(p.BaseURL)
	}
	if p.APIKeyEnv != "" || containsKey(p.fieldOrder, "env_key") {
		known["env_key"] = StringConfigValue(p.APIKeyEnv)
	}
	if p.WireAPI != "" || containsKey(p.fieldOrder, "wire_api") {
		known["wire_api"] = StringConfigValue(p.WireAPI)
	}
	return encodeOrderedLines(p.fieldOrder, known, p.Fields)
}

func (c Config) encodeRootBlock() string {
	known := map[string]ConfigValue{}
	if c.Model.Profile != "" || containsKey(c.Model.fieldOrder, "profile") {
		known["profile"] = StringConfigValue(c.Model.Profile)
	}
	if c.Model.Name != "" || containsKey(c.Model.fieldOrder, "model") {
		known["model"] = StringConfigValue(c.Model.Name)
	}
	if c.Model.ReasoningEffort != "" || containsKey(c.Model.fieldOrder, "model_reasoning_effort") {
		known["model_reasoning_effort"] = StringConfigValue(c.Model.ReasoningEffort)
	}

	lines := make([]string, 0, len(c.Fields)+len(c.Model.Fields)+len(known))
	for _, key := range orderedKeys(c.fieldOrder, c.Fields) {
		lines = append(lines, encodeFieldLine(key, c.Fields[key]))
	}
	lines = append(lines, encodeOrderedLines(c.Model.fieldOrder, known, c.Model.Fields)...)
	return strings.Join(lines, "\n")
}

func encodeOrderedLines(order []string, known map[string]ConfigValue, extras ConfigFields) []string {
	lines := make([]string, 0, len(order)+len(extras)+len(known))
	seen := make(map[string]struct{}, len(order))
	for _, key := range order {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if value, ok := known[key]; ok {
			lines = append(lines, encodeFieldLine(key, value))
			continue
		}
		if value, ok := extras[key]; ok {
			lines = append(lines, encodeFieldLine(key, value))
		}
	}

	for _, key := range orderedMapKeys(known) {
		if _, exists := seen[key]; exists {
			continue
		}
		lines = append(lines, encodeFieldLine(key, known[key]))
		seen[key] = struct{}{}
	}

	for _, key := range orderedKeys(order, extras) {
		if _, exists := seen[key]; exists {
			continue
		}
		lines = append(lines, encodeFieldLine(key, extras[key]))
		seen[key] = struct{}{}
	}
	return lines
}

func encodeSectionBlock(path []string, lines []string) string {
	header := "[" + joinSectionPath(path) + "]"
	if len(lines) == 0 {
		return header
	}
	return header + "\n" + strings.Join(lines, "\n")
}

func encodeFieldLine(key string, value ConfigValue) string {
	return key + " = " + value.Encode()
}

func encodeAnonymousValue(value ConfigValue) string {
	switch current := value.Interface().(type) {
	case nil:
		return `""`
	case string:
		return strconv.Quote(current)
	case bool:
		if current {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(current, 10)
	case float64:
		return strconv.FormatFloat(current, 'g', -1, 64)
	case []ConfigValue:
		parts := make([]string, 0, len(current))
		for _, item := range current {
			parts = append(parts, item.Encode())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return strconv.Quote(value.String())
	}
}

func parseConfigValue(raw string) ConfigValue {
	raw = trimInlineComment(raw)
	if raw == "" {
		return StringConfigValue("")
	}

	if text, ok := unquoteTOMLString(raw); ok {
		return ConfigValue{Raw: raw, value: text}
	}

	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		if values, ok := parseArray(raw[1 : len(raw)-1]); ok {
			return ConfigValue{Raw: raw, value: values}
		}
	}

	if raw == "true" || raw == "false" {
		return ConfigValue{Raw: raw, value: raw == "true"}
	}

	if integer, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return ConfigValue{Raw: raw, value: integer}
	}

	if number, err := strconv.ParseFloat(raw, 64); err == nil && strings.ContainsAny(raw, ".eE") {
		return ConfigValue{Raw: raw, value: number}
	}

	return ConfigValue{Raw: raw, value: raw}
}

func parseArray(body string) ([]ConfigValue, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return []ConfigValue{}, true
	}

	parts := splitDelimited(body, ',')
	if len(parts) == 0 {
		return nil, false
	}

	values := make([]ConfigValue, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, parseConfigValue(part))
	}
	return values, true
}

func parseSectionHeader(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return nil, false
	}
	body := strings.TrimSpace(line[1 : len(line)-1])
	if body == "" {
		return nil, false
	}
	path := splitDottedPath(body)
	if len(path) == 0 {
		return nil, false
	}
	return path, true
}

func splitKeyValue(line string) (string, string, bool) {
	inSingle := false
	inDouble := false
	escape := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		switch {
		case escape:
			escape = false
		case inDouble && char == '\\':
			escape = true
		case char == '"' && !inSingle:
			inDouble = !inDouble
		case char == '\'' && !inDouble:
			inSingle = !inSingle
		case char == '=' && !inSingle && !inDouble:
			key := strings.TrimSpace(line[:index])
			value := trimInlineComment(line[index+1:])
			return key, value, key != ""
		}
	}
	return "", "", false
}

func trimInlineComment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	inSingle := false
	inDouble := false
	escape := false
	arrayDepth := 0
	braceDepth := 0

	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case escape:
			escape = false
		case inDouble && char == '\\':
			escape = true
		case char == '"' && !inSingle:
			inDouble = !inDouble
		case char == '\'' && !inDouble:
			inSingle = !inSingle
		case !inSingle && !inDouble && char == '[':
			arrayDepth++
		case !inSingle && !inDouble && char == ']':
			if arrayDepth > 0 {
				arrayDepth--
			}
		case !inSingle && !inDouble && char == '{':
			braceDepth++
		case !inSingle && !inDouble && char == '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case char == '#' && !inSingle && !inDouble && arrayDepth == 0 && braceDepth == 0:
			return strings.TrimSpace(value[:index])
		}
	}

	return strings.TrimSpace(value)
}

func splitDottedPath(value string) []string {
	parts := splitDelimited(value, '.')
	if len(parts) == 0 {
		return nil
	}

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil
		}
		if text, ok := unquoteTOMLString(part); ok {
			part = text
		}
		if part == "" {
			return nil
		}
		result = append(result, part)
	}
	return result
}

func splitDelimited(value string, delimiter byte) []string {
	parts := []string{}
	start := 0
	inSingle := false
	inDouble := false
	escape := false
	arrayDepth := 0
	braceDepth := 0

	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case escape:
			escape = false
		case inDouble && char == '\\':
			escape = true
		case char == '"' && !inSingle:
			inDouble = !inDouble
		case char == '\'' && !inDouble:
			inSingle = !inSingle
		case !inSingle && !inDouble && char == '[':
			arrayDepth++
		case !inSingle && !inDouble && char == ']':
			if arrayDepth > 0 {
				arrayDepth--
			}
		case !inSingle && !inDouble && char == '{':
			braceDepth++
		case !inSingle && !inDouble && char == '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case char == delimiter && !inSingle && !inDouble && arrayDepth == 0 && braceDepth == 0:
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func unquoteTOMLString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return "", false
	}
	if value[0] == '"' && value[len(value)-1] == '"' {
		result, err := strconv.Unquote(value)
		if err != nil {
			return "", false
		}
		return result, true
	}
	if value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], true
	}
	return "", false
}

func ensureProfile(config *Config, index map[string]int, name string) int {
	if existing, ok := index[name]; ok {
		return existing
	}
	config.Profiles = append(config.Profiles, ProfileConfig{
		Name:   name,
		Fields: make(ConfigFields),
	})
	current := len(config.Profiles) - 1
	index[name] = current
	return current
}

func ensureProvider(config *Config, index map[string]int, name string) int {
	if existing, ok := index[name]; ok {
		return existing
	}
	config.ModelProviders = append(config.ModelProviders, ProviderConfig{
		Name:   name,
		Fields: make(ConfigFields),
	})
	current := len(config.ModelProviders) - 1
	index[name] = current
	return current
}

func ensureSection(config *Config, index map[string]int, path []string) int {
	key := strings.Join(path, "\x00")
	if existing, ok := index[key]; ok {
		return existing
	}
	config.Sections = append(config.Sections, ConfigSection{
		Path:   cloneStrings(path),
		Fields: make(ConfigFields),
	})
	current := len(config.Sections) - 1
	index[key] = current
	return current
}

func orderedKeys(order []string, values ConfigFields) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(order))
	for _, key := range order {
		if _, ok := values[key]; !ok {
			continue
		}
		if _, duplicated := seen[key]; duplicated {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}

	extra := make([]string, 0, len(values))
	for key := range values {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	return append(result, extra...)
}

func orderedMapKeys(values map[string]ConfigValue) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func joinSectionPath(path []string) string {
	parts := make([]string, 0, len(path))
	for _, part := range path {
		parts = append(parts, formatSectionPart(part))
	}
	return strings.Join(parts, ".")
}

func formatSectionPart(value string) string {
	if value == "" {
		return `""`
	}
	for _, char := range value {
		if !isBareSectionChar(char) {
			return strconv.Quote(value)
		}
	}
	return value
}

func isBareSectionChar(char rune) bool {
	return char == '-' || char == '_' ||
		(char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

func appendUnique(values []string, value string) []string {
	if containsKey(values, value) {
		return values
	}
	return append(values, value)
}

func removeKey(values []string, value string) []string {
	if len(values) == 0 {
		return values
	}
	filtered := values[:0]
	for _, current := range values {
		if current != value {
			filtered = append(filtered, current)
		}
	}
	return filtered
}

func containsKey(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func profileProviderKey(order []string) string {
	if containsKey(order, "provider") && !containsKey(order, "model_provider") {
		return "provider"
	}
	return "model_provider"
}

func alternateProfileProviderKey(key string) string {
	if key == "provider" {
		return "model_provider"
	}
	return "provider"
}

func samePath(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type configTarget int

const (
	configTargetRoot configTarget = iota
	configTargetProfile
	configTargetProvider
	configTargetSection
)
