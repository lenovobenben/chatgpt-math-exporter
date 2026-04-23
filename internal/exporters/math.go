package exporters

import mdrender "github.com/lihd/chatgpt-math-exporter/internal/render/markdown"

type normalizeMathOptions struct{}

func normalizeMathText(input string, opts normalizeMathOptions) (string, []warningRecord) {
	normalized, warnings := mdrender.NormalizeMathText(input, mdrender.NormalizeOptions{})
	return normalized, toWarningRecords(warnings)
}

func toWarningRecords(warnings []mdrender.Warning) []warningRecord {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]warningRecord, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, warningRecord{
			Code:    warning.Code,
			Message: warning.Message,
		})
	}
	return out
}
