package ui

import (
	"log"
	"os/exec"
	"syscall"
)

// startAssistant keeps the assistant available to Console without a window.
// The app's D-Bus action makes repeated starts harmless on shell restart.
// What it prints goes to the shell's log, and so does its end.
func (s *shellUI) startAssistant() {
	if !s.cfg.Assistant.Enable {
		return
	}
	go func() {
		cmd := exec.Command("inua-assistant", "--background")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdout, cmd.Stderr = log.Writer(), log.Writer()
		if err := cmd.Start(); err != nil {
			log.Printf("assistant: %v", err)
			return
		}
		log.Printf("assistant: started, pid %d", cmd.Process.Pid)
		log.Printf("assistant: pid %d ended: %v", cmd.Process.Pid, cmd.Wait())
	}()
}
