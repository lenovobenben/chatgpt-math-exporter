package main

import (
	"fmt"
	"os"

	"github.com/lihd/chatgpt-math-exporter/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
