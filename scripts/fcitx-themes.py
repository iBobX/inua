#!/usr/bin/env python3
"""Write Fcitx 5 themes that look like inua: its colour tokens
(ui/themes/<name>.css), its card for the panel (surface, hairline border,
4px corners, a soft shadow) and its selection for the chosen candidate (the
accent at 9% with a 38% border, 2px corners). Fcitx has no corner radius,
so the panel and the highlight are 9-slice images drawn here.

  scripts/fcitx-themes.py [INUA-THEME:FCITX-THEME ...]

writes config/fcitx5/themes/<FCITX-THEME>/; the default is the light and
dark pair, aojashin:inua-light and yoru:inua-dark. `make install-fcitx`
copies them to ~/.local/share/fcitx5/themes. Needs Pillow.
"""

import os
import re
import sys

from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
SS = 4  # supersampling for smooth edges

SHADOW = 8  # px of shadow around the panel, outside its border
RADIUS = 4  # the panel's corners
HL_RADIUS = 2  # the highlight's corners


def tokens(name):
    css = open(os.path.join(ROOT, "ui", "themes", name + ".css")).read()
    return dict(re.findall(r"@define-color\s+([\w-]+)\s+(#[0-9A-Fa-f]{6})", css))


def rgb(hex_):
    return tuple(int(hex_[i : i + 2], 16) for i in (1, 3, 5))


def rgba(hex_, alpha):
    return rgb(hex_) + (round(alpha * 255),)


def dark(t):
    return sum(rgb(t["surface"])) < 3 * 128


def rounded(size, radius, fill, border):
    """A rounded rectangle with a 1px border, drawn large and scaled down."""
    w, h = size
    im = Image.new("RGBA", (w * SS, h * SS), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    d.rounded_rectangle((0, 0, w * SS - 1, h * SS - 1), radius * SS, fill=border)
    d.rounded_rectangle((SS, SS, w * SS - 1 - SS, h * SS - 1 - SS), max(radius - 1, 0) * SS, fill=fill)
    return im.resize((w, h), Image.LANCZOS)


def panel(t):
    """The card: surface, line border, RADIUS corners, a soft shadow below."""
    inner = 2 * (RADIUS + 4)  # a stretchable middle around the corners
    size = inner + 2 * SHADOW
    im = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    shadow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    ImageDraw.Draw(shadow).rounded_rectangle(
        (SHADOW, SHADOW + 2, SHADOW + inner - 1, SHADOW + inner + 1), RADIUS, fill=(0, 0, 0, 120 if dark(t) else 60)
    )
    im.alpha_composite(shadow.filter(ImageFilter.GaussianBlur(SHADOW / 2.5)))
    im.alpha_composite(rounded((inner, inner), RADIUS, rgba(t["surface"], 0.98), rgb(t["line"]) + (255,)), (SHADOW, SHADOW))
    return im


def highlight(t):
    """The selection: the accent at 9%, a 38% accent border."""
    return rounded((12, 12), HL_RADIUS, rgba(t["accent"], 0.09), rgba(t["accent"], 0.38))


def caret(t, direction, size=16):
    """Phosphor's caret, as a 1.5px chevron in the secondary text colour."""
    im = Image.new("RGBA", (size * SS, size * SS), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    s = size * SS
    pts = {
        "left": [(0.62, 0.25), (0.38, 0.5), (0.62, 0.75)],
        "right": [(0.38, 0.25), (0.62, 0.5), (0.38, 0.75)],
    }[direction]
    d.line([(x * s, y * s) for x, y in pts], fill=rgb(t["t1"]) + (255,), width=round(1.5 * SS), joint="curve")
    return im.resize((size, size), Image.LANCZOS)


def mark(t, size=16):
    """The status mark: a filled 7px square in the accent (a checked item)."""
    im = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    o = (size - 7) // 2
    ImageDraw.Draw(im).rectangle((o, o, o + 6, o + 6), fill=rgb(t["accent"]) + (255,))
    return im


def element(image, color, margin):
    m = margin
    return f"""Image={image}
Color={color}
BorderColor=#00000000
BorderWidth=0

[{{p}}/Margin]
Left={m}
Right={m}
Top={m}
Bottom={m}
"""


def conf(inua, name, t):
    edge = SHADOW + RADIUS + 1  # the panel's corners, shadow included
    pad_x, pad_y = 6, 5  # between the card's border and the candidates
    content = f"""Left={SHADOW + pad_x}
Right={SHADOW + pad_x}
Top={SHADOW + pad_y}
Bottom={SHADOW + pad_y}"""
    shadow = f"""Left={SHADOW}
Right={SHADOW}
Top={SHADOW}
Bottom={SHADOW}"""
    variant = "dark" if dark(t) else "light"
    return f"""# Written by inua's scripts/fcitx-themes.py from ui/themes/{inua}.css.
[Metadata]
Name=inua {inua.capitalize()}
Version=1
Author=inua
Description=Fcitx 5 in inua's {inua} colours ({variant}): its card and its selection.
ScaleWithDPI=True

[InputPanel]
NormalColor={t["t0"]}
HighlightCandidateColor={t["t0"]}
HighlightColor={t["surface"]}
HighlightBackgroundColor={t["accent"]}
EnableBlur=False
FullWidthHighlight=True
Spacing=0

[InputPanel/Background]
{element("panel.png", t["surface"], edge).replace("{p}", "InputPanel/Background")}
[InputPanel/Highlight]
{element("highlight.png", t["surface2"], HL_RADIUS + 2).replace("{p}", "InputPanel/Highlight")}
[InputPanel/Highlight/HighlightClickMargin]
Left=0
Right=0
Top=0
Bottom=0

[InputPanel/ContentMargin]
{content}

[InputPanel/TextMargin]
Left=7
Right=7
Top=3
Bottom=3

[InputPanel/ShadowMargin]
{shadow}

[InputPanel/PrevPage]
Image=prev.png

[InputPanel/PrevPage/ClickMargin]
Left=4
Right=4
Top=4
Bottom=4

[InputPanel/NextPage]
Image=next.png

[InputPanel/NextPage/ClickMargin]
Left=4
Right=4
Top=4
Bottom=4

[Menu]
NormalColor={t["t0"]}
HighlightCandidateColor={t["t0"]}
Spacing=2

[Menu/Background]
{element("panel.png", t["surface"], edge).replace("{p}", "Menu/Background")}
[Menu/Highlight]
{element("highlight.png", t["surface2"], HL_RADIUS + 2).replace("{p}", "Menu/Highlight")}
[Menu/Separator]
Image=
Color={t["line"]}
BorderColor=#00000000
BorderWidth=0

[Menu/CheckBox]
Image=radio.png

[Menu/SubMenu]
Image=arrow.png

[Menu/ContentMargin]
{content}

[Menu/TextMargin]
Left=8
Right=8
Top=4
Bottom=4
"""


def write(inua, name):
    t = tokens(inua)
    out = os.path.join(ROOT, "config", "fcitx5", "themes", name)
    os.makedirs(out, exist_ok=True)
    with open(os.path.join(out, "theme.conf"), "w") as f:
        f.write(conf(inua, name, t))
    panel(t).save(os.path.join(out, "panel.png"))
    highlight(t).save(os.path.join(out, "highlight.png"))
    caret(t, "left").save(os.path.join(out, "prev.png"))
    caret(t, "right").save(os.path.join(out, "next.png"))
    caret(t, "right").save(os.path.join(out, "arrow.png"))
    mark(t).save(os.path.join(out, "radio.png"))
    print(f"{out} ({inua})")


if __name__ == "__main__":
    pairs = sys.argv[1:] or ["aojashin:inua-light", "yoru:inua-dark"]
    for p in pairs:
        inua, _, name = p.partition(":")
        write(inua, name or "inua-" + inua)
