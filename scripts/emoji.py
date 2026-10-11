#!/usr/bin/env python3
"""Rebuild internal/emoji/emoji.tsv, the launcher's emoji and symbols, from
Unicode's data: emoji-test.txt (which characters are emoji, in Unicode's
order) and CLDR's English annotations (names and search keywords).

    python3 scripts/emoji.py            # downloads into a temporary folder

Emoji newer than MAX (fonts lag behind) and skin-tone variants are left
out; symbols are the annotated characters that are not emoji, past ASCII.
The data is under the Unicode License (internal/emoji/LICENSE-UNICODE)."""
import json, re, subprocess, sys, tempfile
from pathlib import Path

MAX = (15, 1)
CLDR = "46.0.0"
SOURCES = {
    "emoji-test.txt": "https://unicode.org/Public/emoji/latest/emoji-test.txt",
    "annotations.json": f"https://cdn.jsdelivr.net/npm/cldr-annotations-full@{CLDR}/annotations/en/annotations.json",
    "derived.json": f"https://cdn.jsdelivr.net/npm/cldr-annotations-derived-full@{CLDR}/annotationsDerived/en/annotations.json",
}
out = Path(__file__).resolve().parent.parent / "internal" / "emoji" / "emoji.tsv"

dl = Path(tempfile.mkdtemp(prefix="inua-emoji-"))
for name, url in SOURCES.items():
    subprocess.run(["curl", "-sSfL", "--max-time", "60", "-o", str(dl / name), url], check=True)

ann = json.load(open(dl / "annotations.json", encoding="utf-8"))["annotations"]["annotations"]
der = json.load(open(dl / "derived.json", encoding="utf-8"))["annotationsDerived"]["annotations"]

def look(ch):
    for k in (ch, ch.replace("️", "")):
        if k in ann:
            return ann[k]
        if k in der:
            return der[k]
    return {}

tones = set(chr(c) for c in range(0x1F3FB, 0x1F400))
rows, seen, group = [], set(), ""
for line in open(dl / "emoji-test.txt", encoding="utf-8"):
    if line.startswith("# group:"):
        group = line.split(":", 1)[1].strip()
        continue
    m = re.match(r"^([0-9A-F ]+)\s*;\s*fully-qualified\s*#\s*(\S+)\s*E(\d+)\.(\d+)\s+(.*)$", line)
    if not m:
        continue
    ch, version, name = m.group(2), (int(m.group(3)), int(m.group(4))), m.group(5).strip()
    if version > MAX or any(t in ch for t in tones) or group == "Component":
        continue
    kw = [k for k in look(ch).get("default", []) if k.lower() != name.lower()]
    rows.append(("e", ch, name, kw))
    seen.update((ch, ch.replace("️", "")))
for ch, v in ann.items():
    if len(ch) != 1 or ch in seen or ch in tones or ord(ch) < 0xA0:
        continue
    name = (v.get("tts") or [""])[0]
    if name:
        rows.append(("s", ch, name, [k for k in v.get("default", []) if k.lower() != name.lower()]))

clean = lambda s: s.replace("\t", " ").replace("\n", " ")
with open(out, "w", encoding="utf-8") as f:
    f.write("# kind\tcharacter\tname\tkeywords: emoji (e) up to Emoji %d.%d without skin tones, then symbols (s).\n" % MAX)
    f.write("# From Unicode's emoji-test.txt and CLDR's English annotations; see LICENSE-UNICODE. Made by scripts/emoji.py.\n")
    for k, ch, name, kw in rows:
        f.write(f"{k}\t{ch}\t{clean(name)}\t{clean(', '.join(kw))}\n")
print(f"{out}: {sum(r[0] == 'e' for r in rows)} emoji, {sum(r[0] == 's' for r in rows)} symbols", file=sys.stderr)
