# slidepack format v1

This document specifies the packed file format completely enough to write an
independent unpacker. Everything here is normative unless marked otherwise.

- [1. Overview](#1-overview)
- [2. The HTML envelope](#2-the-html-envelope)
- [3. The manifest](#3-the-manifest)
- [4. The payload](#4-the-payload)
- [5. The TAR subset](#5-the-tar-subset)
- [6. Package paths](#6-package-paths)
- [7. MIME types](#7-mime-types)
- [8. Determinism](#8-determinism)
- [9. Reading a packed file](#9-reading-a-packed-file)
- [10. The browser runtime](#10-the-browser-runtime)
- [11. Diagnostic codes](#11-diagnostic-codes)
- [12. Security model](#12-security-model)
- [13. What v1 does not do](#13-what-v1-does-not-do)

---

## 1. Overview

A packed slidepack presentation is one ordinary `.html` file containing:

```
HTML bootstrap
  + JSON manifest
  + inline browser runtime
  + base64( gzip( deterministic USTAR tar of the source tree ) )
```

There is exactly **one** archive payload. Source assets are not embedded
individually; the entire source directory is a single compressed blob.

The file opens directly from `file://` with no server, no browser extension,
no companion directory and no network access. The runtime expands the archive
in memory, mints `blob:` URLs for the resources, rewrites the entrypoint's
references to point at them, and renders the result in a full-viewport
`srcdoc` iframe. Nothing is written to disk.

---

## 2. The HTML envelope

### 2.1 Marker

A packed file MUST contain this exact byte sequence:

```html
<!-- SLIDEPACK FORMAT 1 -->
```

A reader MUST check for the marker before anything else, so that an ordinary
HTML document produces "this is not a slidepack file" rather than a confusing
report about a missing element.

### 2.2 Structure

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="generator" content="slidepack format 1">
<title>PRESENTATION TITLE</title>
<!-- SLIDEPACK FORMAT 1 -->
<!-- (a human-readable notice explaining this is generated output) -->
<style id="slidepack-bootstrap-style">
  ... bootstrap CSS ...
</style>
</head>
<body>
<div id="slidepack-status">
  <p class="slidepack-loading">Loading presentation…</p>
  <noscript>... JavaScript-required message ...</noscript>
</div>
<script id="slidepack-manifest" type="application/json">
  ... manifest JSON ...
</script>
<script id="slidepack-payload"
        type="application/octet-stream"
        data-format="tar"
        data-compression="gzip"
        data-encoding="base64">
  ... base64, one unwrapped line ...
</script>
<script id="slidepack-runtime">
  ... browser runtime ...
</script>
</body>
</html>
```

### 2.3 Element identifiers

| id | Contents | Required |
|---|---|---|
| `slidepack-manifest` | Manifest JSON | yes |
| `slidepack-payload` | Base64 of the gzip stream | yes |
| `slidepack-runtime` | The browser runtime | yes for viewing, not for unpacking |
| `slidepack-status` | Loading / error surface | yes for viewing |
| `slidepack-bootstrap-style` | Bootstrap CSS | no |

The `slidepack-payload` element MUST carry `data-format="tar"`,
`data-compression="gzip"` and `data-encoding="base64"`. A reader SHOULD check
these against the manifest and report a mismatch.

### 2.4 Locating the elements without an HTML parser

A conforming reader can find both elements with substring searches, because
neither element's content can contain the sequence `</script`:

- The manifest is JSON produced with HTML escaping on, so `<`, `>` and `&` are
  emitted as `<`, `>` and `&`. A file named `</script>.png`
  therefore cannot terminate the element.
- The payload is base64, whose alphabet is `A-Za-z0-9+/=`.

The reference algorithm is:

1. Find `id="slidepack-manifest"` (also accept `'` quoting and unquoted form).
2. Scan backwards for the nearest `<script`. If a `>` occurs between that and
   the id, the id was not inside a start tag: reject.
3. Scan forward from the id to the next `>`. The element's content starts one
   byte later.
4. Scan forward to the next `</script`. That is the end of the content.
5. Trim surrounding ASCII whitespace.

This tolerates attribute reordering and extra attributes.

### 2.5 Title

The outer `<title>` is copied from the entrypoint's `<title>` at pack time, so
that a browser tab is labelled correctly before any JavaScript runs. It is
HTML-escaped. It is informational only; readers MUST NOT rely on it.

---

## 3. The manifest

### 3.1 Shape

```json
{
  "format": "slidepack",
  "version": 1,
  "generator": "slidepack/1.0.0",
  "entrypoint": "index.html",
  "payload": {
    "archive": "tar",
    "compression": "gzip",
    "encoding": "base64",
    "sha256": "0303c59d71974499696ae0bc6b00b336b80723b18c857e83e1470d95529c3473",
    "compressedSize": 6491,
    "archiveSize": 24064
  },
  "files": [
    {
      "path": "assets/Revenue Chart – Europe.webp",
      "size": 436,
      "sha256": "8e4d2b8463aff2d9358c234cbe5f3e3183258b4a9fabb89a133f1a2da8200959",
      "mime": "image/webp",
      "mode": "0644"
    }
  ]
}
```

### 3.2 Fields

| Field | Type | Meaning |
|---|---|---|
| `format` | string | Always `"slidepack"`. |
| `version` | number | Format version. This document specifies `1`. |
| `generator` | string | Producing tool and version. Informational. |
| `entrypoint` | string | Package path of the document to render. MUST appear in `files`. |
| `payload.archive` | string | Always `"tar"` in v1. |
| `payload.compression` | string | Always `"gzip"` in v1. |
| `payload.encoding` | string | Always `"base64"` in v1. |
| `payload.sha256` | string | Hex SHA-256 of the **gzip stream** — after base64 decoding, before decompressing. 64 lowercase hex characters. |
| `payload.compressedSize` | number | Length in bytes of the gzip stream. |
| `payload.archiveSize` | number | Length in bytes of the uncompressed tar stream. |
| `files[].path` | string | Canonical package path (see §6). |
| `files[].size` | number | File length in bytes. |
| `files[].sha256` | string | Hex SHA-256 of the file contents. |
| `files[].mime` | string | MIME type the runtime assigns to the resource's Blob. |
| `files[].mode` | string | Permission bits as four octal digits, e.g. `"0644"`. |

`files` MUST be sorted by `path`, ascending, **byte-wise** — not by any
locale-aware collation, which would make ordering depend on the machine.
Duplicate paths are invalid.

A reader MUST reject a `version` it does not implement, and MUST say so
distinctly rather than reporting the file as corrupt.

### 3.3 What the manifest deliberately omits

No build timestamps. No random identifiers. No host, user or path information.
Anything of that sort would defeat reproducible packing (§8).

### 3.4 On the digests

The SHA-256 values are **integrity checks, not signatures**. They detect
truncation, bit rot and casual editing. They prove nothing about who produced
the file: anyone who alters the payload can recompute them. slidepack has no
authenticity mechanism; use one layer down (signed distribution, checksums
published separately) if you need one.

---

## 4. The payload

### 4.1 Encoding

The payload element's text is standard base64 (RFC 4648 §4, `+` and `/`, `=`
padding) on a single unwrapped line, surrounded by a newline on each side.

A reader MUST trim surrounding ASCII whitespace. A reader SHOULD reject a
payload whose length is not a multiple of 4 before attempting to decode.

The line is deliberately not wrapped: an unwrapped string can be decoded in
fixed-size slices without first copying the whole thing to strip whitespace,
which matters for a presentation of tens of megabytes.

### 4.2 Compression

The decoded bytes are a gzip stream (RFC 1952) containing the tar archive.

Header fields are fixed for reproducibility: MTIME is `0`, the OS byte is
`255` ("unknown"), and no FNAME, FCOMMENT or FEXTRA field is present. The
reference packer uses deflate level 9.

A reader MUST NOT depend on the compression level; any valid gzip stream
decompresses correctly.

### 4.3 Verification order

A reader MUST verify in this order, stopping at the first failure:

1. base64 decodes
2. decoded length equals `payload.compressedSize` (if non-zero)
3. SHA-256 of the decoded bytes equals `payload.sha256`
4. gzip decompresses
5. decompressed length equals `payload.archiveSize` (if non-zero)
6. tar parses
7. archive contents agree with `files`
8. each file's SHA-256 matches

Step 4 MUST be bounded: decompress into at most `payload.archiveSize` bytes
(rejecting a declared size beyond a sane ceiling) rather than expanding first
and checking the length afterwards. See §12.

Checking the digest before decompressing means a truncated file reports
"integrity check failed" rather than a baffling gzip error.

---

## 5. The TAR subset

The archive is plain **USTAR** (POSIX.1-1988). It is deliberately a narrow
subset so that the browser runtime's reader can stay around a hundred lines
and be read in full by anyone auditing the format.

### 5.1 Header layout

Each record is a 512-byte header followed by the file contents padded with NUL
bytes to a 512-byte boundary.

| Offset | Size | Field | Value written by slidepack |
|---:|---:|---|---|
| 0 | 100 | name | Path, or its portion after the last prefix separator |
| 100 | 8 | mode | Permission bits, 7 octal digits + NUL |
| 108 | 8 | uid | `0000000\0` |
| 116 | 8 | gid | `0000000\0` |
| 124 | 12 | size | 11 octal digits + NUL |
| 136 | 12 | mtime | `00000000000\0` |
| 148 | 8 | chksum | 6 octal digits, NUL, space |
| 156 | 1 | typeflag | `'0'` (regular file) |
| 157 | 100 | linkname | all NUL |
| 257 | 6 | magic | `ustar\0` |
| 263 | 2 | version | `00` |
| 265 | 32 | uname | all NUL |
| 297 | 32 | gname | all NUL |
| 329 | 8 | devmajor | all NUL |
| 337 | 8 | devminor | all NUL |
| 345 | 155 | prefix | Leading path components, or all NUL |
| 500 | 12 | padding | all NUL |

The checksum is the unsigned sum of all 512 header bytes with the checksum
field itself treated as eight spaces. A reader SHOULD also accept the signed
interpretation, which some historical tars produce.

The archive ends with two 512-byte zero blocks. A reader MUST stop at the
first zero block and MUST NOT interpret trailing bytes.

### 5.2 Name and prefix

A path of 100 bytes or fewer goes entirely in `name`, with `prefix` all NUL.
A longer path is split at a `/` such that `prefix` is at most 155 bytes and
`name` at most 100. The reference packer chooses the split that makes `prefix`
as long as possible. A reader reconstructs the path as:

```
prefix ? prefix + "/" + name : name
```

If no valid split exists, the packer MUST fail with a clear error rather than
emitting a PAX or GNU long-name record, which the browser reader cannot parse.

### 5.3 UTF-8 names

Non-ASCII path bytes are stored **verbatim as UTF-8** in `name` and `prefix`.

This is the one place the format departs from what Go's `archive/tar` will
write: that package treats a non-ASCII name as ineligible for USTAR and
silently upgrades the record to PAX. Every mainstream tar implementation
stores UTF-8 in these fields, and GNU tar and bsdtar both read slidepack
archives correctly, so the reference packer writes the headers directly.

### 5.4 Entry types

Only typeflag `'0'` (regular file) and `'\0'` (historical regular file) carry
content. Typeflag `'5'` (directory) MAY appear and MUST be skipped; the
reference packer does not emit directory entries, so **empty directories are
not preserved**.

Every other typeflag — symlink `'2'`, hardlink `'1'`, character device `'3'`,
block device `'4'`, FIFO `'6'`, contiguous `'7'`, and all extension types — is
invalid in v1 and MUST cause a reader to fail. A slidepack archive is only
ever produced by a slidepack packer, so a deviation means corruption or
tampering, and reporting it is more useful than skipping it.

### 5.5 Ordering

Entries appear in ascending byte-wise path order, matching the manifest.

---

## 6. Package paths

A **package path** is the name a file has inside the archive and the manifest.

A valid package path:

- is valid UTF-8;
- contains no NUL and no other control character (`< 0x20`, `0x7F`);
- uses `/` separators only — a backslash is rejected outright, because it is a
  separator on Windows and a legal filename byte on Unix;
- is relative: no leading `/`, no `//` UNC prefix, no `C:` drive letter;
- contains no `.` segment, no `..` segment, and no empty segment;
- is already in cleaned form;
- has no trailing `/`;
- is at most 255 bytes.

Readers MUST validate every archive path against these rules **before** using
it to build a filesystem path, and MUST additionally confirm that the resolved
destination lies inside the extraction root.

### 6.1 Reference resolution

Within a presentation, a reference is resolved against the file that contains
it:

| Reference in | Resolves against |
|---|---|
| `index.html` | the package root |
| `css/deck.css` | `css/` |
| `css/theme/base.css` | `css/theme/` |
| an inline `<style>` or `style=""` | the containing document |

A reference beginning with `/` is resolved against the **package root**, not
the host filesystem, and excess `..` segments clamp at the root — the same
behaviour a browser gives `/../x`.

A relative reference that climbs above the package root is an error.

References are percent-decoded before lookup, so
`assets/Revenue%20Chart%20%E2%80%93%20Europe.webp` finds
`assets/Revenue Chart – Europe.webp`. If the decoded form does not exist, the
undecoded spelling is tried, for the unusual tree whose file names contain a
literal `%`.

A `?query` is dropped. A `#fragment` is preserved and re-appended to the
resulting `blob:` URL, so `icons.svg#warning` keeps its fragment.

These reference forms are never package paths and are left untouched:
`data:`, `blob:`, `javascript:`, `mailto:`, `tel:`, `sms:`, `about:`, `geo:`,
`cid:`, and a bare `#fragment`.

---

## 7. MIME types

Types come from the manifest. A reader without a manifest entry falls back to
a fixed extension table; unknown extensions are `application/octet-stream`.

The table is deliberately built in rather than taken from the operating
system, so that packing the same tree on macOS, Linux and Windows produces
identical output.

| Extension | Type |
|---|---|
| `.html` `.htm` | `text/html; charset=utf-8` |
| `.css` | `text/css; charset=utf-8` |
| `.js` `.mjs` | `text/javascript; charset=utf-8` |
| `.json` `.map` | `application/json` |
| `.svg` | `image/svg+xml` |
| `.png` | `image/png` |
| `.jpg` `.jpeg` | `image/jpeg` |
| `.gif` | `image/gif` |
| `.webp` | `image/webp` |
| `.avif` | `image/avif` |
| `.woff` | `font/woff` |
| `.woff2` | `font/woff2` |
| `.ttf` | `font/ttf` |
| `.otf` | `font/otf` |
| `.mp4` `.m4v` | `video/mp4` |
| `.webm` | `video/webm` |
| `.mp3` | `audio/mpeg` |
| `.wav` | `audio/wav` |
| `.ogg` `.oga` | `audio/ogg` |
| `.vtt` | `text/vtt; charset=utf-8` |
| anything else | `application/octet-stream` |

Getting these right is load-bearing, not cosmetic: Firefox refuses a
stylesheet that is not served as `text/css` when the document is in standards
mode, and a script needs a JavaScript type.

---

## 8. Determinism

Given the same source paths, the same source bytes, the same permission bits,
the same entrypoint and the same slidepack version, packing produces a
**byte-for-byte identical** file.

This is achieved by:

- fixing tar `uid`, `gid`, `mtime` to `0` and leaving `uname`/`gname` empty;
- ordering archive members and manifest entries by byte-wise path;
- fixing the gzip MTIME to `0` and the OS byte to `255`;
- pinning the deflate level;
- using a built-in MIME table rather than the OS database;
- putting no timestamp or generated identifier in the manifest.

Filesystem modification times have no effect on output.

Two caveats worth knowing:

- **Permission bits are an input.** Git records only the executable bit, so a
  tree cloned on two machines may pack differently if it contains modes other
  than `0644`/`0755`. Prefer those two.
- **The generator string is in the manifest.** Output changes across slidepack
  versions by design.

---

## 9. Reading a packed file

A minimal independent unpacker:

```
1.  Read the whole file as bytes.
2.  Require the marker "<!-- SLIDEPACK FORMAT 1 -->".
3.  Extract the manifest element's text (§2.4); parse it as JSON.
4.  Require format == "slidepack".
5.  Require version == 1, else report an unsupported version.
6.  Extract the payload element's text (§2.4); trim whitespace.
7.  base64-decode it.
8.  Compare its SHA-256 with payload.sha256.
9.  gzip-decompress.
10. Parse the USTAR records (§5).
11. Validate every path (§6) BEFORE creating any file.
12. Compare the archive against manifest.files: same set, same sizes,
    same digests.
13. Write each file, resolving its destination against the extraction root
    and confirming the result stays inside it.
14. Apply the recorded permission bits.
```

Never write a file before step 11 has passed for the whole archive.

---

## 10. The browser runtime

The runtime is first-party JavaScript with no third-party dependencies. It
uses no `eval()` and no `new Function()`, and defines a single global,
`window.slidepack`, a frozen object carrying the format version and a
diagnostics record.

### 10.1 Sequence

1. Parse the manifest; reject an unknown version.
2. Decode the base64 in 256 KiB slices into a preallocated `Uint8Array`.
3. Verify the payload SHA-256 with `crypto.subtle.digest`. (`file://` is a
   secure context in Chrome and Firefox, so SubtleCrypto is available; if it
   is not, the check is skipped with a console warning rather than failing.)
4. Decompress with `new DecompressionStream("gzip")`.
5. Parse the tar into an in-memory map of `path -> { bytes, mime }`.
6. Parse the entrypoint with `DOMParser`, which builds a tree without running
   a script or fetching a subresource.
7. Rewrite every package-local reference to a `blob:` URL, creating blobs
   lazily so unreferenced files are never duplicated in memory.
8. Serialize and mount the result in a `srcdoc` iframe.

### 10.2 What gets rewritten

`script[src]`, `link[href]` (for resource `rel` values), `img[src]`,
`img[srcset]`, `source[src]`, `source[srcset]`, `link[imagesrcset]`,
`video[src]`, `video[poster]`, `audio[src]`, `track[src]`, `object[data]`,
`embed[src]`, `input[src]`, `body/table/td/th[background]`, SVG `image[href]`
and `image[xlink:href]`, SVG `use[href]` and `use[xlink:href]`, the contents of
`<style>`, and every `style=""` attribute.

Stylesheets are rewritten transitively: a `.css` file's own `url()` and
`@import` references are resolved relative to that stylesheet before its blob
is created, so `@import` works to arbitrary depth. A circular `@import` is
detected, left unrewritten and reported to the console — the same thing a
browser does with a recursive import.

CSS is scanned with a three-state tokenizer (default / string / comment), not
a regular expression, so `content: "url(x)"`, `/* url(x) */` and
`url(data:...)` are correctly left alone.

### 10.3 The injected `<base>`

The runtime inserts `<base href="about:srcdoc">` as the first child of the
frame document's `<head>`.

This is not cosmetic. A `srcdoc` document inherits its base URL from the
parent, so `<a href="#slide-3">` would otherwise resolve against the packed
file's own `file://` URL; following such a link navigates the frame to the
packed file and reloads the entire presentation from the top. Anchoring the
base at `about:srcdoc` makes `#x` a same-document fragment navigation again,
so hash links scroll and fire `hashchange` exactly as on an ordinary page.

It is safe only because every package-local reference has already become an
absolute `blob:` URL by that point; no relative resource URL remains for the
base to affect. Any author-supplied `<base>` is removed first.

### 10.4 Failure surface

Before JavaScript runs, the document shows "Loading presentation…" and a
`<noscript>` explanation. On failure the runtime replaces that with a visible
panel:

```
This presentation could not be loaded.

Reason
<one-line reason>

This is a slidepack format v1 presentation. …
```

The full error and the stage it failed at go to the console; the panel shows
only the reason, because a stack trace tells the person looking at a broken
slide deck nothing they can act on. A packed file MUST never present a blank
page or an indefinite loading state.

---

## 11. Diagnostic codes

These codes are a stable public contract. Adding a code is a compatible
change; changing what an existing one means is not. They appear in
`slidepack validate --json` under `errors[].code` and `warnings[].code`.

### Source structure

| Code | Meaning |
|---|---|
| `MISSING_ENTRYPOINT` | The entry document does not exist in the tree. |
| `INVALID_ENTRYPOINT` | The entry document is not `.html`/`.htm`. |
| `EMPTY_SOURCE` | The source directory contains no files. |
| `INVALID_PATH` | A path violates §6. |
| `PATH_TOO_LONG` | A path does not fit a USTAR header (§5.2). |
| `UNREADABLE_FILE` | A file could not be read. |

### Resource resolution

| Code | Meaning |
|---|---|
| `MISSING_RESOURCE` | A statically referenced local file does not exist. |
| `REMOTE_RESOURCE` | A rendering dependency is loaded over the network. |
| `ESCAPING_REFERENCE` | A reference resolves outside the package root. |

### Unsupported constructs

| Code | Severity | Meaning |
|---|---|---|
| `ES_MODULE` | error | `<script type="module">`, `modulepreload`, or module syntax. |
| `IMPORT_MAP` | error | `<script type="importmap">`. |
| `BASE_ELEMENT` | error | A `<base>` element. |
| `SERVICE_WORKER` | error | Service worker registration. |
| `LOCAL_IFRAME` | error | An `<iframe>`/`<frame>` whose `src` is a package path. |
| `DYNAMIC_LOCAL_FETCH` | error | `fetch()` of a literal package-local path. |
| `WEB_WORKER` | error | `new Worker(...)`, `new SharedWorker(...)`, `importScripts(...)`. |
| `DYNAMIC_IMPORT` | warning | `import(...)`. |
| `LOCAL_NAVIGATION_LINK` | warning | `<a>` to a package-local HTML document. |
| `META_REFRESH` | warning | `<meta http-equiv="refresh">`. |
| `POSSIBLE_DYNAMIC_RESOURCE` | warning | `XMLHttpRequest`, `WebSocket`, `EventSource`, `fetch()` with a computed URL. |

### Filesystem object types

| Code | Meaning |
|---|---|
| `SYMLINK` | The source tree contains a symbolic link. |
| `SPECIAL_FILE` | The source tree contains a device, FIFO or socket. |

### Packed documents

| Code | Meaning |
|---|---|
| `NOT_SLIDEPACK` | No format marker, or no manifest element. |
| `MALFORMED_ENVELOPE` | The envelope structure or payload attributes are wrong. |
| `MALFORMED_MANIFEST` | The manifest is not valid or violates §3. |
| `UNSUPPORTED_VERSION` | A format version this build cannot read. |
| `CORRUPT_BASE64` | The payload is not valid base64. |
| `PAYLOAD_HASH_MISMATCH` | The payload digest or length does not match. |
| `CORRUPT_GZIP` | The payload is not a decompressible gzip stream. |
| `CORRUPT_TAR` | The archive structure is invalid. |
| `MANIFEST_MISMATCH` | The manifest and the archive describe different trees. |
| `FILE_HASH_MISMATCH` | A file's digest does not match the manifest. |

### JSON shape

```json
{
  "valid": false,
  "target": "deck.html",
  "kind": "package",
  "errors": [
    {
      "code": "REMOTE_RESOURCE",
      "path": "index.html",
      "line": 7,
      "detail": "img[src]",
      "message": "…"
    }
  ],
  "warnings": []
}
```

`errors` and `warnings` are always arrays, never `null`. `path`, `line` and
`detail` are omitted when they do not apply. Diagnostics are sorted by path,
then line, then code, so output is stable across runs.

---

## 12. Security model

> A slidepack presentation is HTML and JavaScript. Opening one executes the
> JavaScript contained in that presentation with the privileges browsers
> normally grant a local HTML document. **Only open presentations from sources
> you trust.**

The tooling itself never executes presentation code. `pack`, `validate`,
`inspect` and `unpack` tokenize HTML, scan CSS lexically and pattern-match
JavaScript; no JavaScript engine is involved at any point. Only a browser
viewing the presentation runs presentation code.

Archive extraction treats every package path as untrusted:

- paths are validated against §6 before anything is written;
- destinations are resolved against the extraction root and re-checked for
  containment;
- each directory component is created individually and rejected if it is a
  symbolic link, because `MkdirAll` would happily traverse one;
- a file is never written through a symbolic link;
- when the destination does not exist, files are built in a staging directory
  and renamed into place, so a failure leaves nothing behind.

A reader MUST also bound decompression. A few hundred kilobytes of gzip can
expand to gigabytes, so the reference unpacker limits the expansion to the
`archiveSize` the manifest declares — and rejects a declared size above 2 GiB
outright — rather than decompressing first and checking afterwards.

One residual limitation, stated plainly: extracting into a directory that
already exists (`--force`) is not immune to a local attacker who can modify
that directory concurrently, since a component verified as a real directory
could be replaced with a symbolic link before the file is opened. The staging
path used for a fresh destination is not exposed to this, because the staging
directory is created with a fresh random name. Do not extract into a directory
that untrusted local processes can write to.

The SHA-256 digests are integrity checks, not signatures (§3.4).

---

## 13. What v1 does not do

Out of scope by design, not by omission:

- **ES module graphs.** Bundle to a classic script before packing.
- **Import maps.**
- **Dynamic local resource loading** — `fetch()`, `XMLHttpRequest`, `Worker`
  against package paths. Resources exist only as `blob:` URLs, so a runtime
  lookup of a source path cannot resolve. Inline the data instead.
- **Service workers.** They cannot be registered from `file://` anyway.
- **Local iframes** embedding another packaged document.
- **`<base>`**, which makes resource resolution ambiguous.
- **Filesystem-relative page navigation.** `<a href="appendix.html">` will not
  navigate; `<a href="#slide-12">` works normally.
- **Empty directories**, which are not represented in the archive.
- **mtimes and ownership**, which are deliberately not part of the canonical
  source representation.
