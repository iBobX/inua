package system

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// VoxtypeModel is a speech model voxtype can download for one of its
// engines: Name as voxtype lists it, Arg as its download and its config
// name it ("sensevoice-small" for SenseVoice's "small").
type VoxtypeModel struct {
	Engine, Name, Arg string
	Installed         bool
}

// VoxtypeCatalog is what the program at hand can do: the engines compiled
// into it, in its order, their models, and the engine and model in use.
type VoxtypeCatalog struct {
	Engines       []string
	Models        map[string][]VoxtypeModel
	Engine, Model string // in use (Model is an Arg)
	// More is set when this is a build with Whisper alone and the release
	// has one with every engine for this processor (UpgradeVoxtype).
	More bool
}

// VoxtypeModels asks voxtype for its catalog.
func VoxtypeModels(ctx context.Context) (VoxtypeCatalog, error) {
	var c VoxtypeCatalog
	bin := VoxtypePath()
	if bin == "" {
		return c, errors.New("voxtype is not installed")
	}
	out, err := exec.CommandContext(ctx, bin, "info", "models", "--json").Output()
	if err != nil {
		return c, fmt.Errorf("voxtype info models: %w", err)
	}
	var raw struct {
		Engines map[string]struct {
			Models []struct {
				Name        string `json:"name"`
				DownloadArg string `json:"download_arg"`
				Installed   bool   `json:"installed"`
			}
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return c, err
	}
	c.Models = map[string][]VoxtypeModel{}
	for engine, e := range raw.Engines {
		for _, m := range e.Models {
			c.Models[engine] = append(c.Models[engine], VoxtypeModel{Engine: engine, Name: m.Name, Arg: m.DownloadArg, Installed: m.Installed})
		}
	}
	if out, err = exec.CommandContext(ctx, bin, "info", "engines").Output(); err != nil {
		return c, fmt.Errorf("voxtype info engines: %w", err)
	}
	c.Engines = compiledEngines(string(out))
	if out, err := exec.CommandContext(ctx, bin, "config", "get", "engine").Output(); err == nil {
		c.Engine = strings.TrimSpace(string(out))
	}
	if out, err := exec.CommandContext(ctx, bin, "config", "get", c.Engine+".model").Output(); err == nil && c.Engine != "" {
		c.Model = strings.TrimSpace(string(out))
	}
	_, onnx := voxtypeSums[voxtypeVariant()]
	c.More = onnx && strings.Contains(voxtypeVariant(), "onnx") && len(c.Engines) == 1
	return c, nil
}

// compiledEngines reads `voxtype info engines`: a line for each engine,
// marked "compiled" when this build has it.
func compiledEngines(out string) []string {
	var engines []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "compiled" && !slices.Contains(engines, f[1]) {
			engines = append(engines, f[1])
		}
	}
	return engines
}

// UseVoxtypeModel downloads m if it is not there, makes it the one voxtype
// uses, and restarts the daemon to load it. progress hears each file's
// share as it downloads (pct < 0 when its size is not known), from the
// goroutine it runs on.
func UseVoxtypeModel(ctx context.Context, m VoxtypeModel, progress func(file string, pct float64)) error {
	bin := VoxtypePath()
	if bin == "" {
		return errors.New("voxtype is not installed")
	}
	if err := downloadModel(ctx, bin, m.Arg, progress); err != nil {
		return err
	}
	return runStep(ctx, "systemctl", "--user", "restart", "voxtype.service")
}

// downloadModel runs voxtype's download with --activate, reading its
// progress events (NDJSON, src/setup/progress.rs): "progress" with the
// file, its bytes and pct (null when the size is not known), then "done"
// or "error" with a message.
func downloadModel(ctx context.Context, bin, arg string, progress func(file string, pct float64)) error {
	cmd := exec.CommandContext(ctx, bin, "setup", "--download", "--model", arg, "--activate", "--progress-format", "json")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var failed string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		var ev struct {
			Event, File, Message string
			Pct                  *float64
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "progress":
			pct := -1.0
			if ev.Pct != nil {
				pct = *ev.Pct
			}
			progress(ev.File, pct)
		case "error":
			failed = ev.Message
		}
	}
	err = cmd.Wait()
	switch {
	case failed != "":
		return errors.New(failed)
	case err != nil:
		if msg := lastLine(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// UpgradeVoxtype puts voxtype's build with every engine in place of one
// with Whisper alone (VoxtypeCatalog.More), checked as InstallVoxtype
// checks it, and restarts the daemon on it. The models stay.
func UpgradeVoxtype(ctx context.Context, done func(float64)) error {
	variant := voxtypeVariant()
	sum, ok := voxtypeSums[variant]
	if !ok || !strings.Contains(variant, "onnx") {
		return fmt.Errorf("no voxtype build with every engine for %s", variant)
	}
	dest := voxtypeInstallPath()
	if dest == "" {
		return errors.New("no home directory")
	}
	url := fmt.Sprintf("%s/v%s/voxtype-%s-linux-%s", voxtypeReleases, VoxtypeVersion, VoxtypeVersion, variant)
	if err := fetchChecked(ctx, url, dest, sum, done); err != nil {
		return err
	}
	return runStep(ctx, "systemctl", "--user", "restart", "voxtype.service")
}

// VoxtypeStartModel is the model InstallVoxtype starts with for a language
// (i18n.Lang: "zh_CN", "ja" …; "" for English), as an engine and its Arg:
// SenseVoice for Chinese, Japanese and Korean when the build has it,
// Whisper's multilingual base otherwise, its English one for English.
func VoxtypeStartModel(lang string, onnx bool) (engine, arg string) {
	switch l, _, _ := strings.Cut(lang, "_"); {
	case onnx && (l == "zh" || l == "ja" || l == "ko"):
		return "sensevoice", "sensevoice-small"
	case l == "" || l == "en":
		return "whisper", "base.en"
	}
	return "whisper", "base"
}
