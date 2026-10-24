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
// choose, by variant, with their SHA-256.
var voxtypeSums = map[string]string{
	"x86_64-avx512":   "6a23ccf9da9e6c6eaff4b23086cea4f97cab53b12526ec88200344d597dea305",
	"x86_64-avx2":     "3462262980e5a75391ffdeb7d3b63f9f638d6261df042f04f9119dfb7aab7770",
	"x86_64-baseline": "cc051aac0870e21d7378edbcde3ceff10238af12a11650ab897d0ad9195225af",
	"aarch64-cpu":     "f29bdbf6c3c2535ebe8be3abb6ded7d6c0ac9b16392ea3b5ba231e06a7b05687",
}

// voxtypeReleases is where the release's files are; a test serves its own.
var voxtypeReleases = "https://github.com/peteonrails/voxtype/releases/download"

// voxtypeVariant is the build for this machine: the widest vector
// instructions the processor has, or "" when there is none for it.
func voxtypeVariant() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64-cpu"
	case "amd64":
		flags := cpuFlags()
		switch {
		case flags["avx512f"]:
			return "x86_64-avx512"
		case flags["avx2"]:
			return "x86_64-avx2"
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
	VoxtypeDownloading VoxtypeStep = iota // the program; Done is the share so far
	VoxtypeModel                          // its speech model
	VoxtypeService                        // its user service, started
)

// InstallVoxtype downloads voxtype's release for this machine into
// ~/.local/bin, refuses it unless its checksum is the one carried here,
// then has it download its default model and set up and start its user
// service. progress hears each step, from the goroutine it runs on.
func InstallVoxtype(ctx context.Context, progress func(step VoxtypeStep, done float64)) error {
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
	progress(VoxtypeModel, 0)
	if err := runStep(ctx, dest, "setup", "--download"); err != nil {
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
