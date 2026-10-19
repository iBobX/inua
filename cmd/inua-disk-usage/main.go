// Command inua-disk-usage is Disk Usage (docs/disk-usage.md): what takes the
// room on a disk, as a map, the developer's and the agents' folders it
// knows, what the assistant says a folder is, and whether it can go.
//
//	inua-disk-usage [FOLDER]        open the window, scanning FOLDER if given
//	inua-disk-usage --scan FOLDER   the largest items and the known folders in it
//	inua-disk-usage --why PATH      whether PATH can be deleted, check by check
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/lyricat/inua/internal/apps/disk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/diskui"
	"github.com/lyricat/inua/internal/i18n"
)

func main() {
	uiDir := flag.String("ui-dir", "", "read layouts and styles from this directory instead of the embedded copy")
	scan := flag.String("scan", "", "measure `FOLDER` and print its largest items and the folders a rule knows")
	asJSON := flag.Bool("json", false, "with --scan: print JSON")
	top := flag.Int("top", 15, "with --scan: how many of the largest items")
	why := flag.String("why", "", "whether `PATH` can be deleted, check by check")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: inua-disk-usage [-ui-dir DIR] [FOLDER]")
		fmt.Fprintln(os.Stderr, "       inua-disk-usage --scan FOLDER [--json] [--top N]")
		fmt.Fprintln(os.Stderr, "       inua-disk-usage --why PATH")
		flag.PrintDefaults()
	}
	flag.Parse()
	switch {
	case *scan != "":
		os.Exit(runScan(*scan, *top, *asJSON))
	case *why != "":
		os.Exit(runWhy(*why))
	case flag.NArg() > 1:
		flag.Usage()
		os.Exit(2)
	}
	diskui.Register()
	os.Exit(appui.Run("disk", flag.Args(), *uiDir))
}

func initLang() {
	cfg, _, _ := config.Load(config.DefaultPath())
	i18n.Init(cfg.Language)
}

type item struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Files   int64  `json:"files"`
	Kind    string `json:"kind,omitempty"`
	What    string `json:"what,omitempty"`
	Verdict string `json:"verdict,omitempty"`
	Clean   string `json:"clean,omitempty"`
}

func runScan(dir string, top int, asJSON bool) int {
	initLang()
	dir, _ = filepath.Abs(dir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	root, err := disk.Scan(ctx, dir, disk.Options{}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "inua-disk-usage:", err)
		return 1
	}
	home, _ := os.UserHomeDir()
	var largest []item
	for i, k := range root.Kids {
		if i == top {
			break
		}
		largest = append(largest, item{Path: k.Path(), Size: k.Size, Files: k.Files})
	}
	found := map[string][]item{}
	for _, k := range disk.Kinds {
		for _, n := range disk.Found(root, k) {
			it := item{Path: n.Path(), Size: n.Size, Files: n.Files, Kind: k.ID(), What: n.Rule.What, Clean: n.Rule.Clean}
			if !n.Rule.Group {
				it.Verdict = verdictID(n.Rule.Verdict)
			}
			found[k.ID()] = append(found[k.ID()], it)
		}
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"path": root.Path(), "size": root.Size, "files": root.Files, "largest": largest, "found": found,
		})
		return 0
	}
	fmt.Printf("%s  %s  %s\n", disk.Tilde(root.Path(), home), disk.Size(root.Size),
		i18n.Tf("%d files, %.1f s", root.Files, time.Since(start).Seconds()))
	fmt.Println()
	for _, it := range largest {
		fmt.Printf("%9s  %s\n", disk.Size(it.Size), disk.Tilde(it.Path, home))
	}
	for _, k := range disk.Kinds {
		list := found[k.ID()]
		if len(list) == 0 {
			continue
		}
		var all, safe int64
		for _, it := range list {
			all += it.Size
			if it.Verdict == "safe" {
				safe += it.Size
			}
		}
		fmt.Printf("\n%s  %s  %s\n", k.Name(), disk.Size(all), i18n.Tf("%s safe to delete", disk.Size(safe)))
		for i, it := range list {
			if i == top {
				fmt.Println("   …")
				break
			}
			fmt.Printf("%9s  %-7s %s  (%s)\n", disk.Size(it.Size), it.Verdict, disk.Tilde(it.Path, home), i18n.T(it.What))
		}
	}
	return 0
}

func verdictID(v disk.Verdict) string {
	switch v {
	case disk.Safe:
		return "safe"
	case disk.Review:
		return "review"
	case disk.Keep:
		return "keep"
	}
	return ""
}

func runWhy(path string) int {
	initLang()
	path, _ = filepath.Abs(path)
	if _, err := os.Lstat(path); err != nil {
		fmt.Fprintln(os.Stderr, "inua-disk-usage:", err)
		return 1
	}
	r := disk.Decide(context.Background(), path, "")
	for i, s := range r.Steps {
		fmt.Printf("%d. %s  %s\n", i+1, s.Check, s.Found)
	}
	fmt.Println()
	if r.Verdict == disk.Unknown {
		fmt.Println(i18n.T("No rule settles it: ask the assistant in the window."))
	} else {
		fmt.Println(r.Verdict.Name())
	}
	if r.Command.Text != "" {
		fmt.Println(i18n.T("The proper way:"), r.Command.Text)
	}
	return 0
}
