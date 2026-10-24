package pty

import (
	"os"
	"os/signal"
)

// defaultSignals gives back the default to those this process inherited
// as ignored (started in the background by a non-interactive shell, say),
// for its children: they inherit what is ignored, and Go leaves an
// ignored signal ignored; without this, Ctrl+C would not stop a program
// in the terminal. macOS has no sigaction call in x/sys, so they are
// caught here and dropped: a caught signal is back at the default in a
// program started, as exec leaves only the ignored ones ignored.
func defaultSignals() {
	defaultOnce.Do(func() {
		var ignored []os.Signal
		for _, sig := range jobSignals {
			if signal.Ignored(sig) {
				ignored = append(ignored, sig)
			}
		}
		if len(ignored) == 0 {
			return
		}
		c := make(chan os.Signal, 1)
		signal.Notify(c, ignored...)
		go func() {
			for range c {
			}
		}()
	})
}
