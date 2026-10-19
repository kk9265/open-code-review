// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type sourceEvidence struct {
	SourceRef string `json:"source_ref,omitempty"`
	LookupID  int64  `json:"lookup_id"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line,omitempty"`
	Text      string `json:"text,omitempty"`
}

type businessPath struct {
	ID              string                 `json:"id"`
	Group           string                 `json:"group"`
	Targets         []string               `json:"targets"`
	Chain           string                 `json:"chain"`
	HeldState       string                 `json:"held_state"`
	SideEffects     string                 `json:"side_effects"`
	CounterEvidence string                 `json:"counterevidence"`
	Status          string                 `json:"status"`
	Conclusion      string                 `json:"conclusion"`
	Evidence        []sourceEvidence       `json:"evidence"`
	Assumptions     []dependencyAssumption `json:"assumptions,omitempty"`
}

type findingFacts struct {
	RequirementBasis string           `json:"requirement_basis,omitempty"`
	Countercheck     string           `json:"countercheck,omitempty"`
	CounterEvidence  []sourceEvidence `json:"counterevidence,omitempty"`
	PathID           string           `json:"path_id"`
	Confidence       string           `json:"confidence"`
	Origin           string           `json:"origin"`
	Trigger          string           `json:"trigger"`
	Expected         string           `json:"expected"`
	Impact           string           `json:"impact"`
	Evidence         []sourceEvidence `json:"evidence"`
}

type businessFinding struct {
	ID       string       `json:"id"`
	Path     string       `json:"path"`
	Line     int          `json:"line"`
	Severity string       `json:"severity"`
	Content  string       `json:"content"`
	Facts    findingFacts `json:"facts"`
}

func findingKey(path, content string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + content))
	return fmt.Sprintf("%x", sum)
}

// Resolve citations from saved reads so the model never has to transcribe source.
func (r *reviewContext) validateSourceEvidence(ctx context.Context, evidence []sourceEvidence) error {
	if len(evidence) == 0 || len(evidence) > 20 {
		return errors.New("Provide 1-20 source evidence entries from successful file_read calls.")
	}
	for i := range evidence {
		e := &evidence[i]
		if e.SourceRef != "" {
			id, err := strconv.ParseInt(strings.TrimPrefix(e.SourceRef, "S"), 10, 64)
			if err != nil || e.SourceRef != fmt.Sprintf("S%d", id) || id < 1 || (e.LookupID != 0 && e.LookupID != id) {
				return fmt.Errorf("Evidence %d has an invalid or conflicting source_ref.", i+1)
			}
			e.LookupID = id
		}
		if e.EndLine == 0 {
			e.EndLine = e.Line
		}
		if e.LookupID < 1 || e.Line < 1 || e.EndLine < e.Line || e.EndLine-e.Line >= 50 {
			return fmt.Errorf("Evidence %d needs lookup_id and a positive line range of at most 50 lines.", i+1)
		}
		var name, revision, arguments, result, state string
		err := r.server.store.db.QueryRowContext(ctx, "SELECT name,revision,arguments,result,state FROM review_tool_calls WHERE job_id=? AND id=?", r.job.ID, e.LookupID).Scan(&name, &revision, &arguments, &result, &state)
		if err != nil || name != "file_read" || revision != r.head || state != "completed" {
			return fmt.Errorf("Evidence %d must reference a successful file_read from this run at the final SHA.", i+1)
		}
		var args map[string]any
		if json.Unmarshal([]byte(arguments), &args) != nil {
			return fmt.Errorf("Evidence %d has invalid saved lookup arguments.", i+1)
		}
		path, _ := args["file_path"].(string)
		if path == "" {
			return fmt.Errorf("Evidence %d has no saved source path.", i+1)
		}
		lines := sourceReadLines(result)
		var actual []string
		for line := e.Line; line <= e.EndLine; line++ {
			text, exists := lines[line]
			if !exists {
				return fmt.Errorf("Evidence %d is outside the saved read range; read the missing lines.", i+1)
			}
			actual = append(actual, text)
		}
		e.SourceRef = fmt.Sprintf("S%d", e.LookupID)
		e.Path, e.Text = path, strings.Join(actual, "\n")
	}
	return nil
}

func businessPathSchemaProperties() map[string]any {
	return map[string]any{
		"id":              map[string]any{"type": "string", "maxLength": 80},
		"targets":         map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": map[string]any{"type": "string"}},
		"chain":           map[string]any{"type": "string", "maxLength": 2000},
		"held_state":      map[string]any{"type": "string", "maxLength": 2000},
		"side_effects":    map[string]any{"type": "string", "maxLength": 2000},
		"counterevidence": map[string]any{"type": "string", "maxLength": 2000},
		"status":          map[string]any{"type": "string", "enum": []string{"checked", "gap", "not_applicable"}},
		"conclusion":      map[string]any{"type": "string", "maxLength": 2000},
		"evidence":        evidenceSchema(),
		"assumptions":     dependencyAssumptionsSchema(),
	}
}

func evidenceSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": evidenceReferenceSchema()}
}

func evidenceReferenceSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"source_ref": map[string]any{"type": "string", "pattern": "^S[1-9][0-9]*$"}, "line": map[string]any{"type": "integer", "minimum": 1}, "end_line": map[string]any{"type": "integer", "minimum": 1, "description": "Optional inclusive end, at most 50 lines total."}}, "required": []string{"source_ref", "line"}, "additionalProperties": false}
}

func mergeBusinessPath(old, delta businessPath, fields map[string]any) businessPath {
	for _, target := range delta.Targets {
		found := false
		for _, existing := range old.Targets {
			found = found || existing == target
		}
		if !found {
			old.Targets = append(append([]string(nil), old.Targets...), target)
		}
	}
	for _, e := range delta.Evidence {
		found := false
		for _, saved := range old.Evidence {
			sameSource := (saved.SourceRef != "" && saved.SourceRef == e.SourceRef) || (saved.LookupID > 0 && saved.LookupID == e.LookupID)
			found = found || (sameSource && saved.Line == e.Line && max(saved.EndLine, saved.Line) == max(e.EndLine, e.Line))
		}
		if !found {
			old.Evidence = append(append([]sourceEvidence(nil), old.Evidence...), e)
		}
	}
	if _, ok := fields["chain"]; ok {
		old.Chain = delta.Chain
	}
	if _, ok := fields["held_state"]; ok {
		old.HeldState = delta.HeldState
	}
	if _, ok := fields["side_effects"]; ok {
		old.SideEffects = delta.SideEffects
	}
	if _, ok := fields["counterevidence"]; ok {
		old.CounterEvidence = delta.CounterEvidence
	}
	if _, ok := fields["status"]; ok {
		old.Status = delta.Status
	}
	if _, ok := fields["conclusion"]; ok {
		old.Conclusion = delta.Conclusion
	}
	if _, ok := fields["assumptions"]; ok {
		old.Assumptions = delta.Assumptions
	}
	return old
}

func businessFactsSchema() map[string]any {
	props := map[string]any{"evidence": evidenceSchema(), "confidence": map[string]any{"type": "string", "enum": []string{"confirmed", "suspected"}}, "origin": map[string]any{"type": "string", "enum": []string{"introduced", "existing", "unknown"}}}
	for _, key := range []string{"path_id", "trigger", "expected", "impact"} {
		props[key] = map[string]any{"type": "string", "maxLength": 8000}
	}
	return map[string]any{"type": "object", "properties": props, "required": []string{"path_id", "confidence", "origin", "trigger", "expected", "impact", "evidence"}}
}
