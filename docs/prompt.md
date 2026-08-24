# Build `slidepack`: a Reversible Self-Contained HTML Presentation Packer

You are implementing a production-quality command-line tool called **`slidepack`**.

Do not merely scaffold this project. Implement it completely, test it, exercise it in real Firefox and Chromium browsers, fix failures, and continue iterating until the acceptance criteria at the end of this specification pass.

This task is intended to be suitable for an autonomous/Ralph-style development loop. Treat the acceptance criteria as the definition of done.

---

# 1. Problem

An LLM-based presentation-generation system currently creates presentations as giant self-contained HTML files.

Those HTML files contain:

* HTML
* CSS
* JavaScript
* SVGs
* PNG/JPEG/WebP images
* fonts
* other static resources

Resources are generally embedded directly using data URLs and base64.

This is convenient for distribution because the recipient receives exactly one `.html` file and can double-click it to view the presentation.

It is terrible as an authoring format:

* individual assets are inaccessible
* diffs are nearly useless
* LLM agents must edit huge HTML documents
* replacing an image is awkward
* CSS and JavaScript cannot conveniently be manipulated as ordinary files
* generated base64 consumes enormous context
* source control becomes painful

We want to separate the **authoring representation** from the **distribution representation**.

The canonical presentation should be an ordinary directory tree.

For example:

```text
my-presentation/
├── index.html
├── css/
│   └── presentation.css
├── js/
│   └── presentation.js
├── fonts/
│   ├── inter-regular.woff2
│   └── inter-bold.woff2
└── assets/
    ├── architecture.svg
    ├── chart.webp
    └── photo.jpg
```

The user should be able to run:

```bash
slidepack pack my-presentation -o my-presentation.html
```

and receive:

```text
my-presentation.html
```

The resulting file MUST:

1. be a single ordinary `.html` file;
2. open directly from the local filesystem;
3. require no extraction;
4. require no browser extension;
5. require no HTTP server;
6. require no network connection;
7. work in current Firefox;
8. work in current Chromium/Google Chrome;
9. contain all presentation files;
10. faithfully display the presentation.

It must also be reversible:

```bash
slidepack unpack my-presentation.html -o restored
```

must reproduce the original source tree.

The critical architectural principle is:

> **The directory is source. The HTML file is a compiled distribution artifact.**

LLM agents should work on the directory, never on the packed representation.

---

# 2. Scope

Implement a Go command-line application named:

```text
slidepack
```

The initial release is **format version 1**.

Commands required:

```text
slidepack pack
slidepack unpack
slidepack validate
slidepack inspect
slidepack version
```

Provide normal `--help` support at the root and command level.

The final binary must not require Node.js, Python, npm, a browser extension, or another runtime.

Node.js and Playwright MAY be used for development-only browser tests.

Prefer the Go standard library. A small Go dependency is acceptable where there is a compelling reason, but avoid building a dependency-heavy CLI.

---

# 3. Explicit Architectural Decision

For format v1, DO NOT implement:

* MHTML
* MAFF
* WARC
* Web Bundles
* a SingleFile-compatible format
* an HTML/ZIP polyglot
* a service-worker-based filesystem
* an HTTP server
* a custom browser extension

Do not spend implementation cycles debating these alternatives.

Format v1 will use:

```text
HTML bootstrap
    +
base64(
    gzip(
        deterministic TAR of presentation source
    )
)
```

There must be **one archive payload**, not a separate base64 block for every source asset.

Conceptually:

```text
presentation.html
┌──────────────────────────────────────┐
│ small static bootstrap HTML          │
│                                      │
│ format manifest                      │
│                                      │
│ browser loader/runtime               │
│                                      │
│ one base64-encoded .tar.gz payload   │
│                                      │
└──────────────────────────────────────┘
```

The browser runtime expands the archive **in memory only**.

It MUST NOT write files to disk.

---

# 4. Source Directory Contract

A valid presentation source directory contains an HTML entry point.

Default:

```text
index.html
```

Allow another entry point via:

```bash
slidepack pack deck --entry slides.html
```

Internal paths always use `/` regardless of host OS.

Supported source resources include:

* `.html`
* `.css`
* classic `.js`
* `.svg`
* `.png`
* `.jpg`
* `.jpeg`
* `.gif`
* `.webp`
* `.avif`
* `.woff`
* `.woff2`
* `.ttf`
* `.otf`
* `.json`
* audio/video resources that browsers can normally render
* arbitrary additional binary resources when referenced statically

The following ordinary patterns should work:

```html
<link rel="stylesheet" href="css/presentation.css">
<script src="js/presentation.js"></script>
<img src="assets/chart.webp">
<img src="assets/diagram.svg">
```

CSS:

```css
@font-face {
    font-family: "Presentation Font";
    src: url("../fonts/font.woff2") format("woff2");
}

.hero {
    background-image: url("../assets/hero.webp");
}
```

Paths containing spaces and Unicode characters MUST work.

For example:

```text
assets/Revenue Chart – Europe.webp
```

and:

```html
<img src="assets/Revenue%20Chart%20%E2%80%93%20Europe.webp">
```

must resolve correctly.

Root-relative package paths should also work:

```html
<img src="/assets/logo.svg">
```

---

# 5. Intentionally Unsupported Features in Format v1

Do not attempt to turn this into an arbitrary web-site archiver.

The source format is intentionally a constrained static presentation.

The validator must detect and clearly report known unsupported constructs.

Format v1 does NOT need to support:

### JavaScript module graphs

For example:

```html
<script type="module" src="app.js"></script>
```

or local ES module imports.

Reject these with an actionable validation error.

Presentation-generation systems should bundle JavaScript into classic browser scripts before packing.

### Dynamic local resource loading

For example:

```javascript
fetch("./presentation.json")
new Worker("./worker.js")
```

or runtime XHR against local files.

These are outside the v1 resource model.

Static detection in arbitrary JavaScript cannot be perfect. The validator should detect obvious cases and report warnings or errors, but document that generated presentation JavaScript must not dynamically resolve local package paths.

### Service workers

Reject or flag them.

### Local iframes

A presentation that embeds another packaged HTML file through a local iframe is outside v1.

### `<base>`

Reject `<base>` elements because they make resource resolution ambiguous.

### Import maps

Reject them.

### Local CSS `@import`

For v1, either:

1. correctly support it, including path resolution relative to the imported stylesheet; OR
2. reject it with a clear validation error.

Do not silently produce a broken presentation.

Option 1 is preferred if it can be implemented cleanly without compromising reliability, but it is not required for v1.

### Arbitrary filesystem-relative navigation

Links such as:

```html
<a href="appendix.html">
```

do not need to work as page navigation in v1.

Hash links such as:

```html
<a href="#slide-12">
```

must work normally.

---

# 6. Self-Containment

A packed presentation must not depend on network resources for rendering.

These should cause validation errors when used as loaded resources:

```html
<script src="https://example.com/foo.js">
<link rel="stylesheet" href="https://example.com/foo.css">
<img src="https://example.com/foo.png">
```

Likewise remote resources referenced from CSS must be reported.

External hyperlinks are fine:

```html
<a href="https://example.com">Documentation</a>
```

They do not constitute a rendering dependency.

Existing `data:` resources may remain unchanged.

The browser end-to-end test must verify that presentation rendering succeeds with network access unavailable.

---

# 7. Format v1 Envelope

Create a stable and documented envelope.

The exact formatting is up to the implementation, but use unambiguous markers and IDs analogous to:

```html
<!-- SLIDEPACK FORMAT 1 -->

<script
  id="slidepack-manifest"
  type="application/json">
...
</script>

<script
  id="slidepack-payload"
  type="application/octet-stream"
  data-format="tar"
  data-compression="gzip"
  data-encoding="base64">
...
</script>
```

The output must also contain the inline loader/runtime.

The format must be easy for `slidepack unpack` to identify without trying to interpret arbitrary HTML heuristically.

Document the format in:

```text
docs/format-v1.md
```

---

# 8. Manifest

Include a machine-readable manifest in the packed HTML.

At minimum it should identify:

```json
{
  "format": "slidepack",
  "version": 1,
  "entrypoint": "index.html",
  "payload": {
    "archive": "tar",
    "compression": "gzip",
    "encoding": "base64",
    "sha256": "..."
  },
  "files": []
}
```

For each file, record enough information to inspect and validate the package, including:

* normalized path
* size
* SHA-256
* MIME type
* Unix file mode where meaningful

For example:

```json
{
  "path": "assets/chart.webp",
  "size": 184221,
  "sha256": "...",
  "mime": "image/webp",
  "mode": "0644"
}
```

Entries MUST be sorted by canonical path.

Do not include build timestamps or random IDs that destroy reproducibility.

The SHA-256 values provide **integrity checking, not cryptographic authenticity**. Do not describe them as signatures.

---

# 9. Deterministic Packing

Packing must be deterministic.

Given:

* the same source paths;
* the same source bytes;
* the same relevant file modes;
* the same entrypoint;
* the same slidepack binary/version;

the resulting HTML must be byte-for-byte identical.

Filesystem mtimes MUST NOT influence output.

Archive member order MUST be deterministic.

Normalize TAR metadata:

* deterministic timestamps;
* deterministic uid/gid;
* deterministic uname/gname;
* deterministic ordering;
* no host-specific absolute paths.

Configure the gzip header deterministically as well.

This must pass:

```bash
slidepack pack fixture -o one.html
sleep 2
touch fixture/index.html
slidepack pack fixture -o two.html

cmp one.html two.html
```

assuming only the mtime changed.

---

# 10. TAR Restrictions

Use a simple TAR subset so the browser parser can remain small and auditable.

The packer should generate ordinary USTAR-compatible records.

Format v1 source trees contain:

* directories
* regular files

Reject:

* symlinks
* hardlinks
* sockets
* devices
* FIFOs
* other special filesystem objects

It is acceptable to omit explicit directory entries and archive regular files only.

If USTAR path-length constraints are exceeded, report a clear error rather than silently emitting another TAR dialect that the browser runtime does not understand.

Support:

* spaces
* non-ASCII UTF-8 filenames
* nested directories

Reject pathnames containing unsafe/control characters where appropriate.

---

# 11. Browser Loader

The output HTML must contain a small first-party JavaScript runtime.

Do not fetch runtime libraries from the internet.

Prefer no third-party JavaScript dependencies.

Use the browser's native:

```javascript
new DecompressionStream("gzip")
```

to decompress the payload.

Implement the small TAR reader directly.

The TAR reader should:

* parse the USTAR records emitted by slidepack;
* validate headers sufficiently to detect obvious corruption;
* combine prefix/name correctly;
* recognize regular files;
* respect file sizes and 512-byte alignment;
* stop at the TAR terminator;
* reject malformed or unexpected entries.

The runtime should expose no unnecessary globals.

Avoid `eval()` and `new Function()`.

---

# 12. Base64 Decoding

Do not assume the payload is tiny.

Avoid unnecessarily duplicating very large base64 strings in memory.

A chunked decoder is preferred over a single enormous:

```javascript
atob(payload)
```

if the latter would create avoidable memory spikes.

The generated HTML may contain presentations tens of megabytes in size.

Correctness matters more than micro-optimization, but avoid obviously pathological memory handling.

---

# 13. In-Memory Virtual Filesystem

After decompression, construct an in-memory representation:

```text
path -> bytes + MIME type
```

MIME types should come from the manifest or a deterministic MIME table.

Ensure correct MIME types for at least:

* HTML
* CSS
* JavaScript
* SVG
* PNG
* JPEG
* WebP
* AVIF
* WOFF
* WOFF2
* TTF
* OTF
* JSON

Firefox can be stricter than Chromium about stylesheet/script MIME types, so test this.

---

# 14. Rendering Strategy

The packed presentation must render from `file://`.

Do not depend on browser `fetch()` access to the containing HTML file.

Recommended strategy:

1. decode the archive payload;
2. decompress it;
3. parse TAR into the in-memory VFS;
4. load the entrypoint HTML as text;
5. parse it using `DOMParser`;
6. create Blob URLs for packaged resources;
7. rewrite package-local resource references to their Blob URLs;
8. serialize the rewritten entrypoint;
9. render it inside a full-viewport `iframe` using `srcdoc`;
10. focus the presentation frame after load.

The outer bootstrap should occupy the entire viewport and the presentation iframe should behave visually like the original page:

```text
width: 100vw
height: 100vh
border: 0
margin: 0
```

The source presentation's JavaScript should execute inside the frame.

Do not sandbox the iframe in a way that breaks ordinary presentation JavaScript.

Allow fullscreen behavior if reasonably possible.

Set the outer document title from the presentation's `<title>`.

---

# 15. Resource Rewriting

Implement correct path resolution.

HTML resources to handle should include at minimum:

* `script[src]`
* `link[href]` for stylesheets/icons
* `img[src]`
* `img[srcset]`
* `source[src]`
* `source[srcset]`
* `video[src]`
* `video[poster]`
* `audio[src]`
* `track[src]`
* `object[data]`
* `embed[src]`
* SVG `image[href]`
* SVG `image[xlink:href]`
* ordinary CSS in `<style>`
* CSS in `style=""`

CSS rewriting must handle local:

```css
url(...)
```

including quoted and unquoted normal CSS URL forms.

Do not implement CSS rewriting as a dangerously broad regex that corrupts quoted strings or data URLs. Use a small tokenizer/state machine or another robust method.

Resource paths must resolve relative to the file containing the reference.

Thus:

```text
css/presentation.css
```

containing:

```css
url("../assets/background.webp")
```

must resolve to:

```text
assets/background.webp
```

Fragments must survive resolution, particularly SVG sprite references:

```text
icons.svg#warning
```

Existing:

```text
data:
blob:
#
mailto:
tel:
javascript:
```

URLs should not be mistaken for package paths.

Remote rendering resources should already have failed validation.

If a referenced local file does not exist, validation and `pack` must fail rather than creating a knowingly broken package.

---

# 16. Loader User Experience

A packed file must never silently display a blank white page when unpacking fails.

Before JavaScript runs, the outer HTML should contain a minimal loading state such as:

```text
Loading presentation…
```

Include a `<noscript>` message explaining that JavaScript is required.

If loading fails, replace the loading state with a concise visible error panel:

```text
This presentation could not be loaded.

Reason:
<useful error>

This is a slidepack format v1 presentation.
```

Do not expose a giant JavaScript stack trace by default.

Logging diagnostic details to the console is fine.

Corrupt payloads must therefore produce:

* a visible user-facing failure;
* a useful console diagnostic;
* no infinite loading indicator.

---

# 17. `slidepack pack`

Primary syntax:

```bash
slidepack pack <directory> -o <presentation.html>
```

Options should include at least:

```text
-o, --output
--entry
--force
```

Behavior:

* validate before packing;
* fail if entrypoint does not exist;
* fail on known unsupported constructs;
* fail on missing local resources;
* fail on remote rendering dependencies;
* fail if output already exists unless `--force`;
* reject an output path inside the source directory rather than accidentally archiving the output;
* create parent output directories where sensible;
* return nonzero on error;
* never leave a partially written output on failure.

Write to a temporary file and atomically rename where practical.

---

# 18. `slidepack unpack`

Syntax:

```bash
slidepack unpack <presentation.html> -o <directory>
```

Options:

```text
-o, --output
--force
```

The unpacker must:

1. recognize slidepack format v1;
2. locate manifest and payload;
3. base64-decode payload;
4. verify payload SHA-256;
5. gzip-decompress;
6. read TAR;
7. validate file paths before writing anything;
8. verify individual file hashes;
9. restore source paths and bytes;
10. restore reasonable file modes on platforms where this is meaningful.

Security is important.

Protect against:

* `../`
* absolute paths
* Windows drive paths
* path separator tricks
* NULs
* archive traversal
* writing through symlinks in the destination

Never allow a malicious packed file to write outside the requested destination.

If destination exists and is non-empty, fail unless explicitly allowed by `--force`.

Prefer extracting into a temporary directory and moving it into place when the destination does not already exist.

---

# 19. Round-Trip Definition

The following must be true:

```text
unpack(pack(source))
```

reproduces:

* every regular file;
* every relative path;
* exact file bytes;
* relevant Unix mode bits where supported.

It does NOT need to reproduce:

* source mtimes;
* directory mtimes;
* uid/gid ownership.

Those are deliberately not part of the canonical source representation.

---

# 20. `slidepack validate`

Support both:

```bash
slidepack validate ./source-directory
```

and:

```bash
slidepack validate presentation.html
```

Directory validation should check:

* entrypoint exists;
* path validity;
* filesystem object types;
* missing resources;
* remote loaded resources;
* unsupported HTML constructs;
* known unsupported CSS constructs;
* obvious unsupported JavaScript resource-loading patterns;
* entrypoint validity.

Packed-file validation should additionally check:

* envelope;
* manifest;
* supported format version;
* base64;
* payload SHA-256;
* gzip;
* TAR structure;
* manifest/archive agreement;
* individual file SHA-256 hashes;
* source-level validation after extraction into memory.

Provide:

```bash
slidepack validate --json ...
```

for machine-readable output.

JSON should have stable fields suitable for an LLM agent, e.g.:

```json
{
  "valid": false,
  "errors": [
    {
      "code": "REMOTE_RESOURCE",
      "path": "index.html",
      "message": "..."
    }
  ],
  "warnings": []
}
```

Error codes should be stable and documented.

---

# 21. `slidepack inspect`

Example:

```bash
slidepack inspect presentation.html
```

Human-readable output should include:

* format/version;
* entrypoint;
* number of files;
* compressed payload size;
* uncompressed archive size if available;
* source-content size;
* payload SHA-256;
* file listing.

Support:

```bash
slidepack inspect --json presentation.html
```

Do not extract files merely to provide inspection output.

---

# 22. `slidepack version`

Implement:

```bash
slidepack version
```

and:

```bash
slidepack --version
```

Use build-time version injection if convenient, with a sensible development fallback.

---

# 23. Exit Behavior

Use consistent exit semantics.

At minimum:

* `0`: success / valid
* nonzero: validation, operational, corruption, or usage failure

Human-facing errors go to stderr.

Machine-readable `--json` output should remain parseable and should not be polluted by logging.

---

# 24. MIME Handling

Use deterministic MIME detection.

Do not depend solely on the operating system's MIME database.

Provide explicit mappings for common presentation formats so results are portable across macOS, Linux, and Windows.

Unknown resources may use:

```text
application/octet-stream
```

---

# 25. Security Model

Document clearly:

> A slidepack presentation is HTML and JavaScript. Opening one executes the JavaScript contained in that presentation with the privileges browsers normally grant a local HTML document. Only open presentations from sources you trust.

The packer itself must not execute presentation JavaScript.

`validate`, `inspect`, and `unpack` must not execute it either.

Only an actual browser viewing the presentation executes presentation code.

Archive extraction must treat all package paths as untrusted.

---

# 26. Repository Structure

Use a clean Go project structure approximately like:

```text
cmd/
  slidepack/

internal/
  archive/
  envelope/
  manifest/
  pack/
  unpack/
  validate/
  inspect/
  runtime/

internal/runtime/
  bootstrap.js
  bootstrap.css

docs/
  format-v1.md
  source-format.md

testdata/
  basic/
  nested/
  invalid/

tests/
  browser/

scripts/
  verify.sh

README.md
go.mod
go.sum
```

Exact names can differ when there is a good reason.

Embed the browser bootstrap into the Go binary using `go:embed`.

---

# 27. Do Not Over-Engineer

Format v1 should be intentionally understandable.

Prefer:

```text
Go stdlib
TAR
gzip
base64
plain HTML
plain JavaScript
native browser APIs
```

over adding large frameworks.

Do not introduce:

* React
* Vue
* webpack
* Vite
* an application server
* Electron
* a database
* browser extensions

The presentation itself may contain whatever ordinary static presentation JavaScript it needs, subject to the source contract.

The packer should remain small.

---

# 28. Tests

Tests are not optional.

Implement:

## Go unit tests

Cover at minimum:

* path normalization;
* safe extraction;
* traversal attacks;
* MIME mapping;
* deterministic manifest generation;
* deterministic TAR generation;
* deterministic gzip output;
* envelope parsing;
* corrupt base64;
* corrupt gzip;
* corrupt TAR;
* payload hash mismatch;
* file hash mismatch;
* unsupported version;
* output collision behavior;
* unsupported file types;
* USTAR path-length rejection.

## Go integration tests

Create source trees programmatically and verify:

```text
source
  -> pack
  -> validate
  -> unpack
  -> byte-for-byte comparison
```

Include:

* empty file;
* binary file;
* nested directories;
* Unicode filename;
* filename containing spaces;
* CSS;
* JS;
* SVG;
* raster image fixture;
* font fixture or other test capable of proving font resource resolution;
* file with no extension.

Test reproducibility:

```text
pack(source) == pack(source)
```

byte for byte.

Then change only source mtimes and prove output is still identical.

Then change one byte of source and prove output changes.

---

# 29. Real Browser End-to-End Tests

This is a hard requirement.

Use Playwright or an equivalently credible automated browser harness.

Test both:

* Chromium
* Firefox

Do NOT serve the packed HTML through HTTP.

The test must open:

```text
file:///.../presentation.html
```

directly.

The browser fixture must include at least:

### External local stylesheet

```html
<link rel="stylesheet" href="css/style.css">
```

Assert the expected computed style.

### External classic JavaScript

```html
<script src="js/app.js"></script>
```

Have it set an observable marker and assert that it executed.

### Local raster image

Assert:

```javascript
img.complete === true
img.naturalWidth > 0
```

### External SVG

Assert it loads successfully.

### CSS background image

Use a local resource referenced from a nested stylesheet and assert it resolves.

### Local font

Use a packaged font and verify through `document.fonts` or another reliable mechanism that the custom font resource loads.

### Paths with spaces and Unicode

At least one browser-visible asset must use such a path.

### Hash navigation / presentation JavaScript

Demonstrate that ordinary keyboard or hash-based slide navigation continues functioning inside the presentation frame.

### Console health

Fail the test on unexpected:

* `pageerror`
* console errors
* failed local resource loads

### Network independence

Ensure no `http:` or `https:` resource request is necessary for rendering.

The fixture should render successfully even with network access denied/intercepted.

Both Chromium and Firefox must pass.

---

# 30. Browser Corruption Test

Create a valid presentation, corrupt its payload, and open the HTML in Chromium or Firefox.

Assert that:

* the page does not remain indefinitely at "Loading";
* a visible error state is displayed;
* the browser does not crash;
* a useful diagnostic is emitted.

---

# 31. Validation Fixtures

Include invalid-source tests for at least:

### Missing file

```html
<img src="missing.png">
```

Expected: validation failure.

### Remote rendering dependency

```html
<img src="https://example.com/image.png">
```

Expected: validation failure.

### Module

```html
<script type="module" src="app.js"></script>
```

Expected: validation failure.

### Base element

```html
<base href="...">
```

Expected: validation failure.

### Symlink in source tree

Expected: pack failure.

### Archive traversal attempt

A malicious archive entry such as:

```text
../../outside.txt
```

must NEVER escape extraction root.

---

# 32. Performance / Scale Test

Do not optimize prematurely, but ensure the implementation is not accidentally quadratic.

Include a generated integration test or benchmark using a presentation containing at least several megabytes of binary data and many resources.

The test need not impose a fragile wall-clock limit.

Its purpose is to catch:

* obvious repeated full-payload copies;
* pathological concatenation;
* O(n²) archive behavior;
* integer truncation;
* tiny-size assumptions.

Use streaming Go APIs where reasonable.

---

# 33. Documentation

`README.md` must explain the mental model first:

> Edit a directory. Pack it when you want one file to distribute.

Include examples:

```bash
slidepack pack ./quarterly-review -o quarterly-review.html

open quarterly-review.html

slidepack inspect quarterly-review.html

slidepack validate quarterly-review.html

slidepack unpack quarterly-review.html -o ./quarterly-review-restored
```

Also document:

* Firefox and Chromium are the primary targets;
* files open directly through `file://`;
* no server is needed;
* JavaScript is required;
* packed HTML is generated output and should generally not be edited;
* limitations of format v1;
* security implications;
* reproducibility guarantees.

Create:

```text
docs/format-v1.md
docs/source-format.md
```

The format specification should be detailed enough that another developer could independently write an unpacker.

---

# 34. LLM / Agent Workflow Documentation

This tool specifically exists to support generated presentations.

Include a short section suitable for presentation-generation agents:

```text
Authoring workflow:

1. Create or unpack the presentation directory.
2. Modify ordinary source files and assets.
3. Run `slidepack validate`.
4. Run `slidepack pack`.
5. Treat the resulting HTML as immutable generated output.
```

Explicitly discourage agents from reading or editing the base64 payload.

---

# 35. Verification Command

Create one top-level verification command:

```bash
./scripts/verify.sh
```

It must run, at minimum:

```text
gofmt check
go vet ./...
go test ./...
integration tests
browser end-to-end tests in Chromium
browser end-to-end tests in Firefox
```

If browser-test dependencies are maintained separately, the script may invoke the appropriate package manager.

CI should be capable of running the same checks.

Add CI configuration if the repository environment supports it.

---

# 36. Ralph / Autonomous Development Loop Instructions

Maintain:

```text
RALPH_STATUS.md
```

during autonomous implementation.

It should contain each acceptance criterion ID below and one of:

```text
TODO
IN PROGRESS
PASS
BLOCKED
```

Do not mark an item PASS merely because code exists.

Mark PASS only when there is executable evidence.

When iterating:

1. inspect existing work;
2. run the current tests;
3. select the highest-value failing criterion;
4. implement the smallest complete improvement;
5. run targeted tests;
6. run broader regression tests;
7. update `RALPH_STATUS.md`;
8. continue.

Do not stop because the code "looks complete."

Do not stop after writing unit tests.

Do not replace real Firefox/Chromium tests with mocks.

Do not weaken a test solely to make it pass.

If an implementation choice proves invalid, change the implementation.

The job is finished when the behavior passes, not when the original implementation idea has been preserved.

---

# 37. Acceptance Criteria

These are the authoritative definition of done.

## AC-001 — Binary builds

```bash
go build ./cmd/slidepack
```

succeeds.

---

## AC-002 — Basic packing

Given a valid presentation directory:

```bash
slidepack pack testdata/basic -o /tmp/basic.html
```

creates exactly one HTML file and requires no companion directory.

---

## AC-003 — Direct Firefox opening

Opening the resulting file directly through a `file://` URL in current Firefox renders the test presentation successfully with no server and no extension.

---

## AC-004 — Direct Chromium opening

Opening the same file directly through a `file://` URL in current Chromium renders the test presentation successfully with no server and no extension.

---

## AC-005 — Offline rendering

The browser presentation renders successfully without fetching any rendering dependency over HTTP or HTTPS.

---

## AC-006 — Stylesheet

A separately authored local CSS file is applied correctly after packing.

---

## AC-007 — JavaScript

A separately authored classic JavaScript file executes correctly after packing.

---

## AC-008 — Raster images

At least one packaged raster image loads correctly in Firefox and Chromium.

---

## AC-009 — SVG

At least one separately packaged SVG resource loads correctly.

---

## AC-010 — CSS asset references

An image referenced through:

```css
url(...)
```

from a nested stylesheet resolves and renders correctly.

---

## AC-011 — Font

A font stored as a separate local file and referenced through `@font-face` loads successfully in both target browsers.

---

## AC-012 — Unicode and spaces

Resources whose paths contain spaces and Unicode characters resolve successfully.

---

## AC-013 — Exact content round trip

For every regular source file:

```text
source bytes == unpack(pack(source)) bytes
```

---

## AC-014 — Path round trip

Every source relative path is restored exactly.

---

## AC-015 — Mode round trip

Relevant Unix file mode bits are restored where the host platform supports them.

---

## AC-016 — Deterministic output

Packing identical source content twice produces byte-for-byte identical HTML.

---

## AC-017 — Mtime independence

Changing only source mtimes does not alter packed output.

---

## AC-018 — Content sensitivity

Changing a source byte changes the packed output and corresponding content hash.

---

## AC-019 — Payload integrity

Corrupting the packed payload causes:

```bash
slidepack validate presentation.html
```

to fail.

---

## AC-020 — Browser corruption UX

A corrupt payload displays a visible useful error rather than a blank page or permanent loading state.

---

## AC-021 — Per-file integrity

A mismatch between manifest file metadata and archive content is detected.

---

## AC-022 — Traversal protection

A malicious archive containing:

```text
../../evil
```

cannot write outside the requested extraction root.

---

## AC-023 — Absolute-path protection

Absolute Unix or Windows archive paths cannot escape the extraction directory.

---

## AC-024 — Symlink source rejection

Packing a source tree containing a symlink fails with a clear error.

---

## AC-025 — Missing resource detection

A statically referenced local asset that does not exist causes validation to fail.

---

## AC-026 — Remote resource detection

A remote rendering dependency causes validation to fail.

An ordinary external hyperlink does not.

---

## AC-027 — Unsupported module detection

Local ES module usage causes a clear validation failure.

---

## AC-028 — Base-tag detection

An HTML `<base>` element causes a clear validation failure.

---

## AC-029 — Packed validation

```bash
slidepack validate valid.html
```

returns success.

Invalid/corrupt packages return nonzero.

---

## AC-030 — Source validation

```bash
slidepack validate source-directory
```

works without first creating an HTML package.

---

## AC-031 — JSON validation output

```bash
slidepack validate --json ...
```

returns valid parseable JSON with stable diagnostic codes.

---

## AC-032 — Inspection

```bash
slidepack inspect presentation.html
```

reports useful package metadata without extracting the presentation.

---

## AC-033 — JSON inspection

```bash
slidepack inspect --json presentation.html
```

returns valid machine-readable JSON.

---

## AC-034 — Existing-output protection

`pack` does not silently overwrite an existing output without `--force`.

---

## AC-035 — Existing-destination protection

`unpack` does not silently overwrite a non-empty destination without explicit permission.

---

## AC-036 — Failed-pack atomicity

A failed pack does not leave a misleading partially written final HTML artifact.

---

## AC-037 — Failed-unpack safety

A failed/corrupt unpack does not result in files being written outside the extraction destination.

---

## AC-038 — Presentation interaction

Keyboard/hash-based presentation interaction in the browser fixture operates correctly after packing.

---

## AC-039 — No unexpected browser errors

The browser fixture produces no unexpected JavaScript page errors or console errors in Chromium or Firefox.

---

## AC-040 — No runtime dependency

The distributed `slidepack` executable requires only the executable itself.

The generated presentation requires only a compatible browser.

---

## AC-041 — Single payload

The generated HTML uses a single compressed source-tree payload rather than independently embedding every source asset as a packager-level base64 object.

Existing `data:` URLs authored intentionally inside the presentation are permitted.

---

## AC-042 — Source-tree scale

A generated multi-megabyte fixture with many files packs, validates, unpacks, and round-trips correctly.

---

## AC-043 — Help

The following all return useful help/version information:

```bash
slidepack --help
slidepack pack --help
slidepack unpack --help
slidepack validate --help
slidepack inspect --help
slidepack version
slidepack --version
```

---

## AC-044 — Documentation

README and format/source documentation accurately describe:

* usage;
* architecture;
* limitations;
* browser behavior;
* security;
* deterministic output;
* agent authoring workflow.

---

## AC-045 — Complete verification

The repository's canonical verification command:

```bash
./scripts/verify.sh
```

passes completely.

It must include real automated Firefox and Chromium tests.

---

# 38. Definition of Done

Do not declare completion until:

```bash
./scripts/verify.sh
```

passes and every acceptance criterion is either:

```text
PASS
```

or, only when genuinely impossible because of the execution environment:

```text
BLOCKED
```

A criterion may not be considered blocked merely because implementing it is difficult.

Before finalizing, manually review the implementation for:

* archive traversal vulnerabilities;
* accidental network dependencies;
* nondeterministic metadata;
* browser-specific behavior;
* unnecessarily large dependencies;
* confusing error behavior.

The final response should summarize:

1. what was implemented;
2. important architecture decisions;
3. commands used to verify it;
4. Firefox result;
5. Chromium result;
6. any remaining limitations.

Do not provide a speculative completion statement. Report the actual test status.
