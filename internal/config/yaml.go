package config

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

func parseSimpleYAML(data []byte, cfg *Config) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	section := ""

	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !strings.HasPrefix(raw, " ") && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if section == "" {
			return fmt.Errorf("line %d: expected a top-level section such as source:, output:, or options:", lineNo)
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("line %d: expected key: value", lineNo)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"`)

		switch section {
		case "source":
			switch key {
			case "type":
				cfg.Source.Type = value
			case "path":
				cfg.Source.Path = value
			case "project":
				cfg.Source.Project = value
			case "project_url":
				cfg.Source.ProjectURL = value
			case "url_list":
				cfg.Source.URLList = value
			case "cookie_file":
				cfg.Source.CookieFile = value
			default:
				return fmt.Errorf("line %d: unknown source key %q", lineNo, key)
			}
		case "output":
			switch key {
			case "dir":
				cfg.Output.Dir = value
			case "assets_dir":
				cfg.Output.AssetsDir = value
			default:
				return fmt.Errorf("line %d: unknown output key %q", lineNo, key)
			}
		case "options":
			switch key {
			case "write_readme":
				cfg.Options.WriteReadme = parseBool(value)
			case "write_warnings":
				cfg.Options.WriteWarnings = parseBool(value)
			case "preserve_links":
				cfg.Options.PreserveLinks = parseBool(value)
			case "overwrite_existing":
				cfg.Options.OverwriteExisting = parseBool(value)
			default:
				return fmt.Errorf("line %d: unknown options key %q", lineNo, key)
			}
		default:
			return fmt.Errorf("line %d: unknown section %q", lineNo, section)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on", "1":
		return true
	default:
		return false
	}
}
