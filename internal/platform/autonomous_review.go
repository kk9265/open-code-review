// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alibaba/open-code-review/internal/llm"
	"github.com/alibaba/open-code-review/internal/tool"
)

func (r *reviewContext) saveAutonomousReport(ctx context.Context, report, reason string) (string, error) {
	if strings.TrimSpace(report) == "" || len(report) > 128<<10 {
		return r.report, errors.New("The model did not return a usable Markdown report. Saved lookup evidence remains available.")
	}
	r.report = report
	phase := "complete"
	if reason != "" {
		phase = "partial"
	}
	if _, err := r.server.store.db.ExecContext(ctx, "UPDATE review_jobs SET result=? WHERE id=? AND state='running'", report, r.job.ID); err != nil {
		return report, err
	}
	if _, err := r.server.store.db.ExecContext(ctx, "UPDATE review_executions SET phase=?,stop_reason=? WHERE job_id=?", phase, reason, r.job.ID); err != nil {
		return report, err
	}
	if reason != "" {
		return report, errors.New(reason)
	}
	return report, nil
}

func (r *reviewContext) runAutonomousReview(ctx context.Context, client *reviewUsageClient, registry *tool.Registry, defs []llm.ToolDef, system string, maxTokens int) (string, error) {
	defer func() {
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = r.server.store.db.ExecContext(saveCtx, "UPDATE review_executions SET phase='partial',stop_reason=CASE WHEN stop_reason='' THEN 'Review stopped before report completion.' ELSE stop_reason END WHERE job_id=? AND phase NOT IN ('complete','partial')", r.job.ID)
	}()
	if r.persistErr != nil {
		return "", r.persistErr
	}
	_, err := r.server.store.db.ExecContext(ctx, "UPDATE review_executions SET phase='investigating' WHERE job_id=?", r.job.ID)
	if err != nil {
		return "", err
	}
	messages := []llm.Message{llm.NewTextMessage("system", system), llm.NewTextMessage("user", r.job.Input), llm.NewTextMessage("user", "Context availability (not review findings):\n"+r.contextNotice)}
	investigate := ctx
	cancel := func() {}
	if deadline, ok := ctx.Deadline(); ok {
		investigate, cancel = context.WithDeadline(ctx, deadline.Add(-3*time.Minute))
	}
	defer cancel()
	stopReason := "Investigation request limit reached; report coverage may be incomplete."
	for round := 0; round < 24; round++ {
		if investigate.Err() != nil {
			stopReason = "Investigation time reserved for reporting; coverage may be incomplete."
			break
		}
		response, err := client.CompletionsWithCtx(investigate, llm.ChatRequest{Model: r.job.Model, Messages: messages, Tools: defs, MaxTokens: maxTokens})
		if err != nil {
			stopReason = "Investigation request failed; coverage is incomplete: " + err.Error()
			break
		}
		calls := response.ToolCalls()
		if len(calls) == 0 {
			return r.saveAutonomousReport(ctx, response.VisibleContent(), "")
		}
		messages = append(messages, llm.NewToolCallMessage(response.Content(), calls, response.Native(), response.ReasoningContent()))
		for _, call := range calls {
			var args map[string]any
			var result string
			callErr := json.Unmarshal([]byte(call.Function.Arguments), &args)
			if callErr == nil {
				if provider, ok := registry.Get(call.Function.Name); ok {
					result, callErr = provider.Execute(investigate, args)
				} else {
					callErr = errors.New("Unknown tool; use an advertised read-only tool or return your final report.")
				}
			}
			if callErr != nil {
				result = fmt.Sprintf("Lookup failed (not evidence of absence): %s\n%s", callErr, result)
			}
			messages = append(messages, llm.NewToolResultMessage(call.ID, result))
		}
		if r.persistErr != nil {
			return r.report, r.persistErr
		}
		if r.calls >= 80 {
			stopReason = "Code lookup limit reached; report coverage may be incomplete."
			break
		}
	}
	if _, err = r.server.store.db.ExecContext(ctx, "UPDATE review_executions SET phase='reporting',stop_reason=? WHERE job_id=?", stopReason, r.job.ID); err != nil {
		return r.report, err
	}
	messages = append(messages, llm.NewTextMessage("user", "Investigation stopped: "+stopReason+"\nReturn a concise Markdown report now using only existing evidence. No tools are available. Explicitly state incomplete coverage. Do not turn unverified possibilities into confirmed defects."))
	response, err := client.CompletionsWithCtx(ctx, llm.ChatRequest{Model: r.job.Model, Messages: messages, MaxTokens: maxTokens})
	if err != nil {
		return r.report, fmt.Errorf("Report generation failed; saved lookup evidence remains available: %w", err)
	}
	if len(response.ToolCalls()) != 0 {
		return r.report, errors.New("The final response requested unavailable tools instead of returning a report.")
	}
	return r.saveAutonomousReport(ctx, response.VisibleContent(), stopReason)
}
