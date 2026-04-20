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
	Source  SourceConfig  `json:"source"`
	Output  OutputConfig  `json:"output"`
	Options OptionConfig  `json:"options"`
}

type SourceConfig struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Project    string `json:"project"`
	ProjectURL string `json:"project_url"`
}

type OutputConfig struct {
	Dir       string `json:"dir"`
	AssetsDir string `json:"assets_dir"`
}

type OptionConfig struct {
	WriteReadme   bool `json:"write_readme"`
	WriteWarnings bool `json:"write_warnings"`
	PreserveLinks bool `json:"preserve_links"`
}

func Default() Config {
	return Config{
		Output: OutputConfig{
			Dir: "./output",
		},
		Options: OptionConfig{
			WriteReadme:   true,
			WriteWarnings: true,
			PreserveLinks: true,
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
	default:
		return errors.New("source type must be `bundle` or `project_url`")
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
