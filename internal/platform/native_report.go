// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"fmt"
	"regexp"
	"strings"
)

type nativeDecision struct {
	Index          int    `json:"index"`
	State          string `json:"state"`
	Reason         string `json:"reason"`
	EvidenceCommit string `json:"evidence_commit"`
	EvidencePath   string `json:"evidence_path"`
	EvidenceLine   string `json:"evidence_line"`
}

func (r *reviewContext) nativeReport(run nativeReviewRun) string {
	return r.server.nativeResult(run)
}

var reviewAdviceLabel = regexp.MustCompile("(?i)^(?:suggestions?|recommendations?|recommended fix|proposed fix|how to fix|fix|remediation|\u5efa\u8bae|\u4fee\u590d\u5efa\u8bae|\u4fee\u590d\u65b9\u6848|\u4fee\u590d\u65b9\u5f0f|\u4fee\u590d\u4ee3\u7801|\u4fee\u6539\u5efa\u8bae)(?:\\s*[:\\x{ff1a}]|\\s*$)")

// Historical comments retain their original text; only explicitly labeled advice is omitted from the report.
func findingDescription(content string) string {
	var kept []string
	skipHeading, skipParagraph, pendingCode := 0, false, false
	fence, omitFence := "", false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if !omitFence {
				kept = append(kept, line)
			}
			if strings.HasPrefix(trimmed, fence) {
				fence, omitFence = "", false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			omitFence = pendingCode || skipHeading > 0 || skipParagraph
			if !omitFence {
				kept = append(kept, line)
			}
			pendingCode, skipParagraph = false, false
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if skipHeading > 0 {
			if level == 0 || level > skipHeading {
				continue
			}
			skipHeading = 0
		}
		label := strings.TrimSpace(strings.TrimLeft(strings.ReplaceAll(trimmed, "**", ""), "#- \t"))
		if reviewAdviceLabel.MatchString(label) {
			skipHeading, skipParagraph, pendingCode = level, true, true
			continue
		}
		if trimmed == "" {
			skipParagraph = false
		} else if skipParagraph {
			continue
		} else {
			pendingCode = false
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func (s *Server) nativeResult(run nativeReviewRun) string {
	t := s.text
	var report strings.Builder
	if run.Direct != nil {
		for i, finding := range run.Findings {
			fmt.Fprintf(&report, "\n## %s %d · `%s:%d`\n\n**%s: %s** · %s: %s\n\n%s\n\n%s: %s\n\n%s: %s\n", t("Finding"), i+1, finding.Path, finding.Line, t("Model assessment"), t("finding."+finding.Facts.Confidence), t("Severity"), finding.Severity, findingDescription(finding.Content), t("Trigger"), finding.Facts.Trigger, t("Impact"), finding.Facts.Impact)
		}
		if run.Partial {
			fmt.Fprintf(&report, "\n%s\n", t("Review incomplete; saved findings are provisional and coverage gaps remain in the review process."))
		}
		if len(run.Findings) == 0 && !run.Partial {
			fmt.Fprintf(&report, "%s\n", t("No findings were reported."))
		}
		return report.String()
	}
	verdicts := map[string]focusedVerdict{}
	for _, finding := range run.Findings {
		for _, verdict := range run.Verdicts {
			if verdict.FindingID == finding.ID {
				verdicts[findingKey(finding.Path, finding.Content)] = verdict
			}
		}
	}
	decisions := make(map[int]nativeDecision)
	for _, decision := range run.Decisions {
		decisions[decision.Index] = decision
	}
	index := 0
	reported := map[string]bool{}
	incomplete := run.Partial || run.VerificationState == "incomplete"
	for _, commit := range run.Commits {
		incomplete = incomplete || commit.Error != "" || len(commit.Excluded) > 0 || (commit.Manifest != nil && commit.Manifest.TerminalState != "complete")
		for _, cm := range commit.Comments {
			verdict, evaluated := verdicts[findingKey(cm.Path, cm.Content)]
			if evaluated && verdict.State == "rejected" {
				continue
			}
			reported[findingKey(cm.Path, cm.Content)] = true
			decision := decisions[index]
			fmt.Fprintf(&report, "\n## %s %d · `%s:%d`\n\n%s: %s\n\n", t("Finding"), index+1, cm.Path, cm.StartLine, t("Severity"), cm.Severity)
			if len(run.Commits) > 1 {
				fmt.Fprintf(&report, "%s: `%s`\n\n", t("Commit"), commit.Commit)
			}
			if decision.State != "" {
				fmt.Fprintf(&report, "**%s**: %s\n\n", t("native.finding."+decision.State), decision.Reason)
			}
			if evaluated {
				fmt.Fprintf(&report, "**%s**: %s\n\n", t("finding."+verdict.State), verdict.Reason)
			}
			fmt.Fprintf(&report, "%s\n", findingDescription(cm.Content))
			if decision.State == "resolved" {
				fmt.Fprintf(&report, "\n%s: `%s` · `%s`\n\n```\n%s\n```\n", t("Later selected fix"), decision.EvidenceCommit, decision.EvidencePath, decision.EvidenceLine)
			}
			index++
		}
	}
	for _, finding := range run.Findings {
		rejected := false
		for _, verdict := range run.Verdicts {
			rejected = rejected || (verdict.FindingID == finding.ID && verdict.State == "rejected")
		}
		if rejected || reported[findingKey(finding.Path, finding.Content)] {
			continue
		}
		index++
		fmt.Fprintf(&report, "\n## %s %d · `%s:%d`\n\n**%s**\n\n%s\n", t("Finding"), index, finding.Path, finding.Line, t("Candidate saved; verification incomplete. This is not a confirmed finding."), findingDescription(finding.Content))
	}
	if index == 0 {
		if incomplete {
			fmt.Fprintf(&report, "%s\n", t("Review incomplete; no finding conclusion was produced. Saved checks and evidence remain available in the review process."))
		} else {
			fmt.Fprintf(&report, "%s\n", t("No findings were reported."))
		}
	}
	return report.String()
}
