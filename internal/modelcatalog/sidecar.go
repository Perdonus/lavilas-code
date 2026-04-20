package modelcatalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/state"
)

var ErrCatalogNotFound = errors.New("model catalog not found")

type SidecarRef struct {
	ProfileName string
	Path        string
}

func ProfilesDir(codexHome string) string {
	return apphome.NewLayout(codexHome).ProfilesDir()
}

func DefaultSidecarPath(codexHome, profileName string) string {
	return filepath.Join(ProfilesDir(codexHome), strings.TrimSpace(profileName)+".models.json")
}

func ResolveProfileSidecarPath(profile state.ProfileConfig, codexHome string) string {
	if customPath := strings.TrimSpace(profile.Fields.Text("model_catalog_json")); customPath != "" {
		return expandPath(customPath, codexHome)
	}
	return DefaultSidecarPath(codexHome, profile.Name)
}

func ResolveConfigSidecarPath(config state.Config, profileName, codexHome string) (string, bool) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		profileName = strings.TrimSpace(config.ActiveProfileName())
	}
	if profileName == "" {
		profileName = strings.TrimSpace(config.Model.Profile)
	}
	if profileName == "" {
		return "", false
	}

	profile, ok := config.Profile(profileName)
	if !ok {
		return "", false
	}

	return ResolveProfileSidecarPath(profile, codexHome), true
}

func ProviderIDFromProfile(profile state.ProfileConfig) string {
	switch {
	case strings.TrimSpace(profile.Provider) != "":
		return NormalizeProviderID(profile.Provider)
	case strings.TrimSpace(profile.Fields.Text("model_provider")) != "":
		return NormalizeProviderID(profile.Fields.Text("model_provider"))
	case strings.TrimSpace(profile.Fields.Text("provider")) != "":
		return NormalizeProviderID(profile.Fields.Text("provider"))
	default:
		return ""
	}
}

func DiscoverSidecars(codexHome string) ([]SidecarRef, error) {
	entries, err := os.ReadDir(ProfilesDir(codexHome))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	result := make([]SidecarRef, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".models.json") {
			continue
		}
		result = append(result, SidecarRef{
			ProfileName: strings.TrimSuffix(name, ".models.json"),
			Path:        filepath.Join(ProfilesDir(codexHome), name),
		})
	}

	sort.Slice(result, func(left, right int) bool {
		return result[left].ProfileName < result[right].ProfileName
	})
	return result, nil
}

func LoadSnapshot(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, ErrCatalogNotFound
		}
		return Snapshot{}, err
	}
	return ParseSnapshot(data)
}

func LoadProfileSnapshot(profile state.ProfileConfig, codexHome string) (Snapshot, error) {
	snapshot, err := LoadSnapshot(ResolveProfileSidecarPath(profile, codexHome))
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.ProfileName == "" {
		snapshot.ProfileName = profile.Name
	}
	if snapshot.ProviderID == "" {
		snapshot.ProviderID = ProviderIDFromProfile(profile)
	}
	return snapshot, nil
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err == nil && snapshot.Models != nil {
		return snapshot, nil
	}

	var models []Model
	if err := json.Unmarshal(data, &models); err == nil {
		return Snapshot{Models: models}, nil
	}

	return Snapshot{}, json.Unmarshal(data, &snapshot)
}

func SaveSnapshot(path string, snapshot Snapshot) error {
	if snapshot.Models == nil {
		snapshot.Models = []Model{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".models-*.json")
	if err != nil {
		return err
	}

	tempPath := tempFile.Name()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func SaveProfileSnapshot(profile state.ProfileConfig, codexHome string, snapshot Snapshot) error {
	if snapshot.ProfileName == "" {
		snapshot.ProfileName = profile.Name
	}
	if snapshot.ProviderID == "" {
		snapshot.ProviderID = ProviderIDFromProfile(profile)
	}
	return SaveSnapshot(ResolveProfileSidecarPath(profile, codexHome), snapshot)
}

func expandPath(value, codexHome string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value == "~" {
		return apphome.HomeDir()
	}
	if strings.HasPrefix(value, "~/") {
		return filepath.Join(apphome.HomeDir(), strings.TrimPrefix(value, "~/"))
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(apphome.ResolveCodexHome(codexHome), value))
}
