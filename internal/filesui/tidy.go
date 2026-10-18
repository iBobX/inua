package filesui

import (
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
)

// Tidy with AI: the folder's path goes to inua's assistant, which looks,
// plans (Mister Morph's plan_create) and moves the files with bash. The
// question says how: inside the folder only, nothing deleted or
// overwritten, the moves in one command, and a script that undoes them.

// tidyPrompt is the question for the assistant: %[1]s is the folder.
var tidyPrompt = i18n.M(`Tidy the folder %[1]s.

First look at what is in it: the names, kinds, sizes and dates, and the first lines of a text file whose name says too little. Then make a plan with plan_create and show it to me: the folders to make, which file goes where, and why.

Rules:
- Stay inside %[1]s: make folders in it and move its own files into them. Touch nothing outside it, and leave its subfolders' contents as they are.
- Never delete and never overwrite: use mkdir -p and mv -n, and leave a file alone when its new name is taken.
- Use the folders that are already there when they fit.
- Leave hidden files (names starting with a dot) where they are.
- Do all the moves in one bash command, so I approve them once.
- Write the moves back, in the same order reversed, to %[1]s/.tidy-undo.sh, so "sh .tidy-undo.sh" in the folder undoes it.

When it is done, tell me what went where.`)

// tidy asks the assistant to tidy dir, or says what it needs first.
func (w *window) tidy(dir string) {
	if dir == "" {
		dir = files.Home()
	}
	c, _, _ := config.Load(config.DefaultPath())
	switch _, err := exec.LookPath("inua-assistant"); {
	case err != nil:
		w.flash(i18n.T("Tidying needs the assistant (inua-assistant), which is not installed."))
		return
	case !c.Assistant.Enable:
		w.flash(i18n.T("Turn on the assistant in Settings → AI to tidy folders."))
		return
	case !slices.Contains(c.Assistant.Tools, "bash"):
		w.flash(i18n.T("Turn on Run commands in Settings → AI to let the assistant tidy folders."))
		return
	}
	if err := desktop.Spawn([]string{"inua-assistant", "--ask", i18n.Tf(tidyPrompt, dir)}, ""); err != nil {
		w.flash(err.Error())
		return
	}
	w.flash(i18n.Tf("Asked the assistant to tidy %s.", filepath.Base(dir)))
}
