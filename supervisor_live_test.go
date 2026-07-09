package main

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

// TestForwardLive exercises a real kubectl port-forward end to end. It is
// gated behind FWD_LIVE_TEST so normal `go test` runs stay hermetic.
//
//	FWD_LIVE_TEST=1 FWD_NS=flux-system FWD_SVC=flux-operator \
//	FWD_REMOTE=http-web FWD_LOCAL=19080 go test -run TestForwardLive -v
func TestForwardLive(t *testing.T) {
	if os.Getenv("FWD_LIVE_TEST") == "" {
		t.Skip("set FWD_LIVE_TEST=1 to run against a live cluster")
	}

	f := &forward{
		id:        1,
		namespace: env("FWD_NS", "flux-system"),
		service:   env("FWD_SVC", "flux-operator"),
		remote:    env("FWD_REMOTE", "http-web"),
		localPort: env("FWD_LOCAL", "19080"),
		status:    statusConnecting,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.run(ctx, func(int) {})

	// Wait for the supervisor to report the tunnel active.
	if !waitFor(20*time.Second, func() bool {
		return f.snapshot().status == statusActive
	}) {
		t.Fatalf("forward never became active; last state: %+v", f.snapshot())
	}
	t.Logf("forward active on 127.0.0.1:%s", f.localPort)

	// The local port must actually accept a TCP connection.
	addr := net.JoinHostPort("127.0.0.1", f.localPort)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	conn.Close()
	t.Logf("dialed %s successfully", addr)

	// Cancelling must stop the supervisor and settle it to stopped.
	cancel()
	if !waitFor(5*time.Second, func() bool {
		return f.snapshot().status == statusStopped
	}) {
		t.Fatalf("forward did not stop after cancel; state: %s", f.snapshot().status)
	}
	t.Log("forward stopped cleanly after cancel")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
