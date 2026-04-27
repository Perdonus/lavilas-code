package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/version"
)

const packageID = "lvls"

type CheckResult struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	PackageSpec    string
	NVPath         string
}

func PackageSpec() string {
	channel := strings.TrimSpace(version.Channel)
	if channel == "" || channel == "latest" {
		return packageID + "@latest"
	}
	return packageID + "@" + channel
}

func Check(ctx context.Context) (CheckResult, error) {
	result := CheckResult{
		CurrentVersion: strings.TrimSpace(version.Version),
		PackageSpec:    PackageSpec(),
	}
	nvPath, err := EnsureNV(ctx)
	if err != nil {
		return result, err
	}
	result.NVPath = nvPath
	latest, err := LatestVersion(ctx, nvPath, result.PackageSpec)
	if err != nil {
		return result, err
	}
	result.LatestVersion = latest
	result.Available = IsNewerVersion(latest, result.CurrentVersion)
	return result, nil
}

func EnsureNV(ctx context.Context) (string, error) {
	if path, err := exec.LookPath("nv"); err == nil && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if path := findNVInCommonLocations(); path != "" {
		return path, nil
	}
	installCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := installNV(installCtx); err != nil {
		return "", err
	}
	if path, err := exec.LookPath("nv"); err == nil && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if path := findNVInCommonLocations(); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("nv installed but executable was not found in PATH")
}

func LatestVersion(ctx context.Context, nvPath string, packageSpec string) (string, error) {
	nvPath = strings.TrimSpace(nvPath)
	if nvPath == "" {
		return "", fmt.Errorf("nv executable path is empty")
	}
	packageSpec = strings.TrimSpace(packageSpec)
	if packageSpec == "" {
		packageSpec = PackageSpec()
	}
	cmd := exec.CommandContext(ctx, nvPath, "view", packageSpec, "version", "--json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("nv view %s: %s", packageSpec, message)
	}
	versionValue := decodeNVVersion(stdout.Bytes())
	if versionValue == "" {
		return "", fmt.Errorf("nv view %s returned no version", packageSpec)
	}
	return versionValue, nil
}

func Install(ctx context.Context, nvPath string, packageSpec string) error {
	var err error
	if strings.TrimSpace(nvPath) == "" {
		nvPath, err = EnsureNV(ctx)
		if err != nil {
			return err
		}
	}
	packageSpec = strings.TrimSpace(packageSpec)
	if packageSpec == "" {
		packageSpec = PackageSpec()
	}
	cmd := exec.CommandContext(ctx, nvPath, "install", packageSpec)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type InstallResult struct {
	Scheduled bool
	Script    string
}

func InstallOrSchedule(ctx context.Context, nvPath string, packageSpec string) (InstallResult, error) {
	if runtime.GOOS != "windows" {
		return InstallResult{}, Install(ctx, nvPath, packageSpec)
	}
	script, err := scheduleWindowsInstall(ctx, nvPath, packageSpec)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Scheduled: true, Script: script}, nil
}

func scheduleWindowsInstall(ctx context.Context, nvPath string, packageSpec string) (string, error) {
	var err error
	if strings.TrimSpace(nvPath) == "" {
		nvPath, err = EnsureNV(ctx)
		if err != nil {
			return "", err
		}
	}
	packageSpec = strings.TrimSpace(packageSpec)
	if packageSpec == "" {
		packageSpec = PackageSpec()
	}
	pid := os.Getpid()
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("lvls-update-%d.cmd", pid))
	script := strings.Join([]string{
		"@echo off",
		"setlocal",
		"title Go Lavilas update",
		fmt.Sprintf("set \"LOG=%%TEMP%%\\lvls-update-%d.log\"", pid),
		":wait",
		fmt.Sprintf("tasklist /FI \"PID eq %d\" 2>NUL | find \"%d\" >NUL", pid, pid),
		"if not errorlevel 1 (",
		"  timeout /t 1 /nobreak >NUL",
		"  goto wait",
		")",
		fmt.Sprintf("%s install %s > \"%%LOG%%\" 2>&1", cmdFileQuote(nvPath), cmdFileQuote(packageSpec)),
		"set \"code=%ERRORLEVEL%\"",
		"del \"%~f0\" >NUL 2>NUL",
		"exit /b %code%",
		"",
	}, "\r\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "cmd", "/C", "start", "", "/min", "cmd", "/C", scriptPath)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return scriptPath, nil
}

func cmdFileQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func installNV(ctx context.Context) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://sosiskibot.ru/install/nv.ps1 | iex")
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", "curl -fsSL https://sosiskibot.ru/install/nv.sh | sh")
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install nv: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

func findNVInCommonLocations() string {
	candidates := make([]string, 0, 6)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if runtime.GOOS == "windows" {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "nv.exe"))
		} else {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "nv"))
		}
	}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); strings.TrimSpace(localAppData) != "" {
			candidates = append(candidates, filepath.Join(localAppData, "NV", "nv.exe"))
			candidates = append(candidates, filepath.Join(localAppData, "Nv", "nv.exe"))
		}
	} else {
		candidates = append(candidates, "/usr/local/bin/nv", "/usr/bin/nv")
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func decodeNVVersion(data []byte) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	if value := decodeVersionField(raw["version"]); value != "" {
		return value
	}
	if value := decodeVersionField(raw["latest_version"]); value != "" {
		return value
	}
	if variantsRaw, ok := raw["variants"]; ok {
		var variants []struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(variantsRaw, &variants) == nil {
			for _, variant := range variants {
				if value := strings.TrimSpace(variant.Version); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func decodeVersionField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var object struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return strings.TrimSpace(object.Version)
	}
	return ""
}

func IsNewerVersion(latest string, current string) bool {
	latest = normalizeVersionString(latest)
	current = normalizeVersionString(current)
	if latest == "" || current == "" || latest == current {
		return false
	}
	if cmp, ok := compareVersions(latest, current); ok {
		return cmp > 0
	}
	return latest != current
}

type parsedVersion struct {
	Numbers []int
	PreName string
	PreNum  int
	HasPre  bool
}

func compareVersions(left string, right string) (int, bool) {
	l, okL := parseVersion(left)
	r, okR := parseVersion(right)
	if !okL || !okR {
		return 0, false
	}
	maxLen := len(l.Numbers)
	if len(r.Numbers) > maxLen {
		maxLen = len(r.Numbers)
	}
	for index := 0; index < maxLen; index++ {
		ln := 0
		rn := 0
		if index < len(l.Numbers) {
			ln = l.Numbers[index]
		}
		if index < len(r.Numbers) {
			rn = r.Numbers[index]
		}
		if ln > rn {
			return 1, true
		}
		if ln < rn {
			return -1, true
		}
	}
	if !l.HasPre && r.HasPre {
		return 1, true
	}
	if l.HasPre && !r.HasPre {
		return -1, true
	}
	if l.HasPre && r.HasPre {
		if l.PreName > r.PreName {
			return 1, true
		}
		if l.PreName < r.PreName {
			return -1, true
		}
		if l.PreNum > r.PreNum {
			return 1, true
		}
		if l.PreNum < r.PreNum {
			return -1, true
		}
	}
	return 0, true
}

func parseVersion(value string) (parsedVersion, bool) {
	value = normalizeVersionString(value)
	if value == "" {
		return parsedVersion{}, false
	}
	base := value
	pre := ""
	if before, after, ok := strings.Cut(value, "-"); ok {
		base = before
		pre = after
	}
	parts := strings.Split(base, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return parsedVersion{}, false
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return parsedVersion{}, false
		}
		numbers = append(numbers, number)
	}
	parsed := parsedVersion{Numbers: numbers}
	if pre != "" {
		parsed.HasPre = true
		segments := strings.Split(pre, ".")
		parsed.PreName = segments[0]
		if len(segments) > 1 {
			parsed.PreNum, _ = strconv.Atoi(segments[len(segments)-1])
		} else if name, number, ok := strings.Cut(parsed.PreName, "."); ok {
			parsed.PreName = name
			parsed.PreNum, _ = strconv.Atoi(number)
		} else if idx := strings.LastIndex(parsed.PreName, "alpha"); idx >= 0 {
			_ = idx
		}
		if strings.Contains(parsed.PreName, "alpha.") {
			parsed.PreName = strings.ReplaceAll(parsed.PreName, "alpha.", "alpha")
		}
		if strings.HasPrefix(parsed.PreName, "alpha") && parsed.PreNum == 0 {
			suffix := strings.TrimPrefix(parsed.PreName, "alpha")
			if n, err := strconv.Atoi(strings.Trim(suffix, ".-")); err == nil {
				parsed.PreName = "alpha"
				parsed.PreNum = n
			}
		}
	}
	return parsed, len(numbers) > 0
}

func normalizeVersionString(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}
