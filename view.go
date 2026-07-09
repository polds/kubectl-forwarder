package main

import (
	"fmt"
	"strings"
)

func (m model) View() string {
	switch m.view {
	case viewDashboard:
		return m.viewDashboard()
	case viewLocalPort:
		return m.viewLocalPort()
	default:
		// namespace / service / port pickers all render the shared list.
		return m.picker.View()
	}
}

func (m model) viewDashboard() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("kubectl-forwarder"))
	b.WriteString("\n\n")

	if len(m.forwards) == 0 {
		b.WriteString(dimRow.Render("No active forwards. Press n to add one."))
		b.WriteString("\n")
	} else {
		header := fmt.Sprintf("  %-12s %-18s %-22s %s",
			"STATUS", "NAMESPACE", "SERVICE", "LOCAL -> REMOTE")
		b.WriteString(headerStyle.Render(header))
		b.WriteString("\n")

		for i, f := range m.forwards {
			v := f.snapshot()
			cursor := "  "
			rowStyle := dimRow
			if i == m.selected {
				cursor = "> "
				rowStyle = selectedRow
			}
			mapping := fmt.Sprintf("%s -> %s", v.localPort, v.remote)
			restarts := ""
			if v.restarts > 0 {
				restarts = fmt.Sprintf("  (restarts: %d)", v.restarts)
			}
			b.WriteString(cursor)
			b.WriteString(statusBadge(v.status))
			b.WriteString(" ")
			b.WriteString(rowStyle.Render(fmt.Sprintf("%-18s %-22s %s%s",
				trunc(v.namespace, 18), trunc(v.service, 22), mapping, restarts)))
			b.WriteString("\n")

			if i == m.selected {
				if v.lastErr != "" {
					b.WriteString("    ")
					b.WriteString(errStyle.Render(trunc(v.lastErr, m.width-6)))
					b.WriteString("\n")
				}
				for _, ln := range v.logs {
					b.WriteString("    ")
					b.WriteString(logStyle.Render(trunc(ln, m.width-6)))
					b.WriteString("\n")
				}
			}
		}
	}

	if m.errLine != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.errLine))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("n new  •  r restart  •  d stop  •  ↑/↓ select  •  q quit"))
	return b.String()
}

func (m model) viewLocalPort() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("New forward"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  %s / %s\n", m.wizardNS, m.wizardSvc.Name))
	b.WriteString(fmt.Sprintf("  remote port: %s\n\n", m.wizardPort.label()))
	b.WriteString("  ")
	b.WriteString(m.localInput.View())
	b.WriteString("\n")
	if m.errLine != "" {
		b.WriteString("\n  ")
		b.WriteString(errStyle.Render(m.errLine))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter confirm  •  esc cancel"))
	return b.String()
}

func trunc(s string, n int) string {
	if n < 1 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
