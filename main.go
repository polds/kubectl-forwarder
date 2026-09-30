package main

import (
	"bytes"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-version", "version":
			fmt.Println("kubectl-forwarder", versionString())
			return
		}
	}

	ref := &programRef{}
	p := tea.NewProgram(newModel(ref), tea.WithAltScreen())
	ref.p = p // supervisors send messages through this once it exists

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// firstLine returns the first non-empty line of stderr bytes, trimmed.
func firstLine(b []byte) string {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if s := bytes.TrimSpace(line); len(s) > 0 {
			return string(s)
		}
	}
	return "unknown error"
}
