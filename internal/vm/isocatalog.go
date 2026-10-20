package vm

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The install images a user can download are not a list in inua's source tree:
// they are what osinfo-db publishes. osinfo-db is a data package every
// distribution carries and updates - `/usr/share/osinfo`, `libosinfo` its
// reader - and GNOME Boxes asks it the same question, so inua stays current
// without shipping URLs. This file is that question: the media of the operating
// systems the database calls released or rolling, for an architecture this
// build can run, newest first.
//
// See docs/iso-download.md, sections 1, 2 and 7.1.

// ISOMedia is one downloadable installation medium: the unit the window's
// download list is made of. A medium is one file, so one operating system
// appears once per variant and architecture - Fedora 42 Workstation live,
// Fedora 42 Server netinst and so on.
type ISOMedia struct {
	// OSID is the operating system's short id as the database gives it:
	// "debian12", "fedora42", "ubuntu24.04". It is stable, short and is what a
	// choice is remembered by; OSName is what the user reads.
	OSID string
	// OSName is the operating system's name: "Debian 12".
	OSName string
	// Release is the operating system's version: "12", "42", "24.04". It is
	// often part of OSName already, and is carried separately because the
	// window may want it on its own.
	Release string
	// Variant is the medium's variant - Workstation, Server, netinst - by name
	// when the database names one and by id when it does not. It is empty when
	// the medium has no variant, which is a medium with no variant and not an
	// error: much of the database's Ubuntu, for one, has none.
	Variant string
	// Arch is the architecture the medium is for, as the database spells it:
	// "x86_64", "i686". Only media of an architecture this build runs are in a
	// catalog at all.
	Arch string
	// URL is the file to download.
	URL string
	// Live is a live image, which can be tried without installing.
	Live bool
	// Installer says the medium installs an operating system. A medium is not
	// both: a live image is not an installer unless the database says so.
	Installer bool
	// VolumeSize is the medium's own size in bytes, from <iso><volume-size>;
	// zero when the database does not know it, which is often. The downloader
	// uses it to refuse a download that cannot fit, and the window shows it so
	// that a 2 GB live image reads differently from a 700 MB netinst.
	VolumeSize int64
	// ReleaseDate is the operating system's release date, which is what the
	// list is sorted by; the zero time when the database gives none.
	ReleaseDate time.Time
	// Platform is the family the system belongs to in the words the list's
	// filter uses: ISOPlatformLinux, ISOPlatformBSD, ISOPlatformWindows or
	// ISOPlatformOther. The database's <family> is a vendor as often as a
	// platform ("Fedora", "Oracle Linux"), so it is read together with
	// <distro>, which is the clean one, and the answer is one of four.
	Platform string
	// Distro is the database's own distribution id - "fedora", "debian",
	// "opensuse", "win" - shared by every release of one distribution. It is
	// what the recommended list names and what tells two releases of one
	// distribution apart from two systems that merely look alike.
	Distro string
	// Category is what kind of medium this is, from its variant id:
	// ISOCategoryDesktop, ISOCategoryServer, ISOCategoryMinimal,
	// ISOCategoryCloud, ISOCategoryLive, ISOCategoryRescue, or empty when the
	// variant says nothing this code knows. It is for the list's own grouping
	// and never a reason to hide a medium.
	Category string
	// Status is the database's release-status as it gives it - "released",
	// "rolling", "prerelease", "snapshot" - lowercased, and empty when the
	// entry says nothing, which the database means as released. The list shows
	// released and rolling media and leaves the rest to a switch, so the status
	// has to be carried rather than applied while reading.
	Status string
	// Ended says the system is past its support date at the time the catalog
	// was read. Like Status, it is carried so that the list can leave such
	// systems out by default and offer them when the user asks for them, which
	// is what the guide's rule becomes once the window has a switch for it.
	Ended bool
}

// The platforms the list's filter offers. The database's <family> mixes
// platforms with vendors, so every entry is placed in one of these four.
const (
	ISOPlatformLinux   = "linux"
	ISOPlatformBSD     = "bsd"
	ISOPlatformWindows = "windows"
	ISOPlatformOther   = "other"
)

// The medium categories, from a variant id. An id not in the table leaves the
// category empty rather than being guessed at: the list shows the variant's own
// name beside it anyway.
const (
	ISOCategoryDesktop = "desktop"
	ISOCategoryServer  = "server"
	ISOCategoryMinimal = "minimal"
	ISOCategoryCloud   = "cloud"
	ISOCategoryLive    = "live"
	ISOCategoryRescue  = "rescue"
)

// The two places an install list can come from. The window says which, so that
// a user who is offered an old list knows why.
const (
	// ISOSourceOsinfo is the osinfo-db on this host: the distributions' own
	// catalog.
	ISOSourceOsinfo = "osinfo-db"
	// ISOSourceBuiltin is inua's own fallback list, used only when there is no
	// usable osinfo-db.
	ISOSourceBuiltin = "built in"
)

// ISOCatalog is what the window's download page shows: the media that can be
// downloaded, newest first, and which list they came from.
type ISOCatalog struct {
	// Source is ISOSourceOsinfo or ISOSourceBuiltin.
	Source string
	Media  []ISOMedia
}

// OSInfoRoots are the two directories osinfo-db is read from: the data
// package's own and the user's additions. libosinfo merges them and so does
// LoadISOCatalog, the user's file winning when both name the same operating
// system, which is how a distributor's or a user's own entry overrides the
// packaged one.
type OSInfoRoots struct {
	// System is the data package's directory: /usr/share/osinfo on every
	// distribution that ships osinfo-db, with the operating systems under
	// os/<vendor>/*.xml.
	System string
	// User is the user's own directory: ~/.local/share/osinfo, with the same
	// layout. It does not have to exist; most users have none.
	User string
}

// DefaultOSInfoRoots is where this host's osinfo-db is: the packaged database
// under /usr/share/osinfo and the user's own under ~/.local/share/osinfo. This
// is what the shell reads; a test passes a fixture tree instead.
func DefaultOSInfoRoots() OSInfoRoots {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return OSInfoRoots{
		System: "/usr/share/osinfo",
		User:   filepath.Join(home, ".local", "share", "osinfo"),
	}
}

// LoadISOCatalog reads the installation media osinfo-db publishes, in the
// order the database itself is asked: released or rolling and not past its
// support date, media with a URL, of an architecture this build runs, newest
// first. A host with no usable database gets inua's own small fallback list
// instead, and the catalog says so.
//
// It reads files rather than running osinfo-query, so it needs neither
// libosinfo nor a subprocess, and a database in a version this code does not
// know is read as far as it can be.
func LoadISOCatalog(roots OSInfoRoots) ISOCatalog {
	oses := readOSInfoDir(roots.System)
	// The user's entries are merged in last, so they override the packaged ones
	// of the same operating system - the whole entry, as libosinfo overrides.
	for id, o := range readOSInfoDir(roots.User) {
		oses[id] = o
	}
	ids := make([]string, 0, len(oses))
	for id := range oses {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	now := time.Now()
	var media []ISOMedia
	for _, id := range ids {
		media = append(media, oses[id].media(now)...)
	}
	if len(media) == 0 {
		return ISOCatalog{Source: ISOSourceBuiltin, Media: builtinISOs()}
	}
	sortISOMedia(media)
	return ISOCatalog{Source: ISOSourceOsinfo, Media: media}
}

// sortISOMedia puts the newest release first, as Boxes' download page does.
// Media of one operating system share a date and so stay together; the
// comparisons after the date are only there to make the order out of one
// database the same on every run, which is what lets a test name the list.
// A medium whose operating system gives no release date sorts last.
func sortISOMedia(media []ISOMedia) {
	sort.SliceStable(media, func(i, j int) bool {
		a, b := media[i], media[j]
		if !a.ReleaseDate.Equal(b.ReleaseDate) {
			return a.ReleaseDate.After(b.ReleaseDate)
		}
		if a.OSID != b.OSID {
			return a.OSID < b.OSID
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		return a.URL < b.URL
	})
}

// sortSystemISOMedia orders one system's media the way its row expands: the
// categories in isoCategoryRank's order - the desktop image before the server,
// the minimal, the cloud, the live and the rescue one - then the variant's own
// name, the architecture and the file. The date is not part of it (every medium
// here is the same release) and the last comparisons are what make the order
// the same on every run.
func sortSystemISOMedia(media []ISOMedia) {
	sort.SliceStable(media, func(i, j int) bool {
		a, b := media[i], media[j]
		if ra, rb := isoCategoryRank(a.Category), isoCategoryRank(b.Category); ra != rb {
			return ra < rb
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		return a.URL < b.URL
	})
}

// The architectures the list is filtered by, as families rather than as the
// database's own spellings: a filter that says "x86" covers i386, i486, i586,
// i686, x86_64 and amd64, which are one architecture for a person choosing and
// six spellings in the database.
const (
	ISOArchFamilyX86     = "x86"
	ISOArchFamilyARM     = "arm64"
	ISOArchFamilyRISCV   = "riscv64"
	ISOArchFamilyS390X   = "s390x"
	ISOArchFamilyPowerPC = "ppc64le"
)

// ISOArchFamily places one of the database's architecture spellings in a
// family. An architecture this code does not know is its own family, lowercased
// and taken as it is, so that an entry the database grew is still offered and
// still separable rather than silently dropped.
func ISOArchFamily(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "i386", "i486", "i586", "i686", "x86_64", "x86-64", "amd64", "x86":
		return ISOArchFamilyX86
	case "aarch64", "arm64", "armv7l", "armv7hl", "arm":
		return ISOArchFamilyARM
	case "riscv64":
		return ISOArchFamilyRISCV
	case "s390x":
		return ISOArchFamilyS390X
	case "ppc64le", "ppc64":
		return ISOArchFamilyPowerPC
	}
	return strings.ToLower(strings.TrimSpace(arch))
}

// HostArchFamily is the family of the architecture this build runs on: what the
// list offers unless the user asks for another one. A guest of another
// architecture runs without KVM - slower, as the guide says - so the host's own
// family is the default and not the only choice.
func HostArchFamily() string {
	return ISOArchFamily(runtime.GOARCH)
}

// isoPlatformsByDistro places a distribution id in one of the four platforms.
// The database's <distro> is the clean field - one id shared by every release
// of a distribution - so it decides when it is known; <family> covers the rest,
// and its vendor names ("Fedora", "Oracle Linux") are read for the platform
// they are part of rather than taken at face value.
var isoPlatformsByDistro = isoDistroPlatforms(
	[]string{
		"almalinux", "alpinelinux", "alt", "altlinux", "android-x86", "anolis",
		"archlinux", "asianux", "caasp", "centos", "circle", "clearlinux",
		"debian", "elementaryos", "eos", "eurolinux", "fedora", "gentoo",
		"gnome", "guix-system", "hyperbola", "mageia", "mandrake", "mandriva",
		"manjaro", "mes", "miraclelinux", "nixos", "oel", "ol", "opensuse",
		"popos", "pureos", "rhel", "rhl", "rocky", "scientificlinux", "sle",
		"sled", "slem", "sles", "trisquel", "ucs", "voidlinux",
	},
	ISOPlatformLinux,
	isoDistroPlatforms([]string{"freebsd", "openbsd", "netbsd", "dragonflybsd"}, ISOPlatformBSD,
		isoDistroPlatforms([]string{"win", "winnt"}, ISOPlatformWindows, nil)),
)

// isoDistroPlatforms turns a list of distribution ids into the map from id to
// platform, adding to the one it is given so that the three lists read as one
// table.
func isoDistroPlatforms(ids []string, platform string, into map[string]string) map[string]string {
	if into == nil {
		into = map[string]string{}
	}
	for _, id := range ids {
		into[id] = platform
	}
	return into
}

// isoPlatformsByFamily is the fallback for an entry with no <distro>, or one
// whose id this table does not know.
var isoPlatformsByFamily = map[string]string{
	"linux":   ISOPlatformLinux,
	"freebsd": ISOPlatformBSD, "openbsd": ISOPlatformBSD,
	"netbsd": ISOPlatformBSD, "dragonflybsd": ISOPlatformBSD,
	"winnt": ISOPlatformWindows, "win16": ISOPlatformWindows,
	"win9x": ISOPlatformWindows, "dos": ISOPlatformWindows,
	"darwin": ISOPlatformOther, "solaris": ISOPlatformOther,
	"illumos": ISOPlatformOther, "haiku": ISOPlatformOther,
	"hurd": ISOPlatformOther, "netware": ISOPlatformOther,
}

// isoPlatform is the platform an entry belongs to: its distribution id when
// that is known, the family it names when it is not, and other otherwise. An
// entry nobody recognises is "other" rather than a family of its own, because
// the filter is a short list a person reads.
func isoPlatform(family, distro string) string {
	if p, ok := isoPlatformsByDistro[strings.ToLower(strings.TrimSpace(distro))]; ok {
		return p
	}
	if p, ok := isoPlatformsByFamily[strings.ToLower(strings.TrimSpace(family))]; ok {
		return p
	}
	return ISOPlatformOther
}

// isoVariantCategories maps a variant id to what kind of medium it is. The ids
// are the database's own, the common ones spelled out: a medium whose variant
// this table does not know keeps an empty category and is still offered, in the
// order its own variant name gives, rather than being guessed at.
var isoVariantCategories = map[string]string{
	"desktop": ISOCategoryDesktop, "workstation": ISOCategoryDesktop,
	"gnome": ISOCategoryDesktop, "kde": ISOCategoryDesktop,
	"xfce": ISOCategoryDesktop, "plasma5": ISOCategoryDesktop,
	"kworkstation": ISOCategoryDesktop, "kworkstation-live": ISOCategoryDesktop,
	"simply": ISOCategoryDesktop, "simply-live": ISOCategoryDesktop,
	"server": ISOCategoryServer, "server-v": ISOCategoryServer,
	"standard": ISOCategoryServer, "base": ISOCategoryServer,
	"everything": ISOCategoryServer, "dvd": ISOCategoryServer,
	"extended": ISOCategoryServer, "enterprise": ISOCategoryServer,
	"netinst": ISOCategoryMinimal, "netinstall": ISOCategoryMinimal,
	"minimal": ISOCategoryMinimal, "everything-netinst": ISOCategoryMinimal,
	"server-netinst": ISOCategoryMinimal, "workstation-netinst": ISOCategoryMinimal,
	"universal-netinst":      ISOCategoryMinimal,
	"netinst-serial-console": ISOCategoryMinimal,
	"virtual":                ISOCategoryCloud, "genericcloud": ISOCategoryCloud,
	"nocloud": ISOCategoryCloud, "openstack": ISOCategoryCloud,
	"xen":  ISOCategoryCloud,
	"live": ISOCategoryLive, "livecd": ISOCategoryLive,
	"rescue": ISOCategoryRescue, "live-rescue": ISOCategoryRescue,
}

// isoCategoryRank orders the categories for a system's own media: the image the
// system's own website offers first, then the kinds, so that a medium this
// table does not know comes last rather than first.
func isoCategoryRank(category string) int {
	switch category {
	case ISOCategoryDesktop:
		return 0
	case ISOCategoryServer:
		return 1
	case ISOCategoryMinimal:
		return 2
	case ISOCategoryCloud:
		return 3
	case ISOCategoryLive:
		return 4
	case ISOCategoryRescue:
		return 5
	}
	return 6
}

func isoVariantCategory(variantID string) string {
	return isoVariantCategories[strings.ToLower(strings.TrimSpace(variantID))]
}

// ISOFilters is what the download list is showing: the search text, the
// platform and architecture family it is limited to, and whether the media the
// guide's rule leaves out are offered anyway. The zero value is the guide's own
// list - every platform, every architecture, released and rolling systems that
// are still supported - so a caller that asks for nothing gets what the
// document describes, and the window narrows it from there.
type ISOFilters struct {
	// Search is matched, case-insensitively, against the system's name, short
	// id, distribution, release and the medium's variant.
	Search string
	// Platform is ISOPlatformLinux, ISOPlatformBSD, ISOPlatformWindows or
	// ISOPlatformOther; empty means every platform.
	Platform string
	// ArchFamily is a family from ISOArchFamily; empty means every
	// architecture. The window defaults it to HostArchFamily().
	ArchFamily string
	// IncludePrerelease also offers prerelease, snapshot and development
	// systems, which the guide's rule leaves out.
	IncludePrerelease bool
	// IncludeEnded also offers systems past their support date.
	IncludeEnded bool
}

// isostatusReleased is the database's own default: an entry with no
// <release-status> is a released system, and the two a switch is not needed for
// are released and rolling.
func isostatusReleased(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "released", "rolling", "eol":
		return true
	}
	return false
}

// MatchesFilter says whether one medium is in what the filters ask for.
func MatchesFilter(m ISOMedia, f ISOFilters) bool {
	if !f.IncludePrerelease && !isostatusReleased(m.Status) {
		return false
	}
	if !f.IncludeEnded && m.Ended {
		return false
	}
	if f.Platform != "" && m.Platform != f.Platform {
		return false
	}
	if f.ArchFamily != "" && ISOArchFamily(m.Arch) != f.ArchFamily {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Search)); q != "" {
		hay := strings.ToLower(strings.Join([]string{
			m.OSName, m.OSID, m.Release, m.Distro, m.Variant,
		}, "\n"))
		if !strings.Contains(hay, q) {
			return false
		}
	}
	return true
}

// FilterISOMedia is the media the filters ask for, in the list's own order:
// newest release first.
func FilterISOMedia(media []ISOMedia, f ISOFilters) []ISOMedia {
	out := make([]ISOMedia, 0, len(media))
	for _, m := range media {
		if MatchesFilter(m, f) {
			out = append(out, m)
		}
	}
	sortISOMedia(out)
	return out
}

// ISOSystem is one operating system release with the media the filters left of
// it: what the list shows as one row, expanded to the media inside it.
type ISOSystem struct {
	// OSID, OSName, Release, Distro and Platform are the system's own.
	OSID, OSName, Release, Distro, Platform string
	// Media are its media, newest first. Never empty: a system with no medium
	// left is not in a catalog at all.
	Media []ISOMedia
	// Newest is the newest release date among its media, the zero time when the
	// database gives none.
	Newest time.Time
	// Ended says the system is past its support date; Status is the database's
	// release-status as it gives it.
	Ended  bool
	Status string
}

// Installers is how many of the system's media install an operating system, and
// counts only the media the filters left.
func (s ISOSystem) Installers() int {
	n := 0
	for _, m := range s.Media {
		if m.Installer {
			n++
		}
	}
	return n
}

// Live is how many of the system's media are live images.
func (s ISOSystem) Live() int {
	n := 0
	for _, m := range s.Media {
		if m.Live {
			n++
		}
	}
	return n
}

// GroupISOSystems is the media as one entry per operating system release: the
// unit the list shows a row for. The order is the list's own - the newest
// system first - and a system whose database entry gives no release date sorts
// last, as its media do.
func GroupISOSystems(media []ISOMedia) []ISOSystem {
	byID := map[string]*ISOSystem{}
	var order []string
	for _, m := range media {
		s, ok := byID[m.OSID]
		if !ok {
			s = &ISOSystem{
				OSID: m.OSID, OSName: m.OSName, Release: m.Release,
				Distro: m.Distro, Platform: m.Platform,
				Status: m.Status, Ended: m.Ended,
			}
			byID[m.OSID] = s
			order = append(order, m.OSID)
		}
		s.Media = append(s.Media, m)
		if m.ReleaseDate.After(s.Newest) {
			s.Newest = m.ReleaseDate
		}
	}
	out := make([]ISOSystem, 0, len(order))
	for _, id := range order {
		s := byID[id]
		sortSystemISOMedia(s.Media)
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Newest.Equal(b.Newest) {
			return a.Newest.After(b.Newest)
		}
		if a.OSName != b.OSName {
			return a.OSName < b.OSName
		}
		return a.OSID < b.OSID
	})
	return out
}

// RecommendedISOList is the systems the list puts first, by the database's own
// distribution id rather than by URL: an id ages far more slowly than a link,
// and an id a host's database does not have is simply left out. The order is
// the order they are offered in.
var RecommendedISOList = []string{
	"fedora", "ubuntu", "debian", "opensuse", "archlinux", "freebsd",
	"rocky", "alpinelinux",
}

// RecommendedSystems is the newest release of each recommended distribution, in
// the list's own order, at most one per id. A distribution whose newest release
// has no installer is still offered - a live image is a way to try a system -
// and one with nothing in this catalog is skipped, which is what keeps the
// section from going stale.
func RecommendedSystems(systems []ISOSystem) []ISOSystem {
	var out []ISOSystem
	for _, distro := range RecommendedISOList {
		for _, s := range systems {
			if s.Distro != distro {
				continue
			}
			out = append(out, s)
			break
		}
	}
	return out
}

// readOSInfoDir reads one osinfo-db location, keyed by the operating system's

// id, so that the two locations can be merged by id. Missing directories are
// not an error: a host without the data package is a host without osinfo-db.
//
// A file that does not parse is skipped rather than failing the whole catalog.
// osinfo-db is a data package that moves ahead of any code that reads it, and
// one file in a newer shape must not take the download page down with it; the
// files are read in name order, so what a broken file hides is the same list
// every run.
func readOSInfoDir(dir string) map[string]osinfoOS {
	out := map[string]osinfoOS{}
	if dir == "" {
		return out
	}
	files, err := filepath.Glob(filepath.Join(dir, "os", "*", "*.xml"))
	if err != nil {
		return out
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc osinfoDocument
		if err := xml.Unmarshal(data, &doc); err != nil {
			continue
		}
		for _, o := range doc.OS {
			if id := strings.TrimSpace(o.ID); id != "" {
				out[id] = o
			}
		}
	}
	return out
}

// osinfoDocument is one file of the database, which holds one operating
// system; the slice tolerates a file that holds more.
type osinfoDocument struct {
	OS []osinfoOS `xml:"os"`
}

// osinfoOS is the part of an <os> entry a download needs: what it is called,
// when it was released, whether it is still supported, and its media. The
// variants are here only for their names.
type osinfoOS struct {
	ID            string          `xml:"id,attr"`
	ShortIDs      []string        `xml:"short-id"`
	Names         []string        `xml:"name"`
	Version       string          `xml:"version"`
	Family        string          `xml:"family"`
	Distro        string          `xml:"distro"`
	ReleaseDate   string          `xml:"release-date"`
	EOLDate       string          `xml:"eol-date"`
	ReleaseStatus string          `xml:"release-status"`
	Variants      []osinfoVariant `xml:"variant"`
	Media         []osinfoMedia   `xml:"media"`
}

type osinfoVariant struct {
	ID    string   `xml:"id,attr"`
	Names []string `xml:"name"`
}

type osinfoMedia struct {
	Arch      string `xml:"arch,attr"`
	Live      string `xml:"live,attr"`
	Installer string `xml:"installer,attr"`
	Variant   struct {
		ID string `xml:"id,attr"`
	} `xml:"variant"`
	URL string `xml:"url"`
	ISO struct {
		VolumeSize string `xml:"volume-size"`
	} `xml:"iso"`
}

// media is this operating system's downloadable media, each stamped with what
// the window shows and filters by.
//
// What the guide calls the database's rule - released or rolling, not past its
// support date, a URL, an architecture this build runs, newest first - is not
// applied here any more except for the URL: the window has switches for the
// status and the support date and a chooser for the architecture, so the facts
// have to reach it. The catalog is what the database publishes; the view is
// what the user asked to see, and ISOFilters is where the two meet.
func (o osinfoOS) media(now time.Time) []ISOMedia {
	name := firstText(o.Names)
	releaseDate, _ := parseISODate(o.ReleaseDate)
	status := strings.ToLower(strings.TrimSpace(o.ReleaseStatus))
	// A system is ended when the database says so by status or by date: the two
	// are the database's own ways of saying the same thing, and a date in the
	// future does not undo a status of eol.
	ended := status == "eol"
	if eol, ok := parseISODate(o.EOLDate); ok && isopast(eol, now) {
		ended = true
	}
	platform := isoPlatform(o.Family, o.Distro)

	var out []ISOMedia
	for _, m := range o.Media {
		url := strings.TrimSpace(m.URL)
		arch := strings.ToLower(strings.TrimSpace(m.Arch))
		// A medium with no architecture at all is not usable, and one with no
		// URL is not downloadable: those two are the only entries left out
		// while reading.
		if url == "" || arch == "" {
			continue
		}
		// A live image is not an installer unless the database says it is; a
		// netinst is one that is neither said to be live nor not to install.
		live := strings.EqualFold(strings.TrimSpace(m.Live), "true")
		installer := !live
		if said := strings.TrimSpace(m.Installer); said != "" {
			installer = strings.EqualFold(said, "true")
		}
		variant := ""
		variantID := ""
		if id := strings.TrimSpace(m.Variant.ID); id != "" {
			variantID = id
			variant = o.variantName(id)
		}
		out = append(out, ISOMedia{
			OSID:        o.shortID(),
			OSName:      name,
			Release:     strings.TrimSpace(o.Version),
			Variant:     variant,
			Arch:        arch,
			URL:         url,
			Live:        live,
			Installer:   installer,
			VolumeSize:  parseISOBytes(m.ISO.VolumeSize),
			ReleaseDate: releaseDate,
			Platform:    platform,
			Distro:      strings.ToLower(strings.TrimSpace(o.Distro)),
			Category:    isoVariantCategory(variantID),
			Status:      status,
			Ended:       ended,
		})
	}
	return out
}

// shortID is what this operating system is called in a list: its first
// short-id, or its id when the database gives none.
func (o osinfoOS) shortID() string {
	if id := firstText(o.ShortIDs); id != "" {
		return id
	}
	return strings.TrimSpace(o.ID)
}

// variantName is a variant's name from the operating system's own variant
// list, or its id when the list names none: several of the database's versions
// of Debian, for one, name their variant after the operating system itself.
func (o osinfoOS) variantName(id string) string {
	for _, v := range o.Variants {
		if strings.TrimSpace(v.ID) != id {
			continue
		}
		if name := firstText(v.Names); name != "" {
			return name
		}
		break
	}
	return id
}

// firstText is the first of an entry's <name> or <short-id> elements, which
// carry a translation each: the untranslated one is the first. The text is
// trimmed, because a formatted XML file leaves newlines around it.
func firstText(values []string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// parseISODate reads the database's dates, which are days: "2024-10-29".
// Anything else - an empty element, a value from a newer schema - leaves the
// date zero and is not an error.
func parseISODate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseISOBytes reads a numeric element: <volume-size>. Anything unreadable is
// zero, which every caller reads as "the database does not know".
func parseISOBytes(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isopast says whether a date the database gives is behind today. A date is a
// day, not an instant: an operating system whose support ends today is still
// offered, as Boxes offers it.
func isopast(date time.Time, now time.Time) bool {
	y, m, d := now.UTC().Date()
	return date.Before(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

// builtinISOs is what the download page offers when osinfo-db is not
// installed: the current netinst or live server installer of three
// distributions, x86_64 only.
//
// It is a fallback, not the catalog. The URLs were copied from osinfo-db
// 0.20250606 and a release's own file name carries its version, so a URL here
// ages and can go away when the distribution moves a point release into its
// archive. osinfo-db, which every distribution updates, is the list inua
// wants; this is what is left when it is missing, and the catalog says which
// of the two a user is looking at. The variants are named in plain words
// because there is no database to take names from.
func builtinISOs() []ISOMedia {
	return []ISOMedia{
		{
			OSID: "fedora42", OSName: "Fedora Linux 42", Release: "42",
			Variant: "Fedora Server 42", Arch: "x86_64", Installer: true,
			URL:         "https://download.fedoraproject.org/pub/fedora/linux/releases/42/Server/x86_64/iso/Fedora-Server-netinst-x86_64-42-1.1.iso",
			ReleaseDate: time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			OSID: "ubuntu24.04", OSName: "Ubuntu 24.04 LTS", Release: "24.04",
			Arch: "x86_64", Live: true,
			URL:         "https://releases.ubuntu.com/noble/ubuntu-24.04-latest-live-server-amd64.iso",
			ReleaseDate: time.Date(2024, 4, 25, 0, 0, 0, 0, time.UTC),
		},
		{
			OSID: "debian12", OSName: "Debian 12", Release: "12",
			Variant: "netinst", Arch: "x86_64", Installer: true,
			URL:         "https://cdimage.debian.org/cdimage/release/12.11.0/amd64/iso-cd/debian-12.11.0-amd64-netinst.iso",
			ReleaseDate: time.Date(2023, 6, 10, 0, 0, 0, 0, time.UTC),
		},
	}
}
