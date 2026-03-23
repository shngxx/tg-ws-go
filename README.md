# tg-ws-go

> **Disclaimer:** This project is intended for educational and research purposes only — to study WebSocket tunneling techniques, MTProto protocol obfuscation, and network proxy implementation in Go. The author does not encourage circumventing any laws or regulations. Users are solely responsible for ensuring their use of this software complies with the laws of their jurisdiction.

A lightweight SOCKS5 proxy that tunnels Telegram's MTProto traffic over WebSocket (WSS) connections to Telegram's `kws*.web.telegram.org` relay servers instead of connecting directly to Telegram DC IP addresses.

This makes Telegram traffic look like ordinary HTTPS/WebSocket traffic to a Telegram-owned web domain, bypassing ISP-level IP blocks.

**Zero external dependencies** — pure Go standard library only.

> **Based on** [tg-ws-proxy](https://github.com/Flowseal/tg-ws-proxy) by [@Flowseal](https://github.com/Flowseal) — original Python implementation. This project is a Go rewrite.

---

## How it works

1. Telegram Desktop/Mobile connects to the proxy via SOCKS5.
2. The proxy intercepts the 64-byte MTProto obfuscation init packet and detects the target DC id.
3. Instead of a raw TCP connection to the DC IP, it opens a WebSocket connection to `kwsN.web.telegram.org` over TLS, mimicking the Telegram web client.
4. MTProto messages are relayed as individual WebSocket binary frames in both directions.
5. If WebSocket is unavailable, the proxy falls back to a direct TCP connection.

```
Telegram Desktop / Mobile
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

| OS | Run | `make install` |
|---|---|---|
| Linux | ✅ | ✅ (systemd --user) |
| macOS | ✅ | ❌ (manual setup only) |
| Windows | ✅ | ❌ (manual setup only) |
| FreeBSD / OpenBSD | ✅ | ❌ (manual setup only) |
| OpenWrt | ✅ | see [openwrt/README.md](openwrt/README.md) |

`make install` is Linux-only because it relies on systemd user units.

---

## Requirements

- Go 1.22+ *(only if building from source)*
- Telegram Desktop or Mobile with SOCKS5 proxy configured

---

## Quick start

### Option A — download pre-built binary (no Go required)

```bash
make fetch                   # auto-detects arch, downloads latest release to dist/
make install                 # install + start systemd service
```

### Option B — build from source

```bash
make build                   # produces dist/tg-ws-go
make install
```

### VPS install with authentication

```bash
make fetch
make install HOST=0.0.0.0 SOCKS5_USER=alice SOCKS5_PASS=secret
```

---

## Install (systemd user service)

```bash
make install
```

This will:
- Download or build the binary and copy it to `~/.local/bin/tg-ws-go`
- Generate a `systemd --user` unit at `~/.config/systemd/user/tg-ws-go.service`
- Enable and start the service immediately

```bash
make reinstall HOST=0.0.0.0 SOCKS5_USER=alice SOCKS5_PASS=secret   # reapply with new config
make uninstall                                                        # stop and remove
```

---

## Usage

```
./tg-ws-go [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-host` | `127.0.0.1` | Listen address (`0.0.0.0` for VPS/public) |
| `-port` | `1080` | Listen port |
| `--user` | _(none)_ | SOCKS5 username — enables authentication if set |
| `--pass` | _(none)_ | SOCKS5 password |
| `--dc-ip` | DC1–DC5, DC203 | `DC:IP` mapping for target DCs (repeatable) |
| `-v` | `false` | Verbose (debug) logging |
| `-log-file` | _(none)_ | Write logs to file in addition to stderr |
| `-log-max-mb` | `5` | Rotate log file when it exceeds this size (MB) |
| `-log-backups` | `0` | Number of rotated log files to keep |
| `-buf-kb` | `256` | TCP socket send/recv buffer size in KB |
| `-pool-size` | `4` | Idle WebSocket connections to pre-connect per DC |

### Examples

```bash
# Default — listen on 127.0.0.1:1080, no auth
./tg-ws-go

# VPS — listen on all interfaces with auth
./tg-ws-go -host 0.0.0.0 -port 1080 --user alice --pass secret

# Custom DCs and verbose logging
./tg-ws-go -v --dc-ip 1:149.154.175.53 --dc-ip 2:149.154.167.220

# Log to file with rotation
./tg-ws-go -log-file /var/log/tg-ws-go.log -log-max-mb 10 -log-backups 3
```

---

## Telegram configuration

### Telegram Desktop

1. **Settings → Advanced → Connection type → Use custom proxy**
2. Add SOCKS5 proxy: host `127.0.0.1`, port `1080`
3. If auth is enabled — enter username and password

### Telegram Mobile (iOS / Android)

1. **Settings → Data and Storage → Proxy → Add Proxy**
2. Type: SOCKS5, host and port of your proxy
3. If auth is enabled — enter username and password

---

## Features

- **WebSocket tunnel** — wraps MTProto in WS frames over TLS, indistinguishable from Telegram web traffic
- **IPv6 support** — recognizes and routes Telegram IPv6 DC addresses, falls back to IPv4 when needed
- **SOCKS5 authentication** — optional username/password auth (RFC 1929) for public/VPS deployments
- **MTProto message splitting** — aligns each MTProto message to its own WS frame as required by the relay protocol
- **Idle connection pool** — pre-warms WebSocket connections per DC to reduce latency
- **TCP fallback** — automatically falls back to direct TCP when WebSocket is unavailable
- **DC blacklist & cooldown** — temporarily or permanently disables WebSocket for a DC after repeated failures
- **Non-Telegram passthrough** — acts as a generic SOCKS5 proxy for non-Telegram destinations
- **Log rotation** — built-in size-based log file rotation with optional numbered backups
- **Zero dependencies** — no third-party packages, only Go standard library

---

## OpenWrt

See [openwrt/README.md](openwrt/README.md) for router deployment instructions.

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
