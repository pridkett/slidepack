# slidepack

**Edit a directory. Pack it when you want one file to distribute.**

A presentation is an ordinary directory of HTML, CSS, JavaScript, images and
fonts. `slidepack` compiles that directory into a single `.html` file that
opens straight from the filesystem — no server, no browser extension, no
companion folder, no network — and expands it back into the original directory
whenever you want.

```
The directory is source. The HTML file is a build artifact.
```

**[Documentation](https://pridkett.github.io/slidepack)** ·
[Source format](https://pridkett.github.io/slidepack/source-format.html) ·
[Packed format](https://pridkett.github.io/slidepack/format-v1.html) ·
[For agents](https://pridkett.github.io/slidepack/agents.html)

That separation is the whole point. Self-contained HTML is a fine way to hand
someone a deck and a miserable way to author one: assets are inaccessible,
diffs are useless, replacing an image means surgery on a megabyte of base64,
and version control gives up. Keep the directory under source control, treat
the `.html` as generated output, and both problems go away.

---

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [How it works](#how-it-works)
- [Commands](#commands)
- [What a presentation may contain](#what-a-presentation-may-contain)
- [Reproducible output](#reproducible-output)
- [Browser support](#browser-support)
- [Security](#security)
- [For presentation-generating agents](#for-presentation-generating-agents)
- [Limitations of format v1](#limitations-of-format-v1)
- [Development](#development)

---

## Install

```bash
go build -o slidepack ./cmd/slidepack
```

Or with a version stamped in:

```bash
go build -ldflags "-X main.version=1.0.0" -o slidepack ./cmd/slidepack
```

The result is a single static executable. It needs no Node.js, no Python, no
npm, no runtime of any kind — the browser bootstrap is compiled into the
binary. Node and Playwright are used only to run the browser tests during
development.

---

## Quick start

```bash
# Build one distributable file from a directory
slidepack pack ./quarterly-review -o quarterly-review.html

# Open it. That is all there is to it.
open quarterly-review.html

# See what is inside, without unpacking it
slidepack inspect quarterly-review.html

# Check that it is intact
slidepack validate quarterly-review.html

# Get the source directory back
slidepack unpack quarterly-review.html -o ./quarterly-review-restored
```

`unpack(pack(source))` reproduces every file, every path, every byte and every
Unix permission bit.

---

## How it works

```
presentation.html
┌────────────────────────────────────────────┐
│ small static bootstrap HTML                │
│   "Loading presentation…" + <noscript>     │
├────────────────────────────────────────────┤
│ JSON manifest                              │
│   paths, sizes, SHA-256s, MIME types, modes│
├────────────────────────────────────────────┤
│ ONE base64 blob                            │
│   = gzip( deterministic USTAR tar of src ) │
├────────────────────────────────────────────┤
│ inline first-party runtime                 │
└────────────────────────────────────────────┘
```

Opening the file, the runtime:

1. decodes the base64 in slices, without copying the whole payload;
2. verifies the payload SHA-256 with `crypto.subtle`;
3. decompresses with the browser's native `DecompressionStream("gzip")`;
4. parses the tar into an in-memory `path -> bytes + MIME` map;
5. mints `blob:` URLs and rewrites the entrypoint's references to them;
6. renders the result in a full-viewport `srcdoc` iframe and focuses it.

Nothing is written to disk. No request leaves the machine. There is no
`eval()` and no `new Function()`, and the only global defined is
`window.slidepack`, a frozen diagnostics object.

There is **one** archive payload, not one base64 blob per asset — which is why
a deck full of images compresses like a directory rather than inflating by a
third.

Full specification: [docs/format-v1.md](docs/format-v1.md).

---

## Commands

### `slidepack pack <directory> -o <file.html>`

Validates the source, then builds the packed file.

| Option | Meaning |
|---|---|
| `-o`, `--output` | Path of the `.html` file to write (required). |
| `--entry` | Entry document; defaults to `index.html`. |
| `--force` | Replace an existing output file. |
| `--quiet` | Print nothing on success. |

Packing fails, **without writing anything**, if the entrypoint is missing, a
referenced local file does not exist, a rendering resource is loaded over the
network, the tree contains a symlink, or the source uses a construct v1 cannot
serve. Output is written to a temporary file and renamed into place, so a
failure never leaves a half-written `.html` that looks real.

`pack` also refuses an output path inside the directory being packed, rather
than archiving the output into itself.

### `slidepack unpack <file.html> -o <directory>`

| Option | Meaning |
|---|---|
| `-o`, `--output` | Directory to write the source tree into (required). |
| `--force` | Write into a destination that already contains files. |
| `--quiet` | Print nothing on success. |

The payload digest and every per-file digest are verified before a single byte
is written. Archive paths are treated as untrusted throughout. When the
destination does not exist, files are built in a staging directory and renamed
into place.

### `slidepack validate <directory | file.html>`

Works on either a source directory or a packed file.

| Option | Meaning |
|---|---|
| `--json` | Machine-readable output on stdout. |
| `--entry` | Entry document, for directories. |
| `--strict` | Treat warnings as failures. |
| `--explain` | Print the remedy for each finding — what to change, not just what is wrong. |

A directory is checked for its entrypoint, path legality, filesystem object
types, missing local resources, remote rendering dependencies, and unsupported
HTML/CSS/JavaScript constructs.

A packed file is checked for all of that, plus the envelope, the manifest, the
format version, the base64, the payload digest, the gzip stream, the tar
structure, manifest/archive agreement and every per-file digest — after which
the recovered tree is validated in memory, exactly as a directory would be.

Diagnostic codes are stable and documented in
[docs/format-v1.md §11](docs/format-v1.md#11-diagnostic-codes).

### `slidepack inspect <file.html>`

```
quarterly-review.html

  format          slidepack v1
  generator       slidepack/1.0.0
  entrypoint      index.html
  files           11

  source content  14.2 KiB
  archive (tar)   23.5 KiB
  payload (gzip)  6.3 KiB   (45% of source)
  document        43.9 KiB

  payload sha256  0303c59d71974499696ae0bc6b00b336b80723b18c857e83e1470d95529c3473

  MODE     SIZE  TYPE             PATH
  0644    436 B  image/webp       assets/Revenue Chart – Europe.webp
  0644    428 B  image/png        assets/chart.png
  0644  3.3 KiB  font/ttf         fonts/slidepack-test.ttf
  0644  3.1 KiB  text/html        index.html  ← entrypoint
  0644  1.6 KiB  text/javascript  js/deck.js
  …
```

Reads only the envelope and the manifest, so inspecting a 40 MB presentation
costs a file read and a JSON parse.

| Option | Meaning |
|---|---|
| `--json` | Machine-readable output on stdout. |
| `--files` | Print only the file paths, one per line. |
| `--digests` | Add a full SHA-256 column to the listing. |

### `slidepack version`

Also `slidepack --version` and `slidepack -v`. Add `--json` to read it from a
script.

### `slidepack help [<command>]`

The tool explains itself. `slidepack help` gives an overview; `slidepack help
pack` gives that command's full description, its options with defaults, worked
examples and exit codes. `--help` and `-h` work on every command and are
equivalent. `slidepack help --all` prints the whole manual in one pass.

`slidepack help --json` prints the complete interface as a machine-readable
document — see [For presentation-generating agents](#for-presentation-generating-agents).

### Global options

Accepted by every command:

| Option | Meaning |
|---|---|
| `--color <when>` | `auto` (default), `always`, or `never`. |
| `--no-color` | The same as `--color never`. |
| `-h`, `--help` | Show that command's help and exit. |

Options may be written `--name value`, `--name=value`, `-n value` or `-n=value`,
and may appear before or after the positional arguments — `slidepack pack ./deck
-o deck.html` works, and so does the reverse order. Boolean short forms bundle:
`-fq` is `--force --quiet`. A `--` ends option parsing, so a directory whose
name begins with a dash is still packable.

### Terminal output

Output is coloured when it is going to a terminal and plain when it is not, so
piping into a file or a log never captures escape sequences. The rules, in
order: `--color never` and `--no-color` disable it; `NO_COLOR` in the
environment disables it and beats even `--color always`; `--color always` and
`CLICOLOR_FORCE` enable it; `TERM=dumb` disables it; otherwise it depends on
whether the destination is a terminal.

`--json` output is never coloured, whatever the setting.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success, or a valid target. |
| `1` | Operational failure: I/O, corruption, a refused overwrite. |
| `2` | Usage error. |
| `3` | The target was readable but is not valid. |

Human-facing messages go to stderr. With `--json`, stdout carries the JSON
document and nothing else.

---

## What a presentation may contain

The short version:

```html
<link rel="stylesheet" href="css/deck.css">
<script src="js/deck.js"></script>
<img src="assets/chart.webp">
<img src="/assets/logo.svg">
<img src="assets/Revenue Chart – Europe.webp">
<a href="#slide-12">Jump ahead</a>
<a href="https://example.com">External links are fine</a>
```

```css
@import url("theme/base.css");

@font-face {
  font-family: "Inter";
  src: url("../fonts/inter-regular.woff2") format("woff2");
}

.hero { background-image: url("../assets/hero.webp"); }
```

References resolve relative to the file containing them, so a `url()` in
`css/theme/base.css` reaches assets with `../../assets/…`. Paths with spaces
and non-ASCII characters work, percent-encoded or not. Fragments survive, so
`icons.svg#warning` keeps its fragment. Hand-authored `data:` URLs are left
exactly as written.

The full contract, including every rewritten attribute and every rejected
construct, is in [docs/source-format.md](docs/source-format.md).

---

## Reproducible output

Given the same source paths, bytes, permission bits and entrypoint, and the
same slidepack version, packing produces a **byte-for-byte identical** file.

```bash
slidepack pack fixture -o one.html
sleep 2
touch fixture/index.html
slidepack pack fixture -o two.html
cmp one.html two.html          # identical
```

Modification times have no effect: tar `uid`, `gid` and `mtime` are fixed at
zero, `uname`/`gname` are empty, members and manifest entries are ordered
byte-wise by path, the gzip header carries no timestamp, and MIME types come
from a built-in table rather than the operating system's database.

Two caveats: permission bits are an input, and Git records only the executable
bit — so prefer `0644`/`0755` if you want identical output after a fresh
clone. And the generator string is in the manifest, so output changes across
slidepack versions by design.

---

## Browser support

**Firefox and Chromium/Chrome are the primary targets**, and both are exercised
by an automated end-to-end suite on every verification run. The suite opens a
real packed file at a real `file://` URL with the network blocked, and asserts
that stylesheets apply, scripts execute, raster images and SVGs load, CSS
background images resolve from a nested stylesheet, a packaged `@font-face`
font is genuinely applied, paths with spaces and non-ASCII characters resolve,
keyboard and hash navigation work, and no console error or failed request
occurs.

Requirements for viewing:

- **JavaScript must be enabled.** A packed file with scripting off shows an
  explanation, not a blank page.
- The browser needs `DecompressionStream` — Chrome 80+, Firefox 113+, Edge
  80+, Safari 16.4+. Older browsers get a clear message naming the problem.
- No server. No extension. No network.

If loading fails for any reason, the file shows a readable error panel naming
the reason, with full diagnostics in the console. It never sits on "Loading"
and never shows a blank page.

---

## Security

> A slidepack presentation is HTML and JavaScript. Opening one executes the
> JavaScript contained in that presentation with the privileges browsers
> normally grant a local HTML document. **Only open presentations from sources
> you trust.**

This is the same trust decision as opening any `.html` file someone sends you.
slidepack does not add risk, and it does not remove it either.

The tooling itself never executes presentation code. `pack`, `validate`,
`inspect` and `unpack` tokenize HTML, scan CSS lexically and pattern-match
JavaScript; no JavaScript engine is involved. Only a browser viewing the
presentation runs presentation code.

Extraction treats every archive path as untrusted: `..`, absolute paths,
Windows drive letters, backslashes, NUL bytes and control characters are all
rejected before anything is written; destinations are re-checked for
containment; directory components are created one at a time and refused if any
is a symbolic link; and files are never written through a symlink.

The SHA-256 digests are **integrity checks, not signatures**. They detect
truncation and corruption. They say nothing about who produced the file.

---

## For presentation-generating agents

This tool exists to make generated presentations tractable to edit.

**Work on the directory. Never on the packed file.**

```
1. Create the presentation directory, or `slidepack unpack` an existing file.
2. Edit ordinary source files and assets.
3. Run `slidepack validate --json ./presentation`.
4. Run `slidepack pack ./presentation -o presentation.html`.
5. Treat the resulting HTML as immutable generated output.
```

### Learn the interface, do not scrape it

```bash
slidepack help --json
```

returns one document describing the whole program: every command, every option
with its type, default and whether it is required, every exit code, the output
conventions, and the **complete diagnostic catalogue** — each code with its
severity, what it means, and a remedy saying what to change.

```jsonc
{
  "$schema": "https://slidepack.dev/schema/cli-interface/v1",
  "name": "slidepack",
  "formatVersion": 1,
  "commands": [ /* pack, unpack, validate, inspect, version, help */ ],
  "exitCodes": [ { "code": 3, "name": "invalid", "summary": "…" } ],
  "diagnostics": [
    {
      "code": "DYNAMIC_LOCAL_FETCH",
      "severity": "error",
      "category": "unsupported",
      "summary": "fetch() of a literal package-local path.",
      "remedy": "Resources exist only as blob: URLs at runtime, so a source path cannot resolve. Inline the data in a <script type=\"application/json\"> block and read it from the DOM."
    }
  ],
  "conventions": { "jsonOutputStream": "stdout", "optionsAfterArguments": true }
}
```

`slidepack help --json <command>` narrows it to one command. Nothing here needs
help text parsing, and the catalogue is checked against the code in CI, so it
cannot drift.

The same documents are published, so you can read them without running
anything:

| URL | Contents |
|---|---|
| [`/llms.txt`](https://pridkett.github.io/slidepack/llms.txt) | An index of this project, in the [llms.txt](https://llmstxt.org) convention. |
| [`/llms-full.txt`](https://pridkett.github.io/slidepack/llms-full.txt) | Every documentation page concatenated as Markdown. |
| [`/cli.json`](https://pridkett.github.io/slidepack/cli.json) | The complete CLI interface, identical to `slidepack help --json`. |

Concretely:

- **Do not read the base64 payload.** It is one opaque blob and reading it
  burns context to learn nothing. Use `slidepack inspect --json` for the file
  listing, sizes, types and digests, and `slidepack unpack` when you need the
  bytes.
- **Do not edit the packed HTML.** Every hand edit is lost at the next pack,
  and altering the payload breaks its digest.
- **Use `validate --json`** and match on the `code` field. Codes are stable and
  documented; messages are for humans and may be reworded. `--explain` adds the
  remedy to human output; `help --json` carries the same text for every code.
- **Bundle JavaScript to a classic script** before packing. Module graphs are
  rejected with `ES_MODULE`.
- **Inline data instead of fetching it.** `fetch("./data.json")` cannot resolve
  inside a packed file; put the JSON in a
  `<script type="application/json">` block and read it from the DOM.
- **Keep the packed file out of the source directory**, or `pack` will refuse
  to write it.

A validation loop that exits `0` means the presentation will render.

---

## Limitations of format v1

Out of scope by design:

- ES module graphs and import maps — bundle first.
- Dynamic local resource loading: `fetch()`, `XMLHttpRequest`, `Worker` against
  package paths. Resources exist only as `blob:` URLs.
- Service workers, which cannot be registered from `file://` anyway.
- Local iframes embedding another packaged document.
- `<base>`, which makes resource resolution ambiguous.
- Filesystem-relative page navigation. `<a href="appendix.html">` will not
  navigate; `<a href="#slide-12">` works normally.
- Empty directories, mtimes and ownership, which are deliberately not part of
  the canonical source representation.

`slidepack validate` reports every one of these with a stable code, so you find
out while authoring rather than after distributing.

---

## Development

```bash
./scripts/verify.sh              # everything, including both browsers
./scripts/verify.sh --no-browser # Go only
./scripts/verify.sh --no-site    # skip the documentation site build
./scripts/verify.sh --short      # skip the multi-megabyte scale test

./scripts/build-site.sh --serve  # preview the documentation site locally
```

The verification gate runs `gofmt`, `go vet`, the full Go test suite, a
command-line smoke test against the built binary, a check that the
machine-readable interface still carries the fields agents parse, the
documentation site build, and the Playwright suite in Chromium **and** Firefox.
The browser tests are not optional: a packed file is only correct if two real
engines agree that it renders.

```
cmd/slidepack/        CLI
internal/archive/     deterministic USTAR + gzip
internal/diag/        stable diagnostic vocabulary
internal/envelope/    HTML container: write and parse
internal/inspect/     manifest-only reporting
internal/manifest/    the embedded index
internal/mimes/       host-independent MIME table
internal/pack/        walk → validate → archive → envelope
internal/pathutil/    path normalization and extraction safety
internal/runtime/     bootstrap.js / bootstrap.css (go:embed)
internal/source/      HTML, CSS and JS scanners; source trees
internal/unpack/      decode, verify, extract safely
internal/validate/    the format v1 source contract
docs/                 format and authoring specifications
site/                 the documentation site and its generator
testdata/             valid and invalid fixtures
tests/browser/        Playwright suites
```

The site is a handful of static pages with one stylesheet, no JavaScript and no
web fonts — it makes no network request of its own, which is the same promise a
packed presentation makes. Its generator is a separate Go module so that
slidepack itself stays at exactly one dependency. The two specification pages
are rendered from the very `docs/*.md` files above, and `cli.json` comes from
running the binary, so the site cannot document a format or a flag that does not
exist. Every internal link is checked at build time.

The only third-party Go dependency is `golang.org/x/net/html`, used to
tokenize HTML. That is a correctness argument rather than a convenience one:
resource discovery has to agree with what a browser sees, and a regular
expression disagrees with a browser on comments, raw-text elements and
malformed markup.
