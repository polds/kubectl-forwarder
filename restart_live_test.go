package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestForwardRestartsOnDrop kills the underlying kubectl process and verifies
// the supervisor restarts it — the core babysitting guarantee.
//
//	FWD_LIVE_TEST=1 go test -run TestForwardRestartsOnDrop -v
func TestForwardRestartsOnDrop(t *testing.T) {
	if os.Getenv("FWD_LIVE_TEST") == "" {
		t.Skip("set FWD_LIVE_TEST=1 to run against a live cluster")
	}

	f := &forward{
		id:        1,
		namespace: env("FWD_NS", "flux-system"),
		service:   env("FWD_SVC", "flux-operator"),
		remote:    env("FWD_REMOTE", "http-web"),
		localPort: env("FWD_LOCAL", "19081"),
		status:    statusConnecting,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.run(ctx, func(int) {})

	if !waitFor(20*time.Second, func() bool { return f.snapshot().status == statusActive }) {
		t.Fatalf("never became active: %+v", f.snapshot())
	}
	if got := f.snapshot().restarts; got != 0 {
		t.Fatalf("expected 0 restarts before kill, got %d", got)
	}

	// Kill the kubectl child out from under the supervisor.
	killKubectl(t, f.localPort)

	// The supervisor should notice, restart, and get back to active.
	if !waitFor(25*time.Second, func() bool {
		s := f.snapshot()
		return s.restarts >= 1 && s.status == statusActive
	}) {
		t.Fatalf("did not recover after kill: %+v", f.snapshot())
	}
	t.Logf("recovered: restarts=%d status=%s", f.snapshot().restarts, f.snapshot().status)
}

// killKubectl kills the kubectl port-forward process bound to localPort.
func killKubectl(t *testing.T, localPort string) {
	t.Helper()
	// pgrep for the port-forward whose mapping contains the local port.
	out, _ := exec.Command("pgrep", "-f", "port-forward.*"+localPort).Output()
	pids := strings.Fields(string(out))
	if len(pids) == 0 {
		t.Fatalf("no kubectl port-forward process found for :%s", localPort)
	}
	for _, pid := range pids {
		_ = exec.Command("kill", "-9", pid).Run()
	}
	t.Logf("killed kubectl pids %v", pids)
}
