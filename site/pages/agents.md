# For agents

<p class="lede">slidepack exists to make generated presentations tractable to edit. This page is
the short version of how a program should drive it.</p>

## The one rule

**Work on the directory. Never on the packed file.**

The `.html` file is a build artifact whose content is a single opaque base64
blob. Reading it burns context to learn nothing, editing it is lost at the next
pack, and altering it breaks its digest. Everything you might want from a packed
file has a command:

| You want | Use |
|---|---|
| the file listing, sizes, types, digests | `slidepack inspect --json deck.html` |
| the actual bytes | `slidepack unpack deck.html -o ./deck` |
| to know whether it is intact | `slidepack validate deck.html` |

## Learn the interface, do not scrape it

```bash
slidepack help --json
```

returns one document describing the whole program. It is also published here, so
you can read it without running anything:

**[`/cli.json`](cli.json)**

It carries every command and option with its type, default and whether it is
required; every exit code; the output conventions; and the **complete diagnostic
catalogue** — each code with its severity, what it means, and a remedy saying
what to change.

```json
{
  "$schema": "https://slidepack.dev/schema/cli-interface/v1",
  "name": "slidepack",
  "formatVersion": 1,
  "commands": [ "…pack, unpack, validate, inspect, version, help…" ],
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

`slidepack help --json <command>` narrows it to one command. A test asserts that
every diagnostic code the tool can emit appears in the catalogue, so the two
cannot drift.

## The authoring loop

```bash
# 1. Start from a directory, or recover one.
slidepack unpack existing.html -o ./deck

# 2. Edit ordinary files: ./deck/index.html, ./deck/css/deck.css, ./deck/assets/…

# 3. Check it. Exit 0 means it will render.
slidepack validate --json ./deck

# 4. Build the artifact.
slidepack pack ./deck -o deck.html

# 5. Treat deck.html as immutable output.
```

Step 3 is the useful one. `pack` runs the same validation and refuses to write
anything if it fails, so a successful pack is also a successful validate.

## Reading validation output

```bash
slidepack validate --json ./deck
```

```json
{
  "valid": false,
  "target": "./deck",
  "kind": "source",
  "errors": [
    {
      "code": "MISSING_RESOURCE",
      "path": "index.html",
      "line": 14,
      "detail": "img[src]",
      "message": "references \"assets/chart.webp\", which resolves to \"assets/chart.webp\", but no such file exists in the source tree"
    }
  ],
  "warnings": []
}
```

- **Match on `code`.** Codes are stable; messages are written for people and may
  be reworded.
- `errors` and `warnings` are always arrays, never `null`.
- Exit **0** valid, **3** invalid, **2** usage error, **1** operational failure.
- `--strict` makes warnings fail too. `--explain` adds each remedy to the human
  output.
- With `--json`, stdout carries only the document. Every human-facing message
  goes to stderr, so you can parse stdout unconditionally.

## Constraints worth knowing before you generate

These are the ones a generator gets wrong most often.

**Bundle your JavaScript.** ES modules are rejected (`ES_MODULE`). Emit one
classic script and load it with `<script src="js/deck.js"></script>`.

**Inline your data.** `fetch("./data.json")` cannot resolve inside a packed file
(`DYNAMIC_LOCAL_FETCH`); resources exist only as `blob:` URLs minted at load
time. Put the data in the document instead:

```html
<script id="deck-data" type="application/json">{"slides": [ … ]}</script>
<script src="js/deck.js"></script>
```

```js
var data = JSON.parse(document.getElementById("deck-data").textContent);
```

A `<script type="application/json">` block is inert data, and the validator does
not scan it as JavaScript.

**Everything must be local.** Any `http:` or `https:` rendering dependency is an
error (`REMOTE_RESOURCE`). Download fonts and images into the tree. Ordinary
hyperlinks are fine.

**One page, fragment navigation.** `<a href="#slide-12">` works normally.
`<a href="appendix.html">` will not navigate; fold extra pages into the entry
document.

**No `<base>`, no import maps, no service workers, no local iframes.**

**Keep the output outside the source directory**, or `pack` refuses to write it.

## Paths

References resolve relative to the file containing them, as in a browser:

```
index.html          →  href="css/deck.css"          →  css/deck.css
css/deck.css        →  url("../fonts/inter.woff2")  →  fonts/inter.woff2
css/theme/base.css  →  url(../../assets/bg.webp)    →  assets/bg.webp
```

A leading `/` means the package root, not the filesystem. Spaces and non-ASCII
characters work, percent-encoded or not. Fragments survive, so
`icons.svg#warning` keeps its fragment. Hand-authored `data:` URLs are left
exactly as written.

## Determinism

Identical source bytes, paths, permission bits and entrypoint produce a
byte-identical file. This makes packed output safe to commit and to compare
across builds. Two caveats: permission bits are an input and Git records only the
executable bit, so prefer `0644` and `0755`; and the generator version is
recorded in the manifest, so output changes across slidepack releases.

## Everything, as plain text

- [`/llms.txt`](llms.txt) — this site's index, in the
  [llms.txt](https://llmstxt.org) convention
- [`/llms-full.txt`](llms-full.txt) — every page on this site concatenated as
  Markdown, for when you would rather read once than crawl
- [`/cli.json`](cli.json) — the complete interface description
