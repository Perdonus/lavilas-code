package tooling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

type approvalKeyPayload struct {
	Tool             string         `json:"tool"`
	Target           string         `json:"target,omitempty"`
	WorkingDirectory string         `json:"working_directory,omitempty"`
	Command          string         `json:"command,omitempty"`
	SideEffectKind   SideEffectKind `json:"side_effect_kind,omitempty"`
}

func approvalKeysForCall(name string, arguments json.RawMessage, metadata ToolExecutionMetadata) []string {
	normalizedName := normalizeToolName(name)
	targets := approvalTargetsForCall(normalizedName, arguments, metadata)
	if len(targets) == 0 {
		targets = []approvalKeyPayload{{
			Tool:             normalizedName,
			WorkingDirectory: metadata.WorkingDirectory,
			SideEffectKind:   metadata.SideEffectKind,
		}}
	}

	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		if key := marshalApprovalKey(target); key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		keys = append(keys, marshalApprovalKey(approvalKeyPayload{
			Tool:             normalizedName,
			WorkingDirectory: metadata.WorkingDirectory,
			SideEffectKind:   metadata.SideEffectKind,
		}))
	}
	sort.Strings(keys)
	return dedupeStrings(keys)
}

func approvalTargetsForCall(name string, arguments json.RawMessage, metadata ToolExecutionMetadata) []approvalKeyPayload {
	switch name {
	case "run_shell_command":
		var args shellArgs
		if err := decodeArgs(arguments, &args); err == nil {
			return []approvalKeyPayload{{
				Tool:             name,
				WorkingDirectory: metadata.WorkingDirectory,
				Command:          strings.TrimSpace(args.Cmd),
				SideEffectKind:   metadata.SideEffectKind,
			}}
		}
	case "apply_patch":
		var args patchArgs
		if err := decodeArgs(arguments, &args); err == nil {
			paths := patchTouchedPaths(args.Patch)
			if len(paths) > 0 {
				targets := make([]approvalKeyPayload, 0, len(paths))
				for _, path := range paths {
					targets = append(targets, approvalKeyPayload{
						Tool:             name,
						Target:           path,
						WorkingDirectory: metadata.WorkingDirectory,
						SideEffectKind:   metadata.SideEffectKind,
					})
				}
				return targets
			}
		}
	}

	if len(metadata.ResourceKeys) == 0 {
		return nil
	}
	targets := make([]approvalKeyPayload, 0, len(metadata.ResourceKeys))
	for _, resource := range metadata.ResourceKeys {
		targets = append(targets, approvalKeyPayload{
			Tool:             name,
			Target:           strings.TrimSpace(resource),
			WorkingDirectory: metadata.WorkingDirectory,
			SideEffectKind:   metadata.SideEffectKind,
		})
	}
	return targets
}

func approvalIDForKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return "appr_" + hex.EncodeToString(sum[:8])
}

func marshalApprovalKey(payload approvalKeyPayload) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	dedupe := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := dedupe[value]; ok {
			continue
		}
		dedupe[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
