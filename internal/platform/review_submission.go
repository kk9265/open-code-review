// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/tool"
)

type submissionIssue struct {
	Field      string   `json:"field"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
}

type checkSubmissionResult struct {
	Index      int               `json:"index"`
	ID         string            `json:"id,omitempty"`
	State      string            `json:"state"`
	FindingIDs []string          `json:"finding_ids,omitempty"`
	Issues     []submissionIssue `json:"issues,omitempty"`
}

type reviewProtocolMetrics struct {
	CorrectionRequests int `json:"correction_requests"`
	GateRejections     int `json:"gate_rejections"`
	Submissions        int `json:"submissions"`
	FirstPass          int `json:"first_pass"`
	RejectedChecks     int `json:"rejected_checks"`
	Checks             int `json:"checks"`
	SavedChecks        int `json:"saved_checks"`
	SaveOnly           int `json:"save_only"`
}

func (r *reviewContext) recordReviewDispatch() {
	if !r.correctionPending || r.activeRun == nil {
		return
	}
	r.activeRun.Protocol.CorrectionRequests++
	r.correctionPending = false
	if err := r.saveNativeRun(context.Background(), *r.activeRun); err != nil {
		r.persistErr = err
	}
}

func sourceReadLines(result string) map[int]string {
	lines := map[int]string{}
	last := 0
	for _, line := range strings.Split(result, "\n") {
		prefix, text, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(prefix)
		if err == nil && n > 0 {
			lines[n], last = text, max(last, n)
		}
	}
	if strings.Contains(result, "Output capped at 24000 bytes") {
		delete(lines, last)
	}
	return lines
}

func reviewLookupHeader(id int64, name, revision, head, arguments, result, state string) string {
	if name == "file_read" && revision == head && state == "completed" {
		lines := sourceReadLines(result)
		if len(lines) > 0 {
			var args struct {
				Path string `json:"file_path"`
			}
			if json.Unmarshal([]byte(arguments), &args) == nil && args.Path != "" {
				first, last := int(^uint(0)>>1), 0
				for n := range lines {
					first, last = min(first, n), max(last, n)
				}
				return fmt.Sprintf("Source reference S%d: path=%s, revision=%s, readable lines=%d-%d. Cite {\"source_ref\":\"S%d\",\"line\":%d} (at most 50 lines per citation).", id, args.Path, revision, first, last, id, first)
			}
		}
	}
	return fmt.Sprintf("Query record #%d (%s, revision %s, %s). This is not a source reference; read final-SHA source before citing it.", id, name, revision, state)
}

func (r *reviewContext) submitReviewChecks(ctx context.Context, args map[string]any) (string, error) {
	checks, ok := args["checks"].([]any)
	if !ok || len(checks) == 0 || len(checks) > 30 {
		r.activeRun.Protocol.Submissions++
		r.correctionPending = true
		if err := r.saveNativeRun(ctx, *r.activeRun); err != nil {
			r.persistErr = err
			return "", err
		}
		return `{"issues":[{"field":"checks","message":"Provide 1-30 independent check objects."}]}`, nil
	}
	results := make([]checkSubmissionResult, 0, len(checks))
	allSaved, saveOnly := true, true
	for i, raw := range checks {
		result := checkSubmissionResult{Index: i, State: "rejected"}
		data, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		identity := data
		fields, object := raw.(map[string]any)
		id, _ := fields["id"].(string)
		_, explicitTargets := fields["targets"]
		if object && !explicitTargets && id == "" {
			identity, err = json.Marshal(struct {
				Check   json.RawMessage `json:"check"`
				Targets []string        `json:"current_targets"`
			}{data, tool.ReviewTargets(ctx)})
			if err != nil {
				return "", err
			}
		}
		sum := sha256.Sum256(identity)
		key := fmt.Sprintf("%x", sum)
		if id := r.activeRun.Submissions[key]; id != "" {
			result.ID, result.State = id, "already_saved"
			for _, f := range r.activeRun.Findings {
				if f.Facts.PathID == id {
					result.FindingIDs = append(result.FindingIDs, f.ID)
				}
			}
		} else {
			var check businessPath
			fields, object := raw.(map[string]any)
			if !object {
				result.Issues = append(result.Issues, submissionIssue{Field: "check", Message: "Expected a check object with correctly typed fields."})
			} else {
				result.Issues = append(result.Issues, decodeSubmissionFields(fields, &check, "findings", "discard_findings")...)
				oldIndex := -1
				for j, old := range r.activeRun.Paths {
					if check.ID != "" && old.ID == check.ID {
						oldIndex, check = j, mergeBusinessPath(old, check, fields)
						break
					}
				}
				if check.ID != "" && oldIndex < 0 {
					var ids []string
					for _, old := range r.activeRun.Paths {
						ids = append(ids, old.ID)
					}
					result.Issues = append(result.Issues, submissionIssue{Field: "id", Message: "Use a returned check id for updates, or omit it for a new check.", Candidates: ids})
				}
				if check.ID == "" {
					for n := len(r.activeRun.Paths) + 1; ; n++ {
						check.ID = fmt.Sprintf("C%03d", n)
						found := false
						for _, saved := range r.activeRun.Paths {
							found = found || saved.ID == check.ID
						}
						if !found {
							break
						}
					}
				}
				check.Group = "ticket"
				if len(check.Targets) == 0 && !explicitTargets {
					current := tool.ReviewTargets(ctx)
					if len(current) == 1 {
						check.Targets = current
					}
				}
				result.Issues = append(result.Issues, r.checkSubmissionIssues(ctx, &check)...)
				var discard []string
				if value, exists := fields["discard_findings"]; exists {
					data, err := json.Marshal(value)
					if err != nil || json.Unmarshal(data, &discard) != nil || len(discard) > 30 {
						result.Issues = append(result.Issues, submissionIssue{Field: "discard_findings", Message: "Provide at most 30 returned finding ids belonging to this existing check."})
					}
					var candidates []string
					for _, f := range r.activeRun.Findings {
						if f.Facts.PathID == check.ID {
							candidates = append(candidates, f.ID)
						}
					}
					for _, id := range discard {
						found := false
						for _, allowed := range candidates {
							found = found || allowed == id
						}
						if !found {
							result.Issues = append(result.Issues, submissionIssue{Field: "discard_findings", Message: "Finding must belong to the explicitly updated check.", Candidates: candidates})
						}
					}
				}
				var findings []businessFinding
				if nested, exists := fields["findings"]; exists {
					items, valid := nested.([]any)
					if !valid || len(items) > 30 {
						result.Issues = append(result.Issues, submissionIssue{Field: "findings", Message: "Provide an array of at most 30 findings."})
					} else {
						for j, item := range items {
							finding, issues := r.nestedFinding(ctx, check, item)
							for _, issue := range issues {
								issue.Field = fmt.Sprintf("findings[%d].%s", j, issue.Field)
								result.Issues = append(result.Issues, issue)
							}
							findings = append(findings, finding)
						}
					}
				}
				if len(result.Issues) == 0 {
					staged := *r.activeRun
					staged.Paths = append([]businessPath(nil), staged.Paths...)
					staged.Findings = append([]businessFinding(nil), staged.Findings...)
					for _, finding := range staged.Findings {
						sequence, _ := strconv.Atoi(strings.TrimPrefix(finding.ID, "F"))
						staged.FindingSequence = max(staged.FindingSequence, sequence)
					}
					for _, id := range discard {
						for j, f := range staged.Findings {
							if f.ID == id {
								staged.Findings = append(staged.Findings[:j], staged.Findings[j+1:]...)
								break
							}
						}
					}
					staged.Submissions = map[string]string{}
					for k, v := range r.activeRun.Submissions {
						staged.Submissions[k] = v
					}
					if oldIndex >= 0 {
						staged.Paths[oldIndex] = check
					} else {
						staged.Paths = append(staged.Paths, check)
					}
					for _, finding := range findings {
						index := -1
						for n, saved := range staged.Findings {
							if findingKey(saved.Path, saved.Content) == findingKey(finding.Path, finding.Content) {
								index = n
								break
							}
						}
						if index >= 0 {
							finding.ID = staged.Findings[index].ID
							staged.Findings[index] = finding
						} else {
							staged.FindingSequence++
							finding.ID = fmt.Sprintf("F%03d", staged.FindingSequence)
							staged.Findings = append(staged.Findings, finding)
						}
						result.FindingIDs = append(result.FindingIDs, finding.ID)
					}
					staged.Submissions[key] = check.ID
					if err := r.saveNativeRun(ctx, staged); err != nil {
						r.persistErr = err
						return "", err
					}
					*r.activeRun = staged
					r.candidates = map[string]businessFinding{}
					for _, finding := range staged.Findings {
						r.candidates[findingKey(finding.Path, finding.Content)] = finding
					}
					r.lookupsSinceCheckpoint, r.consecutiveMisses = 0, 0
					result.ID, result.State = check.ID, "saved"
					r.activeRun.Protocol.SavedChecks++
				}
			}
		}
		allSaved = allSaved && result.State != "rejected"
		saveOnly = saveOnly && len(result.FindingIDs) == 0
		if result.State == "rejected" {
			r.activeRun.Protocol.RejectedChecks++
		}
		results = append(results, result)
	}
	r.activeRun.Protocol.Submissions++
	r.activeRun.Protocol.Checks += len(checks)
	if allSaved {
		r.activeRun.Protocol.FirstPass++
	}
	if saveOnly && allSaved {
		r.activeRun.Protocol.SaveOnly++
	}
	r.correctionPending = !allSaved
	if err := r.saveNativeRun(ctx, *r.activeRun); err != nil {
		r.persistErr = err
		return "", err
	}
	encoded, err := json.Marshal(map[string]any{"checks": results, "current_targets": tool.ReviewTargets(ctx), "allowed_targets": r.submissionTargets()})
	return string(encoded), err
}

func (r *reviewContext) submissionTargets() []string {
	paths := make([]string, 0, len(r.nativeDiffs))
	for path := range r.nativeDiffs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (r *reviewContext) checkSubmissionIssues(ctx context.Context, check *businessPath) []submissionIssue {
	var issues []submissionIssue
	add := func(field, message string, candidates ...string) {
		issues = append(issues, submissionIssue{field, message, candidates})
	}
	if len(check.Targets) == 0 || len(check.Targets) > 20 {
		add("targets", "Choose 1-20 changed files explicitly when the work item has multiple targets.", r.submissionTargets()...)
	}
	for i, target := range check.Targets {
		if _, ok := r.nativeDiffs[target]; !ok {
			add(fmt.Sprintf("targets[%d]", i), "Only changed files are review targets; dependencies are evidence only.", r.submissionTargets()...)
		}
	}
	if len(r.activeRun.Paths) >= 200 {
		found := false
		for _, old := range r.activeRun.Paths {
			found = found || old.ID == check.ID
		}
		if !found {
			add("id", "At most 200 checks may be saved; update a returned id.")
		}
	}
	for field, value := range map[string]string{"chain": check.Chain, "held_state": check.HeldState, "side_effects": check.SideEffects, "counterevidence": check.CounterEvidence, "conclusion": check.Conclusion} {
		if len(value) > 2000 {
			add(field, "Keep this field within 2000 bytes.")
		}
	}
	if check.Status != "pending" && strings.TrimSpace(check.Conclusion) == "" {
		add("conclusion", "An explicit conclusion is required; use pending for unfinished work.")
	}
	switch check.Status {
	case "checked", "gap", "not_applicable", "pending":
	default:
		add("status", "Choose an explicit status.", "checked", "gap", "not_applicable", "pending")
	}
	if check.Status != "pending" || len(check.Evidence) > 0 {
		issues = append(issues, r.submissionEvidenceIssues(ctx, "evidence", check.Evidence)...)
	}
	if len(check.Assumptions) > 10 {
		add("assumptions", "Keep at most 10 assumptions.")
	}
	for i := range check.Assumptions {
		for field, value := range map[string]string{"behavior": check.Assumptions[i].Behavior, "basis": check.Assumptions[i].Basis} {
			if strings.TrimSpace(value) == "" || len(value) > 2000 {
				add(fmt.Sprintf("assumptions[%d].%s", i, field), "Provide a nonempty statement within 2000 bytes.")
			}
		}
		if check.Assumptions[i].File == "" {
			add(fmt.Sprintf("assumptions[%d].file", i), "Supply the exact dependency filename, not a dependency id.")
		}
		if err := r.resolveDependencyAssumption(&check.Assumptions[i]); err != nil {
			var absent []string
			for _, d := range r.activeRun.Dependencies {
				if d.State == "absent" && d.Revision == r.head {
					absent = append(absent, d.File)
				}
			}
			add(fmt.Sprintf("assumptions[%d].file", i), err.Error(), absent...)
		}
	}
	return issues
}

func (r *reviewContext) submissionEvidenceIssues(ctx context.Context, field string, evidence []sourceEvidence) []submissionIssue {
	if len(evidence) == 0 || len(evidence) > 20 {
		return []submissionIssue{{Field: field, Message: "Provide 1-20 final-SHA source references.", Candidates: r.sourceCandidates(ctx)}}
	}
	var issues []submissionIssue
	for i := range evidence {
		if evidence[i].SourceRef == "" {
			issues = append(issues, submissionIssue{Field: fmt.Sprintf("%s[%d].source_ref", field, i), Message: "Use the source_ref issued by a successful final-SHA file_read.", Candidates: r.sourceCandidates(ctx)})
			continue
		}
		if err := r.validateSourceEvidence(ctx, evidence[i:i+1]); err != nil {
			issues = append(issues, submissionIssue{Field: fmt.Sprintf("%s[%d]", field, i), Message: err.Error(), Candidates: r.sourceCandidates(ctx)})
		}
	}
	return issues
}

func decodeSubmissionFields(fields map[string]any, target any, nested ...string) []submissionIssue {
	var issues []submissionIssue
	for name, value := range fields {
		skip := false
		for _, field := range nested {
			skip = skip || field == name
		}
		if skip {
			continue
		}
		data, err := json.Marshal(map[string]any{name: value})
		if err == nil {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			err = decoder.Decode(target)
		}
		if err != nil {
			issues = append(issues, submissionIssue{Field: name, Message: err.Error()})
		}
	}
	return issues
}

func (r *reviewContext) sourceCandidates(ctx context.Context) []string {
	rows, err := r.server.store.db.QueryContext(ctx, "SELECT id,arguments,result FROM review_tool_calls WHERE job_id=? AND name='file_read' AND revision=? AND state='completed' ORDER BY id", r.job.ID, r.head)
	if err != nil {
		r.persistErr = err
		return nil
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var id int64
		var arguments, result string
		if err := rows.Scan(&id, &arguments, &result); err != nil {
			r.persistErr = err
			return candidates
		}
		header := reviewLookupHeader(id, "file_read", r.head, r.head, arguments, result, "completed")
		if strings.HasPrefix(header, "Source reference ") {
			candidates = append(candidates, header)
		}
	}
	if err := rows.Err(); err != nil {
		r.persistErr = err
	}
	return candidates
}

func (r *reviewContext) nestedFinding(ctx context.Context, check businessPath, raw any) (businessFinding, []submissionIssue) {
	var input struct {
		Content  string         `json:"content"`
		Severity string         `json:"severity"`
		Category string         `json:"category"`
		Location sourceEvidence `json:"location"`
		Business findingFacts   `json:"business"`
	}
	var issues []submissionIssue
	add := func(field, message string, candidates ...string) {
		issues = append(issues, submissionIssue{field, message, candidates})
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return businessFinding{}, []submissionIssue{{Field: "finding", Message: "Expected a finding object with correctly typed fields."}}
	}
	issues = append(issues, decodeSubmissionFields(fields, &input)...)
	if strings.TrimSpace(input.Content) == "" || findingDescription(input.Content) != strings.TrimSpace(input.Content) {
		add("content", "Describe the defect, trigger and impact without repair advice.")
	}
	if input.Severity != "critical" && input.Severity != "high" && input.Severity != "medium" && input.Severity != "low" {
		add("severity", "Choose a severity.", "critical", "high", "medium", "low")
	}
	facts := input.Business
	facts.PathID = check.ID
	if r.activeRun.Direct != nil {
		for field, value := range map[string]string{"requirement_basis": facts.RequirementBasis, "countercheck": facts.Countercheck} {
			if strings.TrimSpace(value) == "" || len(value) > 8000 {
				add("business."+field, "State the contract basis and the control-flow countercheck explicitly, within 8000 bytes.")
			}
		}
		issues = append(issues, r.submissionEvidenceIssues(ctx, "business.counterevidence", facts.CounterEvidence)...)
		if input.Business.Confidence == "confirmed" && check.Status != "checked" {
			add("business.confidence", "Confirmed confidence requires a checked path with an explicit conclusion.", "suspected")
		}
	}
	if facts.Confidence != "confirmed" && facts.Confidence != "suspected" {
		add("business.confidence", "Choose confidence.", "confirmed", "suspected")
	}
	if facts.Origin != "introduced" && facts.Origin != "existing" && facts.Origin != "unknown" {
		add("business.origin", "Choose origin.", "introduced", "existing", "unknown")
	}
	for field, value := range map[string]string{"trigger": facts.Trigger, "expected": facts.Expected, "impact": facts.Impact} {
		if strings.TrimSpace(value) == "" || len(value) > 8000 {
			add("business."+field, "Provide a nonempty statement within 8000 bytes.")
		}
	}
	if check.Status == "pending" {
		add("business", "Pending checks cannot support findings; record a conclusion first.")
	}
	if check.Status == "gap" && facts.Confidence == "confirmed" {
		add("business.confidence", "An unclosed check cannot support confirmed confidence.", "suspected")
	}
	issues = append(issues, r.submissionEvidenceIssues(ctx, "business.evidence", facts.Evidence)...)
	anchors := []sourceEvidence{input.Location}
	anchorIssues := r.submissionEvidenceIssues(ctx, "location", anchors)
	issues = append(issues, anchorIssues...)
	anchor := anchors[0]
	cm := model.LlmComment{Path: anchor.Path, StartLine: anchor.Line, EndLine: anchor.EndLine, ExistingCode: anchor.Text, Content: input.Content, Severity: input.Severity, Category: input.Category}
	if len(anchorIssues) == 0 {
		linked := false
		for _, target := range check.Targets {
			linked = linked || target == anchor.Path
		}
		if !linked {
			add("location", "The source location must belong to this check's explicit targets.", check.Targets...)
		}
		patch, changed := r.nativeDiffs[anchor.Path]
		if !changed || !nativeCommentInPatch(cm, []model.Diff{{NewPath: anchor.Path, Diff: patch}}) {
			add("location", "Anchor to a changed final-tree line in the unified patch.")
		}
		if old, ok := r.candidates[findingKey(cm.Path, cm.Content)]; ok && old.Facts.PathID != check.ID {
			add("business", "This finding already belongs to another check; update its returned check id.", old.Facts.PathID)
		}
	}
	return businessFinding{Path: cm.Path, Line: cm.StartLine, Severity: cm.Severity, Content: cm.Content, Facts: facts}, issues
}

func (r *reviewContext) reviewSubmissionSchema() map[string]any {
	props := businessPathSchemaProperties()
	props["id"] = map[string]any{"type": "string", "description": "Omit for a new check. For updates use the program-returned id."}
	props["targets"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": map[string]any{"type": "string", "enum": r.submissionTargets()}, "description": "Changed files only. Omit only for a single-target current work item. Evidence files never imply coverage."}
	props["status"] = map[string]any{"type": "string", "enum": []string{"checked", "gap", "not_applicable", "pending"}}
	props["evidence"].(map[string]any)["minItems"] = 0
	facts := businessFactsSchema()
	delete(facts["properties"].(map[string]any), "path_id")
	facts["required"] = []string{"confidence", "origin", "trigger", "expected", "impact", "evidence"}
	if r.activeRun != nil && r.activeRun.Direct != nil {
		factProps := facts["properties"].(map[string]any)
		factProps["requirement_basis"] = map[string]any{"type": "string", "maxLength": 8000, "description": "Established requirement or interface contract violated, distinguishing explicit intent from inference."}
		factProps["countercheck"] = map[string]any{"type": "string", "maxLength": 8000, "description": "Explain why preceding guards, terminal flags, early returns and known counterevidence do not block the claimed execution path."}
		factProps["counterevidence"] = evidenceSchema()
		facts["required"] = append(facts["required"].([]string), "requirement_basis", "countercheck", "counterevidence")
	}
	props["discard_findings"] = map[string]any{"type": "array", "maxItems": 30, "items": map[string]any{"type": "string"}, "description": "Explicitly withdraw refuted findings by their returned ids inside the existing check they belong to."}
	props["findings"] = map[string]any{"type": "array", "maxItems": 30, "items": map[string]any{"type": "object", "properties": map[string]any{"content": map[string]any{"type": "string"}, "severity": map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low"}}, "category": map[string]any{"type": "string"}, "location": evidenceReferenceSchema(), "business": facts}, "required": []string{"content", "severity", "location", "business"}, "additionalProperties": false}}
	return map[string]any{"type": "object", "properties": map[string]any{"checks": map[string]any{"type": "array", "minItems": 1, "maxItems": 30, "items": map[string]any{"type": "object", "properties": props, "required": []string{"status"}, "additionalProperties": false}}}, "required": []string{"checks"}}
}
