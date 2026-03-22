# tg-ws-go — OpenWrt

Instructions for installing tg-ws-go on an OpenWrt router.

## Supported architectures

| File | Architecture | Routers |
|---|---|---|
| `tg-ws-go-mips` | MIPS softfloat | TP-Link, Asus (AR9xxx, MT7620) |
| `tg-ws-go-mipsel` | MIPSel softfloat | Xiaomi Mi Router 4C (MT7628), Ralink RT305x |
| `tg-ws-go-arm` | ARMv7 | Cortex-A7/A9 routers |
| `tg-ws-go-arm64` | AArch64 | Raspberry Pi, newer routers |
| `tg-ws-go-x86_64` | x86_64 | PC Engines APU, x86 routers |

To find your router's architecture:

```bash
ssh root@192.168.1.1 "uname -m"
```

## Installation (one time)

### 1. Copy the init script to the router

```bash
scp openwrt/tg-ws-go root@192.168.1.1:/etc/init.d/tg-ws-go
ssh root@192.168.1.1 "chmod +x /etc/init.d/tg-ws-go"
```

### 2. Enable autostart and launch

```bash
ssh root@192.168.1.1 "/etc/init.d/tg-ws-go enable && /etc/init.d/tg-ws-go start"
```

On first start the script will automatically download the binary from GitHub Releases to `/tmp/tg-ws-go`.

### 3. Check logs

```bash
ssh root@192.168.1.1 "logread | grep tg-ws-go"
```

## Service management

```bash
# Start
/etc/init.d/tg-ws-go start

# Stop
/etc/init.d/tg-ws-go stop

# Restart
/etc/init.d/tg-ws-go restart

# Download latest release and restart
/etc/init.d/tg-ws-go update
```

## Telegram configuration

In Telegram on each device in the network:

**Settings → Privacy and Security → Use Proxy**

- Type: `SOCKS5`
- Host: router IP address (e.g. `192.168.1.1`)
- Port: `1080`
- No authentication

## Low memory routers

For routers with limited RAM, edit the `FLAGS` variable in the init script:

```sh
FLAGS="-host 0.0.0.0 -port 1080 \
  --dc-ip 2:149.154.167.220 \
  --pool-size 0 \
  --buf-kb 32"
```

- `--pool-size 0` — disable pre-warmed connection pool (biggest RAM saving)
- `--buf-kb 32` — reduce socket buffer size

## Changing architecture

By default the script downloads the `mipsel` binary. To use a different architecture, change the `ARCH` variable in `/etc/init.d/tg-ws-go`:

```sh
ARCH=arm64   # or mips, arm, x86_64
```

## Building binaries manually

```bash
# Single architecture
make openwrt-mipsel

# All architectures
make openwrt-all

# With UPX compression (~3x smaller)
make openwrt-all COMPRESS=1
```
