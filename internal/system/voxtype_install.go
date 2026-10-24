package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// VoxtypeVersion is the voxtype release InstallVoxtype puts in place. Its
// binaries' checksums are carried here, from the release's SHA256SUMS.txt
// (and GitHub's own digests): a new release means new sums.
const VoxtypeVersion = "1.1.1"

// voxtypeSums are the release's Linux binaries that InstallVoxtype may
// choose, by variant, with their SHA-256. The ONNX builds have every
// engine (Whisper, SenseVoice, Paraformer, Parakeet, Moonshine …) and need
// nothing more; a processor without AVX2 has the Whisper-only baseline.
var voxtypeSums = map[string]string{
	"x86_64-onnx-avx512": "3f60acd56ed7360dc42ba330119b7cb18da4825aabaef883c0fcd5f7ecf5660f",
	"x86_64-onnx-avx2":   "683683ef66de6616e415efa7f7893873d7f8d5bfe4b2b05ce3cf3e467d4733d5",
	"x86_64-baseline":    "cc051aac0870e21d7378edbcde3ceff10238af12a11650ab897d0ad9195225af",
	"aarch64-onnx":       "e038613888a1d05164b01d4a992666ab08967f415f72e3adb0cea033166d1418",
}

// voxtypeReleases is where the release's files are; a test serves its own.
var voxtypeReleases = "https://github.com/peteonrails/voxtype/releases/download"

// voxtypeVariant is the build for this machine: with every engine, for
// the widest vector instructions the processor has; "" when there is none
// for it.
func voxtypeVariant() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64-onnx"
	case "amd64":
		flags := cpuFlags()
		switch {
		case flags["avx512f"]:
			return "x86_64-onnx-avx512"
		case flags["avx2"]:
			return "x86_64-onnx-avx2"
		}
		return "x86_64-baseline"
	}
	return ""
}

// cpuFlags are the first processor's flags in /proc/cpuinfo.
func cpuFlags() map[string]bool {
	data, _ := os.ReadFile("/proc/cpuinfo")
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "flags" {
			flags := map[string]bool{}
			for _, f := range strings.Fields(v) {
				flags[f] = true
			}
			return flags
		}
	}
	return nil
}

// voxtypeInstallPath is where InstallVoxtype puts the program.
func voxtypeInstallPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "voxtype")
}

// VoxtypeStep is a step of InstallVoxtype, for the page to say.
type VoxtypeStep int

const (
	VoxtypeDownloading      VoxtypeStep = iota // the program; Done is the share so far
	VoxtypeModelDownloading                    // its speech model; done as for the program
	VoxtypeService                             // its user service, started
)

// InstallVoxtype downloads voxtype's release for this machine into
// ~/.local/bin, refuses it unless its checksum is the one carried here,
// then has it download a model for lang (VoxtypeStartModel) and use it,
// and sets up and starts its user service. progress hears each step, with
// its share done (< 0 not known), from the goroutine it runs on.
func InstallVoxtype(ctx context.Context, lang string, progress func(step VoxtypeStep, done float64)) error {
	variant := voxtypeVariant()
	sum, ok := voxtypeSums[variant]
	if !ok {
		return fmt.Errorf("no voxtype build for %s", runtime.GOARCH)
	}
	dest := voxtypeInstallPath()
	if dest == "" {
		return errors.New("no home directory")
	}
	name := fmt.Sprintf("voxtype-%s-linux-%s", VoxtypeVersion, variant)
	url := fmt.Sprintf("%s/v%s/%s", voxtypeReleases, VoxtypeVersion, name)
	if err := fetchChecked(ctx, url, dest, sum, func(f float64) { progress(VoxtypeDownloading, f) }); err != nil {
		return err
	}
	progress(VoxtypeModelDownloading, -1)
	_, arg := VoxtypeStartModel(lang, strings.Contains(variant, "onnx"))
	if err := downloadModel(ctx, dest, arg, func(_ string, pct float64) {
		progress(VoxtypeModelDownloading, max(pct, -100)/100)
	}); err != nil {
		return fmt.Errorf("the model: %w", err)
	}
	progress(VoxtypeService, 0)
	if err := runStep(ctx, dest, "setup", "systemd"); err != nil {
		return fmt.Errorf("the service: %w", err)
	}
	if err := runStep(ctx, "systemctl", "--user", "enable", "--now", "voxtype.service"); err != nil {
		return fmt.Errorf("the service: %w", err)
	}
	return nil
}

// fetchChecked fetches url into dest, through a file beside it, which takes
// dest's place only when its SHA-256 is sum.
func fetchChecked(ctx context.Context, url, dest, sum string, done func(float64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	part, err := os.CreateTemp(filepath.Dir(dest), ".voxtype-*")
	if err != nil {
		return err
	}
	defer os.Remove(part.Name()) // after the rename, nothing
	h := sha256.New()
	var got int64
	buf := make([]byte, 256<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := part.Write(buf[:n]); werr != nil {
				part.Close()
				return werr
			}
			h.Write(buf[:n])
			got += int64(n)
			if resp.ContentLength > 0 {
				done(float64(got) / float64(resp.ContentLength))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			part.Close()
			return err
		}
	}
	if err := part.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return errors.New("the download is not the voxtype release it should be (its checksum differs); nothing was installed")
	}
	if err := os.Chmod(part.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(part.Name(), dest)
}

// runStep runs a step, with its own words as the error when it fails.
func runStep(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if msg := lastLine(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// lastLine is the last line of a command's output that says something.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
