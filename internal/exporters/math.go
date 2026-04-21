package exporters

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type normalizeMathOptions struct {
	Role         string
	FixUserLatex bool
}

var mathSymbolReplacer = strings.NewReplacer(
	"∞", `\infty`,
	"≤", `\le`,
	"≥", `\ge`,
	"→", `\to`,
	"∈", `\in`,
	"≠", `\neq`,
	"±", `\pm`,
	"×", `\times`,
	"÷", `\div`,
)

func normalizeMathText(input string, opts normalizeMathOptions) (string, []warningRecord) {
	lines := strings.Split(input, "\n")
	inFence := false
	warnings := make([]warningRecord, 0)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if opts.FixUserLatex && strings.EqualFold(strings.TrimSpace(opts.Role), "user") {
			line, warnings = wrapObviousUserLatex(line, warnings)
		}
		lines[i], warnings = normalizeInlineCodeFreeText(line, warnings)
	}

	return wrapStandaloneMathBlocks(lines, warnings)
}

func wrapObviousUserLatex(line string, warnings []warningRecord) (string, []warningRecord) {
	if strings.TrimSpace(line) == "" || strings.Contains(line, "$") || strings.Contains(line, "`") {
		return line, warnings
	}

	type span struct {
		start int
		end   int
	}

	spans := make([]span, 0)
	for i := 0; i < len(line); i++ {
		if line[i] != '\\' {
			continue
		}
		start := i
		end := obviousLatexSpanEnd(line, start)
		if end <= start {
			continue
		}
		snippet := strings.TrimSpace(line[start:end])
		if !looksLikeObviousLatexSpan(snippet) {
			continue
		}
		spans = append(spans, span{start: start, end: end})
		i = end - 1
	}
	if len(spans) == 0 {
		return line, warnings
	}

	var b strings.Builder
	last := 0
	wrapped := 0
	for _, s := range spans {
		if s.start < last {
			continue
		}
		b.WriteString(line[last:s.start])
		snippet := strings.TrimSpace(line[s.start:s.end])
		if snippet == "" {
			b.WriteString(line[s.start:s.end])
			last = s.end
			continue
		}
		leading := leadingWhitespace(line[s.start:s.end])
		trailing := trailingWhitespace(line[s.start:s.end])
		b.WriteString(leading)
		b.WriteString("$")
		b.WriteString(strings.TrimSpace(snippet))
		b.WriteString("$")
		b.WriteString(trailing)
		last = s.end
		wrapped++
	}
	b.WriteString(line[last:])
	if wrapped > 0 {
		warnings = append(warnings, warningRecord{
			Code:    "math.user_latex_wrapped",
			Message: fmt.Sprintf("Wrapped %d obvious naked LaTeX span(s) in a user message.", wrapped),
		})
	}
	return b.String(), warnings
}

func obviousLatexSpanEnd(line string, start int) int {
	seenBraceOrCommandPayload := false
	depthCurly := 0
	depthRound := 0
	depthSquare := 0
	i := start
	for i < len(line) {
		r, size := utf8DecodeRuneInString(line[i:])
		if isCJKRune(r) {
			break
		}
		switch r {
		case '{':
			depthCurly++
			seenBraceOrCommandPayload = true
		case '}':
			if depthCurly == 0 {
				return i
			}
			depthCurly--
		case '(':
			depthRound++
		case ')':
			if depthRound > 0 {
				depthRound--
			}
		case '[':
			depthSquare++
		case ']':
			if depthSquare > 0 {
				depthSquare--
			}
		case ' ', '\t':
			if depthCurly == 0 && depthRound == 0 && depthSquare == 0 {
				next := nextNonSpaceRune(line[i+size:])
				if isCJKRune(next) || next == 0 {
					return i
				}
			}
		case ',', '，', '。', '；', ';', '：', ':', '.', '!', '?', '！', '？':
			if depthCurly == 0 && depthRound == 0 && depthSquare == 0 {
				return i
			}
		}
		i += size
		if depthCurly == 0 && depthRound == 0 && depthSquare == 0 && seenBraceOrCommandPayload {
			next := nextNonSpaceRune(line[i:])
			if isCJKRune(next) || next == 0 {
				return i
			}
		}
	}
	return i
}

func looksLikeObviousLatexSpan(s string) bool {
	if s == "" || !strings.Contains(s, `\`) {
		return false
	}
	if !strings.ContainsAny(s, "{}_^") && !strings.Contains(s, `\frac`) && !strings.Contains(s, `\sqrt`) {
		return false
	}
	return obviousLatexCommandPattern.MatchString(s)
}

var obviousLatexCommandPattern = regexp.MustCompile(`\\(frac|sqrt|sum|prod|int|lim|sin|cos|tan|log|ln|alpha|beta|gamma|delta|theta|lambda|mu|pi|sigma|phi|psi|omega|cdot|times|div|le|ge|neq|in|notin|subset|supset|left|right|mathbb|mathrm|mathbf|operatorname|overline|underline)`)

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += size
	}
	return s[:i]
}

func trailingWhitespace(s string) string {
	i := len(s)
	for i > 0 {
		r, size := utf8DecodeLastRuneInString(s[:i])
		if !unicode.IsSpace(r) {
			break
		}
		i -= size
	}
	return s[i:]
}

func nextNonSpaceRune(s string) rune {
	for len(s) > 0 {
		r, size := utf8DecodeRuneInString(s)
		if !unicode.IsSpace(r) {
			return r
		}
		s = s[size:]
	}
	return 0
}

func isCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han)
}

func utf8DecodeRuneInString(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	return r, size
}

func utf8DecodeLastRuneInString(s string) (rune, int) {
	r, size := utf8.DecodeLastRuneInString(s)
	return r, size
}

func normalizeInlineCodeFreeText(line string, warnings []warningRecord) (string, []warningRecord) {
	parts := strings.Split(line, "`")
	for i := 0; i < len(parts); i += 2 {
		updated, replaced := replaceMathSymbols(parts[i])
		parts[i] = updated
		if replaced > 0 {
			warnings = append(warnings, warningRecord{
				Code:    "math.symbol_normalized",
				Message: fmt.Sprintf("Normalized %d Unicode math symbol(s) in plain text.", replaced),
			})
		}
	}
	return strings.Join(parts, "`"), warnings
}

func wrapStandaloneMathBlocks(lines []string, warnings []warningRecord) (string, []warningRecord) {
	out := make([]string, 0, len(lines))
	inFence := false
	mathBlock := make([]string, 0)

	flushMathBlock := func() {
		if len(mathBlock) == 0 {
			return
		}
		out = append(out, "```math")
		out = append(out, mathBlock...)
		out = append(out, "```")
		warnings = append(warnings, warningRecord{
			Code:    "math.block_wrapped",
			Message: fmt.Sprintf("Wrapped %d standalone formula line(s) into a math block.", len(mathBlock)),
		})
		mathBlock = mathBlock[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			flushMathBlock()
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if isStandaloneMathLine(trimmed) {
			mathBlock = append(mathBlock, trimmed)
			continue
		}
		flushMathBlock()
		out = append(out, line)
	}

	flushMathBlock()
	return strings.Join(out, "\n"), warnings
}

var longWordPattern = regexp.MustCompile(`[A-Za-z]{3,}`)

func isStandaloneMathLine(line string) bool {
	if line == "" {
		return false
	}
	if strings.Contains(line, "`") || strings.Contains(line, "http") || strings.Contains(line, "$") {
		return false
	}
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return false
	}
	if strings.ContainsAny(line, ".!?:;。！？：；") {
		return false
	}
	if !hasMathSignal(line) {
		return false
	}

	longWords := longWordPattern.FindAllString(line, -1)
	for _, word := range longWords {
		if !isAllowedMathWord(word) {
			return false
		}
	}

	return true
}

func hasMathSignal(line string) bool {
	signals := []string{
		"=", "^", "_", `\le`, `\ge`, `\in`, `\to`, `\neq`, `\pm`, `\times`, `\div`, `\infty`,
	}
	for _, signal := range signals {
		if strings.Contains(line, signal) {
			return true
		}
	}
	return false
}

func isAllowedMathWord(word string) bool {
	switch strings.ToLower(word) {
	case "sin", "cos", "tan", "log", "ln", "lim", "max", "min":
		return true
	default:
		return false
	}
}

func replaceMathSymbols(input string) (string, int) {
	count := 0
	for _, symbol := range []string{"∞", "≤", "≥", "→", "∈", "≠", "±", "×", "÷"} {
		count += strings.Count(input, symbol)
	}
	return mathSymbolReplacer.Replace(input), count
}
