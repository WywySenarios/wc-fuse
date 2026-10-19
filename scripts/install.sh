#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOST_DIR="$REPO_ROOT/host"

DRY_RUN=0
for arg in "$@"; do
	case "$arg" in
	--dry-run) DRY_RUN=1 ;;
	-h | --help)
		echo "usage: install.sh [--dry-run]"
		exit 0
		;;
	*)
		echo "install.sh: unknown argument: $arg" >&2
		exit 2
		;;
	esac
done

USER_NAME="${SUDO_USER:-${USER:?USER is not set}}"
CONFIG_REPO="${WC_CONFIG_REPO:-https://github.com/WywySenarios/wc-config.git}"
CONFIG_DIR="${WC_CONFIG_DIR:-/etc/wc/config.d}"

header() { printf '==> %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
run() {
	if [[ "$DRY_RUN" -eq 1 ]]; then
		printf '    would run: %s\n' "$*"
	else
		"$@"
	fi
}

header "enable user_allow_other in /etc/fuse.conf"
if grep -q '^user_allow_other' /etc/fuse.conf 2>/dev/null; then
	info "already enabled"
else
	run sudo sh -c 'echo user_allow_other >> /etc/fuse.conf'
fi

header "build + install the daemon"
run mkdir -p "$HOME/.local/bin"
if [[ "$DRY_RUN" -eq 1 ]]; then
	info "would build wc-fuse-daemon -> $HOME/.local/bin/wc-fuse-daemon"
else
	(cd "$REPO_ROOT" && go build -o "$HOME/.local/bin/wc-fuse-daemon" ./cmd/wc-fuse-daemon)
	info "installed $HOME/.local/bin/wc-fuse-daemon"
fi

header "install the supervisor and the user unit"
run mkdir -p "$HOME/.local/libexec/wc" "$HOME/.config/systemd/user"
run install -m 0755 "$HOST_DIR/wc-fuse-supervisor" "$HOME/.local/libexec/wc/wc-fuse-supervisor"
run install -m 0644 "$HOST_DIR/wc-fuse.service" "$HOME/.config/systemd/user/wc-fuse.service"

header "install the root helper, root unit, and tmpfiles snippet"
if [[ "$DRY_RUN" -eq 1 ]]; then
	info "would install wc-fuse-root -> /usr/local/libexec/wc/wc-fuse-root"
	info "would install wc-fuse-root.service (user=$USER_NAME) -> /etc/systemd/system/"
	info "would install wc.conf -> /etc/tmpfiles.d/wc.conf"
else
	sudo install -d -m 0755 /usr/local/libexec/wc
	sudo install -m 0755 "$HOST_DIR/wc-fuse-root" /usr/local/libexec/wc/wc-fuse-root
	sed "s/@USER@/$USER_NAME/g" "$HOST_DIR/wc-fuse-root.service" |
		sudo tee /etc/systemd/system/wc-fuse-root.service >/dev/null
	sudo install -m 0644 "$HOST_DIR/wc.conf" /etc/tmpfiles.d/wc.conf
	sudo systemd-tmpfiles --create /etc/tmpfiles.d/wc.conf
	sudo systemctl daemon-reload
	sudo systemctl enable --now wc-fuse-root.service
fi

header "enable login linger so the user service runs without a session"
run sudo loginctl enable-linger "$USER_NAME"

header "clone or refresh the config repo at $CONFIG_DIR"
if [[ -d "$CONFIG_DIR/.git" ]]; then
	run sudo git -C "$CONFIG_DIR" pull --ff-only
elif [[ "$DRY_RUN" -eq 1 ]]; then
	info "would clone $CONFIG_REPO -> $CONFIG_DIR"
else
	sudo mkdir -p "$CONFIG_DIR"
	sudo git clone "$CONFIG_REPO" "$CONFIG_DIR"
fi

header "start the user service"
run systemctl --user daemon-reload
run systemctl --user enable --now wc-fuse.service

info "done."
