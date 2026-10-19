// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alibaba/open-code-review/internal/config/toolsconfig"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/model"
	"github.com/alibaba/open-code-review/internal/session"
	"github.com/alibaba/open-code-review/internal/tool"
)

type NativeCommitReview struct {
	Sources  []string              `json:"sources,omitempty"`
	Commit   string                `json:"commit"`
	Parent   string                `json:"parent"`
	Session  string                `json:"session"`
	Manifest *session.RunManifest  `json:"manifest,omitempty"`
	Comments []model.LlmComment    `json:"comments"`
	Warnings []legacyReviewWarning `json:"warnings"`
	Excluded []string              `json:"excluded,omitempty"`
	Error    string                `json:"error,omitempty"`
}

type legacyReviewWarning struct {
	File    string `json:"file"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

type nativeReviewRun struct {
	Direct            *directReviewState    `json:"direct,omitempty"`
	FindingSequence   int                   `json:"finding_sequence,omitempty"`
	Submissions       map[string]string     `json:"submissions,omitempty"`
	Protocol          reviewProtocolMetrics `json:"protocol_metrics"`
	TimeLimitSeconds  int                   `json:"time_limit_seconds,omitempty"`
	Partial           bool                  `json:"partial,omitempty"`
	Paths             []businessPath        `json:"paths,omitempty"`
	Dependencies      []dependencyCheck     `json:"dependencies,omitempty"`
	Findings          []businessFinding     `json:"findings,omitempty"`
	Verdicts          []focusedVerdict      `json:"verdicts,omitempty"`
	VerificationState string                `json:"verification_state,omitempty"`
	VerificationError string                `json:"verification_error,omitempty"`
	Mode              string                `json:"mode,omitempty"`
	Commits           []NativeCommitReview  `json:"commits"`
	Decisions         []nativeDecision      `json:"decisions,omitempty"`
}

type focusedVerdict struct {
	FindingID string           `json:"finding_id"`
	State     string           `json:"state"`
	Reason    string           `json:"reason"`
	Evidence  []sourceEvidence `json:"evidence"`
}

func (r *reviewContext) saveNativeRun(ctx context.Context, run nativeReviewRun) error {
	encoded, err := json.Marshal(run)
	if err != nil {
		return err
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err = r.server.store.db.ExecContext(saveCtx, "UPDATE review_executions SET native_json=?,phase='investigating' WHERE job_id=?", string(encoded), r.job.ID)
	return err
}

func (r *reviewContext) reviewTools() (*tool.Registry, []toolsconfig.ToolConfigEntry, error) {
	configured, err := toolsconfig.Load("")
	if err != nil {
		return nil, nil, err
	}
	reg := tool.NewRegistry()
	var entries []toolsconfig.ToolConfigEntry
	for _, entry := range configured {
		switch entry.Name {
		case "file_read", "file_find", "code_search", "file_read_diff":
			var fn map[string]any
			if err := json.Unmarshal(entry.Definition, &fn); err != nil {
				return nil, nil, err
			}
			params := fn["parameters"].(map[string]any)
			params["properties"].(map[string]any)["revision"] = map[string]any{"type": "string", "description": "Exact allowed SHA, never a branch. Defaults to the current target commit."}
			params["properties"].(map[string]any)["decision_gap"] = map[string]any{"type": "string", "description": "Which unresolved fact will this query decide, and what evidence will end this investigation?"}
			params["properties"].(map[string]any)["dependency_file"] = map[string]any{"type": "string", "description": "When investigating a dependency implementation, supply its actual include/declaration filename here. The program checks the complete frozen tree first and closes investigation immediately if absent; no separate check request is needed."}
			required, _ := params["required"].([]any)
			params["required"] = append(required, "decision_gap")
			if entry.Name == "code_search" {
				params["required"] = []string{"decision_gap"}
				props := params["properties"].(map[string]any)
				props["search_terms"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 8}
				props["search_mode"] = map[string]any{"type": "string", "enum": []string{"literal", "regex"}}
			}
			entry.Definition, err = json.Marshal(fn)
			if err != nil {
				return nil, nil, err
			}
			name := entry.Name
			reg.Register(tool.NewBuiltin(tool.OfName(name), func(ctx context.Context, args map[string]any) (string, error) { return r.lookup(ctx, name, args) }))
		case "code_comment":
			continue
		case "task_done":
			reg.Register(tool.NewBuiltin(tool.TaskDone, func(ctx context.Context, args map[string]any) (string, error) {
				return "Review task completed.", nil
			}))
		default:
			continue
		}
		entries = append(entries, entry)
	}
	var externalDefs []llm.ToolDef
	evidenceDef, _ := json.Marshal(map[string]any{"name": "read_review_evidence", "description": "Recover a saved evidence lookup by id without repeating a search. Available during closing.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer", "minimum": 1}}, "required": []string{"id"}}})
	reg.Register(tool.NewBuiltin(tool.Dynamic("read_review_evidence"), func(ctx context.Context, args map[string]any) (string, error) {
		id, _ := args["id"].(float64)
		var name, revision, arguments, result, state string
		err := r.server.store.db.QueryRowContext(ctx, "SELECT name,revision,arguments,result,state FROM review_tool_calls WHERE job_id=? AND id=?", r.job.ID, int64(id)).Scan(&name, &revision, &arguments, &result, &state)
		return reviewLookupHeader(int64(id), name, revision, r.head, arguments, result, state) + "\n" + result, err
	}))
	entries = append(entries, toolsconfig.ToolConfigEntry{Name: "read_review_evidence", MainTask: true, Definition: evidenceDef})
	businessDef, _ := json.Marshal(map[string]any{"name": "submit_review_checks", "description": "Save independent checks with nested candidate findings in one call. The program generates ids, resolves source locations and dependency filenames, and links each finding to its enclosing check. Each check is atomic; valid checks are saved even when others fail. All validation issues are returned with field paths. Identical successful retries are idempotent. Unfinished work must be pending. Explicit targets must come from the changed-file whitelist.", "parameters": r.reviewSubmissionSchema()})
	reg.Register(tool.NewBuiltin(tool.Dynamic("submit_review_checks"), func(ctx context.Context, args map[string]any) (string, error) {
		return r.submitReviewChecks(ctx, args)
	}))
	entries = append(entries, toolsconfig.ToolConfigEntry{Name: "submit_review_checks", MainTask: true, Definition: businessDef})
	dependencyDef, _ := json.Marshal(map[string]any{"name": "check_dependency_file", "description": "Check an exact dependency filename against the entire frozen repository tree, including other directories. Use before assuming a dependency file is missing; scoped code search cannot establish absence.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"file": map[string]any{"type": "string"}}, "required": []string{"file"}}})
	reg.Register(tool.NewBuiltin(tool.Dynamic("check_dependency_file"), r.checkDependencyFile))
	entries = append(entries, toolsconfig.ToolConfigEntry{Name: "check_dependency_file", MainTask: true, Definition: dependencyDef})
	r.registerExternalContext(reg, &externalDefs)
	for _, def := range externalDefs {
		data, err := json.Marshal(def.Function)
		if err != nil {
			return nil, nil, err
		}
		var definition map[string]any
		if err := json.Unmarshal(data, &definition); err != nil {
			return nil, nil, err
		}
		if params, ok := definition["parameters"].(map[string]any); ok {
			if props, ok := params["properties"].(map[string]any); ok {
				props["decision_gap"] = map[string]any{"type": "string", "description": "The unresolved fact this lookup will decide and its stopping condition."}
			}
			data, err = json.Marshal(definition)
			if err != nil {
				return nil, nil, err
			}
		}
		entries = append(entries, toolsconfig.ToolConfigEntry{Name: def.Function.Name, MainTask: true, PlanTask: true, Definition: data})
	}
	return reg, entries, nil
}

var nativeHunkStart = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func nativeCommentInPatch(cm model.LlmComment, diffs []model.Diff) bool {
	end := max(cm.StartLine, cm.EndLine)
	if cm.StartLine < 1 {
		return false
	}
	for _, d := range diffs {
		if d.NewPath != cm.Path {
			continue
		}
		line, inHunk := 0, false
		for _, text := range strings.Split(d.Diff, "\n") {
			if match := nativeHunkStart.FindStringSubmatch(text); match != nil {
				line, _ = strconv.Atoi(match[1])
				inHunk = true
				continue
			}
			if !inHunk || text == "" {
				continue
			}
			if text[0] == '+' && cm.StartLine <= line && line <= end {
				return true
			}
			if text[0] == '+' || text[0] == ' ' {
				line++
			}
		}
	}
	return false
}
