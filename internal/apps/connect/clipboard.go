package connect

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/kdeconnect"
)

// maxClip is the longest text the clipboard shares.
const maxClip = 1 << 20

// clipboardSync shares the clipboard with the paired devices: text copied
// here goes to them, and what they copy comes here. A copy that is
// sensitive (a password) stays here.
type clipboardSync struct {
	s *Service

	// set puts text on this computer's clipboard.
	set func(text string) error

	mu   sync.Mutex
	last string
	when int64 // when last was copied, in ms
}

func newClipboardSync(s *Service) *clipboardSync { return &clipboardSync{s: s, set: wlCopy} }

func wlCopy(text string) error {
	cmd := exec.Command("wl-copy", "--type", "text/plain;charset=utf-8")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// watch runs wl-paste, which hands each text copied here to
// inua-connect --clipboard, which gives it to the service when it is not
// sensitive.
func (c *clipboardSync) watch(ctx context.Context) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", "text", "--watch", self, "--clipboard")
		if err := cmd.Run(); err != nil && ctx.Err() == nil {
			log.Printf("connect: the clipboard's watcher: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// copied is text copied here: the devices are given it.
func (c *clipboardSync) copied(text string) {
	if text == "" || len(text) > maxClip {
		return
	}
	c.mu.Lock()
	if text == c.last {
		c.mu.Unlock()
		return
	}
	c.last, c.when = text, time.Now().UnixMilli()
	c.mu.Unlock()
	for _, d := range c.s.core.Devices() {
		if d.Reachable && d.Pair == kdeconnect.Paired && c.s.Setting(d.ID, SetClipboard) && d.Takes(typeClipboard) {
			c.s.core.Send(d.ID, kdeconnect.NewPacket(typeClipboard, map[string]string{"content": text}))
		}
	}
}

// greet gives a device that came the clipboard, and when it was copied:
// it keeps whichever is newer.
func (c *clipboardSync) greet(id string) {
	c.mu.Lock()
	text, when := c.last, c.when
	c.mu.Unlock()
	if text == "" {
		return
	}
	c.s.core.Send(id, kdeconnect.NewPacket(typeClipConnect, map[string]any{"content": text, "timestamp": when}))
}

// received puts what a device copied on the clipboard here.
func (c *clipboardSync) received(p kdeconnect.Packet) {
	var b struct {
		Content   string `json:"content"`
		Timestamp int64  `json:"timestamp"`
	}
	if p.Decode(&b) != nil || b.Content == "" || len(b.Content) > maxClip {
		return
	}
	c.mu.Lock()
	if p.Type == typeClipConnect && (b.Timestamp == 0 || b.Timestamp <= c.when) || b.Content == c.last {
		c.mu.Unlock()
		return
	}
	c.last, c.when = b.Content, time.Now().UnixMilli()
	if b.Timestamp != 0 {
		c.when = b.Timestamp
	}
	c.mu.Unlock()
	if err := c.set(b.Content); err != nil {
		log.Printf("connect: wl-copy: %v", err)
	}
}
