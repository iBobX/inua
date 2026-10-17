#!/bin/sh
# Run inua-shell in an isolated headless Wayland session, for checking the
# shell on a machine whose own desktop must stay untouched: one reached over
# SSH, one someone is working in, or one running another window manager.
#
#   scripts/test-headless.sh [start|stop|status]
#
# start       launch a private Weston headless session and inua-shell in it
# stop        stop only the processes recorded by this script
# status      report the private session's processes and control socket
#
# The session has its own HOME under $XDG_STATE_HOME/inua-headless, its own
# Wayland socket, its own XDG_RUNTIME_DIR and a separate inua instance. It
# never touches the running desktop, its Hyprland configuration, the user's
# inua configuration or systemd units. It needs weston and gtk4-layer-shell
# from the system's packages, and bin/inua-shell and bin/inua (make build).
#
# Then check the shell through the private control socket, for example:
#
#   INUA_INSTANCE=inua-headless XDG_RUNTIME_DIR=$HOME/.local/state/inua-headless/run \
#     bin/inua call shell state
#
# Weston is not Hyprland: the shell runs with normal windows (-no-layer) and
# without Hyprland's IPC, so workspaces, the overview and the dock's windows
# are not there. Its headless backend offers neither wlr-screencopy nor VNC,
# so there are no screenshots either: this checks state and interaction, not
# looks. For those, use a nested Hyprland in a session of your own.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
state=${XDG_STATE_HOME:-$HOME/.local/state}/inua-headless
session_home=$state/home
runtime=$state/run
socket=inua-headless
instance=inua-headless
binary=$root/bin/inua-shell
mode=${1:-status}

pidfile() { echo "$state/$1.pid"; }

# stop_pid stops exactly the recorded process. Never use pkill by program
# name here: that can match the processes of the desktop already running.
stop_pid() {
	name=$1
	file=$(pidfile "$name")
	[ -f "$file" ] || return 0
	pid=$(cat "$file" 2>/dev/null || true)
	rm -f "$file"
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		kill -TERM "$pid" 2>/dev/null || true
		for _ in $(seq 50); do
			kill -0 "$pid" 2>/dev/null || break
			sleep 0.1
		done
		kill -KILL "$pid" 2>/dev/null || true
		echo "stopped $name (pid $pid)"
	fi
}

stop_session() {
	stop_pid inua-shell
	stop_pid weston
}

start_session() {
	command -v weston >/dev/null || { echo 'weston is not installed; install it from the system packages' >&2; exit 1; }
	if [ ! -x "$binary" ]; then
		echo "$binary is missing; build it first with: go build -o bin/inua-shell ./cmd/inua-shell" >&2
		exit 1
	fi
	if [ -f "$(pidfile weston)" ] || [ -f "$(pidfile inua-shell)" ]; then
		echo 'a test session is already running; run stop first' >&2
		exit 1
	fi
	rm -rf "$session_home" "$runtime"
	mkdir -p "$session_home/.config/inua" "$session_home/.local/share" "$runtime"
	chmod 700 "$runtime"
	cat >"$session_home/.config/inua/config.toml" <<'EOF'
language = "en"
[assistant]
enable = false
[notifications]
enable = false
[clipboard]
enable = false
[overview]
per_monitor = false
EOF
	export XDG_RUNTIME_DIR=$runtime
	export XDG_CONFIG_HOME=$session_home/.config
	export XDG_DATA_HOME=$session_home/.local/share
	export XDG_STATE_HOME=$session_home/.local/state
	export XDG_CACHE_HOME=$session_home/.cache
	export HOME=$session_home
	export INUA_INSTANCE=$instance
	unset HYPRLAND_INSTANCE_SIGNATURE DISPLAY
	weston --backend=headless-backend.so --renderer=pixman --width=1600 --height=1000 \
		--socket=$socket --no-config --log=$state/weston.log >/dev/null 2>&1 &
	echo $! >"$(pidfile weston)"
	for _ in $(seq 100); do
		[ -S "$runtime/$socket" ] && break
		sleep 0.1
	done
	[ -S "$runtime/$socket" ] || { echo "weston did not start (see $state/weston.log)" >&2; exit 1; }
	export WAYLAND_DISPLAY=$socket
	"$binary" -n "$instance" -no-layer -ui-dir "$root/ui" >"$state/shell.log" 2>&1 &
	echo $! >"$(pidfile inua-shell)"
	for _ in $(seq 150); do
		if [ -S "$runtime/inua/$instance.sock" ] && "$root/bin/inua" call -n "$instance" shell state >/dev/null 2>&1; then
			echo "headless session ready"
			echo "  instance $instance, runtime $runtime"
			echo "  logs $state"
			exit 0
		fi
		sleep 0.1
	done
	echo "inua-shell did not become ready (see $state/shell.log)" >&2
	tail -20 "$state/shell.log" >&2 || true
	exit 1
}

case "$mode" in
start) start_session ;;
stop) stop_session ;;
status)
	export XDG_RUNTIME_DIR=$runtime INUA_INSTANCE=$instance
	for name in weston inua-shell; do
		file=$(pidfile "$name")
		if [ -f "$file" ] && kill -0 "$(cat "$file")" 2>/dev/null; then
			echo "$name: pid $(cat "$file")"
		else
			echo "$name: stopped"
		fi
	done
	if [ -S "$runtime/inua/$instance.sock" ]; then
		"$root/bin/inua" call -n "$instance" shell state || true
	fi
	;;
*)
	sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
