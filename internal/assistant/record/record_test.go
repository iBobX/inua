package record

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakePwCat puts a pw-cat on PATH that writes script's output, then
// waits to be stopped.
func fakePwCat(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pw-cat"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A recording is what pw-cat wrote, as WAV, at most MaxLength.
func TestRecord(t *testing.T) {
	// a tenth of a second of loud samples (0x4000), then silence until stopped
	fakePwCat(t, `head -c 3200 /dev/zero | tr '\000' '\100'; exec sleep 30`)
	r, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); r.Length() < 100*time.Millisecond; {
		if time.Now().After(deadline) {
			t.Fatal("nothing recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Level() < 0.8 || r.Full() {
		t.Errorf("level %v, full %v", r.Level(), r.Full())
	}
	wav, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) != 44+3200 || string(wav[:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " ||
		binary.LittleEndian.Uint32(wav[24:]) != 16000 || binary.LittleEndian.Uint32(wav[40:]) != 3200 {
		t.Errorf("wav header % x", wav[:44])
	}
	if !bytes.Equal(wav[44:], bytes.Repeat([]byte{0x40}, 3200)) {
		t.Error("samples changed")
	}
}

// pw-cat failing before it records anything says why.
func TestRecordFails(t *testing.T) {
	fakePwCat(t, `echo "no source" >&2; exit 1`)
	r, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); !r.Full(); {
		if time.Now().After(deadline) {
			t.Fatal("pw-cat's end not seen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := r.Stop(); err == nil || err.Error() != "pw-cat: no source" {
		t.Errorf("err %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Error("pw-cat found on an empty PATH")
	}
	if _, err := Start(); err != ErrNoPwCat {
		t.Errorf("without pw-cat: %v", err)
	}
}

func TestLoudness(t *testing.T) {
	if l := loudness(make([]byte, 320)); l != 0 {
		t.Errorf("silence %v", l)
	}
	quiet := make([]byte, 320)
	for i := 0; i < len(quiet); i += 2 {
		binary.LittleEndian.PutUint16(quiet[i:], 33) // about -60 dB
	}
	if l := loudness(quiet); l > 0.05 {
		t.Errorf("quiet %v", l)
	}
}
