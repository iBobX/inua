package vmui

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// assistantPrompt is what inua says to its own assistant (inua-assistant) when
// a machine cannot be installed because this host is missing something.
//
// The assistant knows nothing about this window, so the message carries
// everything it needs to act: which system the machine is and what it boots,
// what this host is, each check that failed with the reason the window shows
// and where its tool usually comes from, and what is being asked of it - look
// first, read-only, then say what would change and carry that out under the
// assistant's own approval policy.
//
// It carries nothing secret, and nothing that is not already on the page: the
// machine's name, its settings, the host's distribution and the checks. inua
// itself runs no package manager and no privileged install; that is the
// assistant's call, shown to the user as an approval.
func assistantPrompt(s MachineView) string {
	m := s.Machine
	var b strings.Builder

	b.WriteString(i18n.T("A machine in inua's virtual machine manager cannot be installed yet: this host is missing something it needs. Check what is wrong below read-only first, then say what you would change and carry it out, reporting item by item."))
	b.WriteString("\n\n")

	b.WriteString(i18n.Tf("The machine: %s, %s, %d MiB of memory, %d CPUs, %d GiB of disk, run by the %s backend.",
		m.Name, string(m.OS), m.MemoryMiB, m.CPUs, m.DiskGiB, string(m.Backend)))
	b.WriteString("\n")
	b.WriteString(i18n.Tf("What it boots: firmware %s, architecture %s, machine type %s, acceleration %s, install media %s.",
		string(m.Boot.Firmware), string(m.Boot.Arch), m.Boot.Machine, string(m.Boot.Accel), vmInstallMedia(m)))
	b.WriteString("\n")
	b.WriteString(i18n.Tf("How it is seen: %s, %s.", string(m.Display.Kind), string(m.Display.GPU)))
	b.WriteString("\n")
	b.WriteString(i18n.Tf("This host: %s, distribution %s.", s.HostOS, s.Distribution))
	b.WriteString("\n\n")

	failed := failedChecks(s.Checks)
	b.WriteString(i18n.Tf("%d checks failed:", len(failed)) + "\n")
	for i, c := range failed {
		hint := vmPackageHint(c.Name, s.Distribution)
		if hint == "" {
			hint = i18n.T("no package of ours covers it")
		}
		hint = vmPackageHintForAssistant(hint)
		b.WriteString(i18n.Tf("%d. %s — %s (%s). %s",
			i+1, vmCheckName(c.Name), vmCheckReason(c.Name, c.Reason), string(c.Reason), hint))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(i18n.T("First check each one read-only - which, --version, ls, systemctl status - and tell me what you actually see. Do not change anything yet."))
	b.WriteString("\n")
	b.WriteString(i18n.T("Then give me the exact commands for each item and what they would change, and run them with pkexec - for example pkexec apt-get install -y PACKAGE - never with sudo: inua's own polkit agent asks me for the password, and nothing is installed until I answer. Do it one batch at a time: say what each batch installs and why before running it, and give me the command that verifies it afterwards."))
	b.WriteString("\n")
	if s.Distribution == "nixos" {
		b.WriteString(i18n.T("This host is NixOS: tell me what to add to the system configuration (environment.systemPackages, virtualisation, and so on) rather than changing or rebuilding the system yourself."))
		b.WriteString("\n")
	}
	b.WriteString(i18n.T("Finally tell me how to confirm each fix - which command, and what to look for. I will press Check again in inua and continue the installation once the checks pass."))
	b.WriteString("\n")
	b.WriteString(i18n.T("No keys, tokens or passwords are needed for this, and none are in this message."))
	return b.String()
}

// vmInstallMedia is the machine's install media as the prompt names it: the
// ISO it would install from, the kernel it boots directly, or nothing yet.
func vmInstallMedia(m vm.Machine) string {
	switch {
	case strings.TrimSpace(m.Boot.ISO) != "":
		return m.Boot.ISO
	case strings.TrimSpace(m.Boot.Kernel) != "":
		return m.Boot.Kernel
	}
	return i18n.T("not chosen yet")
}

// vmPackageHintForAssistant words an install hint the way the assistant is to
// run it: through pkexec, so inua's own polkit agent asks the user, and with
// apt-get rather than apt, which warns about its command line when nothing can
// answer it. Anything else - a NixOS configuration, a command to copy - is
// left as the window words it.
func vmPackageHintForAssistant(hint string) string {
	pkg, ok := strings.CutPrefix(hint, "sudo apt install ")
	if !ok {
		return hint
	}
	return "pkexec apt-get install -y " + strings.TrimSpace(pkg)
}

// askAssistant hands a prompt to the assistant's own window: inua-assistant
// activates the one that is already running and starts it otherwise, which is
// what its --ask argument does.

// assistantAvailable reports whether the assistant has an enabled profile and
// a saved credential/configuration, so callers do not mistake process startup
// for question delivery.
func askAssistant(prompt string) error {
	if _, err := exec.LookPath("inua-assistant"); err != nil {
		return errors.New(i18n.T("inua-assistant is not installed"))
	}
	if err := desktop.Spawn([]string{"inua-assistant", "--ask", prompt}, ""); err != nil {
		return errors.New(i18n.Tf("The assistant could not be started: %v", err))
	}
	return nil
}
