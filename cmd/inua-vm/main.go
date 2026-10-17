// Command inua-vm is inua's virtual machine manager (docs/inua-vm.md): it runs
// a Windows guest with Docker (Dockur) and KVM and reaches it over RDP.
//
//	inua-vm                    open the manager window
//	inua-vm --doctor           what this host provides for the guest, and what is missing
//	inua-vm --status [--json]  the same, for scripts
//	inua-vm --start             start the guest
//	inua-vm --stop              stop the guest, keeping its disk
//
// The host checks, the configuration and the lifecycle are internal/vm; the
// window is internal/vmui, an app of its own in the shell's look, so the shell
// carries no VM manager. Docker socket access is privileged host access: the
// manager says so, and never offers to add anyone to the docker group.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/vm"
	"github.com/lyricat/inua/internal/vmui"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	doctor := flag.Bool("doctor", false, "check this host for what the Windows guest needs")
	status := flag.Bool("status", false, "the host's checks and the guest's configuration")
	asJSON := flag.Bool("json", false, "with --status: write JSON")
	start := flag.Bool("start", false, "start the guest")
	stop := flag.Bool("stop", false, "stop the guest, keeping its disk")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-vm [-ui-dir DIR] [--doctor | --status [--json] | --start | --stop]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	switch {
	case *doctor, *status:
		report, err := vm.Diagnose("")
		if err != nil {
			fail(err)
		}
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				fail(err)
			}
			return
		}
		fmt.Print(report.Text())
	case *start:
		if err := vmui.Start(); err != nil {
			fail(err)
		}
	case *stop:
		if err := vmui.Stop(); err != nil {
			fail(err)
		}
	default:
		vmui.Register()
		os.Exit(appui.Run("vm", nil, *uiDir))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "inua-vm:", err)
	os.Exit(1)
}
