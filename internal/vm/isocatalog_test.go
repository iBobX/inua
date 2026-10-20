package vm

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The catalog is osinfo-db read as files, so the tests are a fixture tree in
// the database's own shape: an operating system's XML under os/<vendor>/, with
// the media the download page is made of.
func writeOSInfo(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, "os", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// isoFixtureTree is a host with a database: the system's under /usr/share and
// the user's own under ~/.local/share, both inside a fixture root.
func isoFixtureTree(t *testing.T) OSInfoRoots {
	t.Helper()
	root := t.TempDir()
	roots := OSInfoRoots{
		System: filepath.Join(root, "usr", "share", "osinfo"),
		User:   filepath.Join(root, "home", "user", ".local", "share", "osinfo"),
	}

	// A released operating system: no <release-status> means released, and no
	// <eol-date> means it is not past its end. Its media cover what the rule
	// filters on: the host's architecture and another's, a URL and none, a
	// variant and none, live and installer, and the attributes a medium may
	// carry.
	writeOSInfo(t, roots.System, "fedoraproject.org/fedora-42.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://fedoraproject.org/fedora/42">
    <short-id>fedora42</short-id>
    <family>Fedora</family>
    <distro>fedora</distro>
    <name>Fedora Linux 42</name>
    <name xml:lang="ja">Fedora Linux 42</name>
    <version>42</version>
    <release-date>2025-04-15</release-date>
    <variant id="workstation"><name>Fedora Workstation 42</name></variant>
    <variant id="server-netinst"><name>Fedora Server 42</name></variant>
    <media arch="x86_64" live="true" installer-script="false">
      <variant id="workstation"/>
      <url>https://example.org/fedora/Fedora-Workstation-Live-42-1.1.x86_64.iso</url>
      <iso><volume-id>Fedora-WS-Live-42.*</volume-id></iso>
    </media>
    <media arch="x86_64">
      <variant id="server-netinst"/>
      <url>https://example.org/fedora/Fedora-Server-netinst-x86_64-42-1.1.iso</url>
      <iso><volume-id>Fedora-S-dvd-x86_64-42</volume-id></iso>
    </media>
    <media arch="x86_64" installer="true">
      <url>https://example.org/fedora/Fedora-Everything-netinst-x86_64-42-1.1.iso</url>
    </media>
    <media arch="aarch64"><url>https://example.org/fedora/aarch64.iso</url></media>
    <media arch="s390x"><url>https://example.org/fedora/s390x.iso</url></media>
    <media><url>https://example.org/fedora/noarch.iso</url></media>
    <media arch="x86_64"><variant id="workstation"/></media>
    <tree arch="x86_64"><url>https://example.org/fedora/tree/x86_64/os</url></tree>
  </os>
</libosinfo>
`)

	// Released, in support until 2028: offered, and the newest Debian of the
	// two here, which is what its date in the order below hinges on.
	writeOSInfo(t, roots.System, "debian.org/debian-12.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://debian.org/debian/12">
    <short-id>debian12</short-id>
    <family>linux</family>
    <distro>debian</distro>
    <name>Debian 12</name>
    <version>12</version>
    <release-date>2023-06-10</release-date>
    <eol-date>2028-06-30</eol-date>
    <variant id="universal-netinst"><name>Debian 12</name></variant>
    <media arch="x86_64">
      <variant id="universal-netinst"/>
      <url>https://example.org/debian/debian-12.11.0-amd64-netinst.iso</url>
      <iso><volume-id>Debian 12.* amd64 n</volume-id></iso>
    </media>
  </os>
</libosinfo>
`)

	// The same day as Debian 12, so the order of two operating systems of one
	// date is asserted too.
	writeOSInfo(t, roots.System, "almalinux.org/alma-9.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://almalinux.org/almalinux/9">
    <short-id>alma9</short-id>
    <family>AlmaLinux</family>
    <distro>almalinux</distro>
    <name>AlmaLinux 9</name>
    <version>9</version>
    <release-date>2023-06-10</release-date>
    <eol-date>2032-05-31</eol-date>
    <media arch="x86_64">
      <url>https://example.org/alma/AlmaLinux-9-latest-x86_64-boot.iso</url>
    </media>
  </os>
</libosinfo>
`)

	// Released and in support, with live media that carry no variant and a
	// volume-size, which is what the window's line shows beside a name.
	writeOSInfo(t, roots.System, "ubuntu.com/ubuntu-24.04.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://ubuntu.com/ubuntu/24.04">
    <short-id>ubuntu24.04</short-id>
    <family>linux</family>
    <distro>ubuntu</distro>
    <name>Ubuntu 24.04 LTS</name>
    <version>24.04</version>
    <release-status>released</release-status>
    <release-date>2024-04-25</release-date>
    <eol-date>2034-04-25</eol-date>
    <media arch="x86_64" live="true" installer-script="false">
      <url>https://example.org/ubuntu/ubuntu-24.04-latest-desktop-amd64.iso</url>
      <iso>
        <volume-id>Ubuntu 24.04.* amd64</volume-id>
        <volume-size>4932407296</volume-size>
      </iso>
    </media>
    <media arch="i686"><url>https://example.org/ubuntu/ubuntu-24.04-i386.iso</url></media>
  </os>
</libosinfo>
`)

	// A rolling release with no end date: offered.
	writeOSInfo(t, roots.System, "example.org/rolling.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/rolling">
    <short-id>rolling1</short-id>
    <family>linux</family>
    <distro>archlinux</distro>
    <name>Rolling 1</name>
    <release-status>rolling</release-status>
    <release-date>2022-01-01</release-date>
    <media arch="x86_64"><url>https://example.org/rolling/rolling-1.iso</url></media>
  </os>
</libosinfo>
`)

	// Past its end of life, whatever its status says.
	writeOSInfo(t, roots.System, "example.org/old-7.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/old/7">
    <short-id>old7</short-id>
    <family>linux</family>
    <distro>centos</distro>
    <name>Old 7</name>
    <release-date>2015-01-01</release-date>
    <eol-date>2020-01-01</eol-date>
    <media arch="x86_64"><url>https://example.org/old/old-7.iso</url></media>
  </os>
</libosinfo>
`)

	// End of life by status, with an end date still in the future and a newer
	// release date than anything else: skipped on its status alone.
	writeOSInfo(t, roots.System, "example.org/eol-3.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/eol/3">
    <short-id>eol3</short-id>
    <name>EOL 3</name>
    <release-status>eol</release-status>
    <release-date>2026-08-01</release-date>
    <eol-date>2030-01-01</eol-date>
    <media arch="x86_64"><url>https://example.org/eol/eol-3.iso</url></media>
  </os>
</libosinfo>
`)

	// In development: not offered, however inviting.
	writeOSInfo(t, roots.System, "example.org/dev-1.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/dev/1">
    <short-id>dev1</short-id>
    <name>Dev 1</name>
    <release-status>development</release-status>
    <release-date>2026-09-01</release-date>
    <media arch="x86_64"><url>https://example.org/dev/dev-1.iso</url></media>
  </os>
</libosinfo>
`)

	// A file this code cannot read. osinfo-db moves ahead of its readers, so
	// one of these must not take the list down with it.
	writeOSInfo(t, roots.System, "example.org/broken.xml", `<?xml version="1.0"?>
<libosinfo><os id="http://example.org/broken"><short-id>broken`)

	// The user's own additions: they override the packaged entry of the same
	// operating system, and add one of their own.
	writeOSInfo(t, roots.User, "debian.org/debian-12.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://debian.org/debian/12">
    <short-id>debian12</short-id>
    <family>linux</family>
    <distro>debian</distro>
    <name>Debian 12 (my mirror)</name>
    <version>12</version>
    <release-date>2023-06-10</release-date>
    <eol-date>2028-06-30</eol-date>
    <media arch="x86_64">
      <url>https://mirror.example.org/debian-12-netinst.iso</url>
    </media>
  </os>
</libosinfo>
`)
	writeOSInfo(t, roots.User, "example.org/mine.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/mine">
    <short-id>mine1</short-id>
    <name>Mine 1</name>
    <release-date>2023-06-10</release-date>
    <media arch="amd64"><url>https://example.org/mine/mine-1.iso</url></media>
  </os>
</libosinfo>
`)
	return roots
}

// The catalog is the database's media, filtered and ordered by the database's
// own rule: released or rolling, not past its end of life, with a URL, for an
// architecture this build runs, newest release first.
func TestISOCatalogReadsOsinfoDB(t *testing.T) {
	cat := LoadISOCatalog(isoFixtureTree(t))
	if cat.Source != ISOSourceOsinfo {
		t.Fatalf("source = %q, want %q", cat.Source, ISOSourceOsinfo)
	}

	// One line per medium: the operating system's short id, the variant as the
	// database names it, and the file. The order is the assertion: newest
	// release first, and inside one release the media's own order - Fedora 42
	// (2025-04-15), Ubuntu 24.04 (2024-04-25), then the three of 2023-06-10 by
	// short id (alma9, debian12 with the user's own entry, mine1) and rolling1
	// (2022-01-01) last. A tie on the same day is broken by short id, which is
	// what makes the list the same on every run.
	var got []string
	for _, m := range cat.Media {
		got = append(got, m.OSID+"|"+m.Variant+"|"+path.Base(m.URL))
	}
	want := []string{
		"dev1||dev-1.iso",
		"eol3||eol-3.iso",
		"fedora42||aarch64.iso",
		"fedora42||s390x.iso",
		"fedora42||Fedora-Everything-netinst-x86_64-42-1.1.iso",
		"fedora42|Fedora Server 42|Fedora-Server-netinst-x86_64-42-1.1.iso",
		"fedora42|Fedora Workstation 42|Fedora-Workstation-Live-42-1.1.x86_64.iso",
		"ubuntu24.04||ubuntu-24.04-i386.iso",
		"ubuntu24.04||ubuntu-24.04-latest-desktop-amd64.iso",
		"alma9||AlmaLinux-9-latest-x86_64-boot.iso",
		"debian12||debian-12-netinst.iso", // the user's own entry, not the packaged one
		"mine1||mine-1.iso",
		"rolling1||rolling-1.iso",
		"old7||old-7.iso",
	}
	if len(got) != len(want) {
		t.Fatalf("catalog = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("catalog = %v, want %v", got, want)
		}
	}

	byFile := map[string]ISOMedia{}
	for _, m := range cat.Media {
		byFile[path.Base(m.URL)] = m
	}
	// The user's entry replaces the packaged one of the same operating system:
	// its name and its URL are the user's, none of the packaged media is left.
	if m := byFile["debian-12-netinst.iso"]; m.OSName != "Debian 12 (my mirror)" {
		t.Errorf("the user's entry should override the packaged one: %+v", m)
	}
	if _, ok := byFile["debian-12.11.0-amd64-netinst.iso"]; ok {
		t.Error("the packaged Debian 12 entry is still there")
	}

	// A live medium: a live image, not an installer, with its size unknown.
	live := byFile["Fedora-Workstation-Live-42-1.1.x86_64.iso"]
	if !live.Live || live.Installer {
		t.Errorf("a live image is not an installer: %+v", live)
	}
	if live.Arch != "x86_64" || live.Release != "42" || live.OSName != "Fedora Linux 42" || live.VolumeSize != 0 {
		t.Errorf("live medium = %+v", live)
	}
	if !live.ReleaseDate.Equal(time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("release date = %v", live.ReleaseDate)
	}
	// A medium that installs and says nothing else is an installer, and one
	// that says installer="true" is too.
	if m := byFile["Fedora-Server-netinst-x86_64-42-1.1.iso"]; !m.Installer || m.Live {
		t.Errorf("netinst = %+v", m)
	}
	if m := byFile["Fedora-Everything-netinst-x86_64-42-1.1.iso"]; !m.Installer || m.Variant != "" {
		t.Errorf("a medium with no variant is not an error: %+v", m)
	}
	// volume-size is what the window shows so a live image reads differently
	// from a netinst.
	if m := byFile["ubuntu-24.04-latest-desktop-amd64.iso"]; m.VolumeSize != 4932407296 {
		t.Errorf("volume-size = %d, want 4932407296", m.VolumeSize)
	}
	// amd64 is the x86 family too, and a medium with no release date would
	// sort last rather than being dropped.
	if m := byFile["mine-1.iso"]; m.Arch != "amd64" {
		t.Errorf("amd64 media should be offered: %+v", m)
	}

	// What the catalog keeps and the view decides: a medium of another
	// architecture, one whose system is past its support date, one whose system
	// says it is development and one whose release-status is eol. They are read,
	// stamped with the reason, and left out of the default view by ISOFilters -
	// which is what the switches on the list are for.
	for _, file := range []string{"aarch64.iso", "s390x.iso"} {
		m, ok := byFile[file]
		if !ok {
			t.Errorf("%s should be in the catalog, with the architecture it is for", file)
			continue
		}
		if m.Arch != strings.TrimSuffix(file, ".iso") {
			t.Errorf("%s carries the database's own architecture: %+v", file, m)
		}
	}
	if m := byFile["old-7.iso"]; !m.Ended {
		t.Errorf("a system past its support date is marked ended: %+v", m)
	}
	if m := byFile["eol-3.iso"]; m.Status != "eol" || !m.Ended {
		t.Errorf("a status of eol is carried as it is given, and is ended: %+v", m)
	}
	if m := byFile["dev-1.iso"]; m.Status != "development" {
		t.Errorf("a development system says so: %+v", m)
	}
	// A medium with no architecture at all is not usable and is the one entry
	// reading leaves out, together with the ones with no URL.
	for _, file := range []string{"noarch.iso"} {
		if _, ok := byFile[file]; ok {
			t.Errorf("%s should not be in the catalog", file)
		}
	}
}

// A host without osinfo-db - no directory at all, or nothing the rule keeps -
// gets inua's own small list, and the catalog says which of the two it is so
// that the window can say it too.
func TestISOCatalogFallsBackToTheBuiltInList(t *testing.T) {
	root := t.TempDir()
	absent := OSInfoRoots{
		System: filepath.Join(root, "usr", "share", "osinfo"),
		User:   filepath.Join(root, "home", "user", ".local", "share", "osinfo"),
	}
	cat := LoadISOCatalog(absent)
	if cat.Source != ISOSourceBuiltin {
		t.Errorf("source = %q, want %q", cat.Source, ISOSourceBuiltin)
	}
	if len(cat.Media) < 2 || len(cat.Media) > 4 {
		t.Errorf("the fallback should be a small list: %+v", cat.Media)
	}
	for _, m := range cat.Media {
		if m.OSID == "" || m.OSName == "" || m.URL == "" {
			t.Errorf("a fallback entry is not usable: %+v", m)
		}
		if ISOArchFamily(m.Arch) != HostArchFamily() {
			t.Errorf("a fallback entry is for an architecture this build does not run: %+v", m)
		}
		if !m.Live && !m.Installer {
			t.Errorf("a fallback entry is neither a live image nor an installer: %+v", m)
		}
	}
	// Newest first, as the catalog is.
	for i := 1; i < len(cat.Media); i++ {
		if cat.Media[i].ReleaseDate.After(cat.Media[i-1].ReleaseDate) {
			t.Errorf("the fallback is not newest first: %+v", cat.Media)
		}
	}

	// A database that is there and holds media is a catalog even when the
	// default view leaves all of it out: the entry below is a development
	// system, so the window says what its filters left rather than falling back
	// to inua's own list.
	writeOSInfo(t, absent.System, "example.org/dev-1.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/dev/1">
    <short-id>dev1</short-id>
    <name>Dev 1</name>
    <release-status>development</release-status>
    <media arch="x86_64"><url>https://example.org/dev/dev-1.iso</url></media>
  </os>
</libosinfo>
`)
	dev := LoadISOCatalog(absent)
	if dev.Source != ISOSourceOsinfo {
		t.Fatalf("a database holding a medium is a catalog: source = %q", dev.Source)
	}
	if got := FilterISOMedia(dev.Media, ISOFilters{ArchFamily: HostArchFamily()}); len(got) != 0 {
		t.Errorf("the default view leaves a development system out: %+v", got)
	}
	if got := FilterISOMedia(dev.Media, ISOFilters{ArchFamily: HostArchFamily(), IncludePrerelease: true}); len(got) != 1 {
		t.Errorf("the switch offers it: %+v", got)
	}

	// A database that holds nothing downloadable at all - here an entry whose
	// only medium has no URL - is the same thing to a user as no database at
	// all: inua's own list, and the catalog says so.
	none := t.TempDir()
	absentNone := OSInfoRoots{System: filepath.Join(none, "osinfo")}
	writeOSInfo(t, absentNone.System, "example.org/empty.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/empty">
    <short-id>empty1</short-id>
    <name>Empty 1</name>
    <media arch="x86_64"/>
  </os>
</libosinfo>
`)
	if cat := LoadISOCatalog(absentNone); cat.Source != ISOSourceBuiltin {
		t.Errorf("source = %q, want %q", cat.Source, ISOSourceBuiltin)
	}
}

// The rule is a day's, not an instant's: an operating system whose support ends
// today is still offered, and one whose support ended yesterday is marked ended
// and left to the switch that asks for such systems.
func TestISOCatalogEndOfLifeIsADay(t *testing.T) {
	root := t.TempDir()
	roots := OSInfoRoots{System: filepath.Join(root, "osinfo")}
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	writeOSInfo(t, roots.System, "example.org/today.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/today">
    <short-id>today1</short-id><name>Today 1</name>
    <release-date>2020-01-01</release-date>
    <eol-date>`+today+`</eol-date>
    <media arch="x86_64"><url>https://example.org/today/today-1.iso</url></media>
  </os>
</libosinfo>
`)
	writeOSInfo(t, roots.System, "example.org/yesterday.xml", `<?xml version="1.0"?>
<libosinfo version="0.0.1">
  <os id="http://example.org/yesterday">
    <short-id>yesterday1</short-id><name>Yesterday 1</name>
    <release-date>2020-01-01</release-date>
    <eol-date>`+yesterday+`</eol-date>
    <media arch="x86_64"><url>https://example.org/yesterday/yesterday-1.iso</url></media>
  </os>
</libosinfo>
`)
	cat := LoadISOCatalog(roots)
	byID := map[string]ISOMedia{}
	for _, m := range cat.Media {
		byID[m.OSID] = m
	}
	if byID["today1"].Ended {
		t.Error("a system whose support ends today is not ended")
	}
	if !byID["yesterday1"].Ended {
		t.Error("a system whose support ended yesterday is ended")
	}
	var ids []string
	for _, m := range FilterISOMedia(cat.Media, ISOFilters{ArchFamily: HostArchFamily()}) {
		ids = append(ids, m.OSID)
	}
	if len(ids) != 1 || ids[0] != "today1" {
		t.Errorf("the default view = %v, want only today1", ids)
	}
	ended := FilterISOMedia(cat.Media, ISOFilters{ArchFamily: HostArchFamily(), IncludeEnded: true})
	if len(ended) != 2 {
		t.Errorf("the switch offers the ended system too: %+v", ended)
	}
}

// The platform a medium's system belongs to decides the list's family filter.
// The database's <distro> is the clean field and decides; <family> covers an
// entry without one, and a vendor name in it is read for the platform it is
// part of rather than taken at face value.
func TestISOPlatform(t *testing.T) {
	cases := []struct {
		family, distro, want string
	}{
		{"Fedora", "fedora", ISOPlatformLinux},
		{"Red Hat Enterprise Linux", "rhel", ISOPlatformLinux},
		{"AlmaLinux", "almalinux", ISOPlatformLinux},
		{"", "debian", ISOPlatformLinux},
		{"linux", "", ISOPlatformLinux},
		{"freebsd", "freebsd", ISOPlatformBSD},
		{"openbsd", "openbsd", ISOPlatformBSD},
		{"", "netbsd", ISOPlatformBSD},
		{"winnt", "win", ISOPlatformWindows},
		{"win9x", "", ISOPlatformWindows},
		{"", "", ISOPlatformOther},
		{"solaris", "solaris", ISOPlatformOther},
		{"something new", "something-new", ISOPlatformOther},
	}
	for _, c := range cases {
		if got := isoPlatform(c.family, c.distro); got != c.want {
			t.Errorf("isoPlatform(%q, %q) = %q, want %q", c.family, c.distro, got, c.want)
		}
	}
}

// The architecture filter is a family, not one of the database's spellings: a
// filter that says x86 covers i386 and amd64, and an architecture this code does
// not know is its own family rather than being dropped.
func TestISOArchFamily(t *testing.T) {
	cases := map[string]string{
		"x86_64": ISOArchFamilyX86, "amd64": ISOArchFamilyX86,
		"i386": ISOArchFamilyX86, "i686": ISOArchFamilyX86,
		"aarch64": ISOArchFamilyARM, "arm64": ISOArchFamilyARM,
		"riscv64": ISOArchFamilyRISCV, "s390x": ISOArchFamilyS390X,
		"ppc64le":     ISOArchFamilyPowerPC,
		"loongarch64": "loongarch64",
	}
	for arch, want := range cases {
		if got := ISOArchFamily(arch); got != want {
			t.Errorf("ISOArchFamily(%q) = %q, want %q", arch, got, want)
		}
	}
	if HostArchFamily() == "" {
		t.Error("the host's own architecture family is never empty")
	}
}

// The view the guide describes is the filters' zero value plus the host's
// architecture: released and rolling systems, still supported, for an
// architecture this build runs. Every switch widens it by exactly what it says.
func TestISOFilterIsTheGuidesRule(t *testing.T) {
	cat := LoadISOCatalog(isoFixtureTree(t))
	host := HostArchFamily()
	defaults := ISOFilters{ArchFamily: host}

	var got []string
	for _, m := range FilterISOMedia(cat.Media, defaults) {
		got = append(got, m.OSID+"|"+m.Variant+"|"+path.Base(m.URL))
	}
	want := []string{
		"fedora42||Fedora-Everything-netinst-x86_64-42-1.1.iso",
		"fedora42|Fedora Server 42|Fedora-Server-netinst-x86_64-42-1.1.iso",
		"fedora42|Fedora Workstation 42|Fedora-Workstation-Live-42-1.1.x86_64.iso",
		"ubuntu24.04||ubuntu-24.04-i386.iso",
		"ubuntu24.04||ubuntu-24.04-latest-desktop-amd64.iso",
		"alma9||AlmaLinux-9-latest-x86_64-boot.iso",
		"debian12||debian-12-netinst.iso",
		"mine1||mine-1.iso",
		"rolling1||rolling-1.iso",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the default view = %v, want %v", got, want)
	}

	count := func(f ISOFilters) int { return len(FilterISOMedia(cat.Media, f)) }
	if n := count(ISOFilters{}); n != 11 {
		t.Errorf("with no architecture filter = %d media, want 11 (aarch64 and s390x are in, the ended and the unfinished are not)", n)
	}
	if n := count(ISOFilters{ArchFamily: host, IncludePrerelease: true}); n != 10 {
		t.Errorf("the prerelease switch adds the development system: %d", n)
	}
	if n := count(ISOFilters{ArchFamily: host, IncludeEnded: true}); n != 11 {
		t.Errorf("the ended switch adds the unsupported and the eol-status systems: %d", n)
	}
	if n := count(ISOFilters{ArchFamily: host, IncludePrerelease: true, IncludeEnded: true}); n != 12 {
		t.Errorf("both switches: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: ISOArchFamilyARM}); n != 1 {
		t.Errorf("another architecture: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: host, Platform: ISOPlatformLinux}); n != 8 {
		t.Errorf("the linux family: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: host, Platform: ISOPlatformBSD}); n != 0 {
		t.Errorf("a family the fixture has none of: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: host, Search: "workstation"}); n != 1 {
		t.Errorf("a search matches the variant: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: host, Search: "DEBIAN"}); n != 1 {
		t.Errorf("a search is case-insensitive and matches the system: %d media", n)
	}
	if n := count(ISOFilters{ArchFamily: host, Search: "nothing here"}); n != 0 {
		t.Errorf("a search that matches nothing: %d media", n)
	}
}

// The list shows one row per system, with its media inside it, newest system
// first and the media an installer first. What a row says beside the name comes
// from the media the filters left.
func TestISOGroupSystems(t *testing.T) {
	cat := LoadISOCatalog(isoFixtureTree(t))
	systems := GroupISOSystems(FilterISOMedia(cat.Media, ISOFilters{ArchFamily: HostArchFamily()}))
	if len(systems) != 6 {
		t.Fatalf("%d systems, want 6: %+v", len(systems), systems)
	}
	if systems[0].OSID != "fedora42" {
		t.Errorf("the newest system is first: %+v", systems[0])
	}
	if last := systems[len(systems)-1]; last.OSID != "rolling1" {
		t.Errorf("a system with no release date sorts last: %+v", last)
	}
	fedora := systems[0]
	if fedora.OSName != "Fedora Linux 42" || fedora.Distro != "fedora" || fedora.Platform != ISOPlatformLinux {
		t.Errorf("the system's own fields: %+v", fedora)
	}
	if len(fedora.Media) != 3 || fedora.Installers() != 2 || fedora.Live() != 1 {
		t.Errorf("fedora = %d media, %d installers, %d live", len(fedora.Media), fedora.Installers(), fedora.Live())
	}
	if !fedora.Newest.Equal(time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("newest = %v", fedora.Newest)
	}
	// The category table's order, which is what a person opening the system
	// wants first: the desktop image, then the server one, then a medium whose
	// variant the table does not know.
	wantFirst := "Fedora-Workstation-Live-42-1.1.x86_64.iso"
	if got := path.Base(fedora.Media[0].URL); got != wantFirst {
		t.Errorf("a system expands to its desktop image first: %v", got)
	}
	if got := path.Base(fedora.Media[1].URL); got != "Fedora-Server-netinst-x86_64-42-1.1.iso" {
		t.Errorf("then its server image: %v", got)
	}
	if last := fedora.Media[len(fedora.Media)-1]; last.Category != "" {
		t.Errorf("a medium the table does not know is last: %+v", last)
	}
	// Every medium of one system is one system's: no medium appears twice.
	seen := map[string]int{}
	for _, s := range systems {
		for _, m := range s.Media {
			seen[m.OSID]++
		}
	}
	total := 0
	for _, n := range seen {
		total += n
	}
	if total != 9 {
		t.Errorf("the groups hold every medium once: %d", total)
	}
}

// The recommended list names distributions, not URLs: the newest release of
// each that this catalog has, in the list's own order, and nothing at all for a
// distribution the database does not have.
func TestISORecommendedSystems(t *testing.T) {
	cat := LoadISOCatalog(isoFixtureTree(t))
	systems := GroupISOSystems(FilterISOMedia(cat.Media, ISOFilters{ArchFamily: HostArchFamily()}))
	rec := RecommendedSystems(systems)
	if len(rec) != 4 {
		t.Fatalf("%d recommended, want 4 (fedora, ubuntu, debian and the rolling fixture): %+v", len(rec), rec)
	}
	var distros []string
	for _, s := range rec {
		distros = append(distros, s.Distro)
	}
	if strings.Join(distros, ",") != "fedora,ubuntu,debian,archlinux" {
		t.Errorf("recommended = %v, want the list's own order with what is missing left out", distros)
	}
	if len(rec) > len(RecommendedISOList) {
		t.Errorf("%d recommended is more than the list names", len(rec))
	}
	// A release that is not in the view is not recommended either: the ended and
	// the archeology of another architecture do not appear.
	for _, s := range rec {
		if s.Ended {
			t.Errorf("%s is ended and should not be recommended", s.OSID)
		}
	}
}

// The medium categories come from the variant ids the database really uses.
func TestISOMediaCategories(t *testing.T) {
	cases := map[string]string{
		"workstation": ISOCategoryDesktop, "gnome": ISOCategoryDesktop,
		"plasma5": ISOCategoryDesktop, "server": ISOCategoryServer,
		"standard": ISOCategoryServer, "netinst": ISOCategoryMinimal,
		"server-netinst": ISOCategoryMinimal, "minimal": ISOCategoryMinimal,
		"virtual": ISOCategoryCloud, "genericcloud": ISOCategoryCloud,
		"rescue":        ISOCategoryRescue,
		"something-new": "",
	}
	for id, want := range cases {
		if got := isoVariantCategory(id); got != want {
			t.Errorf("isoVariantCategory(%q) = %q, want %q", id, got, want)
		}
	}
}
