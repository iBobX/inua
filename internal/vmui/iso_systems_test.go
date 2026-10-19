package vmui

import (
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/vm"
)

// What a system's row says under its name: what the filters left of it, counted
// the way a person reads a system - installers first, then live images, then
// when it came out.
func TestISOSystemSummary(t *testing.T) {
	media := []vm.ISOMedia{
		{OSID: "fedora42", OSName: "Fedora Linux 42", Variant: "Workstation", Installer: true},
		{OSID: "fedora42", OSName: "Fedora Linux 42", Variant: "Server", Installer: true},
		{OSID: "fedora42", OSName: "Fedora Linux 42", Variant: "Live", Live: true},
	}
	s := vm.ISOSystem{OSID: "fedora42", Media: media, Newest: time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)}
	if got, want := isoSystemSummary(s), "2 installers · 1 live image · newest 2025-04"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	// A system the database gives no date for says what it holds and nothing
	// about when: an invented date would be worse than none.
	noDate := vm.ISOSystem{OSID: "mine1", Media: []vm.ISOMedia{{OSID: "mine1", Installer: true}}}
	if got, want := isoSystemSummary(noDate), "1 installer"; got != want {
		t.Errorf("summary without a date = %q, want %q", got, want)
	}

	// A system whose media are neither (a live medium the database says does not
	// install) still counts its images rather than saying nothing.
	neither := vm.ISOSystem{OSID: "odd1", Media: []vm.ISOMedia{{OSID: "odd1", Live: true}}}
	if got, want := isoSystemSummary(neither), "1 live image"; got != want {
		t.Errorf("summary of a live system = %q, want %q", got, want)
	}
}

// The section's own line counts what the list is arranged in: systems first,
// then the images inside them.
func TestISOCountText(t *testing.T) {
	if got, want := isoCountText(12, 31), "12 systems · 31 install images"; got != want {
		t.Errorf("count = %q, want %q", got, want)
	}
	if got, want := isoCountText(1, 1), "1 system · 1 install image"; got != want {
		t.Errorf("count = %q, want %q", got, want)
	}
	if got, want := isoCountText(0, 0), "0 systems · 0 install images"; got != want {
		t.Errorf("count = %q, want %q", got, want)
	}
}

// The platform and architecture the controls offer, and what they say: the
// families are the database's own words for an architecture and are translated
// for a platform, which is what a person reads.
func TestISOFilterControlText(t *testing.T) {
	platforms := map[string]string{
		"":                    "Every platform",
		vm.ISOPlatformLinux:   "Linux",
		vm.ISOPlatformBSD:     "BSD",
		vm.ISOPlatformWindows: "Windows",
		vm.ISOPlatformOther:   "Other",
	}
	for platform, want := range platforms {
		if got := isoPlatformText(platform); got != want {
			t.Errorf("isoPlatformText(%q) = %q, want %q", platform, got, want)
		}
	}
	arches := map[string]string{
		vm.ISOArchFamilyX86: "x86", vm.ISOArchFamilyARM: "arm64",
		vm.ISOArchFamilyRISCV: "riscv64", "loongarch64": "loongarch64",
	}
	for family, want := range arches {
		if got := isoArchText(family); got != want {
			t.Errorf("isoArchText(%q) = %q, want %q", family, got, want)
		}
	}
	icons := map[string]string{
		vm.ISOPlatformLinux:   "vm-os-linux-symbolic",
		vm.ISOPlatformBSD:     "vm-os-freebsd-symbolic",
		vm.ISOPlatformWindows: "vm-os-windows-symbolic",
		vm.ISOPlatformOther:   "vm-os-custom-symbolic",
	}
	for platform, want := range icons {
		if got := isoPlatformIcon(platform); got != want {
			t.Errorf("isoPlatformIcon(%q) = %q, want %q", platform, got, want)
		}
	}
}

// The choices the two drop-downs make: index 0 is "every" and the rest follow
// the list the control is built with, so a value the catalog does not hold is
// never selected.
func TestISOFilterControlChoices(t *testing.T) {
	if got := isoPlatformChoice(""); got != 0 {
		t.Errorf("every platform = %d, want 0", got)
	}
	if got, want := isoPlatformChoice(vm.ISOPlatformBSD), 2; got != want {
		t.Errorf("BSD = %d, want %d", got, want)
	}
	if got := isoPlatformChoice("a platform nobody has"); got != 0 {
		t.Errorf("an unknown platform falls back to every: %d", got)
	}
	families := []string{vm.ISOArchFamilyX86, vm.ISOArchFamilyARM}
	if got := isoArchChoice(families, ""); got != 0 {
		t.Errorf("every architecture = %d, want 0", got)
	}
	if got := isoArchChoice(families, vm.ISOArchFamilyARM); got != 2 {
		t.Errorf("arm64 = %d, want 2", got)
	}
	if got := isoArchChoice(families, "s390x"); got != 0 {
		t.Errorf("a family the catalog does not hold is not selected: %d", got)
	}
}

// The service turns the filters into the view: the systems the list shows, what
// it counts, and the architecture families the control may offer.
func TestISOViewGroupsAndFilters(t *testing.T) {
	media := []vm.ISOMedia{
		{OSID: "fedora42", OSName: "Fedora Linux 42", Release: "42", Distro: "fedora",
			Platform: vm.ISOPlatformLinux, Arch: "x86_64", Variant: "Workstation",
			Installer: true, URL: "https://example.org/fedora/workstation.iso",
			ReleaseDate: time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)},
		{OSID: "fedora42", OSName: "Fedora Linux 42", Release: "42", Distro: "fedora",
			Platform: vm.ISOPlatformLinux, Arch: "aarch64", Variant: "Server",
			Installer: true, URL: "https://example.org/fedora/server-arm.iso",
			ReleaseDate: time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)},
		{OSID: "debian12", OSName: "Debian 12", Release: "12", Distro: "debian",
			Platform: vm.ISOPlatformLinux, Arch: "x86_64",
			Installer: true, URL: "https://example.org/debian/netinst.iso",
			ReleaseDate: time.Date(2023, 6, 10, 0, 0, 0, 0, time.UTC)},
	}
	s, _, _ := isoService(t, media...)
	m := vm.Machine{Name: "m", Backend: vm.BackendQEMU}

	unfiltered := s.isoView(m)
	if unfiltered.SystemsCount != 2 || unfiltered.MediaCount != 3 {
		t.Errorf("no filters: %d systems, %d media, want 2 and 3",
			unfiltered.SystemsCount, unfiltered.MediaCount)
	}
	if got := unfiltered.Systems[0].System.OSID; got != "fedora42" {
		t.Errorf("the newest system is first: %q", got)
	}
	if len(unfiltered.Systems[0].Rows) != 2 {
		t.Errorf("fedora holds both of its media: %+v", unfiltered.Systems[0])
	}
	if got, want := unfiltered.ArchFamilies, []string{vm.HostArchFamily(), vm.ISOArchFamilyARM}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the architecture control = %v, want %v", got, want)
	}

	// The guide's own list: the architecture this build runs.
	s.SetISOFilters(vm.ISOFilters{ArchFamily: vm.ISOArchFamilyX86})
	hostOnly := s.isoView(m)
	if hostOnly.SystemsCount != 2 || hostOnly.MediaCount != 2 {
		t.Errorf("the host's architecture: %d systems, %d media, want 2 and 2",
			hostOnly.SystemsCount, hostOnly.MediaCount)
	}
	for _, sys := range hostOnly.Systems {
		for _, r := range sys.Rows {
			if vm.ISOArchFamily(r.Media.Arch) != vm.ISOArchFamilyX86 {
				t.Errorf("a medium of another architecture was left in: %+v", r.Media)
			}
		}
	}

	// The recommended group is drawn from the same list: the systems of the
	// distributions the catalog names, and nothing else.
	if got := len(unfiltered.Recommended); got != 2 {
		t.Errorf("%d recommended systems, want fedora and debian", got)
	}
	for _, sys := range unfiltered.Recommended {
		if sys.System.Distro != "fedora" && sys.System.Distro != "debian" {
			t.Errorf("%s is not one of the recommended distributions", sys.System.Distro)
		}
	}
	if got := len(hostOnly.Recommended); got != 2 {
		t.Errorf("the host's architecture leaves both recommended systems: %d", got)
	}

	// A search that names one system leaves that system, and the count says so.
	s.SetISOFilters(vm.ISOFilters{Search: "debian"})
	searched := s.isoView(m)
	if searched.SystemsCount != 1 || searched.MediaCount != 1 {
		t.Errorf("searching for debian: %d systems, %d media", searched.SystemsCount, searched.MediaCount)
	}
	if got := searched.Systems[0].System.OSID; got != "debian12" {
		t.Errorf("the search kept %q", got)
	}

	// A family the catalog has none of empties the list rather than the world:
	// the count is zero and there is nothing to show.
	s.SetISOFilters(vm.ISOFilters{Platform: vm.ISOPlatformBSD})
	if v := s.isoView(m); v.SystemsCount != 0 || v.MediaCount != 0 || len(v.Systems) != 0 {
		t.Errorf("a platform with no systems: %+v", v)
	}
}

// A repaint rebuilds the list when a row's own state changed and leaves it
// alone when nothing did, which is what keeps the scroll position and the
// systems the user opened.
func TestISOSystemsSame(t *testing.T) {
	row := ISORow{Media: vm.ISOMedia{OSID: "fedora42", URL: "https://example.org/a.iso"}}
	a := []ISOSystemRow{{System: vm.ISOSystem{OSID: "fedora42", OSName: "Fedora Linux 42"}, Rows: []ISORow{row}}}
	b := []ISOSystemRow{{System: vm.ISOSystem{OSID: "fedora42", OSName: "Fedora Linux 42"}, Rows: []ISORow{row}}}
	if !isoSystemsSame(a, b) {
		t.Error("the same systems with the same rows should not be rebuilt")
	}
	b[0].Rows[0].Partial = true
	if isoSystemsSame(a, b) {
		t.Error("an unfinished download changes what a row says")
	}
	b[0].Rows[0].Partial = false
	b[0].System.OSID = "debian12"
	if isoSystemsSame(a, b) {
		t.Error("another system is another list")
	}
	if isoSystemsSame(a, nil) {
		t.Error("an empty list is not the same list")
	}
}
