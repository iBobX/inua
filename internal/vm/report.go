package vm

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Report is what the diagnostics answer: what this host provides for a guest,
// what the configuration says and what Inua would allocate. `inua vm
// doctor|status` and `inua-vm --doctor|--status` print it; nothing here
// changes Docker or the host: Docker is only asked for its version.
type Report struct {
	Platform    Platform `json:"platform"`
	Host        Host     `json:"host"`
	Recommended Profile  `json:"recommended_profile"`
	Config      Config   `json:"config"`
	ConfigPath  string   `json:"config_path"`
	ConfigValid bool     `json:"config_valid"`
	ConfigError string   `json:"config_error,omitempty"`
}

// Diagnose reads the host's facilities and the saved configuration. A host
// with no configuration file yet is not an error: the defaults are reported,
// so the checks can be run before anything is installed. Root maps absolute
// paths into a fixture root, for tests.
func Diagnose(root string) (Report, error) {
	if root == "" {
		root = "/"
	}
	host := DetectHost(root)
	platform := DetectPlatform(root, PlatformProbes{})
	cfg := DefaultConfig()
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		saved, err := ParsePersisted(b)
		if err != nil {
			return Report{}, fmt.Errorf("read VM config %s: %w", ConfigPath(), err)
		}
		cfg = saved.Config
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Report{}, fmt.Errorf("read VM config %s: %w", ConfigPath(), err)
	}
	r := Report{Platform: platform, Host: host, Recommended: RecommendProfile(host), Config: cfg, ConfigPath: ConfigPath()}
	if err := cfg.Validate(); err != nil {
		r.ConfigError = err.Error()
	} else {
		r.ConfigValid = true
	}
	return r, nil
}

// Text is the report as both commands print it: one line per check, in the
// order the window shows them.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Windows VM host: %s\n", r.Platform.OS)
	if r.Platform.Distribution != "" {
		fmt.Fprintf(&b, "Distribution: %s\n", r.Platform.Distribution)
	}
	fmt.Fprintf(&b, "Supported: %t\n", r.Platform.Supported)
	for _, item := range r.Platform.Doctor {
		if item.OK {
			fmt.Fprintf(&b, "OK: %s\n", item.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL: %s (%s)\n", item.Name, item.Reason)
	}
	fmt.Fprintf(&b, "Recommended resources: %d MiB RAM, %d CPUs, %d GiB disk\n",
		r.Recommended.MemoryMiB, r.Recommended.CPUs, r.Recommended.DiskGiB)
	fmt.Fprintf(&b, "VM config: %s\n", r.ConfigPath)
	if r.ConfigValid {
		fmt.Fprintf(&b, "Config valid: %s, %d MiB RAM, %d CPUs, %d GiB disk\n",
			r.Config.Name, r.Config.MemoryMiB, r.Config.CPUs, r.Config.DiskGiB)
	} else {
		fmt.Fprintf(&b, "Config invalid: %s\n", r.ConfigError)
	}
	return b.String()
}
