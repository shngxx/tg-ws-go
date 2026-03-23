# tg-ws-go — user-local install + systemd --user unit (Linux)
#
#   make install              — build + install, listen on 127.0.0.1:1080, no auth
#   make reinstall            — uninstall then install (useful after config changes)
#   make uninstall            — stop and remove unit + binary
#   make fetch                — download latest release binary into dist/ (auto-detects arch)
#
# VPS quick install (no Go required):
#   make fetch && make install HOST=0.0.0.0 SOCKS5_USER=alice SOCKS5_PASS=secret
#
# Override individual params:
#   make install PORT=1081
#   make install HOST=0.0.0.0 SOCKS5_USER=alice SOCKS5_PASS=secret PORT=1080
#
# Override all flags at once (advanced):
#   make install TG_WS_GO_FLAGS="-host 0.0.0.0 -port 1080 --user alice --pass secret"

DIST     := dist
PREFIX   := $(HOME)/.local
BINDIR   := $(PREFIX)/bin
UNITDIR  := $(HOME)/.config/systemd/user
UNIT     := $(UNITDIR)/tg-ws-go.service
BINARY   := $(DIST)/tg-ws-go

HOST        ?= 127.0.0.1
PORT        ?= 1080
SOCKS5_USER ?=
SOCKS5_PASS ?=

_BASE_FLAGS := -host $(HOST) -port $(PORT) \
  --dc-ip 1:149.154.175.53 \
  --dc-ip 2:149.154.167.220 \
  --dc-ip 3:149.154.175.100 \
  --dc-ip 4:149.154.167.91 \
  --dc-ip 5:91.108.56.100 \
  --dc-ip 203:91.105.192.100

ifneq ($(SOCKS5_USER),)
_BASE_FLAGS += --user $(SOCKS5_USER) --pass $(SOCKS5_PASS)
endif

TG_WS_GO_FLAGS ?= $(_BASE_FLAGS)

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

GITHUB_REPO ?= shngxx/tg-ws-go
FETCH_URL   := https://github.com/$(GITHUB_REPO)/releases/latest/download

.PHONY: all build fetch install reinstall uninstall \
        openwrt-all openwrt-mips openwrt-mipsel openwrt-arm openwrt-arm64 openwrt-x86

all: build

fetch:
	@mkdir -p $(DIST)
	$(eval _ARCH := $(shell uname -m | sed 's/aarch64/arm64/;s/armv[0-9].*/arm/'))
	$(eval _NAME := tg-ws-go-$(_ARCH))
	@echo "Fetching $(FETCH_URL)/$(_NAME) → $(BINARY)"
	wget -q --show-progress --timeout=30 -O $(BINARY) "$(FETCH_URL)/$(_NAME)"
	chmod +x $(BINARY)
	@echo "Done: $(BINARY) ($$(du -sh $(BINARY) | cut -f1))"

build: $(BINARY)

$(BINARY): $(wildcard *.go)
	mkdir -p $(DIST)
	go build -o $(BINARY) .

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

install:
	@if [ ! -f $(BINARY) ]; then \
		echo "No binary found in $(DIST)/, building from source..."; \
		$(MAKE) build; \
	fi
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
	  'ExecStart=$(BINDIR)/tg-ws-go $(TG_WS_GO_FLAGS) -log-file $(HOME)/tg-ws-go.log -log-max-mb 10 -log-backups 2' \
	  'StandardError=append:$(HOME)/tg-ws-go-crash.log' \
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
	@echo "Logs:   tail -f $(HOME)/tg-ws-go.log"
	@echo "Crash:  tail -f $(HOME)/tg-ws-go-crash.log"

reinstall: uninstall install

uninstall:
	-systemctl --user disable --now tg-ws-go.service
	rm -f $(UNIT)
	rm -f $(BINDIR)/tg-ws-go
	-systemctl --user daemon-reload
	@echo "Removed: unit and binary from $(BINDIR)"
