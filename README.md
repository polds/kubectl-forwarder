# kubectl-forwarder

Interactive TUI that manages `kubectl port-forward` tunnels and **babysits** them —
if a forward dies or the connection drops, it restarts automatically with backoff.

Built for the annoyance of `kubectl port-forward -n flux-system service/flux-operator 9080:http-web`
silently dying after a while.

## Install

```sh
go build -o kubectl-forwarder .
# put it on PATH; because the name is kubectl-<x>, kubectl treats it as a plugin:
mv kubectl-forwarder /usr/local/bin/
kubectl forwarder      # or just: kubectl-forwarder
```

## Usage

Run `kubectl-forwarder`. Dashboard shows every active forward with live status.

| key   | action                                              |
|-------|-----------------------------------------------------|
| `n`   | new forward — wizard picks namespace → service → port → local port |
| `r`   | restart the selected forward                        |
| `d`   | stop + remove the selected forward                  |
| `↑/↓` | select a row (shows its recent logs + last error)   |
| `q`   | quit (stops all forwards)                           |

In the pickers: `/` filters, `enter` selects, `esc` cancels back to the dashboard.
The local port defaults to the service port; the remote side uses the port **name**
when the service defines one (so `flux-operator` forwards to `http-web`, matching
what you'd type by hand).

## How babysitting works

Each forward runs in its own supervised goroutine. When the `kubectl` process exits:

- it restarts with exponential backoff (1s → 15s cap),
- backoff resets after a run stays healthy for 10s (so a transient drop reconnects fast),
- the restart count and last error are shown on the dashboard.

Forwards live only while the TUI is open; quitting cleans up every child process.

## Tests

Unit build is hermetic. Two live tests exercise a real cluster (gated):

```sh
FWD_LIVE_TEST=1 go test -run TestForwardLive -v
FWD_LIVE_TEST=1 go test -run TestForwardRestartsOnDrop -v
# override target: FWD_NS, FWD_SVC, FWD_REMOTE, FWD_LOCAL
```
