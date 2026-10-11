"""Exercise session installation and startup without root or a compositor."""

import configparser
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest


ROOT = Path(__file__).resolve().parent.parent


class SessionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="inua-session-")
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name) / "home with spaces"
        self.bin = self.home / ".local/bin"
        self.bin.mkdir(parents=True)
        self.env = dict(os.environ, HOME=str(self.home), PATH="/usr/bin:/bin")
        self.env.pop("XDG_CONFIG_HOME", None)
        self.config = self.home / ".config/hypr/hyprland.conf"
        self.config.parent.mkdir(parents=True)
        self.config.write_text("# test\n")
        self.executable("inua-shell", "#!/bin/sh\nexit 0\n")
        # inua records how it was called; `inua config hyprland` passes.
        self.calls = self.home / "inua-calls"
        self.executable("inua", f'#!/bin/sh\necho "$*" >> "{self.calls}"\nexit 0\n')
        self.executable("start-hyprland", '#!/bin/sh\nprintf "argc=%s\\nXDG_CONFIG_HOME=%s\\n" "$#" "${XDG_CONFIG_HOME-}"\nexit 17\n')

    def executable(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def launch(self):
        return subprocess.run(
            ["sh", str(ROOT / "scripts/inua-session")], env=self.env,
            text=True, capture_output=True,
        )

    def test_default_config_and_exit_status(self):
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])

    def test_xdg_config_home(self):
        xdg = self.home / "other config"
        config = xdg / "hypr/hyprland.conf"
        config.parent.mkdir(parents=True)
        config.write_text("# custom\n")
        self.env["XDG_CONFIG_HOME"] = str(xdg)
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", f"XDG_CONFIG_HOME={xdg}"])

    def test_missing_config_is_left_to_hyprland(self):
        self.config.unlink()
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])
        self.assertFalse(self.config.exists())

    def test_config_is_checked_for_the_shell(self):
        result = self.launch()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(self.calls.read_text().splitlines(), [f"config hyprland {self.config}"])

    def test_config_without_shell_does_not_start_compositor(self):
        self.executable("inua", '#!/bin/sh\necho "hyprland.conf does not start inua-shell"\nexit 1\n')
        result = self.launch()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("does not start inua-shell", result.stderr)
        self.assertIn("Hyprland session is unaffected", result.stderr)

    def test_missing_shell_does_not_start_compositor(self):
        (self.bin / "inua-shell").unlink()
        result = self.launch()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("inua-shell", result.stderr)

    def test_install_and_reinstall_preserve_other_sessions(self):
        dest = Path(self.tmp.name) / "staged root"
        sessions = dest / "usr/share/wayland-sessions"
        sessions.mkdir(parents=True)
        hypr = sessions / "hyprland.desktop"
        hypr.write_text("existing session\n")
        for _ in range(2):
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "gdm", str(dest)],
                cwd=self.tmp.name, text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(hypr.read_text(), "existing session\n")
        desktop = configparser.ConfigParser()
        desktop.read(sessions / "inua.desktop")
        entry = desktop["Desktop Entry"]
        self.assertEqual(entry["Name"], "Inua")
        self.assertEqual(entry["Type"], "Application")
        self.assertIn("Hyprland", entry["DesktopNames"].split(";"))
        launcher = dest / entry["Exec"].lstrip("/")
        self.assertEqual(entry["TryExec"], entry["Exec"])
        self.assertTrue(os.access(launcher, os.X_OK))
        result = subprocess.run([str(launcher)], env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["argc=0", "XDG_CONFIG_HOME="])

    def test_greetd_configuration_replaces_and_backs_up_existing_settings(self):
        dest = Path(self.tmp.name) / "greetd root"
        for number, existing in enumerate((None, "# first login settings\n", "# second login settings\n")):
            config = dest / "etc/greetd/config.toml"
            if existing is not None:
                config.write_text(existing)
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "greetd", str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            if existing is not None:
                self.assertEqual(config.with_name(f"config.toml.~{number}~").read_text(), existing)
            data = tomllib.loads(config.read_text())
            self.assertEqual(data["default_session"]["user"], "_greetd")
            command = data["default_session"]["command"]
            self.assertIn("tuigreet", command)
            self.assertIn("--sessions /usr/share/wayland-sessions", command)
            self.assertIn("--cmd /usr/local/bin/inua-session", command)
            self.assertNotIn("initial_session", data)
            # An unchanged reinstall must not add backups or replace earlier ones.
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), "greetd", str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(list(config.parent.glob("config.toml.~*~"))), number)
        self.assertEqual(config.with_name("config.toml.~1~").read_text(), "# first login settings\n")
        self.assertFalse(config.with_name("config.toml.inua").exists())

    def test_gdm_and_sddm_do_not_install_greetd_configuration(self):
        for dm in ("gdm", "sddm"):
            dest = Path(self.tmp.name) / dm
            result = subprocess.run(
                ["sh", str(ROOT / "scripts/install-session.sh"), dm, str(dest)],
                text=True, capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((dest / "usr/share/wayland-sessions/inua.desktop").is_file())
            self.assertFalse((dest / "etc/greetd").exists())

    def test_invalid_dm_fails_before_installation(self):
        dest = Path(self.tmp.name) / "invalid"
        result = subprocess.run(
            ["sh", str(ROOT / "scripts/install-session.sh"), "unknown", str(dest)],
            text=True, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(dest.exists())


if __name__ == "__main__":
    unittest.main()
