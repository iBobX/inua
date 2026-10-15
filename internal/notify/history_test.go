package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The history keeps the last HistoryKeep, reads newest first, survives a
// bad line, and clears.
func TestHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inua", "notification-history.jsonl")
	if got, err := ReadHistory(path); err != nil || len(got) != 0 {
		t.Fatalf("none yet: %v %v", got, err)
	}
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for i := range HistoryKeep + 3 {
		if err := AppendHistory(path, Record{ID: uint32(i + 1), App: "Mail", Summary: fmt.Sprint("note ", i), Time: start.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadHistory(path)
	if err != nil || len(got) != HistoryKeep || got[0].ID != HistoryKeep+3 || got[len(got)-1].ID != 4 {
		t.Fatalf("%d records, first %+v, last %+v, %v", len(got), got[0], got[len(got)-1], err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("not json\n")
	f.Close()
	if got, _ := ReadHistory(path); len(got) != HistoryKeep {
		t.Errorf("a bad line: %d records", len(got))
	}

	if err := ClearHistory(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadHistory(path); len(got) != 0 {
		t.Errorf("cleared: %d records", len(got))
	}
	if err := ClearHistory(filepath.Join(t.TempDir(), "none.jsonl")); err != nil {
		t.Errorf("clearing none: %v", err)
	}
}
