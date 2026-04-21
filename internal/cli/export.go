package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lihd/chatgpt-math-exporter/internal/config"
	"github.com/lihd/chatgpt-math-exporter/internal/exporters"
)

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		bundlePath   string
		cookieFile   string
		project      string
		projectURL   string
		urlList      string
		outputDir    string
		assetsDir    string
		configPath   string
		writeReadme  bool
		writeWarning bool
		preserveLink bool
		overwrite    bool
		fixUserLatex bool
	)

	fs.StringVar(&bundlePath, "bundle", "", "Path to a ChatGPT official export directory")
	fs.StringVar(&cookieFile, "cookie-file", "", "Path to a file that contains the ChatGPT session cookie header")
	fs.StringVar(&project, "project", "", "Project name inside the export bundle")
	fs.StringVar(&projectURL, "project-url", "", "ChatGPT project URL")
	fs.StringVar(&urlList, "url-list", "", "Path to a text file that contains one ChatGPT project URL per line")
	fs.StringVar(&outputDir, "output", "", "Output directory")
	fs.StringVar(&assetsDir, "assets-dir", "", "Assets directory override")
	fs.StringVar(&configPath, "config", "", "Path to a config file")
	fs.BoolVar(&writeReadme, "write-readme", true, "Write an output README.md")
	fs.BoolVar(&writeWarning, "write-warnings", true, "Write warnings.json")
	fs.BoolVar(&preserveLink, "preserve-links", true, "Preserve external links in Markdown")
	fs.BoolVar(&overwrite, "overwrite", false, "Overwrite existing successful exports instead of skipping them")
	fs.BoolVar(&fixUserLatex, "fix-user-latex", false, "Conservatively wrap obvious naked LaTeX in user messages for display")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printExportHelp(os.Stdout)
			return nil
		}
		return fmt.Errorf("%w\n\nRun `cgme export --help` to see supported options", err)
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	if configPath == "help" {
		printExportHelp(os.Stdout)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	visited := visitedFlags(fs)
	overrideWithFlags(&cfg, visited, bundlePath, cookieFile, project, projectURL, urlList, outputDir, assetsDir, writeReadme, writeWarning, preserveLink, overwrite, fixUserLatex)

	if err := cfg.Validate(); err != nil {
		return err
	}

	return exporters.Run(cfg)
}

func printExportHelp(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  cgme export [options]

Options:
  --bundle <dir>         Path to ChatGPT official export directory
  --cookie-file <path>   File that contains the ChatGPT session cookie header
  --project <name>       Project name inside the bundle
  --project-url <url>    ChatGPT project URL
  --url-list <path>      Text file with one ChatGPT project URL per line
  --output <dir>         Output directory
  --assets-dir <dir>     Assets directory override
  --config <path>        Optional config file
  --write-readme         Write README.md into the output directory (default true)
  --write-warnings       Write warnings.json into the output directory (default true)
  --preserve-links       Preserve external links in rendered Markdown (default true)
  --overwrite            Overwrite existing successful exports instead of skipping them
  --fix-user-latex       Conservatively wrap obvious naked LaTeX in user messages for display
  --help                 Show this help text

Examples:
  cgme export --bundle ./chatgpt-export --output ./my-notes
  cgme export --bundle ./chatgpt-export --project "Classic Math" --output ./my-notes
  cgme export --cookie-file ~/Desktop/gpt-cookie.txt --project-url "https://chatgpt.com/..." --output ./my-notes
  cgme export --cookie-file ~/Desktop/gpt-cookie.txt --url-list ~/Desktop/math-sessions.txt --output ./my-notes
  cgme export --project-url "https://chatgpt.com/..." --output ./my-notes
  cgme export --config ./cgme.yaml
`)
}

func overrideWithFlags(
	cfg *config.Config,
	visited map[string]bool,
	bundlePath string,
	cookieFile string,
	project string,
	projectURL string,
	urlList string,
	outputDir string,
	assetsDir string,
	writeReadme bool,
	writeWarning bool,
	preserveLink bool,
	overwrite bool,
	fixUserLatex bool,
) {
	if bundlePath != "" {
		cfg.Source.Type = "bundle"
		cfg.Source.Path = bundlePath
	}
	if project != "" {
		cfg.Source.Project = project
	}
	if projectURL != "" {
		cfg.Source.Type = "project_url"
		cfg.Source.ProjectURL = projectURL
	}
	if urlList != "" {
		cfg.Source.Type = "project_url_list"
		cfg.Source.URLList = urlList
	}
	if cookieFile != "" {
		cfg.Source.CookieFile = cookieFile
	}
	if outputDir != "" {
		cfg.Output.Dir = outputDir
	}
	if assetsDir != "" {
		cfg.Output.AssetsDir = assetsDir
	}

	if visited["write-readme"] {
		cfg.Options.WriteReadme = writeReadme
	}
	if visited["write-warnings"] {
		cfg.Options.WriteWarnings = writeWarning
	}
	if visited["preserve-links"] {
		cfg.Options.PreserveLinks = preserveLink
	}
	if visited["overwrite"] {
		cfg.Options.OverwriteExisting = overwrite
	}
	if visited["fix-user-latex"] {
		cfg.Options.FixUserLatex = fixUserLatex
	}
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	out := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		out[f.Name] = true
	})
	return out
}
