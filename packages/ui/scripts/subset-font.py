"""Cut the shipped webfont down to the weights the design actually uses.

The face is a variable font with a `wght` axis running 100-900. The design
uses two points on it, 400 and 500 (`--fw-regular` and `--fw-medium` in
tokens.css), and nothing anywhere renders at any other weight - checked by
walking every element on every page in a browser and reading back the
computed `font-weight`, not by reading the stylesheets and hoping.

Carrying the other 800 units of axis costs 13.5 KB of `gvar` deltas on every
first visit. Restricting the range is not the same as subsetting characters:
every glyph and every codepoint survives, so Portuguese text and whatever
GitHub returns for a repository description still render in the same face.
Only the interpolation data outside 400-500 goes.

    100-900 (source)   34,664 B
    400-700            23,104 B   -33%
    400-600            22,420 B   -35%
    400-500            21,132 B   -39%   <- shipped

The narrow end was taken deliberately. The usual argument for headroom is
that a missing weight fails quietly - ask a 400-500 face for bold and the
browser synthesises one rather than telling you - but the answer to that is
this script rather than 2 KB of speculation: change WEIGHTS below, run it,
and the wider cut is a command away.

Not run at build time, and not checked in CI. The input changes when the
typeface changes, which is approximately never, so the output is committed
and this exists so the transformation is reproducible rather than lore -
which is exactly what the 268-glyph character subset already in the source
file is, since nothing in this repository records who made it or how.

Requires fonttools and brotli, which are the only third-party Python
dependencies anywhere here:

    python3 -m pip install fonttools brotli
"""

import pathlib
import sys

try:
    from fontTools.ttLib import TTFont
    from fontTools.varLib import instancer
except ImportError:
    sys.exit(
        "fonttools is not installed. This script is run by hand, rarely:\n"
        "    python3 -m pip install fonttools brotli"
    )

# The range to keep, inclusive. Both ends have to be real points on the axis.
WEIGHTS = (400, 500)

ROOT = pathlib.Path(__file__).resolve().parent.parent
# Kept out of `public/` on purpose: it is the input, not an asset. Everything
# under `public/` is served, and shipping both cuts would mean paying for the
# saving and the thing it saved us from.
SOURCE = ROOT / "assets" / "fonts" / "hanken-grotesk-var.woff2"
# Into src/, not public/: the bundler resolves the relative url() in
# tokens.css, emits one content-hashed copy under _astro/, and the adapter's
# immutable rule covers it there. The name still carries the axis because
# test/fonts.test.ts reads the range back out of it to check the @font-face
# descriptor is not promising weights this file cannot draw.
OUTPUT = (
    ROOT / "src" / "styles" / "fonts" / f"hanken-grotesk-{WEIGHTS[0]}-{WEIGHTS[1]}.woff2"
)


def main() -> None:
    if not SOURCE.exists():
        sys.exit(f"no source font at {SOURCE}")

    font = TTFont(SOURCE)
    axes = {axis.axisTag: (axis.minValue, axis.maxValue) for axis in font["fvar"].axes}
    if "wght" not in axes:
        sys.exit(f"source has no wght axis, only {sorted(axes)}")

    low, high = axes["wght"]
    if not (low <= WEIGHTS[0] <= WEIGHTS[1] <= high):
        sys.exit(f"WEIGHTS {WEIGHTS} is outside the source axis {low}-{high}")

    instancer.instantiateVariableFont(font, {"wght": WEIGHTS}, inplace=True)
    font.flavor = "woff2"
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    font.save(OUTPUT)

    before = SOURCE.stat().st_size
    after = OUTPUT.stat().st_size
    print(f"{SOURCE.name}  {before:,} B  (wght {low:.0f}-{high:.0f})")
    print(
        f"{OUTPUT.name}  {after:,} B  "
        f"(wght {WEIGHTS[0]}-{WEIGHTS[1]}, {(1 - after / before) * 100:.0f}% smaller)"
    )
    print(
        "\ntokens.css must declare the same range on the @font-face, and "
        "src/fonts.ts\nmust name this file - test/fonts.test.ts checks both."
    )


if __name__ == "__main__":
    main()
