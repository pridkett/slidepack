# slidepack

<p class="lede">Edit a directory. Pack it when you want one file to distribute.</p>

<div class="figure"><b>my-presentation/</b>                        <b>my-presentation.html</b>
├── index.html                          ┌────────────────────────┐
├── css/presentation.css                │ bootstrap + manifest   │
├── js/presentation.js      <i>pack ──></i>    │ inline runtime         │
├── fonts/inter.woff2      <i>&lt;── unpack</i>   │ one base64 .tar.gz     │
└── assets/chart.webp                   └────────────────────────┘
&nbsp;
<i>keep this in git</i>                        <i>send this to people</i></div>

<div class="actions">
<a class="action action-primary" href="https://github.com/pridkett/slidepack/releases">Download</a>
<a class="action" href="source-format.html">Source format</a>
<a class="action" href="format-v1.html">Packed format</a>
<a class="action" href="agents.html">For agents</a>
</div>

## Why

Self-contained HTML is a fine way to hand someone a deck and a miserable way to
author one. Assets are inaccessible. Diffs are useless. Replacing an image means
surgery on a megabyte of base64. Version control gives up.

slidepack separates the two jobs. The presentation lives as an ordinary
directory of HTML, CSS, JavaScript, images and fonts — the thing you edit, review
and commit. The `.html` file is a build artifact: one file, no companion folder,
that opens by double-clicking it.

`unpack(pack(source))` reproduces every file, every path, every byte and every
Unix permission bit. Nothing is one-way.

## Install

```bash
brew install pridkett/tap/slidepack
```

Or with Go:

```bash
go install github.com/pridkett/slidepack/cmd/slidepack@latest
```

Or download a binary from the
[releases page](https://github.com/pridkett/slidepack/releases). It is a single
static executable: no Node.js, no Python, no npm, no runtime of any kind. The
browser bootstrap is compiled into it.

## Use

```bash
# Build one distributable file
slidepack pack ./quarterly-review -o quarterly-review.html

# Open it. That is the whole distribution story.
open quarterly-review.html

# Check a presentation before you send it
slidepack validate ./quarterly-review

# See what is inside a packed file, without expanding it
slidepack inspect quarterly-review.html

# Get the source directory back
slidepack unpack quarterly-review.html -o ./restored
```

The tool explains itself: `slidepack help`, `slidepack help pack`, or
`--help` on any command.

## What is in the file

```
presentation.html
  bootstrap HTML      "Loading…" plus a <noscript> explanation
  JSON manifest       paths, sizes, SHA-256s, MIME types, modes
  ONE base64 payload  gzip(deterministic USTAR tar of the source)
  inline runtime      first-party, no third-party JavaScript
```

Opening the file, the runtime decodes the payload in slices, verifies its
SHA-256 with `crypto.subtle`, decompresses it with the browser's native
`DecompressionStream`, parses the tar into an in-memory map, mints `blob:` URLs,
rewrites the entrypoint's references to point at them, and renders the result in
a full-viewport `srcdoc` iframe.

Nothing is written to disk. No request leaves the machine. There is no `eval()`
and no `new Function()`.

There is **one** archive payload, not one base64 blob per asset — which is why a
deck full of images compresses like a directory instead of inflating by a third.

The whole thing is specified in [packed format v1](format-v1.html), in enough
detail to write an independent unpacker.

## Reproducible

Given the same source paths, bytes, permission bits and entrypoint, and the same
slidepack version, packing produces a **byte-identical** file.

```bash
slidepack pack fixture -o one.html
touch fixture/index.html
slidepack pack fixture -o two.html
cmp one.html two.html          # identical
```

Filesystem timestamps have no effect: tar uid, gid and mtime are fixed at zero,
uname and gname are empty, members are ordered byte-wise by path, the gzip header
carries no timestamp, and MIME types come from a built-in table rather than the
operating system's.

## Browsers

Firefox and Chromium are the primary targets, and both are exercised by an
automated end-to-end suite on every change. The suite opens a real packed file at
a real `file://` URL with the network blocked, and asserts that stylesheets
apply, scripts execute, images and SVGs load, CSS backgrounds resolve from a
nested stylesheet, a packaged `@font-face` font is genuinely applied, paths with
spaces and non-ASCII characters resolve, and keyboard and hash navigation work.

You need JavaScript enabled and a browser with `DecompressionStream` — Chrome 80,
Firefox 113, Safari 16.4, Edge 80, or later. A packed file that cannot load says
so in a readable panel; it never sits on "Loading" and never shows a blank page.

## What it will not do

Format v1 is a constrained static presentation, not a website archiver. It
rejects ES module graphs, import maps, `<base>`, service workers, local iframes,
and runtime resource loading such as `fetch("./data.json")` — resources exist
only as `blob:` URLs, so a source path cannot resolve at runtime.

Every one of these is reported at authoring time with a stable diagnostic code,
so you find out while writing rather than after distributing. The
[source format](source-format.html) covers each in turn, with what to do instead.

## Trust

> A slidepack presentation is HTML and JavaScript. Opening one executes the
> JavaScript it contains with the privileges a browser grants any local
> document. Only open presentations from sources you trust.

That is the same decision as opening any `.html` file someone sends you.
slidepack neither adds risk nor removes it.

The tooling itself never executes presentation code — `pack`, `validate`,
`inspect` and `unpack` tokenize HTML, scan CSS lexically and pattern-match
JavaScript, with no JavaScript engine involved. Extraction treats every archive
path as untrusted. The SHA-256 digests are integrity checks against corruption,
not signatures.
