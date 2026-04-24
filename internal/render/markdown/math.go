package markdown

import (
	"fmt"
	"regexp"
	"strings"
)

type NormalizeOptions struct{}

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

// bareLT/GT patterns: replace raw < and > with \lt / \gt, adding a space
// if needed to prevent \ltxyz being parsed as a single command.
var bareLTPattern = regexp.MustCompile(`<`)
var bareGTPattern = regexp.MustCompile(`>`)

var operatornamePattern = regexp.MustCompile(`\\operatorname\{([^{}]+)\}`)

func rewriteOperatorname(input string) string {
	return operatornamePattern.ReplaceAllString(input, `\mathrm{$1}`)
}

func NormalizeMathText(input string, opts NormalizeOptions) (string, []Warning) {
	lines := strings.Split(input, "\n")
	inFence := false
	warnings := make([]Warning, 0)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i], warnings = normalizeInlineCodeFreeText(line, warnings)
	}

	return wrapStandaloneMathBlocks(lines, warnings)
}

var displayMathBracketPattern = regexp.MustCompile(`\\\[((?:[^\\]|\\[^\]])*?)\\\]`)

// inlineMathPattern matches $...$ inline math (no nesting, no $ inside).
var inlineMathPattern = regexp.MustCompile(`\$([^\$]+)\$`)

func normalizeInlineCodeFreeText(line string, warnings []Warning) (string, []Warning) {
	parts := strings.Split(line, "`")
	for i := 0; i < len(parts); i += 2 {
		updated, replaced := replaceMathSymbols(parts[i])
		parts[i] = updated
		if replaced > 0 {
			warnings = append(warnings, Warning{
				Code:    "math.symbol_normalized",
				Message: fmt.Sprintf("Normalized %d Unicode math symbol(s) in plain text.", replaced),
			})
		}
	}
	joined := strings.Join(parts, "`")
	// Strip inline \[...\] display math brackets, keeping only the content.
	// These are LaTeX display math delimiters that don't work in GitHub Markdown.
	joined = displayMathBracketPattern.ReplaceAllString(joined, "$1")
	// In GitHub Markdown, \{ and \} inside $...$ inline math get their backslash
	// consumed by the Markdown parser, causing MathJax "Extra open brace" errors.
	// Replace with \lbrace/\rbrace which are semantically equivalent and safe.
	joined = inlineMathPattern.ReplaceAllStringFunc(joined, func(match string) string {
		inner := match[1 : len(match)-1]
		inner = strings.ReplaceAll(inner, `\{`, `\lbrace `)
		inner = strings.ReplaceAll(inner, `\}`, `\rbrace `)
		inner = rewriteAngleBrackets(inner)
		return "$" + inner + "$"
	})
	return joined, warnings
}

func wrapStandaloneMathBlocks(lines []string, warnings []Warning) (string, []Warning) {
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
		warnings = append(warnings, Warning{
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
	replaced := mathSymbolReplacer.Replace(input)
	replaced = rewriteOperatorname(replaced)
	return replaced, count
}

// rewriteAngleBrackets replaces raw < and > with \lt{} / \gt{}.
// Only safe to call on pure math expressions or inside $...$ inline math.
// Must NOT be called on paragraph text where > may be plain English.
func rewriteAngleBrackets(input string) string {
	if strings.Contains(input, "<") {
		input = bareLTPattern.ReplaceAllString(input, `\lt{}`)
	}
	if strings.Contains(input, ">") {
		input = bareGTPattern.ReplaceAllString(input, `\gt{}`)
	}
	return input
}

// NormalizeMathExpression applies math-level normalization (symbol replacement,
// operatorname rewrite, angle bracket rewrite) to a pure math expression.
// This is format-agnostic and should be called by any renderer before emitting math content.
func NormalizeMathExpression(text string) string {
	result, _ := replaceMathSymbols(text)
	result = rewriteAngleBrackets(result)
	return result
}
