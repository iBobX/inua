package clipboard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTextCapturePreservesPayloadAndDeduplicates(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "clipboard-history.json")
	text := " 中文\n$(never execute) `nor this` 100%\n"
	for range 2 {
		if err := Capture(path, DefaultLimit, strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Read(path)
	if err != nil || len(entries) != 1 || entries[0].Text != text || entries[0].Time.IsZero() {
		t.Fatalf("capture did not preserve and deduplicate text: %+v %v", entries, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("history must be private")
	}
}

func TestSensitiveClipboardIsNotStored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	for _, state := range []string{"sensitive", "nil", "clear"} {
		t.Setenv("CLIPBOARD_STATE", state)
		if err := Capture(path, DefaultLimit, strings.NewReader("do not store")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("sensitive selection created history")
	}
}

// What is on the clipboard when recording starts is not kept; what is
// copied after is.
func TestSkipFirst(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "inua", "history.json")
	if err := SkipFirst(path); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"copied while off", "copied after"} {
		if err := Capture(path, DefaultLimit, strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := Read(path); len(entries) != 1 || entries[0].Text != "copied after" {
		t.Errorf("%+v", entries)
	}
}

func TestImageCapture(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "history.json")
	data := []byte("\x89PNG\r\n\x1a\nimage payload")
	if err := Capture(path, DefaultLimit, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	entries, err := Read(path)
	if err != nil || len(entries) != 1 || entries[0].MIME != "image/png" {
		t.Fatal("image was not classified correctly", err)
	}
	got, err := os.ReadFile(entries[0].Path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("image bytes changed", err)
	}
	if Size(path) < int64(len(data)) {
		t.Errorf("size %d leaves the image out", Size(path))
	}
}

// Text over MaxText is not kept: the history is one file, rewritten on
// every copy. Text that is not UTF-8 is not kept either.
func TestCaptureLimits(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "history.json")
	for _, data := range [][]byte{[]byte(strings.Repeat("x", MaxText+1)), {0xff, 0xfe, 'a'}, nil} {
		if err := Capture(path, DefaultLimit, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Capture(path, DefaultLimit, strings.NewReader(strings.Repeat("y", MaxText))); err != nil {
		t.Fatal(err)
	}
	if entries, _ := Read(path); len(entries) != 1 || len(entries[0].Text) != MaxText {
		t.Errorf("kept %d entries", len(entries))
	}
}

// The history keeps the newest limit entries; images that fall out, are
// deleted or cleared lose their files, and only those.
func TestLimitAndImageFiles(t *testing.T) {
	t.Setenv("CLIPBOARD_STATE", "data")
	path := filepath.Join(t.TempDir(), "history.json")
	png := func(n int) []byte { return fmt.Appendf([]byte("\x89PNG\r\n\x1a\n"), "image %d", n) }
	for i := range 3 {
		if err := Capture(path, 2, bytes.NewReader(png(i))); err != nil {
			t.Fatal(err)
		}
	}
	files := func() int {
		f, _ := os.ReadDir(imageDir(path))
		return len(f)
	}
	entries, _ := Read(path)
	if len(entries) != 2 || files() != 2 {
		t.Fatalf("limit 2: %d entries, %d image files", len(entries), files())
	}
	if err := Delete(path, entries[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entries[1].Path); !os.IsNotExist(err) || files() != 1 {
		t.Errorf("a deleted image keeps its file: %v, %d files", err, files())
	}
	if err := Clear(path); err != nil {
		t.Fatal(err)
	}
	if entries, _ := Read(path); len(entries) != 0 || files() != 0 {
		t.Errorf("clear left %d entries, %d image files", len(entries), files())
	}
}

// A history written before entries had a time still reads.
func TestReadOldHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	old := `[{"type": "text", "text": "a"}, {"type": "image", "mime": "image/png", "path": "/x.png", "capturedAt": "Monday 15:04"}]`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if entries, err := Read(path); err != nil || len(entries) != 2 || entries[1].Path != "/x.png" || !entries[0].Time.IsZero() {
		t.Errorf("%+v %v", entries, err)
	}
}

func TestConcurrentUpdatesAndDeleteByIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Add(path, DefaultLimit, Entry{Type: "text", Text: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	entries, err := Read(path)
	if err != nil || len(entries) != 30 {
		t.Fatal("concurrent writers lost history", err)
	}
	selected := entries[10]
	if err := Add(path, DefaultLimit, Entry{Type: "text", Text: "new selection"}); err != nil {
		t.Fatal(err)
	}
	if err := Delete(path, selected); err != nil {
		t.Fatal(err)
	}
	entries, err = Read(path)
	if err != nil || len(entries) != 30 {
		t.Fatal("delete removed incorrect number of items", err)
	}
	for _, e := range entries {
		if same(e, selected) {
			t.Fatal("stale selection was not deleted")
		}
	}
}

func TestHistoryPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	if got := HistoryPath(); got != filepath.Join(root, "inua", "clipboard-history.json") {
		t.Errorf("got %s", got)
	}
}
