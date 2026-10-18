package gamestream

import (
	"encoding/binary"
	"unicode/utf8"
)

// Input packets: a header (the packet's size past its first four bytes,
// big-endian, then its kind, little-endian), then the event; mouse
// coordinates and scrolls are big-endian, key codes little-endian.
const (
	magicKeyDown    = 0x03
	magicKeyUp      = 0x04
	magicMouseAbs   = 0x05
	magicMouseRel   = 0x07
	magicButtonDown = 0x08
	magicButtonUp   = 0x09
	magicScroll     = 0x0A
	magicHScroll    = 0x55000001
	magicText       = 0x17
	maxTextBytes    = 32
	WheelDelta      = 120 // one notch of a mouse wheel
	keyCodeHighByte = 0x80 << 8
)

// Modifiers held with a key.
const (
	ModShift = 0x01
	ModCtrl  = 0x02
	ModAlt   = 0x04
	ModMeta  = 0x08
)

// Mouse buttons.
const (
	ButtonLeft   = 1
	ButtonMiddle = 2
	ButtonRight  = 3
	ButtonX1     = 4
	ButtonX2     = 5
)

func inputPacket(magic uint32, body []byte) []byte {
	p := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(p, uint32(4+len(body)))
	binary.LittleEndian.PutUint32(p[4:], magic)
	return append(p, body...)
}

// keyPacket is a key going down or up: vk is its Windows virtual-key code.
func keyPacket(vk uint16, down bool, modifiers uint8) []byte {
	magic := uint32(magicKeyUp)
	if down {
		magic = magicKeyDown
	}
	body := make([]byte, 6)
	body[0] = 0 // flags
	binary.LittleEndian.PutUint16(body[1:], keyCodeHighByte|vk)
	body[3] = modifiers
	return inputPacket(magic, body)
}

// mouseAbsPacket puts the pointer at x, y in a w×h reference area (the
// picture as the client shows it).
func mouseAbsPacket(x, y, w, h int) []byte {
	body := make([]byte, 10)
	binary.BigEndian.PutUint16(body, uint16(int16(x)))
	binary.BigEndian.PutUint16(body[2:], uint16(int16(y)))
	binary.BigEndian.PutUint16(body[6:], uint16(int16(w-1)))
	binary.BigEndian.PutUint16(body[8:], uint16(int16(h-1)))
	return inputPacket(magicMouseAbs, body)
}

// mouseRelPacket moves the pointer by dx, dy.
func mouseRelPacket(dx, dy int16) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint16(body, uint16(dx))
	binary.BigEndian.PutUint16(body[2:], uint16(dy))
	return inputPacket(magicMouseRel, body)
}

func buttonPacket(button uint8, down bool) []byte {
	magic := uint32(magicButtonUp)
	if down {
		magic = magicButtonDown
	}
	return inputPacket(magic, []byte{button})
}

// scrollPacket scrolls by amount, WheelDelta a notch, up positive.
func scrollPacket(amount int16) []byte {
	body := make([]byte, 6)
	binary.BigEndian.PutUint16(body, uint16(amount))
	binary.BigEndian.PutUint16(body[2:], uint16(amount))
	return inputPacket(magicScroll, body)
}

// hscrollPacket scrolls sideways, right positive.
func hscrollPacket(amount int16) []byte {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, uint16(amount))
	return inputPacket(magicHScroll, body)
}

// textPackets types text as it is (a character no key makes): in pieces
// of at most 32 bytes, never splitting a character.
func textPackets(text string) [][]byte {
	var out [][]byte
	for len(text) > 0 {
		n := min(len(text), maxTextBytes)
		for n > 0 && n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		out = append(out, inputPacket(magicText, []byte(text[:n])))
		text = text[n:]
	}
	return out
}
