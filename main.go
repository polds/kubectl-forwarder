package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	ref := &programRef{}
	p := tea.NewProgram(newModel(ref), tea.WithAltScreen())
	ref.p = p // supervisors send messages through this once it exists

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// errAs is a thin wrapper so kubectl.go can unwrap without importing errors.
func errAs(err error, target any) bool { return errors.As(err, target) }

// firstLine returns the first non-empty line of stderr bytes, trimmed.
func firstLine(b []byte) string {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if s := bytes.TrimSpace(line); len(s) > 0 {
			return string(s)
		}
	}
	return "unknown error"
}
