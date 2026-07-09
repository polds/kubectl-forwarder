package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// servicePort is a single port exposed by a Kubernetes service.
type servicePort struct {
	Name string // named port, may be empty
	Port int    // service port number
}

// remote returns the value to hand kubectl for the remote side of a forward.
// Prefer the port name when present (matches how users think, e.g. "http-web").
func (p servicePort) remote() string {
	if p.Name != "" {
		return p.Name
	}
	return strconv.Itoa(p.Port)
}

func (p servicePort) label() string {
	if p.Name != "" {
		return fmt.Sprintf("%s (%d)", p.Name, p.Port)
	}
	return strconv.Itoa(p.Port)
}

// svc is a Kubernetes service with the ports we can forward to.
type svc struct {
	Name  string
	Ports []servicePort
}

// Messages pushed back into the Bubble Tea update loop.
type namespacesLoadedMsg struct {
	namespaces []string
	err        error
}

type servicesLoadedMsg struct {
	namespace string
	services  []svc
	err       error
}

// kubectlTimeout bounds the metadata queries (not the long-lived forwards).
const kubectlTimeout = 15 * time.Second

// loadNamespaces lists namespaces in the current context.
func loadNamespaces() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), kubectlTimeout)
		defer cancel()

		out, err := exec.CommandContext(ctx, "kubectl", "get", "namespaces", "-o", "json").Output()
		if err != nil {
			return namespacesLoadedMsg{err: cmdErr("list namespaces", err)}
		}

		var payload struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if err := json.Unmarshal(out, &payload); err != nil {
			return namespacesLoadedMsg{err: fmt.Errorf("parse namespaces: %w", err)}
		}

		names := make([]string, 0, len(payload.Items))
		for _, it := range payload.Items {
			names = append(names, it.Metadata.Name)
		}
		sort.Strings(names)
		return namespacesLoadedMsg{namespaces: names}
	}
}

// loadServices lists services (and their ports) in a namespace.
func loadServices(namespace string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), kubectlTimeout)
		defer cancel()

		out, err := exec.CommandContext(ctx, "kubectl", "get", "services", "-n", namespace, "-o", "json").Output()
		if err != nil {
			return servicesLoadedMsg{namespace: namespace, err: cmdErr("list services", err)}
		}

		var payload struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					Ports []struct {
						Name string `json:"name"`
						Port int    `json:"port"`
					} `json:"ports"`
				} `json:"spec"`
			} `json:"items"`
		}
		if err := json.Unmarshal(out, &payload); err != nil {
			return servicesLoadedMsg{namespace: namespace, err: fmt.Errorf("parse services: %w", err)}
		}

		services := make([]svc, 0, len(payload.Items))
		for _, it := range payload.Items {
			if len(it.Spec.Ports) == 0 {
				continue // nothing to forward to
			}
			ports := make([]servicePort, 0, len(it.Spec.Ports))
			for _, p := range it.Spec.Ports {
				ports = append(ports, servicePort{Name: p.Name, Port: p.Port})
			}
			services = append(services, svc{Name: it.Metadata.Name, Ports: ports})
		}
		sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
		return servicesLoadedMsg{namespace: namespace, services: services}
	}
}

// cmdErr surfaces stderr from a failed kubectl invocation, which is far more
// useful than the bare "exit status 1".
func cmdErr(what string, err error) error {
	var ee *exec.ExitError
	if errAs(err, &ee) && len(ee.Stderr) > 0 {
		return fmt.Errorf("%s: %s", what, firstLine(ee.Stderr))
	}
	return fmt.Errorf("%s: %w", what, err)
}
