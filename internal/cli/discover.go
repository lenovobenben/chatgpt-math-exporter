package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lihd/chatgpt-math-exporter/internal/exporters"
)

func runDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		projectPageURL string
		cookieFile     string
		outputList     string
	)

	fs.StringVar(&projectPageURL, "project-page-url", "", "ChatGPT project page URL to crawl for conversation links")
	fs.StringVar(&cookieFile, "cookie-file", "", "Path to a file that contains the ChatGPT session cookie header")
	fs.StringVar(&outputList, "output-list", "", "Path to the output text file for discovered conversation URLs")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printDiscoverHelp(os.Stdout)
			return nil
		}
		return fmt.Errorf("%w\n\nRun `cgme discover --help` to see supported options", err)
	}

	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	if projectPageURL == "help" || outputList == "help" {
		printDiscoverHelp(os.Stdout)
		return nil
	}

	if err := requireValue("project-page-url", projectPageURL); err != nil {
		return err
	}
	if err := requireValue("output-list", outputList); err != nil {
		return err
	}

	return exporters.DiscoverProjectPageURLs(projectPageURL, cookieFile, outputList)
}

func printDiscoverHelp(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  cgme discover [options]

Options:
  --project-page-url <url>  ChatGPT project page URL to crawl
  --cookie-file <path>      File that contains the ChatGPT session cookie header
  --output-list <path>      Output text file for discovered conversation URLs
  --help                    Show this help text

Examples:
  cgme discover --project-page-url "https://chatgpt.com/g/..." --cookie-file ~/Desktop/gpt-cookie.txt --output-list ./math-sessions.txt
`)
}
