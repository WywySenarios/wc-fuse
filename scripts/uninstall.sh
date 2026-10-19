#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="${WC_CONFIG_DIR:-/etc/wc/config.d}"

DRY_RUN=0
for arg in "$@"; do
	case "$arg" in
	--dry-run) DRY_RUN=1 ;;
	-h | --help)
		echo "usage: uninstall.sh [--dry-run]"
		exit 0
		;;
	*)
		echo "uninstall.sh: unknown argument: $arg" >&2
		exit 2
		;;
	esac
done
USER_NAME="${SUDO_USER:-${USER:?USER is not set}}"

info() { printf '    %s\n' "$*"; }
run() {
	if [[ "$DRY_RUN" -eq 1 ]]; then
		printf '    would run: %s\n' "$*"
	else
		"$@"
	fi
}

info "unmounting every /dev/wc/workspace mount"
for m in /dev/wc/workspace/*; do
	[[ -e "$m" ]] || continue
	if mountpoint -q "$m"; then
		run fusermount3 -u "$m" || run fusermount3 -uz "$m"
	fi
done

run systemctl --user disable --now wc-fuse.service
run rm -f "$HOME/.config/systemd/user/wc-fuse.service" \
	"$HOME/.local/libexec/wc/wc-fuse-supervisor" \
	"$HOME/.local/bin/wc-fuse-daemon"

run sudo systemctl disable --now wc-fuse-root.service
run sudo rm -f /etc/systemd/system/wc-fuse-root.service \
	/etc/tmpfiles.d/wc.conf \
	/usr/local/libexec/wc/wc-fuse-root
run sudo systemctl daemon-reload
if mountpoint -q /dev/wc; then
	run sudo umount /dev/wc || run sudo umount -l /dev/wc
fi
run sudo rm -rf /dev/wc
run sudo loginctl disable-linger "$USER_NAME"

run sudo rm -rf "$CONFIG_DIR"
if grep -q '^user_allow_other' /etc/fuse.conf 2>/dev/null; then
	run sudo sed -i '/^user_allow_other/d' /etc/fuse.conf
fi

info "done."
