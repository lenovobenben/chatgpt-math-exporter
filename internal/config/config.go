package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Source  SourceConfig `json:"source"`
	Output  OutputConfig `json:"output"`
	Options OptionConfig `json:"options"`
}

type SourceConfig struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Project    string `json:"project"`
	ProjectURL string `json:"project_url"`
	URLList    string `json:"url_list"`
	CookieFile string `json:"cookie_file"`
}

type OutputConfig struct {
	Dir       string `json:"dir"`
	AssetsDir string `json:"assets_dir"`
}

type OptionConfig struct {
	WriteReadme       bool `json:"write_readme"`
	WriteWarnings     bool `json:"write_warnings"`
	PreserveLinks     bool `json:"preserve_links"`
	OverwriteExisting bool `json:"overwrite_existing"`
	FixUserLatex      bool `json:"fix_user_latex"`
}

func Default() Config {
	return Config{
		Output: OutputConfig{
			Dir: "./output",
		},
		Options: OptionConfig{
			WriteReadme:       true,
			WriteWarnings:     true,
			PreserveLinks:     true,
			OverwriteExisting: false,
			FixUserLatex:      false,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse JSON config %q: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := parseSimpleYAML(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse YAML config %q: %w", path, err)
		}
	default:
		return Config{}, fmt.Errorf("unsupported config file %q: use .json, .yaml, or .yml", path)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Source.Type == "" {
		if c.Source.Path != "" {
			c.Source.Type = "bundle"
		} else if c.Source.URLList != "" {
			c.Source.Type = "project_url_list"
		} else if c.Source.ProjectURL != "" {
			c.Source.Type = "project_url"
		}
	}

	switch c.Source.Type {
	case "bundle":
		if err := require("source.path", c.Source.Path); err != nil {
			return err
		}
		if _, err := os.Stat(c.Source.Path); err != nil {
			return fmt.Errorf("bundle path %q is not accessible: %w", c.Source.Path, err)
		}
	case "project_url":
		if err := require("source.project_url", c.Source.ProjectURL); err != nil {
			return err
		}
		if strings.TrimSpace(c.Source.CookieFile) != "" {
			if _, err := os.Stat(c.Source.CookieFile); err != nil {
				return fmt.Errorf("cookie file %q is not accessible: %w", c.Source.CookieFile, err)
			}
		}
	case "project_url_list":
		if err := require("source.url_list", c.Source.URLList); err != nil {
			return err
		}
		if _, err := os.Stat(c.Source.URLList); err != nil {
			return fmt.Errorf("URL list file %q is not accessible: %w", c.Source.URLList, err)
		}
		if strings.TrimSpace(c.Source.CookieFile) != "" {
			if _, err := os.Stat(c.Source.CookieFile); err != nil {
				return fmt.Errorf("cookie file %q is not accessible: %w", c.Source.CookieFile, err)
			}
		}
	default:
		return errors.New("source type must be `bundle`, `project_url`, or `project_url_list`")
	}

	if c.Output.Dir == "" {
		c.Output.Dir = "./output"
	}
	if c.Output.AssetsDir == "" {
		c.Output.AssetsDir = filepath.Join(c.Output.Dir, "assets")
	}

	return nil
}

func require(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}
