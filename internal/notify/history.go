package notify

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// HistoryKeep is how many notifications the history keeps.
const HistoryKeep = 500

// Record is a notification in the history: what came, as plain text.
type Record struct {
	ID      uint32    `json:"id"`
	App     string    `json:"app"`
	Summary string    `json:"summary"`
	Body    string    `json:"body,omitempty"`
	Time    time.Time `json:"time"`
}

// HistoryPath is the history ([notify] history):
// $XDG_STATE_HOME/inua/notification-history.jsonl (~/.local/state/…), a
// notification a line, oldest first.
func HistoryPath() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(root, "inua", "notification-history.jsonl")
}

// ReadHistory is the history at path, newest first; none when there is
// none yet. Lines it cannot read are skipped.
func ReadHistory(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// AppendHistory adds r to the history at path, keeping the last
// HistoryKeep.
func AppendHistory(path string, r Record) error {
	old, err := ReadHistory(path)
	if err != nil {
		return err
	}
	old = old[:min(len(old), HistoryKeep-1)]
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := len(old) - 1; i >= 0; i-- {
		enc.Encode(old[i])
	}
	enc.Encode(r)
	return writeFile(path, buf.Bytes())
}

// ClearHistory empties the history at path.
func ClearHistory(path string) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return writeFile(path, nil)
}

// writeFile replaces path's content at once, readable only by the user.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
