// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"html"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var reviewSeverityLabel = regexp.MustCompile(`(?i)(?:\bSeverity|\x{4e25}\x{91cd}\x{7a0b}\x{5ea6})\s*[:\x{ff1a}]\s*(critical|high|medium|low)\b`)

// Decorate sanitized report text without interpreting model HTML or code examples.
func reviewSeverityBadges(rendered string, t func(string) string) string {
	tokens := xhtml.NewTokenizer(strings.NewReader(rendered))
	var output strings.Builder
	paragraph, codeDepth := false, 0
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			break
		}
		raw := string(tokens.Raw())
		switch kind {
		case xhtml.StartTagToken, xhtml.EndTagToken:
			name, _ := tokens.TagName()
			switch string(name) {
			case "p":
				paragraph = kind == xhtml.StartTagToken
			case "pre", "code", "a":
				if kind == xhtml.StartTagToken {
					codeDepth++
				} else {
					codeDepth--
				}
			}
		case xhtml.TextToken:
			if paragraph && codeDepth == 0 {
				text := string(tokens.Text())
				matches := reviewSeverityLabel.FindAllStringSubmatchIndex(text, -1)
				if len(matches) > 0 {
					cursor := 0
					for _, match := range matches {
						start, end := match[2], match[3]
						severity := strings.ToLower(text[start:end])
						output.WriteString(html.EscapeString(text[cursor:start]))
						output.WriteString(`<span class="severity-badge severity-` + severity + `">`)
						output.WriteString(html.EscapeString(t("severity." + severity)))
						output.WriteString(`</span>`)
						cursor = end
					}
					output.WriteString(html.EscapeString(text[cursor:]))
					continue
				}
			}
		}
		output.WriteString(raw)
	}
	return output.String()
}
