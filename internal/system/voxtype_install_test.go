package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	release := []byte("#!/bin/sh\necho \"voxtype $*\" >>" + calls + "\n" +
		"[ \"$1\" = setup ] && [ \"$2\" = --download ] && echo '{\"event\":\"progress\",\"file\":\"m.bin\",\"bytes\":5,\"total\":10,\"pct\":50}' && echo '{\"event\":\"done\"}'\nexit 0\n")
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
	if err := InstallVoxtype(context.Background(), "", func(VoxtypeStep, float64) {}); err == nil || !strings.Contains(err.Error(), "checksum") {
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
	var modelDone float64
	if err := InstallVoxtype(context.Background(), "zh_CN", func(s VoxtypeStep, done float64) {
		if len(steps) == 0 || steps[len(steps)-1] != s {
			steps = append(steps, s)
		}
		if s == VoxtypeModelDownloading {
			modelDone = max(modelDone, done)
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
	engine, model := VoxtypeStartModel("zh_CN", strings.Contains(voxtypeVariant(), "onnx"))
	want := "voxtype setup --download --model " + model + " --activate --progress-format json\n" +
		"voxtype config set engine " + engine + "\nvoxtype config set " + engine + ".model " + model + "\n"
	if engine == "whisper" {
		want += "voxtype config set whisper.language auto\n"
	}
	want += "voxtype setup systemd\nsystemctl --user enable --now voxtype.service\n"
	if string(got) != want {
		t.Errorf("ran:\n%s\nwant:\n%s", got, want)
	}
	if len(steps) != 3 || steps[0] != VoxtypeDownloading || steps[2] != VoxtypeService {
		t.Errorf("steps %v", steps)
	}
	if modelDone != 0.5 {
		t.Errorf("the model's progress reached %v, want 0.5", modelDone)
	}
}

// A language's first model: SenseVoice for Chinese, Japanese and Korean
// where the build has it; Whisper otherwise, English's own for English.
func TestVoxtypeStartModel(t *testing.T) {
	for _, c := range []struct {
		lang  string
		onnx  bool
		model string
	}{{"zh_CN", true, "sensevoice-small"}, {"ja", true, "sensevoice-small"}, {"zh_TW", false, "base"}, {"", true, "base.en"}, {"fr", true, "base"}} {
		if _, got := VoxtypeStartModel(c.lang, c.onnx); got != c.model {
			t.Errorf("%q onnx=%v: %s, want %s", c.lang, c.onnx, got, c.model)
		}
	}
}

// The engines `voxtype info engines` marks compiled.
func TestCompiledEngines(t *testing.T) {
	out := "Transcription engines\n  compiled  whisper ● active\n            parakeet\n  compiled  sensevoice\n\nSwitch with: voxtype config set engine <NAME>\n"
	if got := compiledEngines(out); !slices.Equal(got, []string{"whisper", "sensevoice"}) {
		t.Errorf("%v", got)
	}
}

// voxtype hands its words to inua, unless the user has a post-process
// command of their own; a set one is not set again.
func TestConfigureVoxtype(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	calls, current := filepath.Join(home, "calls"), filepath.Join(home, "current")
	os.WriteFile(filepath.Join(bin, "voxtype"), []byte("#!/bin/sh\necho \"voxtype $*\" >>"+calls+"\n[ \"$2\" = get ] && cat "+current+"\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >>"+calls+"\n"), 0o755)
	want := VoxtypeHandsOver("/opt/inua's/inua", "default")
	if want != `'/opt/inua'\''s/inua' call -n 'default' voice type -` {
		t.Errorf("command %s", want)
	}
	for _, c := range []struct {
		current string
		changed bool
	}{{"", true}, {"null", true}, {want, false}, {"/home/me/clean.sh", false}, {"'/old/inua' call -n 'default' voice type -", true}} {
		os.WriteFile(current, []byte(c.current+"\n"), 0o644)
		os.Remove(calls)
		changed, err := ConfigureVoxtype(context.Background(), want)
		if err != nil || changed != c.changed {
			t.Errorf("%q: changed %v (%v)", c.current, changed, err)
		}
		got, _ := os.ReadFile(calls)
		if c.changed != strings.Contains(string(got), "systemctl --user restart voxtype.service") {
			t.Errorf("%q: ran\n%s", c.current, got)
		}
	}
}
