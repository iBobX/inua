#!/usr/bin/env python3
"""Monitor/lock regression tests in an isolated, nested Hyprland session.

Build first with `go build -o bin/inua-shell ./cmd/inua-shell` and
`go build -o bin/inua ./cmd/inua`, then run:
    dbus-run-session -- python3 scripts/test-monitor-lock.py
Requires a Wayland session, Hyprland, hyprctl and the shell's GTK libraries.
Only the nested session is locked; no physical outputs, passwords or
suspend operations are used. Test monitors use the headless backend.
"""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[1]


class NestedSession(unittest.TestCase):
    """A nested Hyprland with one headless monitor, TEST-1, and the shell."""

    shell_config = "[bar]\ntray = false\n[notifications]\nenable = false\n"
    desktop_layers = {"inua-bar", "inua-dock", "inua-background"}

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="inua-monitor-test-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.env = os.environ.copy()
        parent_display = Path(self.env["WAYLAND_DISPLAY"])
        if not parent_display.is_absolute():
            parent_display = Path(self.env["XDG_RUNTIME_DIR"]) / parent_display
        for name in ("WAYLAND_DISPLAY", "DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE"):
            self.env.pop(name, None)
        self.env.update(
            XDG_CONFIG_HOME=str(self.directory / "config"),
            XDG_CACHE_HOME=str(self.directory / "cache"),
            XDG_STATE_HOME=str(self.directory / "state"),
            WAYLAND_DISPLAY=str(parent_display),
            GSK_RENDERER="cairo",
            INUA_INSTANCE=self.directory.name,
        )
        for name in ("XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"):
            Path(self.env[name]).mkdir()
        config = self.directory / "hyprland.conf"
        config.write_text("""monitor = , 1280x720@60, auto, 1
misc {
    allow_session_lock_restore = true
    disable_hyprland_logo = true
    disable_splash_rendering = true
}
animations {
    enabled = false
}
""")
        compositor = self.start_process("compositor", ["Hyprland", "--config", str(config)])
        instances = lambda: [i for i in json.loads(self.hypr("instances", "-j")) if i["pid"] == compositor.pid]
        self.wait_for(instances)
        instance = instances()[0]
        self.env["HYPRLAND_INSTANCE_SIGNATURE"] = instance["instance"]
        self.env["WAYLAND_DISPLAY"] = instance["wl_socket"]
        runtime = Path(self.env["XDG_RUNTIME_DIR"])
        self.addCleanup((runtime / ("inua-locked-" + self.env["INUA_INSTANCE"])).unlink, missing_ok=True)
        self.addCleanup((runtime / "inua" / (self.env["INUA_INSTANCE"] + ".sock")).unlink, missing_ok=True)
        self.hypr("output", "create", "headless", "TEST-1")
        self.config = self.directory / "config" / "inua" / "config.toml"
        self.config.parent.mkdir(parents=True)
        self.config.write_text(self.shell_config)
        self.start_process("shell", [str(ROOT / "bin/inua-shell"), "-config", str(self.config)])
        self.wait_for(lambda: self.has_desktop("TEST-1"))

    def start_process(self, name, command):
        log = open(self.directory / (name + ".log"), "w+")
        self.addCleanup(log.close)
        process = subprocess.Popen(command, env=self.env, stdout=log, stderr=log)

        def stop():
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        self.addCleanup(stop)
        return process

    def wait_for(self, predicate):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                if predicate():
                    return
            except (subprocess.CalledProcessError, json.JSONDecodeError):
                pass
            time.sleep(0.05)
        logs = "\n".join(p.read_text() for p in self.directory.rglob("*.log"))
        self.fail("Timed out waiting for session state:\n" + logs[-12000:])

    def hypr(self, *arguments):
        instance = self.env.get("HYPRLAND_INSTANCE_SIGNATURE")
        select = ["-i", instance] if instance else []
        return subprocess.check_output(["hyprctl", *select, *arguments], env=self.env, text=True, timeout=3)

    def call(self, *arguments):
        return subprocess.check_output(
            [str(ROOT / "bin/inua"), "call", *arguments], env=self.env, text=True, timeout=3
        ).strip()

    def layers(self, monitor=None):
        """The layer namespaces on a monitor, or on every monitor."""
        layers = json.loads(self.hypr("layers", "-j"))
        outputs = [layers.get(monitor, {})] if monitor else layers.values()
        return {s["namespace"] for o in outputs for level in o.get("levels", {}).values() for s in level}

    def has_desktop(self, monitor):
        return self.desktop_layers <= self.layers(monitor)

    def log(self):
        return (self.directory / "shell.log").read_text()

    def lock(self):
        self.call("lock", "now")
        self.wait_for(lambda: "lock: locked" in self.log())

    def unlock(self):
        self.call("lock", "unlock")
        self.wait_for(lambda: self.call("lock", "state") == "false")


class MonitorLockTest(NestedSession):
    def test_monitor_replaced_while_locked(self):
        self.lock()
        screens = self.log().count("lock: lock screen on")
        self.hypr("output", "create", "headless", "TEST-2")
        self.hypr("output", "remove", "TEST-1")
        # Wait until both the compositor and GTK have processed the change.
        self.wait_for(lambda: "TEST-1" not in self.call("shell", "state"))
        self.assertFalse(self.has_desktop("TEST-2"), "desktop created before unlock")
        # The new monitor needs its own password field, or nobody can unlock.
        self.wait_for(lambda: self.log().count("lock: lock screen on") > screens)
        self.unlock()
        self.wait_for(lambda: self.has_desktop("TEST-2"))
        self.assertNotIn("stubbed", self.log())

    def test_restart_while_locked(self):
        self.lock()
        starts = self.log().count("started:")
        locks = self.log().count("lock: locked")
        self.call("shell", "restart")
        self.wait_for(lambda: self.log().count("started:") > starts)
        self.wait_for(lambda: self.log().count("lock: locked") > locks)
        self.unlock()
        self.wait_for(lambda: self.has_desktop("TEST-1"))
        self.assertNotIn("stubbed", self.log())


if __name__ == "__main__":
    unittest.main()
