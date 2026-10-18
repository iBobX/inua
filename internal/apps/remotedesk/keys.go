package remotedesk

// VirtualKey is the Windows virtual-key code GameStream sends for a key,
// from its evdev code (a Wayland key's hardware keycode less 8): the key's
// place, not its letter, so the machine's own layout gives it its
// meaning. 0 is a key with no code.
func VirtualKey(evdev uint32) uint16 {
	if evdev < uint32(len(vkByEvdev)) {
		return vkByEvdev[evdev]
	}
	return 0
}

var vkByEvdev = func() [256]uint16 {
	var t [256]uint16
	set := func(evdev uint32, vk uint16) { t[evdev] = vk }
	set(1, 0x1B) // Esc
	for i, vk := range []uint16{0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x30} {
		set(uint32(2+i), vk) // 1 … 0
	}
	set(12, 0xBD) // -
	set(13, 0xBB) // =
	set(14, 0x08) // Backspace
	set(15, 0x09) // Tab
	for i, vk := range []uint16{'Q', 'W', 'E', 'R', 'T', 'Y', 'U', 'I', 'O', 'P'} {
		set(uint32(16+i), vk)
	}
	set(26, 0xDB) // [
	set(27, 0xDD) // ]
	set(28, 0x0D) // Enter
	set(29, 0xA2) // left Ctrl
	for i, vk := range []uint16{'A', 'S', 'D', 'F', 'G', 'H', 'J', 'K', 'L'} {
		set(uint32(30+i), vk)
	}
	set(39, 0xBA) // ;
	set(40, 0xDE) // '
	set(41, 0xC0) // `
	set(42, 0xA0) // left Shift
	set(43, 0xDC) // \
	for i, vk := range []uint16{'Z', 'X', 'C', 'V', 'B', 'N', 'M'} {
		set(uint32(44+i), vk)
	}
	set(51, 0xBC) // ,
	set(52, 0xBE) // .
	set(53, 0xBF) // /
	set(54, 0xA1) // right Shift
	set(55, 0x6A) // keypad *
	set(56, 0xA4) // left Alt
	set(57, 0x20) // Space
	set(58, 0x14) // Caps Lock
	for i := uint32(0); i < 10; i++ {
		set(59+i, uint16(0x70+i)) // F1 … F10
	}
	set(69, 0x90) // Num Lock
	set(70, 0x91) // Scroll Lock
	for evdev, vk := range map[uint32]uint16{71: 0x67, 72: 0x68, 73: 0x69, 75: 0x64, 76: 0x65, 77: 0x66, 79: 0x61, 80: 0x62, 81: 0x63, 82: 0x60} {
		set(evdev, vk) // keypad digits
	}
	set(74, 0x6D)  // keypad -
	set(78, 0x6B)  // keypad +
	set(83, 0x6E)  // keypad .
	set(86, 0xE2)  // the key beside left Shift on ISO keyboards
	set(87, 0x7A)  // F11
	set(88, 0x7B)  // F12
	set(96, 0x0D)  // keypad Enter
	set(97, 0xA3)  // right Ctrl
	set(98, 0x6F)  // keypad /
	set(99, 0x2C)  // Print Screen
	set(100, 0xA5) // right Alt
	set(102, 0x24) // Home
	set(103, 0x26) // Up
	set(104, 0x21) // Page Up
	set(105, 0x25) // Left
	set(106, 0x27) // Right
	set(107, 0x23) // End
	set(108, 0x28) // Down
	set(109, 0x22) // Page Down
	set(110, 0x2D) // Insert
	set(111, 0x2E) // Delete
	set(113, 0xAD) // Mute
	set(114, 0xAE) // Volume down
	set(115, 0xAF) // Volume up
	set(119, 0x13) // Pause
	set(125, 0x5B) // left Super
	set(126, 0x5C) // right Super
	set(127, 0x5D) // Menu
	set(163, 0xB0) // next track
	set(164, 0xB3) // play/pause
	set(165, 0xB1) // previous track
	set(166, 0xB2) // stop
	for i := uint32(0); i < 12; i++ {
		set(183+i, uint16(0x7C+i)) // F13 … F24
	}
	return t
}()

// StreamPoint is where a pointer at x, y in a w×h view falls on a
// streamW×streamH picture shown in it whole and centred (bars at the
// sides or above and below): its pixel, and whether it is on the picture
// at all.
func StreamPoint(x, y, w, h float64, streamW, streamH int) (int, int, bool) {
	if w <= 0 || h <= 0 || streamW <= 0 || streamH <= 0 {
		return 0, 0, false
	}
	scale := min(w/float64(streamW), h/float64(streamH))
	pw, ph := float64(streamW)*scale, float64(streamH)*scale
	ox, oy := (w-pw)/2, (h-ph)/2
	sx, sy := (x-ox)/scale, (y-oy)/scale
	inside := sx >= 0 && sy >= 0 && sx < float64(streamW) && sy < float64(streamH)
	clamp := func(v float64, n int) int { return int(max(0, min(v, float64(n-1)))) }
	return clamp(sx, streamW), clamp(sy, streamH), inside
}

// ScrollAmount is a scroll as GameStream sends it, 120 a notch, up and
// right positive: from a wheel's notches (wheel), or a touchpad's pixels.
func ScrollAmount(delta float64, wheel bool) int {
	if wheel {
		return int(-delta * 120)
	}
	return int(-delta * 120 / 40) // forty pixels make a notch
}
