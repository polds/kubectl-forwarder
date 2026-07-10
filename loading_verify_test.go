package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressN drives the dashboard "n" key and returns the resulting model.
func pressN(t *testing.T) model {
	t.Helper()
	m := newModel(&programRef{})
	m.width, m.height = 80, 24
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("pressing n produced no command (expected load + spinner tick)")
	}
	return next.(model)
}

// TestLoadingScreenRenders verifies that after pressing n the loading view is
// shown with the status line, instead of a frozen dashboard.
func TestLoadingScreenRenders(t *testing.T) {
	m := pressN(t)
	if !m.loading {
		t.Fatal("expected loading=true after pressing n")
	}
	out := m.View()
	if !strings.Contains(out, "loading namespaces") {
		t.Fatalf("loading view missing status line, got:\n%s", out)
	}
}

// TestCancelDuringLoad verifies esc abandons an in-flight lookup and that a
// late-arriving result is dropped rather than popping the picker.
func TestCancelDuringLoad(t *testing.T) {
	m := pressN(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.loading {
		t.Fatal("esc should clear loading")
	}
	if m.view != viewDashboard {
		t.Fatalf("esc should return to dashboard, got view %d", m.view)
	}

	// Stale result arrives after cancel — must not switch to the picker.
	next, _ = m.Update(namespacesLoadedMsg{namespaces: []string{"default"}})
	m = next.(model)
	if m.view != viewDashboard {
		t.Fatalf("stale result should be dropped, got view %d", m.view)
	}
}

// TestClusterErrorSurfaces verifies a failed lookup (e.g. DNS/unreachable)
// clears loading and shows the error on the dashboard.
func TestClusterErrorSurfaces(t *testing.T) {
	m := pressN(t)

	wantErr := "list namespaces: Unable to connect to the server: dial tcp: lookup foo: no such host"
	next, _ := m.Update(namespacesLoadedMsg{err: errors.New(wantErr)})
	m = next.(model)

	if m.loading {
		t.Fatal("error should clear loading")
	}
	if m.view != viewDashboard {
		t.Fatalf("error should land on dashboard, got view %d", m.view)
	}
	out := m.View()
	if !strings.Contains(out, "Unable to connect to the server") {
		t.Fatalf("dashboard should show cluster error, got:\n%s", out)
	}
}
