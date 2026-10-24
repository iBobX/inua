package webterm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeKeyring is a keyring that may be locked (over SSH, a Mac's
// keychain cannot ask for its password).
type fakeKeyring struct {
	token  string
	locked bool
}

func (k *fakeKeyring) load() (string, error) {
	if k.locked {
		return "", errors.New("User interaction is not allowed.")
	}
	return k.token, nil
}

func (k *fakeKeyring) save(t string) error {
	if k.locked {
		return errors.New("User interaction is not allowed.")
	}
	k.token = t
	return nil
}

// The keyring keeps the token; locked, a file of the user's alone does,
// and the link stays the same; renewed with the keyring back, the
// keyring has the new one and the file's old one is gone.
func TestKeptToken(t *testing.T) {
	file := fileStore(filepath.Join(t.TempDir(), "inua", "term-web-token"))
	k := &fakeKeyring{}
	first, err := keptToken(false, k, file)
	if err != nil || len(first) != 32 || k.token != first {
		t.Fatalf("first: %q %v, keyring %q", first, err, k.token)
	}
	if again, _ := keptToken(false, k, file); again != first {
		t.Errorf("the token changed: %q, then %q", first, again)
	}

	locked := &fakeKeyring{locked: true}
	inFile, err := keptToken(false, locked, file)
	if err != nil || len(inFile) != 32 {
		t.Fatalf("locked keyring: %q %v", inFile, err)
	}
	if fi, err := os.Stat(string(file)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the file: %v %v", fi, err)
	}
	if again, _ := keptToken(false, locked, file); again != inFile {
		t.Errorf("the file's token changed: %q, then %q", inFile, again)
	}

	locked.locked = false
	renewed, err := keptToken(true, locked, file)
	if err != nil || renewed == inFile || locked.token != renewed {
		t.Fatalf("renewed: %q %v, keyring %q", renewed, err, locked.token)
	}
	if _, err := os.Stat(string(file)); !os.IsNotExist(err) {
		t.Errorf("the file's old token stayed: %v", err)
	}

	// Kept nowhere (a file where the token's folder goes): an error.
	blocker := filepath.Join(t.TempDir(), "inua")
	os.WriteFile(blocker, nil, 0o600)
	nowhere := fileStore(filepath.Join(blocker, "term-web-token"))
	if _, err := keptToken(true, &fakeKeyring{locked: true}, nowhere); err == nil {
		t.Error("a token kept nowhere was given")
	}
}
