# tg-ws-go — user-local install + systemd --user unit (Linux)
#
#   make install    — build binary, install to ~/.local/bin, register systemd user unit, start
#   make reinstall  — uninstall then install (useful after binary/flag changes)
#   make uninstall  — stop and remove unit + binary
#
# Override proxy flags:
#   make install TG_WS_GO_FLAGS="-v -port 1080 --dc-ip 2:149.154.167.220"

PREFIX   := $(HOME)/.local
BINDIR   := $(PREFIX)/bin
UNITDIR  := $(HOME)/.config/systemd/user
UNIT     := $(UNITDIR)/tg-ws-go.service
BINARY   := tg-ws-go

TG_WS_GO_FLAGS ?= -host 127.0.0.1 -port 1080 --dc-ip 2:149.154.167.220 --dc-ip 4:149.154.167.91

# OpenWrt cross-compilation
#
#   make openwrt-all            — build for all common OpenWrt targets → dist/
#   make openwrt-mips           — mips softfloat  (TP-Link, Asus, old MT7620/AR9xxx)
#   make openwrt-mipsel         — mipsel softfloat (Ralink RT305x, etc.)
#   make openwrt-arm            — armv7 (Cortex-A7/A9 routers)
#   make openwrt-arm64          — aarch64 (Raspberry Pi, newer routers)
#   make openwrt-x86            — x86_64 (PC Engines APU, x86 routers)
#
# Compress with UPX (optional, requires upx in PATH):
#   make openwrt-all COMPRESS=1
#
# Override binary flags after deploy:
#   /tmp/tg-ws-go -host 0.0.0.0 -port 1080 --dc-ip 2:149.154.167.220

DIST     := dist
LDFLAGS  := -s -w
COMPRESS ?= 0

define build-openwrt
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(1) GOMIPS=$(3) go build -ldflags "$(LDFLAGS)" -o $(DIST)/tg-ws-go-$(2) .
	@if [ "$(COMPRESS)" = "1" ] && command -v upx >/dev/null 2>&1; then \
		upx --best $(DIST)/tg-ws-go-$(2); \
	fi
	@ls -lh $(DIST)/tg-ws-go-$(2)
endef

.PHONY: all build install reinstall uninstall \
        openwrt-all openwrt-mips openwrt-mipsel openwrt-arm openwrt-arm64 openwrt-x86

all: build

build:
	go build -o tg-ws-go .

openwrt-mips:
	$(call build-openwrt,mips,mips,softfloat)

openwrt-mipsel:
	$(call build-openwrt,mipsle,mipsel,softfloat)

openwrt-arm:
	$(call build-openwrt,arm,arm,)

openwrt-arm64:
	$(call build-openwrt,arm64,arm64,)

openwrt-x86:
	$(call build-openwrt,amd64,x86_64,)

openwrt-all: openwrt-mips openwrt-mipsel openwrt-arm openwrt-arm64 openwrt-x86
	@echo ""
	@echo "OpenWrt binaries built in $(DIST)/:"
	@ls -lh $(DIST)/

install: build
	install -Dm755 $(BINARY) $(BINDIR)/tg-ws-go
	mkdir -p $(UNITDIR)
	@printf '%s\n' \
	  '[Unit]' \
	  'Description=Telegram WebSocket SOCKS5 proxy (tg-ws-go)' \
	  'Documentation=https://github.com/shngxx/tg-ws-go' \
	  'After=network.target' \
	  '' \
	  '[Service]' \
	  'Type=simple' \
	  'ExecStart=$(BINDIR)/tg-ws-go $(TG_WS_GO_FLAGS)' \
	  'Restart=on-failure' \
	  'RestartSec=5' \
	  '' \
	  '[Install]' \
	  'WantedBy=default.target' \
	  > $(UNIT)
	systemctl --user daemon-reload
	systemctl --user enable --now tg-ws-go.service
	@echo ""
	@echo "Done: proxy installed to $(BINDIR)/tg-ws-go, unit $(UNIT)"
	@echo "Status: systemctl --user status tg-ws-go.service"
	@echo "Logs:   journalctl --user -u tg-ws-go.service -f"

reinstall: uninstall install

uninstall:
	-systemctl --user disable --now tg-ws-go.service
	rm -f $(UNIT)
	rm -f $(BINDIR)/tg-ws-go
	-systemctl --user daemon-reload
	@echo "Removed: unit and binary from $(BINDIR)"
