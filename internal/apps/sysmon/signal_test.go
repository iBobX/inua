package sysmon

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDescendantsAndNamed(t *testing.T) {
	ps := []Process{
		{PID: 1, PPID: 0, Name: "systemd"},
		{PID: 10, PPID: 1, Name: "kitty", UID: 1000},
		{PID: 11, PPID: 10, Name: "zsh", UID: 1000},
		{PID: 12, PPID: 11, Name: "vim", UID: 1000},
		{PID: 13, PPID: 10, Name: "zsh", UID: 1000},
		{PID: 20, PPID: 1, Name: "zsh", UID: 0},
	}
	// Deepest first: vim before its shell, both shells before kitty's end.
	if d := Descendants(ps, 10); !slices.Equal(d, []int{12, 11, 13}) {
		t.Errorf("descendants %v", d)
	}
	if d := Descendants(ps, 12); len(d) != 0 {
		t.Errorf("a leaf has none: %v", d)
	}
	if n := Named(ps, "zsh", 1000); !slices.Equal(n, []int{11, 13}) {
		t.Errorf("named %v", n)
	}
}

func TestSignal(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	pid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()
	own := func(int) bool { return true }
	state := func() byte {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		st, _ := ParseStat(string(b))
		return st.State
	}
	waitState := func(want byte) {
		for range 100 {
			if state() == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("state %c, want %c", state(), want)
	}

	if err := Signal(syscall.SIGSTOP, []int{pid}, own); err != nil {
		t.Fatal(err)
	}
	waitState('T')
	if err := Signal(syscall.SIGCONT, []int{pid}, own); err != nil {
		t.Fatal(err)
	}
	waitState('S')
	if err := Signal(syscall.SIGTERM, []int{pid}, own); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("still running after TERM")
	}
	// Gone now: no error.
	if err := Signal(syscall.SIGTERM, []int{pid}, own); err != nil {
		t.Errorf("signal to a gone process: %v", err)
	}
	if err := Signal(syscall.SIGUSR1, []int{pid}, own); err == nil {
		t.Error("a signal it does not send")
	}
	// PID 1 and below are never sent anything.
	if err := Signal(syscall.SIGTERM, []int{1, 0, -1}, func(int) bool { return true }); err != nil {
		t.Errorf("init: %v", err)
	}
}

func TestSystemInfoAndCPUInfo(t *testing.T) {
	Root = t.TempDir()
	defer func() { Root = "/" }()
	write := func(p, s string) {
		os.MkdirAll(Root+"/"+p[:strings.LastIndexByte(p, '/')], 0o755)
		os.WriteFile(Root+"/"+p, []byte(s), 0o644)
	}
	write("sys/class/dmi/id/sys_vendor", "Framework\n")
	write("sys/class/dmi/id/product_name", "Laptop 13\n")
	write("proc/sys/kernel/osrelease", "6.8.0-138-generic\n")
	write("sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq", "5132000\n")
	if s := ReadSystemInfo(); s.Model != "Framework Laptop 13" || s.Kernel != "6.8.0-138-generic" {
		t.Errorf("system %q %q", s.Model, s.Kernel)
	}
	if c := ReadCPUInfo(); c.MaxMHz != 5132 {
		t.Errorf("max speed %v", c.MaxMHz)
	}
	if e := Elapsed(time.Now().Add(-10 * time.Second)); e != "under a minute" {
		t.Errorf("elapsed %q", e)
	}
}
