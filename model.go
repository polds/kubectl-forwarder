package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// view is the current screen of the TUI.
type view int

const (
	viewDashboard view = iota
	viewPickNamespace
	viewPickService
	viewPickPort
	viewLocalPort
)

// programRef lets supervisor goroutines send messages once the program exists.
type programRef struct{ p *tea.Program }

type model struct {
	ref *programRef

	view   view
	width  int
	height int

	forwards []*forward
	selected int // dashboard cursor
	nextID   int

	// wizard scratch state
	picker         list.Model
	localInput     textinput.Model
	spinner        spinner.Model
	wizardNS       string
	wizardServices []svc
	wizardSvc      svc
	wizardPort     servicePort
	loading        bool
	statusLine     string
	errLine        string
}

func newModel(ref *programRef) model {
	picker := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	picker.SetShowStatusBar(false)
	picker.SetShowTitle(true)
	picker.SetFilteringEnabled(true)

	ti := textinput.New()
	ti.CharLimit = 5
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	ti.Placeholder = "port"

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = headerStyle

	return model{
		ref:        ref,
		view:       viewDashboard,
		picker:     picker,
		localInput: ti,
		spinner:    sp,
	}
}

func (m model) Init() tea.Cmd { return nil }

// pickerItem adapts a label/value pair to the bubbles list interface.
type pickerItem struct {
	title string
	desc  string
}

func (i pickerItem) Title() string       { return i.title }
func (i pickerItem) Description() string { return i.desc }
func (i pickerItem) FilterValue() string { return i.title }

// compactDelegate is a single-line list delegate: no description row and no
// blank spacer between items. Used for the namespace and port pickers, whose
// items carry no description and so waste vertical space under the default
// two-line delegate.
func compactDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	return d
}

// twoLineDelegate is the default title+description delegate used by the
// service picker, tightened to drop the blank spacer between items.
func twoLineDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	// Indent the port-summary line ~2 cols under its service name. The default
	// delegate pads titles by 2 (normal/dimmed) and the selected row by 1, so
	// bump each desc style by 2 over its matching title to keep the offset.
	d.Styles.NormalDesc = d.Styles.NormalDesc.PaddingLeft(4)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.PaddingLeft(3)
	d.Styles.DimmedDesc = d.Styles.DimmedDesc.PaddingLeft(4)
	return d
}

func (m *model) sizePicker() {
	// Leave room for header and help lines.
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	m.picker.SetSize(m.width, h)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sizePicker()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case namespacesLoadedMsg:
		if !m.loading {
			return m, nil // user cancelled; drop the stale result
		}
		m.loading = false
		if msg.err != nil {
			m.view = viewDashboard
			m.errLine = msg.err.Error()
			return m, nil
		}
		items := make([]list.Item, len(msg.namespaces))
		for i, ns := range msg.namespaces {
			items[i] = pickerItem{title: ns}
		}
		m.picker.SetDelegate(compactDelegate())
		m.picker.SetItems(items)
		m.picker.Title = "Select namespace"
		m.picker.ResetSelected()
		m.sizePicker()
		m.view = viewPickNamespace
		return m, nil

	case servicesLoadedMsg:
		if !m.loading {
			return m, nil // user cancelled; drop the stale result
		}
		m.loading = false
		if msg.err != nil {
			m.view = viewDashboard
			m.errLine = msg.err.Error()
			return m, nil
		}
		if len(msg.services) == 0 {
			m.view = viewDashboard
			m.errLine = fmt.Sprintf("no forwardable services in %s", msg.namespace)
			return m, nil
		}
		items := make([]list.Item, len(msg.services))
		for i, s := range msg.services {
			items[i] = pickerItem{title: s.Name, desc: portSummary(s.Ports)}
		}
		m.picker.SetDelegate(twoLineDelegate())
		m.picker.SetItems(items)
		m.picker.Title = fmt.Sprintf("Select service in %s", msg.namespace)
		m.picker.ResetSelected()
		m.sizePicker()
		m.wizardServices = msg.services
		m.view = viewPickService
		return m, nil

	case forwardChangedMsg:
		return m, nil // snapshot-based render; just trigger a redraw

	case spinner.TickMsg:
		if !m.loading {
			return m, nil // stop ticking once the lookup resolves
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Delegate to the active widget.
	var cmd tea.Cmd
	switch m.view {
	case viewPickNamespace, viewPickService, viewPickPort:
		m.picker, cmd = m.picker.Update(msg)
	case viewLocalPort:
		m.localInput, cmd = m.localInput.Update(msg)
	}
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit.
	if msg.Type == tea.KeyCtrlC {
		return m, m.quit()
	}

	// While a cluster lookup is in flight the loading screen owns the keys:
	// esc abandons it and returns to the dashboard. The pending result is
	// dropped by the loaded-msg handlers once m.loading is false.
	if m.loading {
		if msg.Type == tea.KeyEsc {
			m.loading = false
			m.statusLine = ""
			m.view = viewDashboard
		}
		return m, nil
	}

	switch m.view {
	case viewDashboard:
		return m.handleDashboardKey(msg)
	case viewPickNamespace:
		return m.handlePickNamespaceKey(msg)
	case viewPickService:
		return m.handlePickServiceKey(msg)
	case viewPickPort:
		return m.handlePickPortKey(msg)
	case viewLocalPort:
		return m.handleLocalPortKey(msg)
	}
	return m, nil
}

func (m model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, m.quit()
	case "n":
		m.errLine = ""
		m.loading = true
		m.statusLine = "connecting to cluster, loading namespaces…"
		return m, tea.Batch(loadNamespaces(), m.spinner.Tick)
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.forwards)-1 {
			m.selected++
		}
	case "d", "x":
		m.stopSelected()
	case "r":
		m.restartSelected()
	}
	return m, nil
}

func (m model) handlePickNamespaceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.picker.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		return m, nil
	case "enter":
		it, ok := m.picker.SelectedItem().(pickerItem)
		if !ok {
			return m, nil
		}
		m.wizardNS = it.title
		m.loading = true
		m.statusLine = fmt.Sprintf("looking up services in %s…", it.title)
		return m, tea.Batch(loadServices(it.title), m.spinner.Tick)
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m model) handlePickServiceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.picker.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		return m, nil
	case "enter":
		it, ok := m.picker.SelectedItem().(pickerItem)
		if !ok {
			return m, nil
		}
		for _, s := range m.wizardServices {
			if s.Name == it.title {
				m.wizardSvc = s
				break
			}
		}
		// Single port: skip straight to local-port entry.
		if len(m.wizardSvc.Ports) == 1 {
			m.wizardPort = m.wizardSvc.Ports[0]
			return m.enterLocalPort(), nil
		}
		items := make([]list.Item, len(m.wizardSvc.Ports))
		for i, p := range m.wizardSvc.Ports {
			items[i] = pickerItem{title: p.label()}
		}
		m.picker.SetDelegate(compactDelegate())
		m.picker.SetItems(items)
		m.picker.Title = fmt.Sprintf("Select port on %s", m.wizardSvc.Name)
		m.picker.ResetSelected()
		m.view = viewPickPort
		return m, nil
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m model) handlePickPortKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		return m, nil
	case "enter":
		idx := m.picker.Index()
		if idx < 0 || idx >= len(m.wizardSvc.Ports) {
			return m, nil
		}
		m.wizardPort = m.wizardSvc.Ports[idx]
		return m.enterLocalPort(), nil
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

// enterLocalPort moves to the local-port text entry, pre-filled with the
// service port number as a sensible default.
func (m model) enterLocalPort() model {
	m.localInput.SetValue(strconv.Itoa(m.wizardPort.Port))
	m.localInput.CursorEnd()
	m.localInput.Focus()
	m.errLine = ""
	m.view = viewLocalPort
	return m
}

func (m model) handleLocalPortKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		return m, nil
	case "enter":
		local := strings.TrimSpace(m.localInput.Value())
		if !validPort(local) {
			m.errLine = "local port must be 1-65535"
			return m, nil
		}
		m.startForward(local)
		m.view = viewDashboard
		m.errLine = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.localInput, cmd = m.localInput.Update(msg)
	return m, cmd
}

// startForward spawns and supervises a new port-forward.
func (m *model) startForward(localPort string) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &forward{
		id:        m.nextID,
		namespace: m.wizardNS,
		service:   m.wizardSvc.Name,
		remote:    m.wizardPort.remote(),
		localPort: localPort,
		cancel:    cancel,
		status:    statusConnecting,
	}
	m.nextID++
	m.forwards = append(m.forwards, f)
	m.selected = len(m.forwards) - 1
	go f.run(ctx, m.notify())
}

// notify returns a callback the supervisor uses to request a re-render.
func (m *model) notify() notifyFunc {
	ref := m.ref
	return func(id int) { ref.p.Send(forwardChangedMsg{id}) }
}

func (m *model) stopSelected() {
	if m.selected < 0 || m.selected >= len(m.forwards) {
		return
	}
	f := m.forwards[m.selected]
	f.cancel()
	m.forwards = append(m.forwards[:m.selected], m.forwards[m.selected+1:]...)
	if m.selected >= len(m.forwards) && m.selected > 0 {
		m.selected--
	}
}

// restartSelected tears down the current supervisor and starts a fresh one with
// the same parameters.
func (m *model) restartSelected() {
	if m.selected < 0 || m.selected >= len(m.forwards) {
		return
	}
	old := m.forwards[m.selected]
	old.cancel()

	ctx, cancel := context.WithCancel(context.Background())
	f := &forward{
		id:        m.nextID,
		namespace: old.namespace,
		service:   old.service,
		remote:    old.remote,
		localPort: old.localPort,
		cancel:    cancel,
		status:    statusConnecting,
	}
	m.nextID++
	m.forwards[m.selected] = f
	go f.run(ctx, m.notify())
}

// quit cancels every forward before exiting so no kubectl children are left.
func (m *model) quit() tea.Cmd {
	for _, f := range m.forwards {
		f.cancel()
	}
	return tea.Quit
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

func portSummary(ports []servicePort) string {
	labels := make([]string, len(ports))
	for i, p := range ports {
		labels[i] = p.label()
	}
	return strings.Join(labels, ", ")
}

// styles
var (
	// appStyle frames the non-list screens with the same top/left breathing room
	// the bubbles list gives the picker screens, so every view looks aligned.
	appStyle = lipgloss.NewStyle().Padding(1, 2)
	// titleStyle is a blue badge matching the bubbles list picker title, so the
	// dashboard / wizard headers read as headers, not plain bold text.
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)
	// inputBoxStyle frames the local-port field so it reads as an editable form
	// control rather than a bare line of text.
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	selectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	dimRow      = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	logStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func statusBadge(s forwardStatus) string {
	var color string
	switch s {
	case statusActive:
		color = "10" // green
	case statusConnecting:
		color = "11" // yellow
	case statusReconnecting:
		color = "208" // orange
	case statusStopped:
		color = "244" // grey
	default:
		color = "15"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(fmt.Sprintf("%-12s", s.String()))
}
