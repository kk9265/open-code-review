// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

type dependencyCheck struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Revision    string   `json:"revision"`
	State       string   `json:"state"`
	Matches     []string `json:"matches,omitempty"`
	TreeEntries int      `json:"tree_entries"`
}

type dependencyAssumption struct {
	File         string `json:"file,omitempty"`
	DependencyID string `json:"dependency_id"`
	Behavior     string `json:"behavior"`
	Basis        string `json:"basis"`
}

func (r *reviewContext) checkDependencyFile(ctx context.Context, args map[string]any) (string, error) {
	file, _ := args["file"].(string)
	file = strings.ReplaceAll(strings.TrimSpace(file), "\\", "/")
	if file == "" || path.Ext(file) == "" || strings.ContainsAny(file, "*?[]:\x00\r\n") || strings.HasPrefix(file, "/") {
		return "", errors.New("Provide an explicit repository-relative dependency filename, such as util/util_config.h. A symbol search cannot prove a file is missing.")
	}
	for _, part := range strings.Split(file, "/") {
		if part == ".." {
			return "", errors.New("Dependency filenames must stay within the repository.")
		}
	}
	file = path.Clean(file)
	for _, saved := range r.activeRun.Dependencies {
		if strings.EqualFold(saved.File, file) {
			return dependencyCheckResult(saved), nil
		}
	}
	if !r.dependencyTreeLoaded {
		var output bytes.Buffer
		if err := runManagedGitWithOutput(ctx, nil, &output, "--git-dir="+r.path, "ls-tree", "-r", "-z", "--name-only", "--full-tree", r.head); err != nil {
			return "", fmt.Errorf("dependency tree enumeration failed; absence is not established: %w", err)
		}
		r.dependencyTree = strings.Split(strings.TrimSuffix(output.String(), "\x00"), "\x00")
		if output.Len() == 0 {
			r.dependencyTree = nil
		}
		r.dependencyTreeLoaded = true
	}
	check := dependencyCheck{ID: fmt.Sprintf("D%03d", len(r.activeRun.Dependencies)+1), File: file, Revision: r.head, State: "absent", TreeEntries: len(r.dependencyTree)}
	for _, candidate := range r.dependencyTree {
		if strings.EqualFold(path.Base(candidate), path.Base(file)) {
			check.Matches = append(check.Matches, candidate)
			check.State = "present"
		}
	}
	r.activeRun.Dependencies = append(r.activeRun.Dependencies, check)
	if err := r.saveNativeRun(ctx, *r.activeRun); err != nil {
		return "", err
	}
	return dependencyCheckResult(check), nil
}

func dependencyCheckResult(check dependencyCheck) string {
	data, _ := json.Marshal(check)
	if check.State == "absent" {
		return string(data) + "\nDEPENDENCY INVESTIGATION CLOSED: the exact filename is absent from the entire frozen repository tree, not just a search scope. No further search for this file or its implementation is needed. This does not prove a symbol or external implementation does not exist. Infer conventional correct behavior from visible declarations, call sites and API contracts, record the assumption using this dependency id, and continue the business review. Do not report a defect solely because implementation is unavailable. Repeated file requests reuse this result."
	}
	return string(data) + "\nThe file exists. Read matching source if its implementation matters; do not infer behavior on the premise that this file is absent."
}

var dependencyInclude = regexp.MustCompile(`(?m)^\d+\|[ \t]*#[ \t]*include[ \t]*[<"]([^>"\r\n]+)[>"]`)

func (r *reviewContext) checkReadDependencies(ctx context.Context, result string) string {
	if r.dependencyIncludes == nil {
		r.dependencyIncludes = map[string]bool{}
	}
	var notices []string
	for _, match := range dependencyInclude.FindAllStringSubmatch(result, -1) {
		file := path.Clean(strings.ReplaceAll(match[1], "\\", "/"))
		if path.Ext(file) == "" {
			continue
		}
		r.dependencyIncludes[file] = true
		checked, err := r.checkDependencyFile(ctx, map[string]any{"file": file})
		if err != nil {
			notices = append(notices, "Dependency absence was not established for "+file+": "+err.Error())
			continue
		}
		for _, saved := range r.activeRun.Dependencies {
			if strings.EqualFold(saved.File, file) && saved.State == "absent" {
				notices = append(notices, checked)
				break
			}
		}
	}
	if len(notices) == 0 {
		return result
	}
	return result + "\nAutomatic dependency checks from source includes (not source lines):\n" + strings.Join(notices, "\n")
}

// Only literal filenames or names already seen in source includes establish the dependency identity.
func (r *reviewContext) dependencyLookupShortcut(ctx context.Context, name string, args map[string]any, revision string) (string, bool, error) {
	if revision != r.head || r.activeRun == nil {
		return "", false, nil
	}
	file, _ := args["dependency_file"].(string)
	if file == "" {
		switch name {
		case "file_read":
			file, _ = args["file_path"].(string)
		case "file_find":
			query, _ := args["query_name"].(string)
			for _, candidate := range r.dependencyTree {
				if query != "" && strings.Contains(strings.ToLower(candidate), strings.ToLower(query)) {
					return "", false, nil
				}
			}
			if path.Ext(query) != "" && !strings.ContainsAny(query, "*?[]") {
				file = query
			} else if strings.TrimSpace(query) != "" {
				for include := range r.dependencyIncludes {
					if strings.EqualFold(strings.TrimSuffix(path.Base(include), path.Ext(include)), query) {
						file = include
						break
					}
				}
				for _, saved := range r.activeRun.Dependencies {
					if strings.EqualFold(strings.TrimSuffix(path.Base(saved.File), path.Ext(saved.File)), query) {
						file = saved.File
						break
					}
				}
			}
		case "code_search":
			if patterns, ok := args["file_patterns"].([]any); ok && len(patterns) == 1 {
				pattern, _ := patterns[0].(string)
				if path.Ext(pattern) != "" && !strings.ContainsAny(pattern, "*?[]") {
					file = pattern
				}
			}
		}
	}
	if file == "" {
		return "", false, nil
	}
	if path.Ext(file) == "" {
		return "", false, nil
	}
	result, err := r.checkDependencyFile(ctx, map[string]any{"file": file})
	if err != nil {
		return "", true, err
	}
	for _, saved := range r.activeRun.Dependencies {
		if strings.EqualFold(saved.File, path.Clean(strings.ReplaceAll(file, "\\", "/"))) && saved.State == "absent" {
			return result, true, nil
		}
	}
	return "", false, nil
}

func (r *reviewContext) validateDependencyAssumptions(assumptions []dependencyAssumption) error {
	if len(assumptions) > 10 {
		return errors.New("Keep at most 10 dependency assumptions per check.")
	}
	for i := range assumptions {
		assumption := &assumptions[i]
		if err := r.resolveDependencyAssumption(assumption); err != nil {
			return err
		}
		if strings.TrimSpace(assumption.Behavior) == "" || strings.TrimSpace(assumption.Basis) == "" || len(assumption.Behavior) > 2000 || len(assumption.Basis) > 2000 {
			return errors.New("State the assumed normal behavior and its visible interface/call-site basis concisely.")
		}
	}
	return nil
}

func (r *reviewContext) resolveDependencyAssumption(assumption *dependencyAssumption) error {
	for _, check := range r.activeRun.Dependencies {
		matches := check.ID == assumption.DependencyID
		if assumption.File != "" {
			matches = check.File == path.Clean(strings.ReplaceAll(assumption.File, "\\", "/"))
		}
		if matches && check.State == "absent" && check.Revision == r.head {
			assumption.DependencyID, assumption.File = check.ID, check.File
			return nil
		}
	}
	return errors.New("A dependency assumption requires an absent result from check_dependency_file at this final SHA. Empty code searches are insufficient.")
}

func dependencyAssumptionsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"type": "object", "properties": map[string]any{"file": map[string]any{"type": "string"}, "behavior": map[string]any{"type": "string", "maxLength": 2000}, "basis": map[string]any{"type": "string", "maxLength": 2000}}, "required": []string{"file", "behavior", "basis"}}}
}
