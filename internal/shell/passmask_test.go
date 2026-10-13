package shell

import "testing"

func TestPassMask(t *testing.T) {
	var m PassMask
	if m.Hidden() || m.Capture() != NoCapture {
		t.Fatal("hidden with nothing asked")
	}
	m.Toggle()
	if !m.Hidden() || m.Auto() {
		t.Fatal("the user's hiding: want hidden, not auto")
	}
	m.Toggle()
	if m.Hidden() {
		t.Fatal("shown again: want shown")
	}

	// A screencast hides it; the user may show it for that capture only.
	m.Screencast(true)
	if !m.Hidden() || !m.Auto() || m.Capture() != Recording {
		t.Fatalf("screencast: hidden %v auto %v capture %q", m.Hidden(), m.Auto(), m.Capture())
	}
	m.Toggle()
	if m.Hidden() {
		t.Fatal("shown by the user during the screencast")
	}
	m.Screencast(true) // a second one
	m.Screencast(false)
	if m.Hidden() || m.Capture() != Recording {
		t.Fatal("one screencast still runs: stays as the user set it")
	}
	m.Screencast(false)
	m.Screencast(false) // more ends than starts
	if m.Capture() != NoCapture {
		t.Fatal("no screencast left")
	}
	m.Shooting(true)
	if !m.Hidden() || m.Capture() != Screenshot {
		t.Fatal("the next capture hides it again")
	}
	m.Shooting(false)
	m.Recording(true)
	if !m.Hidden() || m.Capture() != Recording {
		t.Fatal("inua's recording hides it")
	}
	m.Recording(false)
	if m.Hidden() {
		t.Fatal("nothing captures: shown")
	}

	// Hidden by the user stays hidden through a capture and after.
	m.Toggle()
	m.Shooting(true)
	m.Shooting(false)
	if !m.Hidden() || m.Auto() {
		t.Fatal("the user's hiding outlasts a capture")
	}
}
