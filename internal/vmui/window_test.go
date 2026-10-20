package vmui

import (
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/vm"
)

// The Docker socket is the one host check whose reason has to carry a
// warning: the socket is there and this user cannot use it, and the fix —
// joining the docker group — would give everything running as that user what
// amounts to root on this host. The window explains that, shows the command
// under it for whoever decides they want it (to copy, never as a button), and
// points at the two ways that do not widen the account's rights.
func TestDockerDeniedExplainsTheGroupInsteadOfOfferingIt(t *testing.T) {
	got := vmCheckReason("Docker", vm.ReasonDenied)
	for _, want := range []string{"docker group", "root", "rootless Docker", "Podman"} {
		if !strings.Contains(got, want) {
			t.Errorf("the reason for a denied Docker socket does not say %q:\n%s", want, got)
		}
	}
	denied := vm.DoctorItem{Name: "Docker", Reason: vm.ReasonDenied}
	if hint, command := vmCheckHint(denied, "debian"); !command || !strings.Contains(hint, "gpasswd") {
		t.Errorf("the hint for a denied Docker socket = %q (command %v), want the gpasswd command", hint, command)
	}
	// Every other check keeps the plain sentence: the kvm group grants one
	// device, and its remedy is a button.
	if got := vmCheckReason("KVM", vm.ReasonDenied); got != "Installed, but your user may not use it." {
		t.Errorf("the reason for a denied KVM check = %q", got)
	}
	if got := vmCheckReason("Docker", vm.ReasonStopped); got != "Installed, but not running." {
		t.Errorf("the reason for a stopped Docker daemon = %q", got)
	}
}

func TestFaultRecoveryRowTextIsVisible(t *testing.T) {
	// Fault names and their load errors are presented in the recovery list,
	// where selecting a row exposes an explicitly confirmed folder deletion.
	fault := vm.Fault{Name: "broken", Error: "invalid machine.toml"}
	if fault.Name != "broken" || fault.Error == "" {
		t.Fatalf("fault entry is incomplete: %+v", fault)
	}
}

// A tool that is there but may not be used, or is not running, is not to be
// installed again: the package line shows only for what is missing.
func TestCheckHintInstallsOnlyWhatIsMissing(t *testing.T) {
	if hint, command := vmCheckHint(vm.DoctorItem{Name: "QEMU", Reason: vm.ReasonMissing}, "debian"); !command || !strings.HasPrefix(hint, "sudo apt install ") {
		t.Errorf("the hint for a missing QEMU on Debian = %q (command %v)", hint, command)
	}
	if hint, _ := vmCheckHint(vm.DoctorItem{Name: "Docker", Reason: vm.ReasonStopped}, "debian"); strings.Contains(hint, "install") {
		t.Errorf("the hint for a stopped Docker installs it: %q", hint)
	}
	if hint, _ := vmCheckHint(vm.DoctorItem{Name: "KVM", Reason: vm.ReasonDenied}, "debian"); hint != "" {
		t.Errorf("the hint for a denied KVM = %q, want none (its fix is a button)", hint)
	}
}

// How a guest is seen decides the rows that show it: Windows in a container
// over Remote Desktop with a web console besides, a QEMU guest by its display,
// and Waydroid by itself.
func TestSeeingFollowsBackendAndDisplay(t *testing.T) {
	for _, c := range []struct {
		m    vm.Machine
		want seeing
	}{
		{vm.Machine{Backend: vm.BackendDockur}, seeRDP},
		{vm.Machine{Backend: vm.BackendQEMU, Display: vm.DisplayConfig{Kind: vm.DisplaySPICE}}, seeSPICE},
		{vm.Machine{Backend: vm.BackendQEMU, Display: vm.DisplayConfig{Kind: vm.DisplayGTK}}, seeWindow},
		{vm.Machine{Backend: vm.BackendQEMU, Display: vm.DisplayConfig{Kind: vm.DisplaySerial}}, seeSerial},
		{vm.Machine{Backend: vm.BackendWaydroid}, seeNone},
	} {
		if got := seeingOf(c.m); got != c.want {
			t.Errorf("seeingOf(%s, %s) = %v, want %v", c.m.Backend, c.m.Display.Kind, got, c.want)
		}
	}
}
