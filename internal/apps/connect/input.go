package connect

import (
	"log"
	"sync"

	"github.com/lyricat/inua/internal/kdeconnect"
	"github.com/lyricat/inua/internal/vinput"
)

// specialKeys are the keys a device names by number, as their keysyms.
var specialKeys = map[int]string{
	1: "BackSpace", 2: "Tab", 3: "Linefeed", 4: "Left", 5: "Up", 6: "Right", 7: "Down", 8: "Page_Up", 9: "Page_Down",
	10: "Home", 11: "End", 12: "Return", 13: "Delete", 14: "Escape", 15: "Sys_Req", 16: "Scroll_Lock",
	17: "Control_L", 18: "Alt_L", 19: "Shift_L", 20: "Super_L",
	21: "F1", 22: "F2", 23: "F3", 24: "F4", 25: "F5", 26: "F6", 27: "F7", 28: "F8", 29: "F9", 30: "F10", 31: "F11", 32: "F12",
}

// remoteInput is a device as a touchpad and keyboard: a virtual pointer
// and keyboard, made when first used.
type remoteInput struct {
	mu sync.Mutex
	in *vinput.Input
}

func newRemoteInput(*Service) *remoteInput { return &remoteInput{} }

// mouseBody is a mousepad packet's body.
type mouseBody struct {
	DX, DY        float64
	SingleClick   bool `json:"singleclick"`
	DoubleClick   bool `json:"doubleclick"`
	MiddleClick   bool `json:"middleclick"`
	RightClick    bool `json:"rightclick"`
	SingleHold    bool `json:"singlehold"`
	SingleRelease bool `json:"singlerelease"`
	Scroll        bool `json:"scroll"`
	Key           string
	SpecialKey    int `json:"specialKey"`
	Ctrl, Alt     bool
	Shift, Super  bool
}

func (r *remoteInput) input() (*vinput.Input, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.in != nil && r.in.Err() == nil {
		return r.in, nil
	}
	if r.in != nil {
		r.in.Close()
		r.in = nil
	}
	in, err := vinput.Open()
	if err != nil {
		return nil, err
	}
	r.in = in
	return in, nil
}

func (r *remoteInput) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.in != nil {
		r.in.Close()
		r.in = nil
	}
}

func (r *remoteInput) packet(p kdeconnect.Packet) {
	var b mouseBody
	if p.Decode(&b) != nil {
		return
	}
	in, err := r.input()
	if err != nil {
		log.Printf("connect: remote input: %v", err)
		return
	}
	mods := vinput.Modifiers{Ctrl: b.Ctrl, Alt: b.Alt, Shift: b.Shift, Super: b.Super}
	switch {
	case b.SingleClick:
		err = in.Click(vinput.ButtonLeft, 1)
	case b.DoubleClick:
		err = in.Click(vinput.ButtonLeft, 2)
	case b.MiddleClick:
		err = in.Click(vinput.ButtonMiddle, 1)
	case b.RightClick:
		err = in.Click(vinput.ButtonRight, 1)
	case b.SingleHold:
		err = in.Button(vinput.ButtonLeft, true)
	case b.SingleRelease:
		err = in.Button(vinput.ButtonLeft, false)
	case b.Scroll:
		err = in.Scroll(b.DX, b.DY)
	case specialKeys[b.SpecialKey] != "":
		err = in.Key(specialKeys[b.SpecialKey], mods)
	case b.Key != "":
		err = in.Type(b.Key, mods)
	case b.DX != 0 || b.DY != 0:
		err = in.Move(b.DX, b.DY)
	}
	if err != nil {
		log.Printf("connect: remote input: %v", err)
		r.close()
	}
}
