package tooling

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

type PermissionGrantScope string

const (
	PermissionGrantScopeTurn    PermissionGrantScope = "turn"
	PermissionGrantScopeSession PermissionGrantScope = "session"
)

type PermissionGrant struct {
	WritableRoots []string `json:"writable_roots,omitempty"`
}

type requestPermissionsArgs struct {
	Reason      string                  `json:"reason,omitempty"`
	Permissions requestPermissionsScope `json:"permissions"`
}

type requestPermissionsScope struct {
	WritableRoots []string `json:"writable_roots,omitempty"`
}

func RequestedPermissionGrant(call ToolCallPlan) (PermissionGrant, string, error) {
	return decodeRequestPermissionsArgs(call.Arguments)
}

func PermissionGrantForApprovedCall(call ToolCallPlan) PermissionGrant {
	switch normalizeToolName(call.Name) {
	case "request_permissions":
		grant, _, err := decodeRequestPermissionsArgs(call.Arguments)
		if err != nil {
			return PermissionGrant{}
		}
		return grant
	case "write_file", "apply_patch":
		return PermissionGrant{WritableRoots: WriteTargetsForCall(call)}
	default:
		return PermissionGrant{}
	}
}

func WriteTargetsForCall(call ToolCallPlan) []string {
	switch normalizeToolName(call.Name) {
	case "write_file", "apply_patch":
		targets := make([]string, 0, len(call.Metadata.ResourceKeys))
		for _, resource := range call.Metadata.ResourceKeys {
			resource = strings.TrimSpace(resource)
			if !strings.HasPrefix(resource, "file:") {
				continue
			}
			targets = append(targets, strings.TrimSpace(strings.TrimPrefix(resource, "file:")))
		}
		return normalizeWritableRoots(targets)
	default:
		return nil
	}
}

func (g PermissionGrant) IsEmpty() bool {
	return len(g.WritableRoots) == 0
}

func (g PermissionGrant) Merge(other PermissionGrant) PermissionGrant {
	return PermissionGrant{
		WritableRoots: mergeWritableRoots(g.WritableRoots, other.WritableRoots),
	}
}

func (g PermissionGrant) MatchingWritableRoots(targets []string) []string {
	normalizedTargets := normalizeWritableRoots(targets)
	if len(normalizedTargets) == 0 || len(g.WritableRoots) == 0 {
		return nil
	}

	matched := make([]string, 0, len(normalizedTargets))
	for _, target := range normalizedTargets {
		found := ""
		for _, root := range g.WritableRoots {
			if !pathWithinRoot(target, root) {
				continue
			}
			found = root
			break
		}
		if found == "" {
			return nil
		}
		matched = append(matched, found)
	}
	return normalizeWritableRoots(matched)
}

func decodeRequestPermissionsArgs(arguments json.RawMessage) (PermissionGrant, string, error) {
	var args requestPermissionsArgs
	if err := decodeArgs(arguments, &args); err != nil {
		return PermissionGrant{}, "", err
	}
	grant := PermissionGrant{
		WritableRoots: normalizeWritableRoots(args.Permissions.WritableRoots),
	}
	if grant.IsEmpty() {
		return PermissionGrant{}, "", errMissingWritableRoots()
	}
	return grant, strings.TrimSpace(args.Reason), nil
}

func marshalRequestPermissionsResult(call ToolCallPlan) string {
	grant, reason, err := decodeRequestPermissionsArgs(call.Arguments)
	if err != nil {
		return marshalResult(map[string]any{
			"ok":    false,
			"tool":  "request_permissions",
			"error": err.Error(),
		})
	}

	grantedRoots := call.Metadata.GrantedWritableRoots
	if len(grantedRoots) == 0 {
		grantedRoots = grant.WritableRoots
	}
	payload := map[string]any{
		"ok":   true,
		"tool": "request_permissions",
		"granted_permissions": map[string]any{
			"writable_roots": grantedRoots,
		},
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if call.Metadata.PermissionGrantScope != "" {
		payload["scope"] = call.Metadata.PermissionGrantScope
	}
	return marshalResult(payload)
}

func normalizeWritableRoots(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	roots := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		roots = append(roots, normalizeResourcePath(value, "."))
	}
	if len(roots) == 0 {
		return nil
	}
	sort.Strings(roots)
	return dedupeStrings(roots)
}

func mergeWritableRoots(left []string, right []string) []string {
	combined := append(append([]string(nil), left...), right...)
	return normalizeWritableRoots(combined)
}

func pathWithinRoot(target string, root string) bool {
	target = strings.TrimSpace(target)
	root = strings.TrimSpace(root)
	if target == "" || root == "" {
		return false
	}
	if target == root {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func errMissingWritableRoots() error {
	return &permissionArgumentError{message: "request_permissions requires at least one writable root"}
}

type permissionArgumentError struct {
	message string
}

func (e *permissionArgumentError) Error() string {
	return e.message
}
