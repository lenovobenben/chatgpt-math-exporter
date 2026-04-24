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

var specialFunctionOperatorReplacer = strings.NewReplacer(
	`\operatorname{erf}`, `\mathrm{erf}`,
	`\operatorname{erfi}`, `\mathrm{erfi}`,
	`\operatorname{erfc}`, `\mathrm{erfc}`,
	`\operatorname{Si}`, `\mathrm{Si}`,
	`\operatorname{Ci}`, `\mathrm{Ci}`,
	`\operatorname{Li}`, `\mathrm{Li}`,
	`\operatorname{Ei}`, `\mathrm{Ei}`,
)

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
	return strings.Join(parts, "`"), warnings
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
	if updated := specialFunctionOperatorReplacer.Replace(replaced); updated != replaced {
		replaced = updated
	}
	return replaced, count
}
