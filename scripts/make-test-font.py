#!/usr/bin/env python3
"""Generate the synthetic test font used by the browser end-to-end fixture.

The font is built from scratch rather than derived from an existing typeface,
so the committed binary carries no licence obligations. Its glyphs are plain
filled boxes of two obviously different widths: 'A'-'Z' are narrow and the
space is empty, which is enough to prove that the browser actually parsed the
font file and applied it (a fallback font produces a visibly different
advance width).

Regenerate with:
    /opt/homebrew/bin/fonttools -m scripts.make_test_font   # or run directly
Requires fontTools. The output is committed, so building slidepack does not.
"""
import sys
from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

UPM = 1000
ASCENT, DESCENT = 800, -200


def box(pen, x0, y0, x1, y1):
    pen.moveTo((x0, y0))
    pen.lineTo((x0, y1))
    pen.lineTo((x1, y1))
    pen.lineTo((x1, y0))
    pen.closePath()


def build(path, family, box_width):
    letters = [chr(c) for c in range(ord("A"), ord("Z") + 1)]
    digits = [chr(c) for c in range(ord("0"), ord("9") + 1)]
    lower = [chr(c) for c in range(ord("a"), ord("z") + 1)]
    chars = letters + lower + digits
    names = [".notdef", "space"] + ["uni%04X" % ord(c) for c in chars]

    fb = FontBuilder(UPM, isTTF=True)
    fb.setupGlyphOrder(names)
    fb.setupCharacterMap({0x20: "space", **{ord(c): "uni%04X" % ord(c) for c in chars}})

    glyphs = {}
    pen = TTGlyphPen(None)
    glyphs[".notdef"] = pen.glyph()
    pen = TTGlyphPen(None)
    glyphs["space"] = pen.glyph()
    for c in chars:
        pen = TTGlyphPen(None)
        box(pen, 60, 0, box_width - 60, 700)
        glyphs["uni%04X" % ord(c)] = pen.glyph()
    fb.setupGlyf(glyphs)

    metrics = {".notdef": (box_width, 0), "space": (box_width, 0)}
    for c in chars:
        metrics["uni%04X" % ord(c)] = (box_width, 60)
    fb.setupHorizontalMetrics(metrics)
    fb.setupHorizontalHeader(ascent=ASCENT, descent=DESCENT)
    fb.setupNameTable({
        "familyName": family,
        "styleName": "Regular",
        "uniqueFontIdentifier": "slidepack test %s" % family,
        "fullName": family,
        "psName": family.replace(" ", ""),
        "version": "Version 1.000",
    })
    fb.setupOS2(sTypoAscender=ASCENT, sTypoDescender=DESCENT, usWinAscent=ASCENT, usWinDescent=-DESCENT)
    fb.setupPost()
    fb.save(path)
    print("wrote", path)


if __name__ == "__main__":
    # 1200/1000 em advance: deliberately far wider than any plausible fallback,
    # so a browser test can prove the font was applied by measuring text, not
    # merely that document.fonts reported it loaded.
    build(sys.argv[1] if len(sys.argv) > 1 else "slidepack-test.ttf",
          "Slidepack Test", 1200)
