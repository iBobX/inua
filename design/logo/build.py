#!/usr/bin/env python3
"""Draws the inua mark and writes its SVG and PNG files.

The mark is a triquetra in a ring, drawn as hollow ribbons: each ribbon is
two thin lines, and a line breaks only where another line crosses over it.
The three strands are arcs of radius R through the centre that turn, past
PHI degrees from their middle, onto tighter arcs of radius R1 out to the
tips, which gives short, full leaves. The small triangle in the middle,
where the three leaves meet, keeps its outline whole.

Needs Python 3 with shapely (2.x) and Pillow. Run from anywhere:

    python3 design/logo/build.py

It writes into design/logo/, copies the favicons and the logo files the
website uses into website/, and writes internal/shell/cardlogo.go: the mark
as a path for About's card and as block art for `inua fetch`.
"""
import math
import os
import shutil
from shapely.affinity import rotate
from shapely.geometry import LineString, Point, Polygon
from shapely.ops import substring, unary_union
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
SITE = os.path.join(HERE, '..', '..', 'website')
GO = os.path.join(HERE, '..', '..', 'internal', 'shell', 'cardlogo.go')

# Geometry, in the units of a 128 grid centred on (64, 64).
CX = CY = 64
R, W, RING, GAP, PHI, R1 = 28, 5.6, 18.3, 0.75, 35, 18
TURN = 60  # one leaf up

INK, PAPER = '#1E1E20', '#F2EDE2'          # washi
NIGHT, LIGHT = '#0B1012', '#E7E4DA'        # yoru
BLUE, WHITE = '#22406E', '#F2F5F8'         # 青写真, blueprint
GROUNDS = {'yoru': (NIGHT, LIGHT), 'washi': (PAPER, INK), 'blueprint': (BLUE, WHITE)}


def polys(g):
    if g.is_empty:
        return []
    if isinstance(g, Polygon):
        return [g]
    return [p for p in getattr(g, 'geoms', []) if isinstance(p, Polygon)]


def closed(pts):
    return LineString(list(pts) + [pts[0]])


def circle(r, n=720):
    return closed([(CX + r * math.cos(2 * math.pi * i / n), CY + r * math.sin(2 * math.pi * i / n)) for i in range(n)])


def local(line, d, half):
    L = line.length
    if d - half < 0 or d + half > L:
        pts = [line.interpolate((d + half * (k / 10 - 1)) % L) for k in range(21)]
        return LineString([(q.x, q.y) for q in pts])
    return substring(line, d - half, d + half)


def strands(rho, ext=0.0, n=600):
    """The three strands, tip to tip, chained end to end."""
    out = []
    ph = math.radians(PHI)
    for k in range(3):
        a = math.radians(-90 + 120 * k)
        ck = (CX + rho * math.cos(a), CY + rho * math.sin(a))
        beta = math.atan2(CY - ck[1], CX - ck[0])

        def p(t, ck=ck, beta=beta):
            if abs(t) <= ph:
                return (ck[0] + R * math.cos(beta + t), ck[1] + R * math.sin(beta + t))
            s = 1 if t > 0 else -1
            c = (ck[0] + (R - R1) * math.cos(beta + s * ph), ck[1] + (R - R1) * math.sin(beta + s * ph))
            return (c[0] + R1 * math.cos(beta + t), c[1] + R1 * math.sin(beta + t))

        rays = [math.radians(-90 + 120 * m + 180) for m in range(3) if m != k]

        def end(sign, p=p, rays=rays):
            # walk out until the strand crosses a tip's ray, well out
            def off(t):
                q = p(sign * t)
                ang = math.atan2(q[1] - CY, q[0] - CX)
                return [(ang - r + math.pi) % (2 * math.pi) - math.pi for r in rays], math.dist(q, (CX, CY))
            t = 0.0
            prev, _ = off(t)
            while t < math.pi * 1.5:
                t += 0.002
                cur, r = off(t)
                if r > 10 and any(x * y < 0 and abs(y) < 0.5 for x, y in zip(prev, cur)):
                    return sign * t
                prev = cur
            raise ValueError('no tip')

        t0, t1 = end(-1) - ext, end(1) + ext
        out.append([p(t0 + (t1 - t0) * i / n) for i in range(n + 1)])
    chain, rest = [out[0]], out[1:]
    while rest:
        e = chain[-1][-1]
        j = min(range(len(rest)), key=lambda i: min(math.dist(e, rest[i][0]), math.dist(e, rest[i][-1])))
        s = rest.pop(j)
        if math.dist(e, s[-1]) < math.dist(e, s[0]):
            s = s[::-1]
        chain.append(s)
    return chain


def crossings(a, b):
    """Where centre lines a and b cross; a with itself when b is None."""
    if b is not None:
        x = a.intersection(b)
        return [x] if x.geom_type == 'Point' else [g for g in getattr(x, 'geoms', []) if g.geom_type == 'Point']
    L = a.length
    segs = [substring(a, L * k / 60, L * (k + 1) / 60) for k in range(60)]
    found = []
    for i in range(60):
        for j in range(i + 2, 60):
            if i == 0 and j == 59:
                continue
            x = segs[i].intersection(segs[j])
            for p in ([x] if x.geom_type == 'Point' else [g for g in getattr(x, 'geoms', []) if g.geom_type == 'Point']):
                if all(p.distance(q) > 1 for q in found):
                    found.append(p)
    return found


def passes(line, p):
    """The two distances along a closed line at which it passes through p."""
    L, ds, best = line.length, [], []
    for i in range(1800):
        q = line.interpolate(L * i / 1800)
        if q.distance(p) < 3:
            best.append((q.distance(p), L * i / 1800))
    for _, d in sorted(best):
        if all(min(abs(d - e), L - abs(d - e)) > 8 for e in ds):
            ds.append(d)
        if len(ds) == 2:
            break
    return ds


def events(lines):
    """Every crossing, and which of the two passes over: along each line
    they go over, under, over …"""
    ev = []
    for ia, A in enumerate(lines):
        for ib in range(ia, len(lines)):
            if ia == ib:
                for p in crossings(A, None):
                    ds = passes(A, p)
                    if len(ds) == 2:
                        ev.append((ia, ds[0], ib, ds[1], p))
            else:
                B = lines[ib]
                for p in crossings(A, B):
                    ev.append((ia, A.project(p), ib, B.project(p), p))
    seq = {}
    for n, (ia, da, ib, db, p) in enumerate(ev):
        seq.setdefault(ia, []).append((da, n, 'a'))
        seq.setdefault(ib, []).append((db, n, 'b'))
    top = {}
    for k, (d, n, side) in enumerate(sorted(seq[min(seq)])):
        top[n] = side if k % 2 == 0 else ('b' if side == 'a' else 'a')
    for s, l in seq.items():
        l = sorted(l)
        known = [(k, n, side) for k, (d, n, side) in enumerate(l) if n in top]
        if not known:
            continue
        k0, n0, side0 = known[0]
        over0 = top[n0] == side0
        for k, (d, n, side) in enumerate(l):
            if n not in top:
                over = over0 if (k - k0) % 2 == 0 else not over0
                top[n] = side if over else ('b' if side == 'a' else 'a')
    return ev, top


def mark(lw, gap=GAP):
    """The mark as filled shapes, for lines of width lw; at lw = W / 2 the
    two lines of a ribbon meet and the ribbons are solid."""
    # the inner edges close a small triangle at the centre
    rho = R - (W - lw) / 2 - 1.4
    chain = strands(rho)
    tips = [c[0] for c in chain]
    P = closed([q for c in chain for q in c[:-1]])
    S3 = [LineString(c) for c in chain]
    off = W / 2 - lw / 2
    E = [[s.offset_curve(d).buffer(lw / 2, cap_style='flat') for d in (off, -off)] for s in S3]
    lines = unary_union([e for es in E for e in es])
    # each tip: the two arms run on past it, kept inside both arms' outer
    # edges, followed as curves; its outline
    ext = strands(rho, ext=0.35)
    X = []
    for c in chain:
        e = min(ext, key=lambda q: min(math.dist(q[0], c[0]) + math.dist(q[-1], c[-1]), math.dist(q[-1], c[0]) + math.dist(q[0], c[-1])))
        X.append(LineString(e if math.dist(e[0], c[0]) < math.dist(e[-1], c[0]) else e[::-1]))
    for k, t in enumerate(tips):
        A, B = X[k - 1], X[k]
        zone, near = Point(t).buffer(W * 3.2), Point(t).buffer(W * 4.5)
        bands = [A.buffer(W / 2, cap_style='flat').intersection(near), B.buffer(W / 2, cap_style='flat').intersection(near)]

        def inside(L, other):
            L = L.intersection(near.buffer(W * 3))
            L = max(getattr(L, 'geoms', [L]), key=lambda g: g.length)
            sides = [L.buffer(d, single_sided=True, cap_style='flat') for d in (W * 12, -W * 12)]
            ref = other.interpolate(other.project(Point(t)) + (W * 2 if other is B else -W * 2))
            ins = max(sides, key=lambda s: s.contains(ref))
            out = sides[1] if ins is sides[0] else sides[0]
            return ins.union(L.buffer(W / 2)).difference(out.difference(L.buffer(W / 2)))

        tip = unary_union(bands).intersection(inside(A, B)).intersection(inside(B, A))
        outline = tip.difference(tip.buffer(-lw, join_style='mitre', mitre_limit=20))
        lines = lines.difference(zone).union(outline.intersection(zone))
    c = Point(CX, CY)
    ring = unary_union([c.buffer(RING + W / 2, resolution=256).difference(c.buffer(RING + W / 2 - lw, resolution=256)),
                        c.buffer(RING - W / 2 + lw, resolution=256).difference(c.buffer(RING - W / 2, resolution=256))])
    S = [P, circle(RING)]
    ev, top = events(S)
    shapes = unary_union([lines, ring])
    paths = S3 + [circle(RING)]
    E.append([ring.difference(c.buffer(RING)), ring.intersection(c.buffer(RING))])
    raw = [unary_union(e) for e in E]

    def which(s, d):
        seg = local(S[s], d, 1.5)
        q = [seg.interpolate(0), seg.interpolate(seg.length)]
        return min(range(4), key=lambda k: sum(paths[k].distance(x) for x in q))

    cuts = []
    for n, (ia, da, ib, db, p) in enumerate(ev):
        (it, dt), (iu, du) = ((ia, da), (ib, db)) if top[n] == 'a' else ((ib, db), (ia, da))
        ro, ru = which(it, dt), which(iu, du)
        zone, under = p.buffer(W * 2.6), raw[ru].buffer(0.3)
        if ia == ib:
            # at the centre the ribbon on top passes over with its outer
            # edge and under with its inner one
            a, b = (e.intersection(zone) for e in E[ro])
            oo, oi = (a, b) if a.centroid.distance(c) > b.centroid.distance(c) else (b, a)
            cuts.append(oo.buffer(gap).difference(oo).intersection(zone).intersection(under))
            u = raw[ru].intersection(zone)
            cuts.append(u.buffer(gap).difference(u).intersection(oi))
        else:
            o = raw[ro]
            cuts.append(o.buffer(gap).difference(o).intersection(zone).intersection(under))
    # the small triangle in the middle keeps its outline whole
    face = next((q for q in polys(c.buffer(W * 3).difference(lines)) if q.contains(c)), None)
    if face is not None:
        keep = face.buffer(lw + 0.05, join_style='mitre', mitre_limit=20)
        cuts = [k.difference(keep) for k in cuts]
    g = shapes.difference(unary_union(cuts))
    g = unary_union([Polygon(q.exterior.coords, [h.coords for h in q.interiors if Polygon(h).area > lw * lw * 0.6])
                     for q in polys(g) if q.area > lw * lw * 1.5])
    return rotate(g, TURN, origin=(CX, CY)).simplify(0.01)


def path_d(g, ox=0.0, oy=0.0, s=1.0):
    out = []
    for p in polys(g):
        for r in [p.exterior] + list(p.interiors):
            c = list(r.coords)[:-1]
            out.append('M' + ' L'.join(f'{(x - ox) * s:.2f} {(y - oy) * s:.2f}' for x, y in c) + 'Z')
    return ''.join(out)


def frame(g, fill):
    """A square around the mark's bounds; fill is the share its width takes."""
    b = g.bounds
    side = (b[2] - b[0]) / fill
    cx, cy = (b[0] + b[2]) / 2, (b[1] + b[3]) / 2
    return cx - side / 2, cy - side / 2, side


def png(g, box, px, colour, ground=None, radius=0.0, ss=4):
    """The mark rendered at px, on a ground (rounded by radius) or clear."""
    ox, oy, side = box
    n = px * ss
    k = n / side
    m = Image.new('L', (n, n), 0)
    d = ImageDraw.Draw(m)
    for p in polys(g):
        d.polygon([((x - ox) * k, (y - oy) * k) for x, y in p.exterior.coords], fill=255)
        for h in p.interiors:
            d.polygon([((x - ox) * k, (y - oy) * k) for x, y in h.coords], fill=0)
    im = Image.new('RGBA', (n, n), (0, 0, 0, 0))
    if ground:
        shape = Image.new('L', (n, n), 0)
        ImageDraw.Draw(shape).rounded_rectangle([0, 0, n - 1, n - 1], radius=radius * n, fill=255)
        im.paste(Image.new('RGBA', (n, n), ground), (0, 0), shape)
    im.paste(Image.new('RGBA', (n, n), colour), (0, 0), m)
    return im.resize((px, px), Image.LANCZOS)


def svg_mark(g, colour, fill=0.92):
    ox, oy, side = frame(g, fill)
    s = 1024 / side
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">'
            f'<path d="{path_d(g, ox, oy, s)}" fill="{colour}" fill-rule="evenodd"/></svg>\n')


def svg_icon(g, ground, colour, radius=0.22, fill=None):
    ox, oy, side = frame(g, fill or ICON_FILL)
    s = 1024 / side
    rx = f' rx="{radius * 1024:.0f}"' if radius else ''
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">'
            f'<rect width="1024" height="1024"{rx} fill="{ground}"/>'
            f'<path d="{path_d(g, ox, oy, s)}" fill="{colour}" fill-rule="evenodd"/></svg>\n')


ICON_FILL = 0.62     # share of an icon's width the mark takes
MARK_SIZES = (256, 512, 1024, 2048)
ICON_SIZES = (16, 32, 48, 64, 128, 180, 192, 256, 512, 1024)


SOLID = (W / 2, 1.6)  # small sizes: solid ribbons, wider breaks


def variant(px):
    # below 64 px the two lines of a ribbon run together: draw it solid
    return SOLID if px < 64 else (1.7, GAP)


def fill_for(px):
    return 0.70 if px < 64 else ICON_FILL


def write(name, data):
    path = os.path.join(HERE, name)
    if isinstance(data, Image.Image):
        data.save(path, optimize=True)
    else:
        with open(path, 'w') as f:
            f.write(data)
    return path


def main():
    marks = {(1.3, GAP): mark(1.3), (1.7, GAP): mark(1.7), SOLID: mark(*SOLID)}
    m = marks[(1.3, GAP)]
    write('inua-mark.svg', svg_mark(m, INK))
    write('inua-mark-light.svg', svg_mark(m, LIGHT))
    write('inua-mark-white.svg', svg_mark(m, '#FFFFFF'))
    box = frame(m, 0.92)
    for px in MARK_SIZES:
        write(f'inua-mark-{px}.png', png(m, box, px, INK))
        write(f'inua-mark-light-{px}.png', png(m, box, px, LIGHT))
    for name, (ground, colour) in GROUNDS.items():
        write(f'inua-icon-{name}.svg', svg_icon(marks[(1.7, GAP)], ground, colour))
        for px in ICON_SIZES:
            g = marks[variant(px)]
            write(f'inua-icon-{name}-{px}.png', png(g, frame(g, fill_for(px)), px, colour, ground, 0.22))
    # favicons: yoru, square corners where the browser rounds them itself
    ground, colour = GROUNDS['yoru']
    # a browser shows favicon.svg at 16 to 32 px: the solid ribbons
    write('favicon.svg', svg_icon(marks[SOLID], ground, colour, fill=fill_for(16)))
    ico = [png(marks[variant(px)], frame(marks[variant(px)], fill_for(px)), px, colour, ground, 0.22) for px in (16, 32, 48)]
    ico[-1].save(os.path.join(HERE, 'favicon.ico'), sizes=[(16, 16), (32, 32), (48, 48)], append_images=ico[:-1])
    g = marks[(1.7, GAP)]
    write('apple-touch-icon.png', png(g, frame(g, ICON_FILL), 180, colour, ground, 0.0).convert('RGB'))
    # what the website uses
    if os.path.isdir(SITE):
        for f in ('favicon.svg', 'favicon.ico', 'apple-touch-icon.png'):
            shutil.copy(os.path.join(HERE, f), os.path.join(SITE, f))
        logo = os.path.join(SITE, 'img', 'logo')
        os.makedirs(logo, exist_ok=True)
        for old in os.listdir(logo):
            os.remove(os.path.join(logo, old))
        for f in ['inua-mark.svg', 'inua-mark-white.svg', 'inua-mark-1024.png', 'inua-icon-yoru.svg'] + \
                 [f"inua-icon-yoru-{px}.png" for px in (128, 256, 512, 1024)]:
            shutil.copy(os.path.join(HERE, f), os.path.join(logo, f))
    with open(GO, 'w') as f:
        f.write(go_file(marks[SOLID], marks[(1.7, GAP)]))


QUADS = {(0, 0, 0, 0): ' ', (1, 0, 0, 0): '▘', (0, 1, 0, 0): '▝', (0, 0, 1, 0): '▖', (0, 0, 0, 1): '▗',
         (1, 1, 0, 0): '▀', (0, 0, 1, 1): '▄', (1, 0, 1, 0): '▌', (0, 1, 0, 1): '▐', (1, 0, 0, 1): '▚',
         (0, 1, 1, 0): '▞', (1, 1, 1, 0): '▛', (1, 1, 0, 1): '▜', (1, 0, 1, 1): '▙', (0, 1, 1, 1): '▟',
         (1, 1, 1, 1): '█'}


def block_art(g, cols=36):
    """The mark in quarter-cell blocks, a terminal's cell twice as tall as
    it is wide, so every quarter is a square."""
    rows = cols // 2
    a = png(g, frame(g, 1.0), cols * 8, '#FFFFFF').split()[3].resize((cols * 2, rows * 2), Image.BOX)
    px = a.load()
    lines = [''.join(QUADS[tuple(int(px[2 * c + dx, 2 * r + dy] > 110) for dy in (0, 1) for dx in (0, 1))]
                     for c in range(cols)) for r in range(rows)]
    while lines and not lines[-1].strip():
        lines.pop()
    while lines and not lines[0].strip():
        lines.pop(0)
    return lines  # each cols wide, so what stands beside it starts in one column


def go_file(path_g, art_g):
    ox, oy, side = frame(path_g, 1.0)
    d = path_d(path_g, ox, oy, 100 / side)
    art = '\n'.join(f'\t"{l}",' for l in block_art(art_g))
    return f"""// Code generated by design/logo/build.py; DO NOT EDIT.

package shell

// CardMark is inua's mark as an SVG path in a 100 by 100 square, filled
// even-odd: the ribbons drawn solid, as it reads small.
const CardMark = "{d}"

// cardLogo is inua's mark for a terminal, in quarter-cell blocks.
var cardLogo = []string{{
{art}
}}
"""


if __name__ == '__main__':
    main()
