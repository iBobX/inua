package web

import (
	"cmp"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/lyricat/inua/internal/apps/sysmon"
)

// sysInfo is what the top of the window's first column says of this
// computer: who and where, the shell, how busy the CPU is, the memory in
// use and the uptime.
type sysInfo struct {
	Who    string  `json:"who"`
	Shell  string  `json:"shell"`
	CPU    float64 `json:"cpu"`    // 0 to 100, -1 until read twice
	Memory float64 `json:"memory"` // 0 to 100, -1 when unknown
	Uptime float64 `json:"uptime"` // seconds
}

// sampler reads the CPU every two seconds, from the first time it is
// asked: its share busy is between two readings.
type sampler struct {
	mu  sync.Mutex
	cpu float64
}

const sampleEvery = 2 * time.Second

func newSampler() *sampler {
	s := &sampler{cpu: -1}
	go func() {
		prev, _ := sysmon.ParseCPU(sysmon.ReadFile("proc/stat"))
		for range time.Tick(sampleEvery) {
			cur, _ := sysmon.ParseCPU(sysmon.ReadFile("proc/stat"))
			if cur.Total > prev.Total {
				s.mu.Lock()
				s.cpu = sysmon.Usage(prev, cur)
				s.mu.Unlock()
			}
			prev = cur
		}
	}()
	return s
}

func (s *sampler) read() sysInfo {
	name := os.Getenv("USER")
	if u, err := user.Current(); name == "" && err == nil {
		name = u.Username
	}
	hostname, _ := os.Hostname()
	_, set := settingsNow()
	info := sysInfo{
		Who:    cmp.Or(name, "?") + "@" + cmp.Or(hostname, "localhost"),
		Shell:  filepath.Base(cmp.Or(set.Shell.Program, os.Getenv("SHELL"), "sh")),
		Memory: -1,
		Uptime: uptime().Seconds(),
	}
	s.mu.Lock()
	info.CPU = s.cpu
	s.mu.Unlock()
	if m := sysmon.ParseMemory(sysmon.ReadFile("proc/meminfo")); m.Total > 0 {
		info.Memory = 100 * float64(m.Used()) / float64(m.Total)
	}
	return info
}
