PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
SYSCONFDIR ?= /etc
DESTDIR ?=

GO ?= go
GOFLAGS ?=

BIN_DIR := bin
BLEH := $(BIN_DIR)/bleh
BLEHD := $(BIN_DIR)/blehd

.PHONY: all build clean install uninstall setcap install-systemd uninstall-systemd install-openrc uninstall-openrc

all: build

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(BLEH) ./cmd/bleh
	$(GO) build $(GOFLAGS) -o $(BLEHD) ./cmd/blehd

clean:
	rm -rf $(BIN_DIR)

# SETCAP:
# - auto (default): apply setcap when installing as root and setcap is available
# - 1: force setcap (fails if not root / setcap missing)
# - 0: never apply setcap
SETCAP ?= auto

install:
	@test -x $(BLEH) || (echo "Missing $(BLEH). Run: make build"; exit 1)
	@test -x $(BLEHD) || (echo "Missing $(BLEHD). Run: make build"; exit 1)
	install -Dm755 $(BLEH) $(DESTDIR)$(BINDIR)/bleh
	install -Dm755 $(BLEHD) $(DESTDIR)$(BINDIR)/blehd
	@case "$(SETCAP)" in \
		auto) \
			if [ "$$(id -u)" = "0" ] && command -v setcap >/dev/null 2>&1; then \
				echo "Applying capabilities to blehd (cap_net_raw,cap_net_admin)..."; \
				setcap cap_net_raw,cap_net_admin=eip $(DESTDIR)$(BINDIR)/blehd; \
			else \
				echo "(Skipping setcap: not root or setcap not found; run 'sudo make setcap' if needed)"; \
			fi \
			;; \
		1) \
			echo "Applying capabilities to blehd (cap_net_raw,cap_net_admin)..."; \
			setcap cap_net_raw,cap_net_admin=eip $(DESTDIR)$(BINDIR)/blehd \
			;; \
		0) \
			echo "(Skipping setcap: SETCAP=0)" \
			;; \
		*) \
			echo "Unknown SETCAP=$(SETCAP) (use auto|1|0)"; exit 2 \
			;; \
	esac
	@if [ "$$(id -u)" = "0" ]; then \
		echo "Tip: to install a service: make install-systemd (or install-openrc)"; \
	fi

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/bleh
	rm -f $(DESTDIR)$(BINDIR)/blehd

# Give blehd the minimum capabilities it needs so it can run unprivileged.
# (Still recommended to run it with a dedicated group for socket access.)
setcap: install
	setcap cap_net_raw,cap_net_admin=eip $(DESTDIR)$(BINDIR)/blehd

install-systemd:
	install -Dm644 dist/systemd/blehd-root.service $(DESTDIR)$(SYSCONFDIR)/systemd/system/blehd.service
	@echo "Installed root unit: $(SYSCONFDIR)/systemd/system/blehd.service"
	@echo ""
	@echo "Optional: create 'bleh' group for socket access:"
	@echo "  sudo groupadd -r bleh"
	@echo "  sudo usermod -aG bleh $$USER"
	@echo "  (then re-login)"
	@echo ""
	@echo "Now run:"
	@echo "  systemctl daemon-reload"
	@echo "  systemctl enable --now blehd.service"

uninstall-systemd:
	systemctl stop blehd.service 2>/dev/null || true
	systemctl disable blehd.service 2>/dev/null || true
	rm -f $(DESTDIR)$(SYSCONFDIR)/systemd/system/blehd.service
	systemctl daemon-reload 2>/dev/null || true
	@echo "Removed systemd service: $(SYSCONFDIR)/systemd/system/blehd.service"

install-openrc:
	install -Dm755 dist/openrc/blehd $(DESTDIR)$(SYSCONFDIR)/init.d/blehd
	@echo "Installed OpenRC service: $(SYSCONFDIR)/init.d/blehd"
	@echo ""
	@echo "Optional: create 'bleh' group for socket access:"
	@echo "  sudo groupadd -r bleh"
	@echo "  sudo usermod -aG bleh $$USER"
	@echo "  (then re-login)"
	@echo ""
	@echo "Now run: rc-update add blehd default && rc-service blehd start"

uninstall-openrc:
	rc-service blehd stop 2>/dev/null || true
	rc-update del blehd default 2>/dev/null || true
	rm -f $(DESTDIR)$(SYSCONFDIR)/init.d/blehd
	@echo "Removed OpenRC service: $(SYSCONFDIR)/init.d/blehd"
