package vmui

import (
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/vm"
)

// assistantView is what the prerequisites page has in hand when it asks the
// assistant for help: a UEFI QEMU machine of another architecture, on a Debian
// host, with two checks failing and two passing.
func assistantView(t *testing.T) MachineView {
	t.Helper()
	m := fromTemplate(t, "fedora", "fedora", vm.Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 40})
	m.Boot.Arch = vm.ArchAarch64
	return MachineView{
		Name:         m.Name,
		Machine:      m,
		Distribution: "debian",
		HostOS:       "linux",
		Checks: []vm.DoctorItem{
			{Name: "Operating system", OK: true},
			{Name: "QEMU", OK: false, Reason: vm.ReasonMissing},
			{Name: "Firmware (UEFI)", OK: false, Reason: vm.ReasonMissing},
			{Name: "Systemd", OK: true},
		},
	}
}

// The prompt stands on its own: the assistant knows nothing of this window, so
// the machine, the host and every failing check have to be in it.
func TestAssistantPromptCarriesTheMachineAndTheHost(t *testing.T) {
	prompt := assistantPrompt(assistantView(t))
	for _, want := range []string{
		"fedora",
		string(vm.OSLinux),
		"qemu",
		"aarch64",
		"uefi",
		"spice",
		"debian",
		"linux",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not name %q:\n%s", want, prompt)
		}
	}
	// Every failing check, with the reason the window words and the reason's
	// own name, and where its tool usually comes from.
	for _, check := range []string{"QEMU", "Firmware (UEFI)"} {
		if !strings.Contains(prompt, vmCheckName(check)) {
			t.Errorf("the prompt does not name the failing check %q:\n%s", check, prompt)
		}
	}
	if !strings.Contains(prompt, vmCheckReason("QEMU", vm.ReasonMissing)) {
		t.Errorf("the prompt does not say why QEMU is a problem:\n%s", prompt)
	}
	if !strings.Contains(prompt, string(vm.ReasonMissing)) {
		t.Errorf("the prompt does not carry the reason's own name:\n%s", prompt)
	}
	// The hint is the window's, worded the way the assistant is to run it.
	if hint := vmPackageHintForAssistant(vmPackageHint("QEMU", "debian")); hint == "" || !strings.Contains(prompt, hint) {
		t.Errorf("the prompt does not say how QEMU is usually installed (%q):\n%s", hint, prompt)
	}
	if !strings.Contains(prompt, "qemu-system-") {
		t.Errorf("the prompt's installation hint names no QEMU package:\n%s", prompt)
	}
	// A check that passed is not the assistant's business.
	if strings.Contains(prompt, vmCheckName("Systemd")) {
		t.Errorf("the prompt asks about a check that passed:\n%s", prompt)
	}
}

// The assistant is asked to look before it touches anything, to explain what
// it would change, and to go through inua's own polkit rather than sudo.
func TestAssistantPromptAsksForReadOnlyFirst(t *testing.T) {
	prompt := assistantPrompt(assistantView(t))
	readOnly := strings.Index(prompt, "read-only")
	then := strings.Index(prompt, "run them with pkexec")
	confirmation := strings.Index(prompt, "confirm each fix")
	switch {
	case readOnly < 0 || then < 0 || confirmation < 0:
		t.Fatalf("the prompt does not say look, then change, then confirm:\n%s", prompt)
	case !(readOnly < then && then < confirmation):
		t.Errorf("the prompt's order is not look, change, confirm:\n%s", prompt)
	}
	if !strings.Contains(prompt, "%") && strings.Count(prompt, "which") == 0 {
		t.Errorf("the prompt does not name the read-only checks to run:\n%s", prompt)
	}
}

// Installing goes through inua's own polkit agent, never through sudo: that is
// what puts a password dialog in front of the user before anything changes.
func TestAssistantPromptInstallsThroughPkexec(t *testing.T) {
	prompt := assistantPrompt(assistantView(t))
	if !strings.Contains(prompt, "pkexec apt-get install -y") {
		t.Errorf("the prompt's installation hint is not a pkexec one:\n%s", prompt)
	}
	if strings.Contains(prompt, "sudo apt install") {
		t.Errorf("the prompt hands the assistant a sudo command:\n%s", prompt)
	}
	if !strings.Contains(prompt, "never with sudo") {
		t.Errorf("the prompt does not forbid sudo:\n%s", prompt)
	}
	// One batch at a time, said before it runs, verified after.
	for _, want := range []string{"one batch at a time", "say what each batch installs"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not say %q:\n%s", want, prompt)
		}
	}
}

// On NixOS the assistant is asked for a configuration, not for a rebuild: what
// changes the system is the user's own act.
func TestAssistantPromptOnNixOSAsksForConfiguration(t *testing.T) {
	nix := assistantView(t)
	nix.Distribution = "nixos"
	prompt := assistantPrompt(nix)
	if !strings.Contains(prompt, "NixOS") || !strings.Contains(prompt, "environment.systemPackages") {
		t.Errorf("a NixOS host is not asked for a configuration:\n%s", prompt)
	}
	if strings.Contains(assistantPrompt(assistantView(t)), "environment.systemPackages") {
		t.Error("a Debian host is asked for a NixOS configuration")
	}
}

// Installing from an ISO or from a kernel file is what the machine is for, and
// the prompt says which one is set - or that none is.
func TestAssistantPromptNamesTheInstallMedia(t *testing.T) {
	tmpl, ok := vm.TemplateByID("custom")
	if !ok {
		t.Fatal("no custom template")
	}
	m := tmpl.Machine("kernel", vm.Profile{MemoryMiB: 1024, CPUs: 1, DiskGiB: 4})
	view := assistantView(t)
	view.Machine = m

	view.Machine.Boot.ISO = "/media/ubuntu.iso"
	if prompt := assistantPrompt(view); !strings.Contains(prompt, "/media/ubuntu.iso") {
		t.Errorf("the prompt does not name the ISO:\n%s", prompt)
	}
	view.Machine.Boot.ISO = ""
	view.Machine.Boot.Kernel = "/boot/vmlinuz"
	if prompt := assistantPrompt(view); !strings.Contains(prompt, "/boot/vmlinuz") {
		t.Errorf("the prompt does not name the kernel:\n%s", prompt)
	}
	view.Machine.Boot.Kernel = ""
	if prompt := assistantPrompt(view); !strings.Contains(prompt, vmInstallMedia(view.Machine)) {
		t.Errorf("the prompt does not say no install media is chosen:\n%s", prompt)
	}
}
