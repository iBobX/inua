#!/usr/bin/env bash
# Build and run the shell, bin/inua-shell (cmd/inua-shell), on layer-shell
# surfaces, controlled with bin/inua call.
#
# Usage: scripts/spike.sh shell <command> [args]
#
#   build            build the binaries (CGO_ENABLED=0)
#   run [args]       run it in the current session (Hyprland, or another
#                    compositor; shell takes -no-layer for GNOME)
#   nested           start a nested Hyprland window (works from GNOME), run
#                    it inside, and stay in the foreground; Ctrl-C stops both
#   stress [N]       open/close the launcher N times (default 100) in the
#                    running instance; reports hangs, missed keyboard focus
#                    and memory
#   ctl <args>       bin/inua call <target> <function> [args]
#
# The shell's control socket is named by INUA_INSTANCE (nested: "nested"
# unless set), so a nested instance runs beside the one in the real session.
#
# Needs libgtk4-layer-shell0.
# INUA_LAYERSHELL_LIB_DIR=<dir> uses a copy of the library from <dir>.
set -euo pipefail

cd "$(dirname "$0")/.."

case "${1:-}" in
shell) BIN=bin/inua-shell LIB=libgtk4-layer-shell ;;
*) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
shift
APPS=(bin/inua call launcher apps) STATE=(bin/inua call shell state) CLOSE=(bin/inua call shell close)

step() { printf '\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

if [ -n "${INUA_LAYERSHELL_LIB_DIR:-}" ]; then
	export LD_LIBRARY_PATH=$INUA_LAYERSHELL_LIB_DIR${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}
fi

build() {
	step "Building $BIN"
	mkdir -p bin
	CGO_ENABLED=0 go build -o "$BIN" ./cmd/inua-shell
	CGO_ENABLED=0 go build -o bin/inua ./cmd/inua
}

check_lib() {
	if [ -z "${INUA_LAYERSHELL_LIB_DIR:-}" ] && ! ldconfig -p | grep "$LIB.so.0" >/dev/null; then
		fail "${LIB}0 is not installed: sudo apt install ${LIB}0"
	fi
}

stress() {
	local n=${1:-100} hung=0 nofocus=0 pid
	pid=$("${STATE[@]}" 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2) || fail "the shell is not running"
	local start mid=""
	start=$(ps -o rss= -p "$pid" | tr -d ' ')
	step "Opening and closing the launcher $n times"
	for i in $(seq 1 "$n"); do
		timeout 3 "${APPS[@]}" >/dev/null || { hung=$i; break; }
		sleep 0.06
		timeout 3 "${STATE[@]}" | grep -q "launcher.focus=true" || nofocus=$((nofocus + 1))
		timeout 3 "${CLOSE[@]}" >/dev/null || { hung=$i; break; }
		sleep 0.03
		[ "$i" = $((n / 2)) ] && mid=$(ps -o rss= -p "$pid" | tr -d ' ')
	done
	echo "cycles=$n hung-at=${hung} (0 = never) opens-without-focus=$nofocus"
	echo "rss KB: start=$start half=$mid end=$(ps -o rss= -p "$pid" | tr -d ' ')"
	[ "$hung" = 0 ] && [ "$nofocus" = 0 ]
}

nested() {
	command -v Hyprland >/dev/null || fail "Hyprland is not installed: sudo apt install hyprland"
	[ -n "${WAYLAND_DISPLAY:-}" ] || fail "run this from a Wayland session (e.g. GNOME)"
	check_lib
	[ -x "$BIN" ] || build

	step "Starting nested Hyprland"
	local log sock # conf, sig, spid, tpid stay global for the EXIT trap
	log=$(mktemp -d "${TMPDIR:-/tmp}/inua-nested.XXXX")
	# Hyprland re-launches itself, so $! is not the compositor; find it by
	# its config path instead. Nested Hyprland can hang on exit: SIGKILL it.
	conf=$PWD/spike/hyprland-nested.conf
	Hyprland -c "$conf" >"$log/hyprland.log" 2>&1 &
	hpid=$!
	# SIGKILL leaves the instance directory behind; remove it as well.
	trap 'kill "${spid:-}" "${tpid:-}" 2>/dev/null || true; pkill -9 -f "Hyprland -c $conf" || true
		[ -n "${sig:-}" ] && rm -rf "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/hypr/$sig"' EXIT
	trap 'exit 130' INT TERM
	for _ in $(seq 50); do
		read -r sig sock < <(hyprctl instances -j 2>/dev/null | python3 -c "
import json, sys
for i in json.load(sys.stdin):
    if i['pid'] == $hpid: print(i['instance'], i['wl_socket'])" 2>/dev/null || true) || true
		[ -n "${sig:-}" ] && break
		sleep 0.2
	done
	[ -n "${sig:-}" ] || fail "nested Hyprland did not start (log: $log/hyprland.log)"
	export HYPRLAND_INSTANCE_SIGNATURE=$sig WAYLAND_DISPLAY=$sock INUA_INSTANCE=${INUA_INSTANCE:-nested}

	step "Starting $BIN in it"
	"$BIN" >"$log/spike.log" 2>&1 &
	spid=$!
	sleep 2
	"${STATE[@]}" || { cat "$log/spike.log"; fail "$BIN did not start"; }
	try="bin/inua call launcher apps   # click into the Hyprland window to type
  bin/inua call launcher windows
  bin/inua call control toggle"
	cat <<EOF

Nested Hyprland: instance $sig, socket $sock. Logs: $log
In another terminal:
  export HYPRLAND_INSTANCE_SIGNATURE=$sig WAYLAND_DISPLAY=$sock INUA_INSTANCE=$INUA_INSTANCE
  $try
  hyprctl dispatch exec kitty
  scripts/spike.sh shell stress 100
Ctrl-C stops both.

EOF
	# Background + wait, so INT/TERM run the cleanup trap right away.
	tail -f "$log/spike.log" &
	tpid=$!
	wait "$spid" || true
	echo "$BIN exited"
}

case "${1:-}" in
build) build ;;
run)
	shift
	check_lib
	[ -x "$BIN" ] || build
	exec "$BIN" "$@"
	;;
nested) nested ;;
stress) shift; stress "${1:-100}" ;;
ctl)
	shift
	exec bin/inua call "$@"
	;;
*) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
