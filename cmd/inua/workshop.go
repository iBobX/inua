package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/workshop"
)

// cmdWorkshop manages the light apps users write (docs/workshop.md).
func cmdWorkshop(args []string) error {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "list":
		return workshopList()
	case "new":
		lang := "python"
		if len(args) > 1 && (args[1] == "--js" || args[1] == "--python") {
			lang = strings.TrimPrefix(args[1], "--")
			args = append(args[:1], args[2:]...)
		}
		if len(args) < 2 || len(args) > 3 {
			usage()
		}
		name := ""
		if len(args) == 3 {
			name = args[2]
		}
		m, err := workshop.New(args[1], name, lang)
		if err != nil {
			return err
		}
		fmt.Printf("Made %s (%s) in %s.\nTry it: inua workshop dev %s\n", m.Name, m.ID, m.Dir, args[1])
		return nil
	case "install":
		yes := len(args) == 3 && (args[1] == "-y" || args[1] == "--yes")
		if yes {
			args = []string{args[0], args[2]}
		}
		if len(args) != 2 {
			usage()
		}
		m, err := workshop.Install(args[1], func(m workshop.Manifest, replaces bool) bool {
			fmt.Printf("%s %s (%s)\n", m.Name, m.Version, m.ID)
			if m.Description != "" {
				fmt.Println("  " + m.Description)
			}
			fmt.Println("  runs:", strings.Join(m.Run, " "))
			uses := "nothing beyond what every app has"
			if len(m.Uses) > 0 {
				uses = strings.Join(m.Uses, ", ")
			}
			fmt.Println("  uses:", uses)
			fmt.Println("  It runs as you, with your files, like any program you run.")
			q := "Install it?"
			if replaces {
				q = "Replace the installed one (it goes to the trash)?"
			}
			if yes {
				return true
			}
			return askTerminal(q)
		})
		if err != nil {
			return err
		}
		fmt.Printf("Installed %s in %s; the launcher lists it.\n", m.Name, m.Dir)
		return nil
	case "remove":
		if len(args) != 2 {
			usage()
		}
		m, err := workshop.Remove(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Removed %s (to the trash); its data stays in %s.\n", m.Name, workshop.DataDir(m.ID))
		return nil
	case "run", "dev":
		if len(args) != 2 {
			usage()
		}
		m, err := workshop.Find(args[1])
		if err != nil {
			return err
		}
		argv := []string{"workshop"}
		if args[0] == "dev" {
			argv = append(argv, "--dev")
			fmt.Fprintf(os.Stderr, "Running %s; it starts again when a file of %s changes. The protocol's lines and the app's stderr follow.\n", m.Name, m.Dir)
		}
		cmd := exec.Command(sibling("inua-apps"), append(argv, m.Dir)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	usage()
	return nil
}

// askTerminal asks a yes-or-no question on stdin, Yes by default; with
// no terminal there, nobody can answer: no (install -y says yes).
func askTerminal(question string) bool {
	if _, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS); err != nil {
		fmt.Println(question, "No terminal to answer in; run it again with -y to install.")
		return false
	}
	fmt.Printf("%s [Y/n] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "" || a == "y" || a == "yes"
}

func workshopList() error {
	apps, broken := workshop.Installed()
	if len(apps) == 0 && len(broken) == 0 {
		fmt.Println("No workshop apps installed (inua workshop install URL|DIR).")
	}
	for _, m := range apps {
		fmt.Printf("%-28s %-10s %s\n", m.ID, m.Version, m.Name)
	}
	for dir, err := range broken {
		fmt.Printf("broken: %s: %v\n", dir, err)
	}
	return nil
}

// sibling is the program beside this one, else name on PATH.
func sibling(name string) string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); isFile(p) {
			return p
		}
	}
	return name
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
