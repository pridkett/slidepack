# The slidepack source format

This is the authoring contract: what a presentation directory may contain, how
references resolve, and which constructs format v1 deliberately refuses.

For the packed file format, see [format-v1.md](format-v1.md).

- [1. The shape of a presentation](#1-the-shape-of-a-presentation)
- [2. File names](#2-file-names)
- [3. Referencing resources](#3-referencing-resources)
- [4. What is supported](#4-what-is-supported)
- [5. What is not supported](#5-what-is-not-supported)
- [6. Self-containment](#6-self-containment)
- [7. Permissions and metadata](#7-permissions-and-metadata)
- [8. Validating your work](#8-validating-your-work)

---

## 1. The shape of a presentation

A presentation is an ordinary directory containing an HTML entry document.
That is the whole requirement. Everything else is convention.

```
quarterly-review/
├── index.html
├── css/
│   ├── deck.css
│   └── theme/
│       └── base.css
├── js/
│   └── deck.js
├── fonts/
│   ├── inter-regular.woff2
│   └── inter-bold.woff2
└── assets/
    ├── architecture.svg
    ├── chart.webp
    └── photo.jpg
```

The entry document defaults to `index.html`. Use another with `--entry`:

```bash
slidepack pack ./deck --entry slides.html -o deck.html
```

The entry document must be `.html` or `.htm`.

### Files that are never packed

A short, fixed list, so that whether a machine has looked at the directory in
Finder cannot change the packed output:

```
.DS_Store   Thumbs.db   desktop.ini   .git/   .hg/   .svn/
```

Everything else in the directory is packed, whether or not it is referenced.
Unreferenced files cost payload size but are otherwise harmless; the browser
runtime only creates a Blob for a file something actually asks for.

### Empty directories

Not preserved. The archive stores regular files only, so a directory with no
files in it does not survive a round trip. Put a file in it if it matters.

---

## 2. File names

Internal paths always use `/`, whatever the host OS.

**Supported:** spaces, non-ASCII UTF-8, nested directories, files with no
extension.

```
assets/Revenue Chart – Europe.webp
fonts/日本語.woff2
LICENSE
```

**Rejected:**

| Rejected | Why |
|---|---|
| A `\` in a name | It is a separator on Windows and a legal byte on Unix. |
| Control characters, NUL | Not safely representable. |
| `.` or `..` segments | Ambiguous, and an extraction hazard. |
| Absolute paths, `C:` prefixes | Not relative to the package. |
| Paths over 255 bytes | Beyond the USTAR field limits. |
| Very long single names | A name must fit 100 bytes and its prefix 155. |

The last one is worth restating: a path longer than 100 bytes must be
splittable at a `/` into a prefix of at most 155 bytes and a name of at most
100. `slidepack pack` reports `PATH_TOO_LONG` rather than silently emitting a
tar dialect the browser runtime cannot read.

### Symlinks and special files

Rejected. `slidepack pack` fails on a symbolic link, device node, FIFO or
socket anywhere in the tree.

Following a symlink could pull in data from outside the source directory;
recording one would make the archive's meaning depend on where it is
extracted. Neither is acceptable in a format whose point is that the file is
self-contained. Copy the file instead of linking it.

---

## 3. Referencing resources

References resolve **relative to the file that contains them** — the same rule
a browser applies.

```
index.html            →  href="css/deck.css"          →  css/deck.css
css/deck.css          →  url("../fonts/inter.woff2")  →  fonts/inter.woff2
css/theme/base.css    →  url(../../assets/bg.webp)    →  assets/bg.webp
```

### Root-relative paths

A leading `/` means the **package root**, not the filesystem root:

```html
<img src="/assets/logo.svg">
```

### Percent-encoding

Either spelling works, so write whichever your tooling produces:

```html
<img src="assets/Revenue Chart – Europe.webp">
<img src="assets/Revenue%20Chart%20%E2%80%93%20Europe.webp">
```

### Fragments and queries

A `#fragment` is preserved through packing, so `icons.svg#warning` keeps its
fragment on the resulting `blob:` URL.

A `?query` is dropped. Cache-busting suffixes are pointless in a packed file
and `blob:` URLs cannot carry a query string.

### References left alone

`data:`, `blob:`, `javascript:`, `mailto:`, `tel:`, `sms:`, `about:`, `geo:`,
`cid:`, and bare `#fragment` links are not package paths and pass through
untouched. A hand-authored `data:` URL survives exactly as written.

---

## 4. What is supported

### HTML

Every attribute below is resolved and rewritten:

```
script[src]                  link[href] (stylesheet, icon, preload, …)
img[src]                     img[srcset]
source[src]                  source[srcset]
video[src]                   video[poster]
audio[src]                   track[src]
object[data]                 embed[src]
input[src]                   body|table|td|th[background]
svg image[href]              svg image[xlink:href]
svg use[href]                svg use[xlink:href]
link[imagesrcset]
```

…plus the contents of `<style>` elements and every `style=""` attribute.

`srcset` is parsed by the HTML algorithm, so descriptors are preserved and
each candidate URL is rewritten individually:

```html
<img srcset="assets/chart.png 1x, assets/chart@2x.png 2x" src="assets/chart.png">
```

### CSS

```css
@import url("theme/base.css");          /* supported, resolved transitively */

@font-face {
  font-family: "Inter";
  src: url("../fonts/inter-regular.woff2") format("woff2");
}

.hero  { background-image: url("../assets/hero.webp"); }
.plain { background-image: url(../assets/hero.webp); }
.quote { background-image: url('../assets/hero.webp'); }
```

Quoted and unquoted forms both work. `@import` is fully supported to arbitrary
depth, with each stylesheet's references resolved relative to itself. A
circular `@import` is broken and reported to the console, as a browser does.

Strings and comments are respected, so these are correctly left alone:

```css
.decoy::after { content: "url(not-a-file.png)"; }
/* url(also-not-a-file.png) */
.data { background: url(data:image/svg+xml,%3Csvg…); }
```

### JavaScript

Ordinary classic scripts. Anything a browser will run from
`<script src="…">` without `type="module"`.

```html
<script src="js/deck.js"></script>
```

Your script runs inside the presentation frame with the DOM, keyboard events,
`localStorage`, `requestAnimationFrame`, the Fullscreen API and everything else
a normal page has.

### Fonts, images, media

`.woff`, `.woff2`, `.ttf`, `.otf`; `.png`, `.jpg`, `.gif`, `.webp`, `.avif`,
`.svg`; `.mp4`, `.webm`, `.mp3`, `.wav`, `.ogg`, `.vtt`. Any other binary file
referenced statically is packed too, with type
`application/octet-stream`.

### Navigation

Fragment navigation works exactly as on an ordinary page:

```html
<a href="#slide-12">Jump to the summary</a>
```

Clicking scrolls, `hashchange` fires, and `location.hash` reads back correctly.
Keyboard handlers work; the runtime focuses the presentation frame on load.

---

## 5. What is not supported

The validator reports each of these with a stable diagnostic code, so you find
out at authoring time rather than when someone opens the file.

### ES modules — `ES_MODULE`

```html
<script type="module" src="app.js"></script>   <!-- rejected -->
```

```js
import { render } from "./render.js";          // rejected
export default function () {}                  // rejected
```

Bundle the module graph into a single classic script before packing. Any
bundler in `--format=iife` mode does this.

### Import maps — `IMPORT_MAP`

```html
<script type="importmap">{ … }</script>        <!-- rejected -->
```

### `<base>` — `BASE_ELEMENT`

```html
<base href="https://example.com/deck/">        <!-- rejected -->
```

It makes resource resolution ambiguous, and the runtime needs the frame's base
URL for fragment navigation. Use paths relative to the document instead.

### Dynamic local resource loading — `DYNAMIC_LOCAL_FETCH`, `WEB_WORKER`

```js
fetch("./presentation.json")                   // rejected
new Worker("./worker.js")                      // rejected
importScripts("./lib.js")                      // rejected
```

Inside a packed file, resources exist only as `blob:` URLs minted at load
time. There is no origin at which `./presentation.json` is reachable, so a
runtime lookup of a source path cannot resolve.

Inline the data instead:

```html
<script id="deck-data" type="application/json">{"slides": [ … ]}</script>
<script src="js/deck.js"></script>
```

```js
var data = JSON.parse(document.getElementById("deck-data").textContent);
```

A `<script type="application/json">` block is inert data, not code, and the
validator does not scan it as JavaScript.

> **Detection is best-effort.** Static analysis of arbitrary JavaScript cannot
> be complete. The validator catches literal cases and warns about computed
> ones (`POSSIBLE_DYNAMIC_RESOURCE`), but the contract is on you: presentation
> JavaScript must not resolve package paths at runtime.

### Service workers — `SERVICE_WORKER`

```js
navigator.serviceWorker.register("sw.js");     // rejected
```

They cannot be registered from a `file://` document in any case.

### Local iframes — `LOCAL_IFRAME`

```html
<iframe src="appendix.html"></iframe>          <!-- rejected -->
```

An iframe pointing at a remote URL is allowed but will not load offline.

### Filesystem-relative navigation — `LOCAL_NAVIGATION_LINK` (warning)

```html
<a href="appendix.html">Appendix</a>           <!-- warning: will not navigate -->
```

Format v1 renders one entrypoint. Fold additional pages into the entry
document and link to them by fragment.

---

## 6. Self-containment

A packed presentation must render with no network at all. Any rendering
dependency on `http:` or `https:` is an error — `REMOTE_RESOURCE`:

```html
<script src="https://cdn.example.com/lib.js"></script>       <!-- rejected -->
<link rel="stylesheet" href="https://fonts.example/x.css">   <!-- rejected -->
<img src="https://example.com/photo.png">                    <!-- rejected -->
```

```css
.hero { background-image: url("https://images.example.com/hero.webp"); }  /* rejected */
```

Download the resource into the tree and reference it by path.

**Ordinary hyperlinks are fine.** They are not rendering dependencies:

```html
<a href="https://example.com/docs">Documentation</a>         <!-- allowed -->
```

A statically referenced local file that does not exist is also an error —
`MISSING_RESOURCE`. `slidepack pack` will not build a package it already knows
is broken.

---

## 7. Permissions and metadata

**Preserved:** the relative path, the exact bytes, and the Unix permission
bits (`mode & 0777`).

**Not preserved:** modification times, ownership, extended attributes,
resource forks, and empty directories. These are deliberately not part of the
canonical source representation — a presentation is its files, not the
filesystem state around them.

Two practical notes, both about permission bits being an *input* to
reproducible packing:

- **Git records only the executable bit.** If you want a tree to pack
  identically after a fresh clone, keep every file at `0644` or `0755`.
- **Windows has no POSIX permission bits.** Go reports `0666` for every
  writable file there and `0444` for a read-only one, and `os.Chmod` can only
  toggle between those two. slidepack therefore canonicalises what it records
  on such a filesystem: writable becomes `0644`, read-only stays `0444`. That
  keeps output byte-identical to the same tree packed on Linux or macOS, and
  stops a Windows-packed presentation from extracting world-writable
  elsewhere. What it cannot do is invent an executable bit that Windows never
  had, so a shell script packed on Windows records `0644`. Content and paths
  round-trip exactly on every platform.

---

## 8. Validating your work

```bash
slidepack validate ./quarterly-review
```

```
./quarterly-review: not a valid slidepack source

ERRORS (2)
  MISSING_RESOURCE         index.html:14
      img[src] references "assets/chart.webp", which resolves to
      "assets/chart.webp", but no such file exists in the source tree

  REMOTE_RESOURCE          css/deck.css:3
      @import loads "https://fonts.example.com/inter.css" over the network;
      a packed presentation must render offline, so add the resource to the
      source tree and reference it by path
```

For scripts and agents:

```bash
slidepack validate --json ./quarterly-review
```

```json
{
  "valid": false,
  "target": "./quarterly-review",
  "kind": "source",
  "errors": [
    { "code": "MISSING_RESOURCE", "path": "index.html", "line": 14,
      "detail": "img[src]", "message": "…" }
  ],
  "warnings": []
}
```

Exit `0` when valid, `3` when not. Add `--strict` to fail on warnings too.

Codes are stable and documented in
[format-v1.md §11](format-v1.md#11-diagnostic-codes).

`slidepack pack` runs the same validation first and refuses to write anything
if it fails, so a successful pack is also a successful validate.
