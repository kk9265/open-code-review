// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alibaba/open-code-review/internal/config/template"
	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/tool"
)

type ReviewExecution struct {
	Direct             *directReviewState
	TimeLimitSeconds   int
	Native             []NativeCommitReview
	Decisions          []nativeDecision
	Paths              []businessPath
	Findings           []businessFinding
	Verdicts           []focusedVerdict
	VerificationState  string
	VerificationError  string
	Dependencies       []dependencyCheck
	Protocol           reviewProtocolMetrics
	Phase, StopReason  string
	Questions          []ReviewQuestion
	ContextSHA, Engine string
	Requests, Tokens   int64
	Calls              []ReviewToolCall
}

type ReviewToolCall struct {
	ID                                                         int64
	Name, Revision, Arguments, Result, Error, State, CreatedAt string
}

func (s *Server) reviewExecution(ctx context.Context, id int64) (*ReviewExecution, error) {
	var execution ReviewExecution
	var nativeJSON string
	err := s.store.db.QueryRowContext(ctx, "SELECT context_sha,engine,requests,tokens,phase,stop_reason,native_json FROM review_executions WHERE job_id=?", id).Scan(&execution.ContextSHA, &execution.Engine, &execution.Requests, &execution.Tokens, &execution.Phase, &execution.StopReason, &nativeJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if nativeJSON != "" {
		var run nativeReviewRun
		if err := json.Unmarshal([]byte(nativeJSON), &run); err != nil {
			return nil, err
		}
		execution.Native = run.Commits
		execution.Direct = run.Direct
		execution.TimeLimitSeconds = run.TimeLimitSeconds
		execution.Decisions = run.Decisions
		execution.Paths = run.Paths
		execution.Dependencies = run.Dependencies
		execution.Protocol = run.Protocol
		execution.Findings = run.Findings
		execution.Verdicts = run.Verdicts
		execution.VerificationState = run.VerificationState
		execution.VerificationError = run.VerificationError
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,name,revision,arguments,result,error,state,created_at FROM review_tool_calls WHERE job_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var call ReviewToolCall
		if err := rows.Scan(&call.ID, &call.Name, &call.Revision, &call.Arguments, &call.Result, &call.Error, &call.State, &call.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		execution.Calls = append(execution.Calls, call)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	execution.Questions, err = s.reviewQuestions(ctx, id)
	return &execution, err
}

type reviewContext struct {
	server                 *Server
	job                    *ReviewJob
	path, head, report     string
	allowed                map[string]bool
	calls                  int
	persistErr             error
	cache                  map[string]string
	defaultRevision        string
	nativeDiffs            map[string]string
	external               map[string]func(context.Context, string) (string, error)
	contextNotice          string
	activeRun              *nativeReviewRun
	candidates             map[string]businessFinding
	reviewDeadline         time.Time
	lookupsSinceCheckpoint int
	consecutiveMisses      int
	dependencyTree         []string
	dependencyTreeLoaded   bool
	dependencyIncludes     map[string]bool
	correctionPending      bool
}

func (s *Server) executeContextReview(ctx context.Context, j *ReviewJob, client llm.LLMClient) (string, error) {
	path, err := s.cachePath(j.RepositoryID)
	if err != nil {
		return "", err
	}
	r := &reviewContext{server: s, job: j, path: path, allowed: make(map[string]bool), cache: make(map[string]string)}
	var versions []string
	var boundaries strings.Builder
	for _, c := range j.Commits {
		parents := strings.Fields(c.Parents)
		parent := "empty tree (root commit)"
		if len(parents) > 0 {
			parent = parents[0]
		}
		fmt.Fprintf(&boundaries, "Selected commit %s; diff base (first parent): %s\n", c.SHA, parent)
		for _, sha := range append([]string{c.SHA}, strings.Fields(c.Parents)...) {
			if !validReviewSHA(sha) {
				return "", errors.New("Invalid frozen review revision.")
			}
			if !r.allowed[sha] {
				r.allowed[sha] = true
				versions = append(versions, sha)
			}
		}
		if r.head != "" {
			if err := runManagedGitWithOutput(ctx, nil, nil, "--git-dir="+path, "merge-base", "--is-ancestor", r.head, c.SHA); err != nil {
				return "", errors.New("Selected commits are not in ancestry order. Reassign commits before reviewing.")
			}
		}
		r.head = c.SHA
	}
	if r.head == "" {
		return "", errors.New("This ticket has no commits to review.")
	}
	if _, err = s.store.db.ExecContext(ctx, `INSERT INTO review_executions(job_id,context_sha,engine,requests,tokens) VALUES(?,?,'ticket-direct-v13',?,?)`, j.ID, r.head, j.PurposeRequests, j.PurposeTokens); err != nil {
		return "", err
	}
	tpl, err := template.LoadDefault()
	if err != nil {
		return "", err
	}
	closeContext := r.connectExternalContext(ctx)
	defer closeContext()
	tracked := &reviewUsageClient{client: client, requests: j.PurposeRequests, used: j.PurposeTokens, onRequest: func(elapsed time.Duration, err error) {
		s.log.Info("review model request finished", "job_id", j.ID, "duration_ms", elapsed.Milliseconds(), "failed", err != nil)
	}, progress: func(requests, tokens int64) error {
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := s.store.db.ExecContext(saveCtx, "UPDATE review_executions SET requests=?,tokens=? WHERE job_id=?", requests, tokens, j.ID)
		return err
	}}
	tracked.onDispatch = r.recordReviewDispatch
	if j.BudgetEnabled {
		tracked.budget = int64(j.Budget)
	}
	background := "Frozen final context SHA: " + r.head + "\nAllowed revisions: " + strings.Join(versions, ", ") + "\nChange attribution boundaries:\n" + boundaries.String() + "\n" + r.ticketReviewBackground(ctx) + "\nContext availability:\n" + r.contextNotice
	return r.runDirectReview(ctx, tracked, tpl.CompletionTokenLimit(), background)
}

func (r *reviewContext) ticketReviewBackground(ctx context.Context) string {
	var background strings.Builder
	fmt.Fprintf(&background, "Change purpose and expected behavior (source: %s; context to verify, not proof of correctness):\n%s\n\nSelected submission descriptions (intent context only):\n", r.job.PurposeSource, r.job.ChangePurpose)
	for _, commit := range r.job.Commits {
		fmt.Fprintf(&background, "\nCommit %s\n%s\n", commit.SHA, commit.Message)
	}
	var previous string
	var err error
	if r.job.ContextJobID != 0 {
		err = r.server.store.db.QueryRowContext(ctx, "SELECT result FROM review_jobs WHERE id=? AND group_id=?", r.job.ContextJobID, r.job.GroupID).Scan(&previous)
	} else {
		err = r.server.store.db.QueryRowContext(ctx, `SELECT result FROM review_jobs WHERE group_id=? AND id<? AND state='completed' ORDER BY id DESC LIMIT 1`, r.job.GroupID, r.job.ID).Scan(&previous)
	}
	if err == nil && previous != "" {
		background.WriteString("\nPrevious review (context only; re-check against this unified selected patch):\n")
		background.WriteString(previous)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		r.persistErr = err
	}
	if r.job.Feedback != "" {
		background.WriteString("\nUser feedback for this re-review (user-authored context; verify every claim against evidence):\n")
		background.WriteString(r.job.Feedback)
	}
	return background.String()
}

func validReviewSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (r *reviewContext) lookup(ctx context.Context, name string, args map[string]any) (string, error) {
	if strings.TrimSpace(fmt.Sprint(args["decision_gap"])) == "" || args["decision_gap"] == nil {
		return "", errors.New("State decision_gap: the unresolved fact this lookup will decide and the stopping condition. Do not repeat an established chain.")
	}
	revision, _ := args["revision"].(string)
	if revision == "" {
		revision = r.defaultRevision
		if revision == "" || r.external[name] != nil {
			revision = r.head
		}
	}
	if !r.allowed[revision] {
		return "", errors.New("Revision is outside this frozen review. Use an exact listed SHA.")
	}
	if name == "file_read_diff" && revision != r.defaultRevision {
		return "", errors.New("file_read_diff exposes the unified ticket patch at the frozen final SHA. Use file_read for another allowed snapshot.")
	}
	if result, handled, err := r.dependencyLookupShortcut(ctx, name, args, revision); handled {
		return result, err
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	lookupArgs := make(map[string]any, len(args))
	for key, value := range args {
		if key != "focus" && key != "decision_gap" && key != "dependency_file" {
			lookupArgs[key] = value
		}
	}
	lookupArgs["revision"] = revision
	delete(lookupArgs, "decision_gap")
	lookupJSON, err := json.Marshal(lookupArgs)
	if err != nil {
		return "", err
	}
	key := name + string(lookupJSON)
	if previous, ok := r.cache[key]; ok {
		return "This exact lookup was already performed. Reuse its evidence instead of repeating it:\n" + previous, nil
	}
	if r.calls >= 80 {
		return "", errors.New("Code lookup limit reached. Submit the report with coverage limitations.")
	}
	r.calls++
	r.lookupsSinceCheckpoint++
	if name == "file_read" && args["end_line"] == nil {
		start, _ := args["start_line"].(float64)
		if start < 1 {
			start = 1
		}
		lookupArgs["start_line"], lookupArgs["end_line"] = start, start+119
	}
	entry, err := r.server.store.db.ExecContext(ctx, `INSERT INTO review_tool_calls(job_id,name,revision,arguments,created_at) VALUES(?,?,?,?,?)`, r.job.ID, name, revision, string(encoded), now())
	if err != nil {
		r.persistErr = err
		return "", err
	}
	id, err := entry.LastInsertId()
	if err != nil {
		r.persistErr = err
		return "", err
	}
	fr := &tool.FileReader{RepoDir: r.path, Mode: tool.ModeCommit, Ref: revision}
	var provider tool.Provider
	switch name {
	case "file_read":
		provider = tool.NewFileRead(fr)
	case "file_find":
		provider = tool.NewFileFind(fr)
	case "code_search":
		provider = tool.NewCodeSearch(fr)
	case "file_read_diff":
		provider = tool.NewFileReadDiff(tool.NewDiffMap(r.nativeDiffs))
	}
	var result string
	var callErr error
	if external, ok := r.external[name]; ok {
		query, _ := args["query"].(string)
		if revision != r.head || strings.TrimSpace(query) == "" || len(query) > 4000 {
			callErr = errors.New("External context requires a nonempty query up to 4000 bytes and the frozen context revision.")
		} else {
			result, callErr = external(ctx, query)
		}
	} else if name == "code_search" {
		result, callErr = executeReviewSearch(ctx, provider, lookupArgs)
	} else if provider != nil {
		result, callErr = provider.Execute(ctx, lookupArgs)
	}
	if callErr == nil && strings.HasPrefix(strings.TrimSpace(result), "Error:") {
		callErr = errors.New(result)
	}
	if callErr == nil && name == "file_read" && revision == r.head && r.activeRun != nil {
		result = r.checkReadDependencies(ctx, result)
	}
	if len(result) > 24000 {
		end := 24000
		for end > 0 && !utf8.RuneStart(result[end]) {
			end--
		}
		result = result[:end] + "\nIS_TRUNCATED: true. Output capped at 24000 bytes; narrow the line range or search."
	}
	result = "Revision: " + revision + "\n" + strings.ToValidUTF8(result, "\uFFFD")
	state, errorText := "completed", ""
	if callErr != nil {
		state = "failed"
		errorText = callErr.Error()
	}
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = r.server.store.db.ExecContext(saveCtx, "UPDATE review_tool_calls SET result=?,error=?,state=? WHERE id=?", result, errorText, state, id); err != nil {
		r.persistErr = err
		return "", err
	}
	if callErr == nil {
		r.cache[key] = reviewLookupHeader(id, name, revision, r.head, string(encoded), result, state) + fmt.Sprintf("\nRecover the saved response with read_review_evidence id=%d.", id)
	}
	if strings.Contains(result, "No matches") || strings.Contains(result, "No files") {
		r.consecutiveMisses++
	} else {
		r.consecutiveMisses = 0
	}
	return reviewLookupHeader(id, name, revision, r.head, string(encoded), result, state) + "\n" + result, callErr
}

func executeReviewSearch(ctx context.Context, provider tool.Provider, args map[string]any) (string, error) {
	query := make(map[string]any, len(args))
	for k, v := range args {
		query[k] = v
	}
	text, _ := args["search_text"].(string)
	mode, _ := args["search_mode"].(string)
	if mode != "" && mode != "literal" && mode != "regex" {
		return "", errors.New("search_mode must be literal or regex.")
	}
	if terms, ok := args["search_terms"].([]any); ok {
		if len(terms) == 0 || len(terms) > 8 || text != "" {
			return "", errors.New("Provide 1-8 search_terms or search_text, not both.")
		}
		var escaped []string
		for _, term := range terms {
			s, ok := term.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return "", errors.New("Each search term must be a nonempty literal string.")
			}
			escaped = append(escaped, regexp.QuoteMeta(s))
		}
		query["search_text"] = strings.Join(escaped, "|")
		query["use_perl_regexp"] = true
	} else {
		regex, _ := args["use_perl_regexp"].(bool)
		if mode != "" {
			regex = mode == "regex"
		}
		if !regex && mode == "" && strings.Contains(text, "|") {
			return "", errors.New("Ambiguous literal query containing '|'. For alternatives use search_terms or search_mode=regex. For an actual pipe character use search_mode=literal. This query was not executed and is not evidence of absence.")
		}
		query["use_perl_regexp"] = regex
	}
	return provider.Execute(ctx, query)
}

type reviewUsageClient struct {
	mu             sync.Mutex
	client         llm.LLMClient
	used, requests int64
	failure        string
	budget         int64
	progress       func(int64, int64) error
	onRequest      func(time.Duration, error)
	onDispatch     func()
	prepareRequest func(context.Context, llm.ChatRequest) llm.ChatRequest
}

func (b *reviewUsageClient) CompletionsWithCtx(ctx context.Context, req llm.ChatRequest) (response *llm.ChatResponse, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.prepareRequest != nil {
		req = b.prepareRequest(ctx, req)
	}
	encoded, err := json.Marshal(struct {
		Messages []llm.Message
		Tools    []llm.ToolDef
	}{req.Messages, req.Tools})
	if err != nil {
		return nil, err
	}
	estimate := int64(llm.CountTokensForModel(string(encoded), req.Model)*6/5 + 1024)
	if b.budget > 0 {
		remaining := b.budget - b.used - estimate
		if remaining < 512 {
			b.failure = "Context review has insufficient remaining token budget for the next request. Saved evidence is retained; increase the budget and review again."
			return nil, errors.New(b.failure)
		}
		req.MaxTokens = min(req.MaxTokens, int(remaining))
	}
	b.requests++
	if b.onDispatch != nil {
		b.onDispatch()
	}
	started := time.Now()
	defer func() {
		if b.onRequest != nil {
			b.onRequest(time.Since(started), err)
		}
		if b.progress != nil {
			if saveErr := b.progress(b.requests, b.used); saveErr != nil && err == nil {
				response, err = nil, saveErr
			}
		}
	}()
	response, err = b.client.CompletionsWithCtx(ctx, req)
	if err != nil {
		b.used += estimate + int64(req.MaxTokens)
		return nil, err
	}
	if response == nil || len(response.Choices) == 0 {
		return nil, errors.New("The model returned no response.")
	}
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		b.used += response.Usage.TotalTokens
	} else {
		b.used += estimate + int64(req.MaxTokens)
	}
	reason := response.Choices[0].FinishReason
	if reason != "stop" && reason != "tool_calls" {
		b.failure = "The model response was incomplete. Saved evidence is retained; check the model output limit and review again."
		return nil, errors.New(b.failure)
	}
	return response, nil
}
