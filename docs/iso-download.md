# Downloading an installation image

Status: implemented, but for what sections 5 and 7.4-7.5 describe (recognising
a finished image by its volume ID, and the unattended install from the same
database). The rest is what GNOME Boxes does and what inua copied from it: the
catalog read from osinfo-db (`internal/vm/isocatalog.go`), the download into
the cache with the resume, the published checksum and the free-space refusal
Boxes does not have (`internal/vm/isodownload.go`), and the window's install
page, which offers the list and downloads from it (`internal/vmui`,
`ui/vm.blp`). The downloader has been run against a real server with a small
image; the page itself has not been worked through by hand in a session yet.

Read with [inua-vm.md](inua-vm.md), whose QEMU backend already takes an ISO.

## The short version

Boxes does not keep a list of download URLs. It asks **libosinfo** what the
operating systems in **osinfo-db** publish, and downloads the `<url>` of the
`<media>` entry that matches the chosen OS, its variant and the host's
architecture. The download itself is a plain libsoup `GET` into
`~/Downloads`, with no checksum and no resume; the ISO is recognised
afterwards by libosinfo from its volume ID, which is what makes the unattended
install scripts apply.

So the work is in two halves, and only the second one is Boxes' own code:

1. a catalog of media (osinfo-db, a distribution-maintained XML database), and
2. a downloader that streams a URL to a file (about 300 lines of Vala).

## 1. The catalog: osinfo-db

`osinfo-db` is a data package (`libosinfo` reads it). Installed here as
`osinfo-db 0.20250606-1`, under `/usr/share/osinfo/`:

```
/usr/share/osinfo/
  os/<vendor>/<os>.xml     one file per OS release, with its <media> entries
  install-script/<os>/     preseed, kickstart, autoyast … for unattended installs
  device/, datamap/, schema/, platform/
```

A real entry, from `/usr/share/osinfo/os/fedoraproject.org/fedora-41.xml`:

```xml
<media arch="x86_64" live="true" installer-script="false">
  <variant id="workstation"/>
  <url>https://download.fedoraproject.org/pub/fedora/linux/releases/41/Workstation/x86_64/iso/Fedora-Workstation-Live-x86_64-41-1.4.iso</url>
  <iso>
    <volume-id>Fedora-WS-Live-41.*</volume-id>
  </iso>
  <kernel>images/pxeboot/vmlinuz</kernel>
  <initrd>images/pxeboot/initrd.img</initrd>
</media>
```

What each field means for a downloader:

| Field | Use |
|---|---|
| `arch` | which host architectures can run this medium (`x86_64`, `aarch64`, `s390x` …) |
| `variant id` | Workstation, Server, netinst, Everything … the list's subtitle |
| `url` | the file to download; absolute `https://`, or `docker://` for container images |
| `iso/volume-id` | volume label pattern; libosinfo uses it to recognise the finished file |
| `iso/volume-size` | expected size in bytes, when the database knows it (many entries omit it) |
| `live` | a live image rather than an installer |
| `installer-script` | whether an unattended install script accompanies it |
| `kernel`, `initrd` | paths inside the medium, for a direct kernel boot |

The same file also carries what the VM should get: `<resources arch="…">` with
`<minimum>` and `<recommended>` CPU, RAM and storage, and `<release-date>`,
`<eol-date>`, `<release-status>`.

Two locations are read: the system one above and `~/.local/share/osinfo/` for a
user's own additions (libosinfo merges them). `osinfo-query os`, `osinfo-query
os short-id=fedora41` and `osinfo-install-script` are the command-line views of
the same data.

## 2. Choosing what to offer

`gnome-boxes/src/os-database.vala`, `list_downloadable_oses()`:

```vala
foreach (var entity in db.get_os_list ().get_elements ()) {
    var os = entity as Os;
    if (os.get_release_status () != ReleaseStatus.RELEASED &&
        os.get_release_status () != ReleaseStatus.ROLLING)
        continue;                            // no development or EOL-only releases
    foreach (var media_entity in os.get_media_list ().get_elements ()) {
        var media = media_entity as Media;
        if (media.url == null) continue;      // only media with a URL are downloadable
        if (!(media.architecture in InstalledMedia.supported_architectures))
            continue;
        var eol = (os as Product).get_eol_date ();
        if (eol == null || now.compare (eol) < 1)
            after_list.append (media);        // skip past EOL dates
    }
}
after_list.sort ((a, b) => b.os.get_release_date ().compare (a.os.get_release_date ()));
```

and `src/installed-media.vala` fixes the architecture filter to the x86 family:

```vala
public const string[] supported_architectures = {
    "i686", "i586", "i486", "i386", "x86_64", "amd64"
};
```

That is the whole selection rule: **released or rolling, not EOL, has a URL,
architecture the build supports, newest first.** There is no ranking by
popularity, no size limit, no mirror choice.

## 3. The "recommended" list

The first group on the download page is a curated list, not a query:

- built in: `gnome-boxes/data/recommended-downloads.xml`, shipped in the
  binary's GResource;
- over the network: the `recommended-downloads-url` GSetting, default
  `https://gnome.pages.gitlab.gnome.org/gnome-boxes-logos/recommended-downloads.xml`,
  so a distributor can host its own without releasing Boxes.

```xml
<list>
  <os id="http://fedoraproject.org/fedora/"/>
  <os id="http://debian.org/debian/"/>
  <os id="http://gnome.org/gnome/nightly">
    <title>GNOME OS Nightly</title>
    <subtitle>The GNOME Project</subtitle>
    <url>https://os.gnome.org/download/latest/gnome_os_installer.iso</url>
  </os>
</list>
```

An `<os id>` without a `<url>` resolves to **the first media entry of that OS's
latest release**: `get_os_by_id()` first, and when the ID is only a prefix (as
bare `http://fedoraproject.org/fedora/` is) `get_latest_release_for_os_prefix()`.
Only that one media is added, and only if it has a URL (RHEL is the exception,
added even without one because its own dialog handles the download). The
`<title>`, `<subtitle>` and `<url>` children override the database's values on
that entry — a distributor can point at its own mirror without touching
osinfo-db.

The fetch of that XML is the downloader itself, into the logo cache
(`get_logo_cache("recommended-downloads.xml")`). On failure it falls back to
the **previously cached copy** of the file, not to the built-in list: with no
cache the recommended group is simply empty, and the "all downloads" query
below it still fills the page.


## 4. The download itself

`gnome-boxes/src/downloader.vala` and `src/downloads-hub.vala`. The relevant
behaviour, in the order it happens:

1. **One session for everything.** A single `Soup.Session`, with
   `user_agent = "Wget/1.0"` — the comment says it plainly: some servers
   redirect or refuse based on user agent, and a CLI-tool agent gets the file.
2. **Only http and https are streamed.** `supported_schemes = { "http",
   "https" }`; anything else is copied as a local file instead.
3. **A temporary file first.** The body goes to
   `/var/tmp/<basename>-XXXXXX`, and is moved to its final name with
   `File.move` only after the transfer succeeded. A failed or cancelled
   download therefore never leaves a half ISO under the final name.
4. **Progress from Content-Length.** The progress fraction is
   `bytes_read / content_length`; if the server sends no length, the bar stays
   indeterminate.
5. **Read in 2 KiB buffers**, `read_async` → `write_all_async`, cancellable.
6. **Status must be exactly 200.** On anything else (or a cancellation) the
   temporary file is deleted and a `G_IO_ERROR` carrying the status and reason
   phrase is raised; the UI shows a toast.
7. **Same URL downloaded once.** A `HashTable<uri, Download>` holds in-flight
   downloads; a second request for the same URI waits on the first (and copies
   the result if it went to another cache path).
8. **Already there means don't download.** If any of the candidate cache
   paths exists, that file is returned without touching the network. For an
   ISO the candidate is
   `GLib.get_user_special_dir (DOWNLOAD)/<basename>` — i.e. `~/Downloads`.
9. **No resume.** No `Range` header, no `.part` file reuse, no retry, no
   mirror fallback: a dropped connection restarts the file.

## 5. After the download: recognition and unattended install

The file is handed to `MediaManager.create_installer_media_for_path()`
(`src/media-manager.vala`). What happens there is why Boxes can install
without questions:

```vala
var media = yield Media.create_from_location_async (media_path, …);
if (db.identify_media (media))
    return media;                 // osinfo-db recognised it
```

`db.identify_media()` reads the medium's volume ID (`<iso><volume-id>` above)
and possibly its contents, and fills in which OS, release and variant it is.
From that:

- the resource requirements come from `<resources>` in the same XML;
- if the OS or the media has an install script for the `desktop` profile
  (`create_unattended_installer()` in `media-manager.vala`, scripts in
  `/usr/share/osinfo/install-script/`), Boxes renders it into a preseed,
  kickstart or autoyast file and boots it with the ISO — the express install;
- the guest gets the ISO as its CD-ROM and the disk from the same profile.

So the "just works" part of Boxes' Linux installs is not a download feature at
all: it is **volume-ID identification plus an install script from the same
database**.

## 6. What Boxes does not do

Worth stating, because these are the parts inua should not copy:

- **No checksum and no signature.** The osinfo schema has no digest field for
  media, and the downloader never verifies the file. The ISO is trusted because
  it came from the URL in the database, over HTTPS. (There is an open libosinfo
  request for image verification APIs; nothing ships.)
- **No resume**, no HTTP retry, no mirror/redirect selection by region.
- **No free-space check** before starting: the file streams until the
  filesystem complains.
- **x86 only** (`supported_architectures` above), so an aarch64 host cannot use
  the download page even though osinfo-db carries `aarch64` media.
- **No verification that the file is an ISO at all** until identification.
- The download is not sandboxed or size-capped; a wrong `<url>` in the
  database is downloaded in full.

## 7. What inua should take from this

For the QEMU backend's install media (the field after "an ISO"), in the order
that pays off:

1. **Use osinfo-db as the catalog, not a list in the source tree.** Read
   `/usr/share/osinfo/os/*/*.xml` (and `~/.local/share/osinfo`), take
   `<media>` entries with `<url>` and `<arch>` matching the host, skip
   `release-status` other than released/rolling and past `<eol-date>`, sort by
   `<release-date>`. That is the same rule as `list_downloadable_oses()` and it
   keeps inua current without shipping URLs: distributions update the package.
   Add `osinfo-db` to the QEMU host checks' package plan (it is not a QEMU
   dependency today), and fall back to a small built-in list when it is absent.
2. **Show the media, not just the OS**: variant (Workstation/Server/netinst),
   architecture, live or installer, and `<volume-size>` when present, so the
   user knows a 2 GB live image from a 700 MB netinst.
3. **Download with the parts Boxes lacks**: stream to
   `$XDG_CACHE_HOME/inua/iso/<name>.part`, resume with an `If-Range`/`Range`
   request, verify a published SHA-256 when the distribution publishes one
   (Debian `SHA256SUMS`, Fedora `Fedora-Server-…-CHECKSUM`), and only then
   rename into place. Refuse to start when free space is less than the
   expected size plus a margin.
4. **Identify the file before offering Install**: volume ID from osinfo-db,
   and for a custom image let the user skip identification. Report the
   recognised OS, release and variant in the window.
5. **Unattended install later, through the install scripts**, not before: the
   scripts are in `/usr/share/osinfo/install-script/<os>/` and are what makes
   Boxes' express install possible; the QEMU backend would render one onto a
   FAT volume or a cloud-init seed. This belongs to a separate step from the
   download.
6. **Keep Boxes' good habits**: one session, a temporary name until the
   transfer completes, Content-Length progress, and a cache that treats an
   existing file as done. Add the retry and the checksum it does not have.

The assistant hook planned for the prerequisites page is complementary: a
missing `wget`/`curl` or a proxy that blocks the mirror is a host problem the
assistant can diagnose, while the download itself stays a first-class inua
feature with progress and verification.

## 8. Sources

- GNOME Boxes, `main` at `7ef7414c89dac7dea31a0871ffb49ae6b6e8ed08`
  (2026-10-14): `src/downloader.vala`, `src/downloads-hub.vala`,
  `src/os-database.vala`, `src/media-manager.vala`, `src/installed-media.vala`,
  `src/installer-media.vala`, `src/assistant/os-downloader.vala`,
  `src/assistant/downloadable-entry.vala`, `src/util-app.vala`,
  `data/recommended-downloads.xml`, `data/org.gnome.boxes.gschema.xml`.
- osinfo-db 0.20250606-1 as installed on this machine:
  `/usr/share/osinfo/os/fedoraproject.org/fedora-41.xml`,
  `/usr/share/osinfo/os/debian.org/debian-12.xml`,
  `/usr/share/osinfo/install-script/`.
- libosinfo 1.12.0 (`libosinfo-1.0-0`): `Osinfo.Media.url`,
  `Osinfo.Db.identify_media`.
