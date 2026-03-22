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

.PHONY: all build install reinstall uninstall

all: build

build:
	go build -o tg-ws-go .

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
