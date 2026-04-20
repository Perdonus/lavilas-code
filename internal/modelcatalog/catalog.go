package modelcatalog

import (
	"path"
	"sort"
	"strings"
	"time"
)

const (
	legacyGeminiFlashModel     = "gemini-flash-latest"
	legacyGeminiFlashLiteModel = "gemini-flash-lite-latest"
	legacyGeminiProModel       = "gemini-pro-latest"
	canonicalGeminiFlashModel  = "gemini-2.5-flash"
	canonicalGeminiFlashLite   = "gemini-2.5-flash-lite"
	canonicalGeminiProModel    = "gemini-2.5-pro"
	mistralVibeCLIModel        = "mistral-vibe-cli"
	mistralVibeCLILatestModel  = "mistral-vibe-cli-latest"
	mistralVibeCLIToolsModel   = "mistral-vibe-cli-with-tools"
	mistralVibeCLIFastModel    = "mistral-vibe-cli-fast"
	mistralCanonicalMedium     = "mistral-medium-latest"
	mistralCanonicalFast       = "mistral-small-latest"
)

var modelFamilySuffixes = []string{"-with-tools", "-tools", "-latest", "-fast"}

type ReasoningLevel struct {
	Effort      string `json:"effort,omitempty"`
	Description string `json:"description,omitempty"`
}

type Model struct {
	Slug                     string           `json:"slug"`
	DisplayName              string           `json:"display_name,omitempty"`
	Description              string           `json:"description,omitempty"`
	DefaultReasoningLevel    string           `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []ReasoningLevel `json:"supported_reasoning_levels,omitempty"`
	Priority                 int              `json:"priority,omitempty"`
	ContextWindow            int              `json:"context_window,omitempty"`
	SupportsParallelTools    bool             `json:"supports_parallel_tool_calls,omitempty"`
	SupportsReasoningSummary bool             `json:"supports_reasoning_summaries,omitempty"`
}

type Snapshot struct {
	FetchedAt     time.Time `json:"fetched_at,omitempty"`
	ETag          string    `json:"etag,omitempty"`
	ClientVersion string    `json:"client_version,omitempty"`
	ProviderID    string    `json:"provider_id,omitempty"`
	ProviderName  string    `json:"provider_name,omitempty"`
	ProfileName   string    `json:"profile_name,omitempty"`
	Models        []Model   `json:"models"`
}

type FamilyGroup struct {
	Key    string
	Label  string
	Models []Model
}

type PresetGroup struct {
	Key    string
	Label  string
	Models []Model
}

type Catalog struct {
	snapshot   Snapshot
	providerID string
	exact      map[string]int
	normalized map[string]int
	canonical  map[string]int
	tails      map[string]int
}

func NewCatalog(providerID string, snapshot Snapshot) Catalog {
	resolvedProvider := NormalizeProviderID(providerID)
	if resolvedProvider == "" {
		resolvedProvider = NormalizeProviderID(snapshot.ProviderID)
	}

	cloned := Snapshot{
		FetchedAt:     snapshot.FetchedAt,
		ETag:          snapshot.ETag,
		ClientVersion: snapshot.ClientVersion,
		ProviderID:    snapshot.ProviderID,
		ProviderName:  snapshot.ProviderName,
		ProfileName:   snapshot.ProfileName,
		Models:        cloneModels(snapshot.Models),
	}

	catalog := Catalog{
		snapshot:   cloned,
		providerID: resolvedProvider,
		exact:      make(map[string]int),
		normalized: make(map[string]int),
		canonical:  make(map[string]int),
		tails:      make(map[string]int),
	}

	for index, model := range cloned.Models {
		storePreferredIndex(catalog.exact, strings.TrimSpace(model.Slug), cloned.Models, index)

		normalized := NormalizeModelSlug(resolvedProvider, model.Slug)
		if normalized != "" {
			storePreferredIndex(catalog.normalized, normalized, cloned.Models, index)
		}

		canonical := CanonicalizeProviderModelSlug(normalized)
		if canonical != "" {
			storePreferredIndex(catalog.canonical, canonical, cloned.Models, index)
			storePreferredIndex(catalog.tails, modelTail(canonical), cloned.Models, index)
		}

		tail := modelTail(normalized)
		if tail != "" {
			storePreferredIndex(catalog.tails, tail, cloned.Models, index)
		}
	}

	return catalog
}

func (c Catalog) Snapshot() Snapshot {
	cloned := c.snapshot
	cloned.Models = cloneModels(c.snapshot.Models)
	return cloned
}

func (c Catalog) ProviderID() string {
	return c.providerID
}

func (c Catalog) Models() []Model {
	return cloneModels(c.snapshot.Models)
}

func (c Catalog) Lookup(slug string) (Model, bool) {
	if model, ok := c.lookupIndex(strings.TrimSpace(slug)); ok {
		return model, true
	}

	normalized := NormalizeModelSlug(c.providerID, slug)
	if model, ok := c.lookupIndex(normalized); ok {
		return model, true
	}

	canonical := CanonicalizeProviderModelSlug(normalized)
	if model, ok := c.lookupIndex(canonical); ok {
		return model, true
	}

	if tail := modelTail(normalized); tail != "" {
		if model, ok := c.lookupTail(tail); ok {
			return model, true
		}
	}

	return Model{}, false
}

func (c Catalog) FamilyGroups() []FamilyGroup {
	groupMap := make(map[string][]Model)
	for _, model := range c.snapshot.Models {
		key := FamilyKey(c.providerID, model.Slug)
		groupMap[key] = append(groupMap[key], model)
	}

	keys := make([]string, 0, len(groupMap))
	for key := range groupMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]FamilyGroup, 0, len(keys))
	for _, key := range keys {
		models := cloneModels(groupMap[key])
		sortModels(models)
		result = append(result, FamilyGroup{
			Key:    key,
			Label:  displayLabelFromKey(key),
			Models: models,
		})
	}

	return result
}

func (c Catalog) PresetGroups() []PresetGroup {
	ordered := []struct {
		Key   string
		Label string
	}{
		{Key: "fast", Label: "Fast"},
		{Key: "balanced", Label: "Balanced"},
		{Key: "power", Label: "Power"},
	}

	buckets := map[string][]Model{
		"fast":     {},
		"balanced": {},
		"power":    {},
	}

	for _, model := range c.snapshot.Models {
		bucket := classifyPresetBucket(model)
		buckets[bucket] = append(buckets[bucket], model)
	}

	result := make([]PresetGroup, 0, len(ordered))
	for _, entry := range ordered {
		if len(buckets[entry.Key]) == 0 {
			continue
		}
		models := cloneModels(buckets[entry.Key])
		sortModels(models)
		result = append(result, PresetGroup{
			Key:    entry.Key,
			Label:  entry.Label,
			Models: models,
		})
	}
	return result
}

func NormalizeProviderID(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.Join(strings.Fields(normalized), "-")

	switch {
	case normalized == "":
		return ""
	case normalized == "openai-api" || normalized == "openaiapi":
		return "openai"
	case strings.Contains(normalized, "openrouter"):
		return "openrouter"
	case strings.Contains(normalized, "anthropic") || strings.Contains(normalized, "claude"):
		return "anthropic"
	case strings.Contains(normalized, "gemini") || strings.Contains(normalized, "google") || strings.Contains(normalized, "generativelanguage"):
		return "gemini"
	case strings.Contains(normalized, "mistral"):
		return "mistral"
	case strings.Contains(normalized, "deepseek"):
		return "deepseek"
	case strings.Contains(normalized, "grok") || strings.Contains(normalized, "xai"):
		return "xai"
	default:
		return normalized
	}
}

func NormalizeModelSlug(providerID, slug string) string {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return ""
	}

	providerID = NormalizeProviderID(providerID)
	if providerID == "gemini" {
		prefix, tail := splitNamespacedSlug(trimmed)
		tail = stripGeminiModelsPrefix(tail)
		if alias := NormalizeProviderModelAliasSlug(tail); alias != "" {
			tail = modelTail(alias)
		}
		tail = strings.ToLower(strings.TrimSpace(tail))
		if prefix != "" {
			return prefix + "/" + tail
		}
		return tail
	}

	if alias := NormalizeProviderModelAliasSlug(trimmed); alias != "" {
		return alias
	}

	return trimmed
}

func NormalizeProviderModelAliasSlug(slug string) string {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return ""
	}

	prefix, tail := splitNamespacedSlug(trimmed)
	switch strings.ToLower(strings.TrimSpace(tail)) {
	case legacyGeminiFlashModel:
		tail = canonicalGeminiFlashModel
	case legacyGeminiFlashLiteModel:
		tail = canonicalGeminiFlashLite
	case legacyGeminiProModel:
		tail = canonicalGeminiProModel
	default:
		return ""
	}

	if prefix != "" {
		return prefix + "/" + tail
	}
	return tail
}

func CanonicalizeProviderModelSlug(slug string) string {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return ""
	}

	prefix, tail := splitNamespacedSlug(trimmed)
	lowerTail := strings.ToLower(strings.TrimSpace(tail))

	var canonical string
	switch lowerTail {
	case mistralVibeCLIFastModel:
		canonical = mistralCanonicalFast
	case mistralVibeCLIModel, mistralVibeCLILatestModel, mistralVibeCLIToolsModel:
		canonical = mistralCanonicalMedium
	default:
		if strings.HasPrefix(lowerTail, mistralVibeCLIModel) {
			switch {
			case strings.HasSuffix(lowerTail, "-fast"):
				canonical = mistralCanonicalFast
			case strings.HasSuffix(lowerTail, "-latest"), strings.HasSuffix(lowerTail, "-with-tools"), strings.HasSuffix(lowerTail, "-tools"):
				canonical = mistralCanonicalMedium
			}
		}
	}

	if canonical == "" {
		return trimmed
	}
	if prefix != "" {
		return prefix + "/" + canonical
	}
	return canonical
}

func FamilyKey(providerID, slug string) string {
	normalized := NormalizeModelSlug(providerID, slug)
	if normalized == "" {
		return ""
	}
	canonical := CanonicalizeProviderModelSlug(normalized)
	prefix, tail := splitNamespacedSlug(canonical)
	for _, suffix := range modelFamilySuffixes {
		if base := strings.TrimSuffix(tail, suffix); base != "" && base != tail {
			tail = base
			break
		}
	}

	if prefix != "" {
		return prefix + "/" + tail
	}
	return tail
}

func cloneModels(models []Model) []Model {
	cloned := make([]Model, len(models))
	copy(cloned, models)
	return cloned
}

func (c Catalog) lookupIndex(key string) (Model, bool) {
	if key == "" {
		return Model{}, false
	}
	if index, ok := c.exact[key]; ok {
		return c.snapshot.Models[index], true
	}
	if index, ok := c.normalized[key]; ok {
		return c.snapshot.Models[index], true
	}
	if index, ok := c.canonical[key]; ok {
		return c.snapshot.Models[index], true
	}
	return Model{}, false
}

func (c Catalog) lookupTail(tail string) (Model, bool) {
	if index, ok := c.tails[tail]; ok {
		return c.snapshot.Models[index], true
	}
	return Model{}, false
}

func storePreferredIndex(indexMap map[string]int, key string, models []Model, candidate int) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}

	existing, ok := indexMap[key]
	if !ok || preferredModel(models[candidate], models[existing]) {
		indexMap[key] = candidate
	}
}

func preferredModel(candidate, existing Model) bool {
	if candidate.Priority != existing.Priority {
		return candidate.Priority < existing.Priority
	}

	candidateName := strings.ToLower(displayName(candidate))
	existingName := strings.ToLower(displayName(existing))
	if candidateName != existingName {
		return candidateName < existingName
	}

	return candidate.Slug < existing.Slug
}

func sortModels(models []Model) {
	sort.SliceStable(models, func(left, right int) bool {
		return preferredModel(models[left], models[right])
	})
}

func displayName(model Model) string {
	if strings.TrimSpace(model.DisplayName) != "" {
		return strings.TrimSpace(model.DisplayName)
	}
	return strings.TrimSpace(model.Slug)
}

func displayLabelFromKey(key string) string {
	if key == "" {
		return "Other"
	}

	prefix, tail := splitNamespacedSlug(key)
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return "Other"
	}

	parts := strings.FieldsFunc(tail, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for index, part := range parts {
		parts[index] = titleSlugPart(part)
	}
	label := strings.Join(parts, " ")
	if prefix == "" {
		return label
	}
	return titleSlugPart(prefix) + " / " + label
}

func classifyPresetBucket(model Model) string {
	slug := strings.ToLower(strings.TrimSpace(model.Slug))
	name := strings.ToLower(strings.TrimSpace(model.DisplayName))
	text := slug + " " + name

	switch {
	case containsAny(text, "flash", "lite", "mini", "fast", "small", "haiku", "instant"):
		return "fast"
	case containsAny(text, "opus", "pro", "large", "max", "ultra", "xlarge"):
		return "power"
	default:
		return "balanced"
	}
}

func containsAny(input string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(input, fragment) {
			return true
		}
	}
	return false
}

func splitNamespacedSlug(slug string) (string, string) {
	prefix, tail, ok := strings.Cut(strings.TrimSpace(slug), "/")
	if !ok {
		return "", strings.TrimSpace(slug)
	}
	return strings.TrimSpace(prefix), strings.TrimSpace(tail)
}

func stripGeminiModelsPrefix(slug string) string {
	if strings.HasPrefix(strings.ToLower(slug), "models/") {
		return slug[len("models/"):]
	}
	return slug
}

func modelTail(slug string) string {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return ""
	}
	return path.Base(trimmed)
}

func titleSlugPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return ""
	}

	switch strings.ToLower(part) {
	case "api":
		return "API"
	case "cli":
		return "CLI"
	case "xai":
		return "xAI"
	}

	runes := []rune(strings.ToLower(part))
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}
