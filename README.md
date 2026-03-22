# tg-ws-go

> **Disclaimer:** This project is intended for educational and research purposes only — to study WebSocket tunneling techniques, MTProto protocol obfuscation, and network proxy implementation in Go. The author does not encourage circumventing any laws or regulations. Users are solely responsible for ensuring their use of this software complies with the laws of their jurisdiction.

A lightweight local SOCKS5 proxy that tunnels Telegram's MTProto traffic over WebSocket (WSS) connections to Telegram's `kws*.web.telegram.org` relay servers instead of connecting directly to Telegram DC IP addresses.

This makes Telegram traffic look like ordinary HTTPS/WebSocket traffic to a Telegram-owned web domain, bypassing ISP-level IP blocks.

**Zero external dependencies** — pure Go standard library only.

---

## How it works

1. Telegram Desktop connects to the proxy via SOCKS5.
2. The proxy intercepts the 64-byte MTProto obfuscation init packet and detects the target DC id.
3. Instead of a raw TCP connection to the DC IP, it opens a WebSocket connection to `kwsN.web.telegram.org` over TLS, mimicking the Telegram web client.
4. MTProto messages are relayed as individual WebSocket binary frames in both directions.
5. If WebSocket is unavailable, the proxy falls back to a direct TCP connection.

```
Telegram Desktop
    │  SOCKS5 CONNECT → 149.154.167.220:443
    ▼
tg-ws-go (127.0.0.1:1080)
    │  parse MTProto init → DC2
    │  TLS dial 149.154.167.220:443, SNI=kws2.web.telegram.org
    │  HTTP GET /apiws → 101 Switching Protocols
    ▼
kws2.web.telegram.org  (Telegram relay)
    ▼
Telegram DC2
```

---

## Supported platforms

The proxy binary compiles and runs on any platform supported by Go:

| OS | Run | `make install` |
|---|---|---|
| Linux | ✅ | ✅ (systemd --user) |
| macOS | ✅ | ❌ (manual setup only) |
| Windows | ✅ | ❌ (manual setup only) |
| FreeBSD / OpenBSD | ✅ | ❌ (manual setup only) |

`make install` is Linux-only because it relies on systemd user units. On other systems, build the binary manually and run it however you prefer (shell script, launchd, Task Scheduler, rc.d, etc.).

## Requirements

- Go 1.22+
- Telegram Desktop with SOCKS5 proxy configured and IPv6 disabled

---

## Build

```bash
make build
# produces ./tg-ws-go
```

Or directly:

```bash
go build -o tg-ws-go .
```

---

## Install (systemd user service)

```bash
make install
```

This will:
- Build the binary and copy it to `~/.local/bin/tg-ws-go`
- Generate a `systemd --user` unit at `~/.config/systemd/user/tg-ws-go.service`
- Enable and start the service immediately

To override the default proxy flags:

```bash
make install TG_WS_GO_FLAGS="-v -port 1080 --dc-ip 2:149.154.167.220 --dc-ip 4:149.154.167.91"
```

To uninstall:

```bash
make uninstall
```

---

## Usage

```
./tg-ws-go [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-host` | `127.0.0.1` | Listen address |
| `-port` | `1080` | Listen port |
| `-v` | `false` | Enable debug (verbose) logging |
| `-log-file` | _(none)_ | Write logs to this file in addition to stderr |
| `-log-max-mb` | `5` | Rotate log file when it exceeds this size (MB) |
| `-log-backups` | `0` | Number of rotated log files to keep |
| `-buf-kb` | `256` | TCP socket send/recv buffer size in KB |
| `-pool-size` | `4` | Idle WebSocket connections to pre-connect per DC |
| `--dc-ip` | DC2+DC4 | `DC:IP` mapping for target DCs (repeatable) |

### Examples

```bash
# Default — listen on 127.0.0.1:1080, tunnel DC2 and DC4
./tg-ws-go

# Custom DCs and verbose logging
./tg-ws-go -v --dc-ip 1:149.154.175.50 --dc-ip 2:149.154.167.220

# Log to file with rotation
./tg-ws-go -log-file /var/log/tg-ws-go.log -log-max-mb 10 -log-backups 3
```

---

## Telegram Desktop configuration

1. **Settings → Advanced → Connection type → Use custom proxy**
2. Add a SOCKS5 proxy: host `127.0.0.1`, port `1080`, no authentication
3. **Disable IPv6**: Settings → Advanced → Disable IPv6 *(required — IPv6 addresses are not proxied)*

---

## Features

- **WebSocket tunnel** — wraps MTProto in WS frames over TLS, indistinguishable from Telegram web traffic
- **MTProto message splitting** — aligns each MTProto message to its own WS frame as required by the `kws` relay protocol
- **Idle connection pool** — pre-warms WebSocket connections per DC to minimize connection latency
- **TCP fallback** — automatically falls back to direct TCP when WebSocket is unavailable
- **DC blacklist & cooldown** — temporarily or permanently disables WebSocket for a DC after repeated failures
- **Non-Telegram passthrough** — acts as a generic SOCKS5 proxy for non-Telegram destinations
- **Log rotation** — built-in size-based log file rotation with optional numbered backups
- **Zero dependencies** — no third-party packages, only Go standard library

---

## Service management

```bash
# Status
systemctl --user status tg-ws-go.service

# Live logs
journalctl --user -u tg-ws-go.service -f

# Restart
systemctl --user restart tg-ws-go.service
```

---

## License

MIT — see [LICENSE](LICENSE).
