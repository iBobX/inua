package connect

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lyricat/inua/internal/kdeconnect"
)

// Command is a command the paired devices may run here, set up in the
// window.
type Command struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Command string `json:"command"` // run by sh -c
}

// commands are the commands, kept in commands.json.
type commands struct {
	s    *Service
	path string

	mu   sync.Mutex
	cmds []Command
}

func newCommands(s *Service) *commands {
	c := &commands{s: s, path: filepath.Join(Dir(), "commands.json")}
	if data, err := os.ReadFile(c.path); err == nil {
		json.Unmarshal(data, &c.cmds)
	}
	return c
}

func (c *commands) list() []Command {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Command{}, c.cmds...)
}

// set replaces the commands, giving each new one a key, and tells the
// devices.
func (c *commands) set(cmds []Command) error {
	for i := range cmds {
		cmds[i].Name = strings.TrimSpace(cmds[i].Name)
		if cmds[i].Name == "" || strings.TrimSpace(cmds[i].Command) == "" {
			return errors.New("connect: a command needs a name and what it runs")
		}
		if cmds[i].Key == "" {
			var b [8]byte
			rand.Read(b[:])
			cmds[i].Key = hex.EncodeToString(b[:])
		}
	}
	data, _ := json.MarshalIndent(cmds, "", "  ")
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(c.path, data, 0600); err != nil {
		return err
	}
	c.mu.Lock()
	c.cmds = cmds
	c.mu.Unlock()
	for _, d := range c.s.core.Devices() {
		if d.Reachable && d.Pair == kdeconnect.Paired {
			c.sendList(d.ID)
		}
	}
	return nil
}

// sendList gives the device the commands it may run: none when its
// setting is off.
func (c *commands) sendList(id string) {
	list := map[string]map[string]string{}
	if c.s.Setting(id, SetCommands) {
		for _, cmd := range c.list() {
			list[cmd.Key] = map[string]string{"name": cmd.Name, "command": cmd.Command}
		}
	}
	js, _ := json.Marshal(list)
	c.s.core.Send(id, kdeconnect.NewPacket(typeCommand, map[string]any{"commandList": string(js), "canAddCommand": true}))
}

// request runs the command the device asks for, sends the list, or opens
// the window to set them up.
func (c *commands) request(d kdeconnect.Device, p kdeconnect.Packet) {
	var b struct {
		RequestCommandList bool   `json:"requestCommandList"`
		Key                string `json:"key"`
		Setup              bool   `json:"setup"`
	}
	if p.Decode(&b) != nil {
		return
	}
	switch {
	case b.RequestCommandList:
		c.sendList(d.ID)
	case b.Setup:
		if self, err := os.Executable(); err == nil {
			exec.Command(self).Start() // the window: This computer has the commands
		}
	case b.Key != "" && c.s.Setting(d.ID, SetCommands):
		for _, cmd := range c.list() {
			if cmd.Key == b.Key {
				run := exec.Command("sh", "-c", cmd.Command)
				run.Dir = os.Getenv("HOME")
				if err := run.Start(); err != nil {
					log.Printf("connect: %s: %v", cmd.Name, err)
					return
				}
				go run.Wait()
				return
			}
		}
	}
}
