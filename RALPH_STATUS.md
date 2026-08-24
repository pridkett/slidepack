# RALPH_STATUS

Acceptance criteria tracking. An item is `PASS` only when there is executable
evidence for it — a named test, or a command whose output was checked. "The
code exists" is not evidence.

Last full run of `./scripts/verify.sh`: **passed** — gofmt clean, `go vet`
clean, all Go packages passing, CLI smoke test passing, machine-readable
interface check passing, 18 browser tests passing in Chromium and 18 in
Firefox.

Reproduce everything below with:

```bash
./scripts/verify.sh
```

---

## Summary

| Status | Count |
|---|---|
| PASS | 45 |
| TODO / IN PROGRESS / BLOCKED | 0 |

---

## Criteria

| ID | Criterion | Status | Evidence |
|---|---|---|---|
| AC-001 | Binary builds | PASS | `go build ./cmd/slidepack`; `verify.sh` step "go build" |
| AC-002 | Basic packing produces exactly one file | PASS | `pack.TestPackWritesOneFileAndNothingElse`; `verify.sh` smoke test counts entries named `basic*` |
| AC-003 | Opens directly in Firefox from `file://` | PASS | `presentation.spec.mjs` project `firefox`, all 18 tests |
| AC-004 | Opens directly in Chromium from `file://` | PASS | `presentation.spec.mjs` project `chromium`, all 18 tests |
| AC-005 | Renders with no http/https request | PASS | `AC-005 nothing is fetched over http or https` (network blocked via `denyNetwork`) and `AC-005 renders with the browser context fully offline` |
| AC-006 | Local stylesheet applied | PASS | `AC-006 the separately authored stylesheet is applied` — asserts computed `font-size: 48px`, `color: rgb(17,34,51)`, plus rules that exist only in the `@import`ed nested sheet |
| AC-007 | Local classic script executes | PASS | `AC-007 the separately authored classic script executed` — `window.__deck.ready`, `slideCount === 4`, `data-deck-ready` attribute |
| AC-008 | Raster images load | PASS | `AC-008 packaged raster images load` — `complete === true` and `naturalWidth > 0` for PNG, JPEG, WebP and a `srcset` candidate, in both engines |
| AC-009 | SVG loads | PASS | `AC-009 a packaged SVG loads, and its fragment survives rewriting` |
| AC-010 | CSS `url()` from a nested stylesheet resolves | PASS | `AC-010 CSS url() references resolve...` — resolves `../../assets/texture.png` from `css/theme/theme.css` and decodes it to a 32×32 image |
| AC-011 | Local `@font-face` font loads | PASS | `AC-011 the packaged @font-face font loads and is actually applied` — `document.fonts` status `loaded`, plus a text-width measurement proving it is in use, not just fetched |
| AC-012 | Spaces and Unicode in paths | PASS | `AC-012 paths with spaces and non-ASCII characters resolve` (`assets/Revenue Chart – Europe.webp`); `pathutil.TestCheckAcceptsOrdinaryPaths`; `archive.TestNonASCIIStaysUSTAR` |
| AC-013 | Exact content round trip | PASS | `integration.TestRoundTripPreservesEveryFileExactly`; `TestRoundTripOfTheCommittedFixtures`; `verify.sh` runs `diff -r` |
| AC-014 | Path round trip | PASS | same tests — every relative path compared, including Unicode, spaces, no-extension and 7-level nesting |
| AC-015 | Unix mode round trip | PASS | `integration.TestRoundTripPreservesEveryFileExactly` (0644/0755/0600); `unpack.TestExtractRestoresModes` (full modes on Unix, the read-only bit on Windows, which is all that platform can express); `unpack.TestExtractCanReplaceAReadOnlyFile`; `archive.TestModesRoundTrip` |
| AC-016 | Deterministic output | PASS | `integration.TestPackingIsReproducible`; `archive.TestDeterministicOutput`; `archive.TestDeterministicGzip`; `envelope.TestWriteIsDeterministic` |
| AC-017 | Mtime independence | PASS | `integration.TestMtimesDoNotAffectOutput`; `verify.sh` smoke test (`touch` then `cmp`); CI `reproducible` job packs from two different absolute paths with different mtimes |
| AC-018 | Content sensitivity | PASS | `integration.TestOneChangedByteChangesTheOutput` — output bytes, the file digest and the payload digest all change |
| AC-019 | Corrupt payload fails `validate` | PASS | `cmd.TestValidateDetectsACorruptPayload`; `unpack.TestOpenRejectsPayloadHashMismatch` |
| AC-020 | Browser corruption UX | PASS | `corruption.spec.mjs`, 3 damage modes × 2 engines — visible panel, no loading state, no frame, console diagnostic, page still responsive |
| AC-021 | Per-file integrity | PASS | `unpack.TestOpenRejectsFileHashMismatch`; `TestOpenRejectsManifestArchiveDisagreement`; `TestOpenRejectsAnArchiveEntryTheManifestDoesNotList` |
| AC-022 | Traversal protection | PASS | `unpack.TestTraversalPathsNeverEscape` (`../../evil.txt`, `a/../../evil.txt`, `..\..\evil.txt`); `archive.TestReadRejectsTraversalPath`; `pathutil.TestSafeJoinContainment` |
| AC-023 | Absolute-path protection | PASS | same tests with `/etc/...` and `C:/Windows/...`; `archive.TestReadRejectsAbsoluteAndDrivePaths` |
| AC-024 | Symlink source rejection | PASS | `pack.TestPackFailsOnASymlink`; `validate.TestSymlinkInSourceTreeIsRejected` |
| AC-025 | Missing resource detection | PASS | `validate.TestInvalidFixturesProduceTheDocumentedCodes` / `invalid/missing-resource`; `verify.sh` smoke test expects exit 3 |
| AC-026 | Remote resource detection; hyperlinks allowed | PASS | `.../invalid/remote-resource` and `.../invalid/remote-css` produce `REMOTE_RESOURCE`; `validate.TestExternalHyperlinksAreAllowed`; browser test `external hyperlinks survive as ordinary links` |
| AC-027 | ES module detection | PASS | `.../invalid/module-script` → `ES_MODULE`; `source.TestScanJSDetectsUnsupportedPatterns`; `source.TestScanHTMLUnsupportedConstructs` |
| AC-028 | `<base>` detection | PASS | `.../invalid/base-element` → `BASE_ELEMENT`; `cmd.TestPackReportsValidationFailuresWithExitCodeThree` |
| AC-029 | Packed validation succeeds / corrupt fails | PASS | `cmd.TestValidatePackedFile` (exit 0); `cmd.TestValidateDetectsACorruptPayload` (non-zero) |
| AC-030 | Source validation without packing | PASS | `cmd.TestValidateDirectory`; `verify.sh` smoke test |
| AC-031 | JSON validation output | PASS | `cmd.TestValidateJSONIsParseableAndUnpolluted`; `cmd.TestValidateJSONEmitsArraysNotNulls`; `cmd.TestJSONOutputIsNeverColored`; `scripts/check-interface.py` asserts the shape in `verify.sh` and CI |
| AC-032 | Inspection without extracting | PASS | `cmd.TestInspectHumanAndJSON`; `integration.TestInspectReportsWithoutExtracting`; `unpack.TestReadManifestDoesNotTouchThePayload` proves the payload is never decoded |
| AC-033 | JSON inspection | PASS | `cmd.TestInspectHumanAndJSON` (unmarshals and checks fields); `scripts/check-interface.py` checks every file entry and the sort order |
| AC-034 | Existing-output protection | PASS | `pack.TestPackRefusesToOverwriteWithoutForce`; `verify.sh` smoke test expects exit 1 then 0 with `--force` |
| AC-035 | Existing-destination protection | PASS | `cmd.TestUnpackRefusesANonEmptyDestination`; `unpack.TestExtractRefusesANonEmptyDestinationWithoutForce` |
| AC-036 | Failed-pack atomicity | PASS | `pack.TestPackFailsValidationWithoutWritingAnything` (output directory left empty); `pack.TestPackFailsOnASymlink` |
| AC-037 | Failed-unpack safety | PASS | `unpack.TestTraversalPathsNeverEscape` (nothing written at all); `unpack.TestExtractRefusesToWriteThroughASymlink` (nothing outside the destination) |
| AC-038 | Keyboard and hash navigation | PASS | `AC-038 keyboard and hash navigation both work inside the frame` — arrow keys advance and retreat, a fragment link navigates and scrolls, in both engines |
| AC-039 | No unexpected browser errors | PASS | `AC-039 no unexpected page errors, console errors or failed loads`; every spec in the file also records `pageerror`, console errors and failed requests |
| AC-040 | No runtime dependency | PASS | Static Go binary; runtime assets embedded with `go:embed`; only dependency is `golang.org/x/net/html`. Browser tests run the packed file with the network blocked and no extension |
| AC-041 | Single payload | PASS | `envelope.TestPayloadIsASingleBlock`; `verify.sh` asserts exactly one `application/octet-stream` element and extracts it with system `tar`; browser test `AC-041 authored data: URLs are left untouched` |
| AC-042 | Source-tree scale | PASS | `integration.TestLargePresentationRoundTrips` — 242 files, ~5.6 MiB of incompressible data, packed, validated, unpacked and compared |
| AC-043 | Help and version | PASS | `cmd.TestEveryCommandAcceptsHelp` (three forms per command), `cmd.TestTopLevelHelp`, `cmd.TestHelpAllCoversEveryCommand`, `cmd.TestVersion`; `verify.sh` smoke test runs 15 help/version invocations. Additionally `cmd.TestHelpJSONDescribesTheWholeInterface` and `scripts/check-interface.py` verify the machine-readable form |
| AC-044 | Documentation | PASS | `README.md`, `docs/format-v1.md`, `docs/source-format.md` — usage, architecture, limitations, browser behaviour, security, determinism and the agent workflow. Diagnostic-code table cross-checked against `internal/diag` |
| AC-045 | Complete verification | PASS | `./scripts/verify.sh` runs gofmt, `go vet`, `go test ./...`, a CLI smoke test, the machine-readable interface check, and Playwright in Chromium **and** Firefox; last run passed |

---

## Notes for future iterations

Things that are deliberately out of scope for v1 and would need a format
version bump, not a patch:

- ES module graphs, import maps, service workers, local iframes, `<base>`.
- Dynamic local resource loading (`fetch`, `XMLHttpRequest`, `Worker` against
  package paths).
- Multi-document navigation between packaged HTML files.
- Empty directories, mtimes and ownership in the archive.

Known limitations that are documented rather than fixed:

- Static detection of dynamic resource loading in arbitrary JavaScript cannot
  be complete. The validator catches literal cases and warns about computed
  ones; the contract is stated in `docs/source-format.md`.
- Extracting into an existing directory with `--force` is not immune to a
  local attacker racing the directory checks. The staging path used for a
  fresh destination is not exposed to this. Documented in
  `docs/format-v1.md` §12.
- Permission bits are an input to reproducible packing, and Git records only
  the executable bit. Trees using modes other than `0644`/`0755` may pack
  differently after a fresh clone. Documented in `docs/format-v1.md` §8.
