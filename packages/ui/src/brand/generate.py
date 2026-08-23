"""Draw the tone wordmark.

Eight-bit, on purpose: every letter is a grid of square pixels, one size,
laid down with no anti-aliasing and no curve that isn't a stair-step. That is
the whole brief - a mark that reads as typed on an old machine, not drawn by
a designer.

Two letters carry a second meaning, because a wordmark that is only shapes
is a missed opportunity:

  - **t** is drawn as a cross: the crossbar runs the letter's full width
    instead of sitting off to one side, so the ascender above it and the
    stem below it read as a crucifix before they read as a letter.
  - **n** is a headstone: an arch over two legs, a post standing taller
    than the slab on the left, and two engraved lines - the epitaph - in
    the ground between them. The post is what makes the lines safe to add:
    a plain arch with lines in its counter reads as "A" once it's sitting
    next to "o" and "e", and it was the post breaking that symmetry that
    let the letter stay readable as "n" with the lines back in.

After the word, one more full glyph-cell is drawn solid: a terminal cursor,
the shape a block caret takes at the end of a line of typed text (see
site-footer.css's own `.reveal__caret` for the same idea in CSS). It is the
one cell in the mark filled from the gradient field rather than from
`currentColor` - see `paintCursor` in src/site/field.ts, which repaints its
stops every frame from whatever colours are on screen. Everything else in
the word is static ink; this one cell is alive, the same relationship the
old mark had to its backslash.

No font dependency - pure geometry, so this runs with a bare Python.

Regenerate:  python3 packages/ui/src/brand/generate.py
"""

import pathlib

OUT = pathlib.Path(__file__).parent

# ---------------------------------------------------------------- metrics --

# One pixel, in SVG user units. Everything below is expressed in whole
# pixels so the grid stays exact - no half-cells, no seams.
PIXEL = 100
COLS = 5
ROWS = 8
GAP_COLS = 1  # blank columns between glyphs

# Row-major bitmaps, top to bottom. "#" is ink, "." is empty. Every glyph is
# COLS wide and ROWS tall - a display face, not a text face: no baseline,
# no x-height, every letter the same block. Row 0 is blank for every letter
# except "n" - see below.
GLYPHS = {
    "t": [
        ".....",
        "..#..",
        "#####",
        "..#..",
        "..#..",
        "..#..",
        "..#..",
        "..#..",
    ],
    "o": [
        ".....",
        ".###.",
        "#...#",
        "#...#",
        "#...#",
        "#...#",
        "#...#",
        ".###.",
    ],
    # A post standing taller than the slab - row 0 is blank for every other
    # letter, but "n" uses it for a spike above the left leg - and two short
    # engraved lines in the counter, the epitaph. Both were tried on a plain
    # arch first and tipped the glyph into reading as "A" once it sat next
    # to "o" and "e"; the spike breaking the arch's left-right symmetry is
    # what let the lines go back in without that happening.
    "n": [
        "#....",
        ".###.",
        "#...#",
        "#...#",
        "#.#.#",
        "#...#",
        "#.#.#",
        "#...#",
    ],
    "e": [
        ".....",
        "#####",
        "#....",
        "#....",
        "####.",
        "#....",
        "#....",
        "#####",
    ],
}

WORD = "tone"


def cells(bitmap: list[str]) -> list[tuple[int, int]]:
    """(col, row) of every filled pixel in a bitmap, top-left origin."""
    return [
        (c, r)
        for r, row in enumerate(bitmap)
        for c, ch in enumerate(row)
        if ch == "#"
    ]


def rect(x: int, y: int, w: int = PIXEL, h: int = PIXEL) -> str:
    return f'<rect x="{x}" y="{y}" width="{w}" height="{h}"/>'


# ------------------------------------------------------------------ layout --

pad = PIXEL // 2
x = pad
letter_groups: list[str] = []
for i, ch in enumerate(WORD):
    rects = "".join(
        rect(x + c * PIXEL, pad + r * PIXEL) for c, r in cells(GLYPHS[ch])
    )
    letter_groups.append(f'<g class="tw__glyph" data-index="{i}">{rects}</g>')
    x += (COLS + GAP_COLS) * PIXEL

# The cursor: one full glyph-cell, solid, gradient-filled. Same advance as a
# letter would take, so the word looks like it still has room to keep going.
cursor_x = x
cursor = (
    f'<rect class="tw__cursor" data-index="{len(WORD)}" '
    f'fill="url(#tw-ramp)" x="{cursor_x}" y="{pad}" '
    f'width="{COLS * PIXEL}" height="{ROWS * PIXEL}"/>'
)
x += COLS * PIXEL

vb_w = x + pad
vb_h = ROWS * PIXEL + pad * 2

# 25 stops so a frame's colour bands (see the gradient field's PROFILE_ROWS)
# map one-to-one onto the cursor's fill; see packages/ui/src/site/field.ts.
# Defaults to white so the cursor is correct before the first frame lands,
# and stays correct if the field never runs at all (e.g. the dashboard).
PROFILE_STOPS = 25
stops = "\n      ".join(
    f'<stop offset="{i / (PROFILE_STOPS - 1):.4f}" stop-color="#ffffff"/>'
    for i in range(PROFILE_STOPS)
)

wordmark_svg = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {vb_w} {vb_h}" fill="none" shape-rendering="crispEdges" role="img" aria-label="tone">
  <defs>
    <linearGradient id="tw-ramp" x1="0" y1="1" x2="0" y2="0">
      {stops}
    </linearGradient>
  </defs>
  <g fill="currentColor">
    {"".join(letter_groups)}
  </g>
  {cursor}
</svg>
"""
(OUT / "wordmark.svg").write_text(wordmark_svg)
print(f"wordmark {vb_w}x{vb_h}  pixel={PIXEL}  glyphs={COLS}x{ROWS}")

# --- mark: the headstone n alone ---------------------------------------------
#
# The mark travels wherever the full word doesn't fit - the favicon, chiefly.
# Cropped tight to the glyph's own bitmap rather than sharing the wordmark's
# padding, since a favicon has no room to spare.
n_cells = cells(GLYPHS["n"])
mark_w = COLS * PIXEL
mark_h = ROWS * PIXEL
mark_body = "".join(rect(c * PIXEL, r * PIXEL) for c, r in n_cells)
mark_svg = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {mark_w} {mark_h}" fill="none" shape-rendering="crispEdges" role="img" aria-label="tone">
  <g fill="currentColor">
    {mark_body}
  </g>
</svg>
"""
(OUT / "mark.svg").write_text(mark_svg)
print(f"mark {mark_w}x{mark_h}")

# --- favicon -----------------------------------------------------------------
#
# The tab icon is the same headstone n, on a plate. Generated here rather
# than drawn by hand so it cannot drift from the mark: a favicon that is
# nearly the logo is worse than one that plainly is.
#
# A plate rather than a bare glyph, because a lone shape on a transparent
# ground disappears into whichever tab colour the browser picks. It flips
# with the browser's colour scheme so the mark reads either way.
#
# Written to both apps' public/ directories - Astro serves favicons from
# there, and there is no import path from a package into public/, so the
# copies are outputs of this script rather than duplicates to maintain.
FAVICON_BOX = 32
FAVICON_GLYPH_H = 22.0  # of 32; the rest is the plate's margin

_scale = FAVICON_GLYPH_H / mark_h
_tx = (FAVICON_BOX - mark_w * _scale) / 2
_ty = (FAVICON_BOX - mark_h * _scale) / 2

favicon_svg = f"""<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {FAVICON_BOX} {FAVICON_BOX}" shape-rendering="crispEdges" role="img" aria-label="tone">
  <rect class="tf__plate" width="{FAVICON_BOX}" height="{FAVICON_BOX}" rx="7" fill="#000000"/>
  <g transform="translate({_tx:.4f},{_ty:.4f}) scale({_scale:.6f})" fill="#ffffff" class="tf__mark">
    {mark_body}
  </g>
  <style>
    @media (prefers-color-scheme: light) {{
      .tf__plate {{ fill: #ffffff; }}
      .tf__mark {{ fill: #000000; }}
    }}
  </style>
</svg>
"""
for app in ("web", "dashboard"):
    dest = OUT.parents[3] / "apps" / app / "public" / "favicon.svg"
    if dest.parent.exists():
        dest.write_text(favicon_svg)
        print(f"favicon -> {dest}")
    else:
        print(f"favicon SKIPPED, no {dest.parent}")

# The raster fallbacks (favicon.ico, icons/*.png) are not generated here:
# rasterising SVG needs a renderer this script deliberately does not depend
# on. Regenerate them from the SVG above when the mark changes - any of
# `rsvg-convert`, ImageMagick, or a headless browser canvas will do.
