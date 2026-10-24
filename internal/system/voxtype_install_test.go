package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// InstallVoxtype puts the release in ~/.local/bin only when its checksum
// is the one carried, then sets up the model and the service.
func TestInstallVoxtype(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	calls := filepath.Join(home, "calls")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	// The "release": a script that notes how it is run.
	release := []byte("#!/bin/sh\necho \"voxtype $*\" >>" + calls + "\n")
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >>"+calls+"\n"), 0o755)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v"+VoxtypeVersion+"/voxtype-"+VoxtypeVersion+"-linux-"+voxtypeVariant()) {
			http.NotFound(w, r)
			return
		}
		w.Write(release)
	}))
	defer srv.Close()
	old, oldSums := voxtypeReleases, voxtypeSums
	defer func() { voxtypeReleases, voxtypeSums = old, oldSums }()
	voxtypeReleases = srv.URL

	// A checksum that differs: refused, nothing in place.
	voxtypeSums = map[string]string{voxtypeVariant(): strings.Repeat("0", 64)}
	if err := InstallVoxtype(context.Background(), func(VoxtypeStep, float64) {}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("wrong sum: %v", err)
	}
	dest := filepath.Join(home, ".local", "bin", "voxtype")
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("installed a file whose checksum differs")
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".local", "bin", ".voxtype-*")); len(left) > 0 {
		t.Errorf("left %v", left)
	}

	sum := sha256.Sum256(release)
	voxtypeSums = map[string]string{voxtypeVariant(): hex.EncodeToString(sum[:])}
	var steps []VoxtypeStep
	if err := InstallVoxtype(context.Background(), func(s VoxtypeStep, _ float64) {
		if len(steps) == 0 || steps[len(steps)-1] != s {
			steps = append(steps, s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dest); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("not installed: %v %v", info, err)
	}
	if VoxtypePath() != dest {
		t.Errorf("VoxtypePath = %q, want %q", VoxtypePath(), dest)
	}
	got, _ := os.ReadFile(calls)
	want := "voxtype setup --download\nvoxtype setup systemd\nsystemctl --user enable --now voxtype.service\n"
	if string(got) != want {
		t.Errorf("ran:\n%s\nwant:\n%s", got, want)
	}
	if len(steps) != 3 || steps[0] != VoxtypeDownloading || steps[2] != VoxtypeService {
		t.Errorf("steps %v", steps)
	}
}
