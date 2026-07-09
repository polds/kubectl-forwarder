package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// forwardStatus is the lifecycle state of a single port-forward.
type forwardStatus int

const (
	statusConnecting forwardStatus = iota
	statusActive
	statusReconnecting
	statusStopped
)

func (s forwardStatus) String() string {
	switch s {
	case statusConnecting:
		return "connecting"
	case statusActive:
		return "active"
	case statusReconnecting:
		return "reconnecting"
	case statusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

const (
	backoffMin = 1 * time.Second
	backoffMax = 15 * time.Second
	// stableAfter is how long a forward must stay up before we treat it as
	// healthy and reset the restart backoff.
	stableAfter = 10 * time.Second
	// logLines is the size of each forward's rolling log buffer.
	logLines = 8
)

// forward is one supervised kubectl port-forward. Fields touched by both the
// UI goroutine and the supervisor goroutine are guarded by mu.
type forward struct {
	id        int
	namespace string
	service   string
	remote    string // service port name or number
	localPort string

	cancel context.CancelFunc

	mu       sync.Mutex
	status   forwardStatus
	restarts int
	lastErr  string
	logs     []string
}

func (f *forward) snapshot() forwardView {
	f.mu.Lock()
	defer f.mu.Unlock()
	logs := make([]string, len(f.logs))
	copy(logs, f.logs)
	return forwardView{
		id:        f.id,
		namespace: f.namespace,
		service:   f.service,
		remote:    f.remote,
		localPort: f.localPort,
		status:    f.status,
		restarts:  f.restarts,
		lastErr:   f.lastErr,
		logs:      logs,
	}
}

// forwardView is an immutable snapshot for rendering.
type forwardView struct {
	id        int
	namespace string
	service   string
	remote    string
	localPort string
	status    forwardStatus
	restarts  int
	lastErr   string
	logs      []string
}

func (f *forward) setStatus(s forwardStatus) {
	f.mu.Lock()
	f.status = s
	f.mu.Unlock()
}

func (f *forward) setErr(msg string) {
	f.mu.Lock()
	f.lastErr = msg
	f.mu.Unlock()
}

func (f *forward) addLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	f.mu.Lock()
	f.logs = append(f.logs, line)
	if len(f.logs) > logLines {
		f.logs = f.logs[len(f.logs)-logLines:]
	}
	f.mu.Unlock()
}

// forwardChangedMsg tells the UI to re-render; state is read via snapshot.
type forwardChangedMsg struct{ id int }

// notifyFunc is called whenever a forward's state changes so the UI can
// re-render. Decoupling from *tea.Program keeps the supervisor testable.
type notifyFunc func(id int)

// run supervises the forward: (re)start kubectl until the context is cancelled,
// backing off between failures and resetting once a run stays healthy.
func (f *forward) run(ctx context.Context, notify notifyFunc) {
	backoff := backoffMin
	for {
		if ctx.Err() != nil {
			return
		}

		f.setStatus(statusConnecting)
		f.setErr("")
		notify(f.id)

		startedAt := time.Now()
		runErr := f.runOnce(ctx, notify)
		uptime := time.Since(startedAt)

		if ctx.Err() != nil {
			f.setStatus(statusStopped)
			notify(f.id)
			return
		}

		f.mu.Lock()
		f.restarts++
		f.status = statusReconnecting
		if runErr != nil {
			f.lastErr = runErr.Error()
		} else {
			f.lastErr = "port-forward exited"
		}
		f.mu.Unlock()
		notify(f.id)

		// A run that stayed up a while is a transient drop, not a config
		// error, so retry promptly instead of climbing the backoff.
		if uptime >= stableAfter {
			backoff = backoffMin
		}

		select {
		case <-ctx.Done():
			f.setStatus(statusStopped)
			notify(f.id)
			return
		case <-time.After(backoff):
		}

		if backoff *= 2; backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// runOnce runs a single kubectl port-forward to completion. It returns nil if
// the process exited on its own, or an error describing why it stopped.
func (f *forward) runOnce(ctx context.Context, notify notifyFunc) error {
	target := fmt.Sprintf("service/%s", f.service)
	mapping := fmt.Sprintf("%s:%s", f.localPort, f.remote)
	cmd := exec.CommandContext(ctx, "kubectl", "port-forward", "-n", f.namespace, target, mapping)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// kubectl prints "Forwarding from ..." on stdout once the tunnel is live.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f.scan(stdout, notify, true)
	}()
	go func() {
		defer wg.Done()
		f.scan(stderr, notify, false)
	}()
	wg.Wait()

	err = cmd.Wait()
	// CommandContext kills the process on cancel; that surfaces as an error we
	// don't want to report as a failure.
	if ctx.Err() != nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return errors.New(lastLog(f))
	}
	return err
}

// scan reads a pipe line by line, logging output and flipping the forward to
// active when kubectl reports the tunnel is up.
func (f *forward) scan(r io.Reader, notify notifyFunc, stdout bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		f.addLog(line)
		if stdout && strings.HasPrefix(line, "Forwarding from") {
			f.setStatus(statusActive)
			f.setErr("")
		}
		notify(f.id)
	}
}

// lastLog returns the most recent log line, used as the failure reason.
func lastLog(f *forward) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.logs) == 0 {
		return "connection lost"
	}
	return f.logs[len(f.logs)-1]
}
