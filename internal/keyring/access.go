package keyring

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Access is a secret handed to an app: the settings window lists them,
// newest first.
type Access struct {
	Time   time.Time `json:"time"`
	App    string    `json:"app"`    // the program: "zed", "chrome"
	PID    int       `json:"pid"`    // 0 when the bus did not say
	Secret string    `json:"secret"` // the item's label
}

// accessKeep is how many accesses the record keeps; it is trimmed to them
// once it holds twice as many.
const accessKeep = 200

// AccessPath is the record of accesses:
// $XDG_STATE_HOME/inua/keyring-access.jsonl (~/.local/state/…).
func AccessPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(dir, "inua", "keyring-access.jsonl")
}

// RecordAccess adds a to the record at path, one JSON line each.
func RecordAccess(path string, a Access) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(a)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Count(data, []byte{'\n'}) <= 2*accessKeep {
		return err
	}
	lines := bytes.SplitAfter(data, []byte{'\n'})
	lines = lines[len(lines)-1-accessKeep:] // the last is empty, after the final newline
	tmp := path + ".new"
	if err := os.WriteFile(tmp, bytes.Join(lines, nil), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadAccess reads the record at path, newest first; none when there is
// no record yet. Lines it cannot read are left out.
func ReadAccess(path string) ([]Access, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Access
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var a Access
		if json.Unmarshal(sc.Bytes(), &a) == nil && a.App != "" {
			out = append(out, a)
		}
	}
	slices.Reverse(out)
	return out, sc.Err()
}
