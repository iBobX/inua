package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lyricat/inua/internal/vm"
)

func cmdVM(args []string) error {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "doctor":
		if len(args) != 1 {
			usage()
		}
		return vmReport(false)
	case "status":
		fs := flag.NewFlagSet("vm status", flag.ContinueOnError)
		jsonOutput := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			usage()
		}
		return vmReport(*jsonOutput)
	default:
		usage()
	}
	return nil
}

// vmReport prints what `inua-vm --doctor` and `--status` print: the checks,
// the recommendation and the saved configuration live in internal/vm, shared
// with the manager app (cmd/inua-vm). Both are read-only.
func vmReport(asJSON bool) error {
	report, err := vm.Diagnose("")
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	fmt.Print(report.Text())
	return nil
}
