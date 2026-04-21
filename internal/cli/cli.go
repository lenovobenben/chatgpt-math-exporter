package cli

import (
	"errors"
	"fmt"
	"io"
)

func Run(args []string) error {
	if len(args) == 0 {
		printRootHelp(defaultStdout())
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printRootHelp(defaultStdout())
		return nil
	case "export":
		return runExport(args[1:])
	case "discover":
		return runDiscover(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\nRun `cgme --help` to see available commands", args[0])
	}
}

func printRootHelp(w io.Writer) {
	_, _ = fmt.Fprint(w, `CGME exports ChatGPT conversations into a local Markdown directory.

Usage:
  cgme <command> [options]

Commands:
  export     Export from a ChatGPT bundle or project URL
  discover   Discover project conversation URLs from a ChatGPT project page
  help       Show help text

Examples:
  cgme export --bundle ./chatgpt-export --output ./my-notes
  cgme discover --project-page-url "https://chatgpt.com/g/..." --output-list ./math-sessions.txt
  cgme export --config ./cgme.yaml

Run "cgme export --help" or "cgme discover --help" for command-specific options.
`)
}

func defaultStdout() io.Writer {
	return stdoutWriter{}
}

type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return fmt.Print(string(p))
}

func requireValue(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

var errHelp = errors.New("help requested")
