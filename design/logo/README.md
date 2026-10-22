# The inua logo

A triquetra in a ring, drawn as hollow ribbons. Each ribbon is two thin
lines; a line breaks only where another crosses over it, and the gap is the
covering line widened. One leaf points up.

- **Leaves**: each of the three strands is an arc through the centre that
  turns, towards its tips, onto a tighter arc tangent to it, so the leaves
  are short and full. The tips are where the two arms' outer edges meet,
  followed as curves.
- **Centre**: there the ribbon on top passes over with its outer edge and
  under with its inner one. The small triangle where the three leaves meet
  keeps its outline whole.
- **Ring**: it runs between the centre crossings and the hollow tips, and
  over and under in turn with the strands.

`build.py` draws all of it from a few numbers and writes every file here.
It needs Python 3 with shapely 2 and Pillow:

```sh
python3 design/logo/build.py
```

It also copies the favicons into `website/` and the logo files into
`website/img/logo/`.

## Files

| File | What |
| --- | --- |
| `inua-mark.svg`, `inua-mark-{256,512,1024,2048}.png` | the mark in ink, on a clear ground |
| `inua-mark-light.svg`, `inua-mark-light-*.png` | the mark in light, for dark grounds |
| `inua-mark-white.svg` | the mark in white |
| `inua-mark-small.svg` | the mark with solid ribbons, for small sizes (the website's header) |
| `inua-icon-{yoru,washi,blueprint}.svg` | the app icon, rounded square |
| `inua-icon-{yoru,washi,blueprint}-{16…1024}.png` | the app icon at 16, 32, 48, 64, 128, 180, 192, 256, 512 and 1024 px |
| `favicon.svg`, `favicon.ico` (16, 32, 48) | the browser icon, yoru |
| `apple-touch-icon.png` | 180 px, square corners (the system rounds them) |

Below 64 px the two lines of a ribbon would run together, so the small
icons and the favicons draw the ribbons solid, with wider breaks, and the
mark a little larger in its square.

## Colours

| Ground | Ground colour | Mark |
| --- | --- | --- |
| 夜 yoru | `#0B1012` | `#E7E4DA` |
| 和紙 washi | `#F2EDE2` | `#1E1E20` |
| 青写真 blueprint | `#22406E` | `#F2F5F8` |

## Use

- Keep the mark whole: don't redraw its lines thicker or fill the ribbons.
  For sizes under 64 px use the small icons, which are drawn for it.
- Leave clear space around the mark of at least a quarter of its width.
- Use the mark in one colour from the table, on its ground. No gradients,
  outlines or shadows.
