#!/usr/bin/env python3
"""Check slidepack's machine-readable interface.

`help --json`, `validate --json` and `inspect --json` are a contract that
agents and CI scripts depend on. This script exercises all three against a
real binary and asserts the shape and the stable fields, so a refactor that
quietly drops a key fails a build rather than a user's automation.

Usage:
    scripts/check-interface.py path/to/slidepack
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
FAILURES: list[str] = []


def check(condition: object, message: str) -> None:
    if not condition:
        FAILURES.append(message)


def run(binary: str, *args: str, expect: int | None = 0) -> str:
    """Runs the binary and returns stdout, asserting the exit code."""
    proc = subprocess.run(
        [binary, *args], capture_output=True, text=True, cwd=REPO
    )
    if expect is not None and proc.returncode != expect:
        FAILURES.append(
            f"{' '.join(args)}: exit {proc.returncode}, expected {expect}\n"
            f"    stderr: {proc.stderr.strip()[:400]}"
        )
    return proc.stdout


def parse(binary: str, *args: str, expect: int | None = 0) -> object:
    out = run(binary, *args, expect=expect)
    try:
        return json.loads(out)
    except json.JSONDecodeError as exc:
        FAILURES.append(
            f"{' '.join(args)}: stdout is not valid JSON ({exc})\n"
            f"    got: {out[:400]!r}"
        )
        return {}


def check_help(binary: str) -> None:
    doc = parse(binary, "help", "--json")
    if not isinstance(doc, dict):
        FAILURES.append("help --json did not return an object")
        return

    for key in ("name", "version", "formatVersion", "summary", "commands",
                "globalOptions", "exitCodes", "diagnostics"):
        check(key in doc, f"help --json is missing the top-level key {key!r}")

    check(doc.get("name") == "slidepack", "help --json name is not 'slidepack'")
    check(doc.get("formatVersion") == 1, "help --json formatVersion is not 1")

    commands = doc.get("commands") or []
    check(commands, "help --json described no commands")
    names = set()
    for c in commands:
        for key in ("name", "summary", "usage", "options"):
            check(key in c, f"command {c.get('name')!r} is missing {key!r}")
        check(c.get("summary"), f"command {c.get('name')!r} has an empty summary")
        names.add(c.get("name"))
        for opt in c.get("options") or []:
            for key in ("name", "type", "summary"):
                check(key in opt,
                      f"option {opt.get('name')!r} of {c.get('name')!r} is missing {key!r}")
            check(opt.get("type") in ("string", "boolean"),
                  f"option {opt.get('name')!r} has unexpected type {opt.get('type')!r}")

    for required in ("pack", "unpack", "validate", "inspect", "version", "help"):
        check(required in names, f"help --json does not describe the {required!r} command")

    exit_codes = doc.get("exitCodes") or []
    check(len(exit_codes) >= 4, "help --json described fewer than four exit codes")
    check(any(e.get("code") == 0 for e in exit_codes),
          "help --json does not describe exit code 0")

    diagnostics = doc.get("diagnostics") or []
    check(len(diagnostics) >= 20,
          f"help --json described only {len(diagnostics)} diagnostic codes")
    for d in diagnostics:
        for key in ("code", "severity", "summary"):
            check(key in d, f"diagnostic {d.get('code')!r} is missing {key!r}")
        check(d.get("severity") in ("error", "warning"),
              f"diagnostic {d.get('code')!r} has unexpected severity {d.get('severity')!r}")
    codes = {d.get("code") for d in diagnostics}
    for required in ("MISSING_RESOURCE", "REMOTE_RESOURCE", "ES_MODULE",
                     "BASE_ELEMENT", "PAYLOAD_HASH_MISMATCH"):
        check(required in codes, f"help --json does not document {required}")

    # Every command must also describe itself individually.
    for name in sorted(names):
        one = parse(binary, "help", name, "--json")
        check(isinstance(one, dict) and one.get("name") == name,
              f"help {name} --json did not describe {name!r}")

    print(f"  help --json: {len(commands)} commands, {len(diagnostics)} diagnostic codes")


def check_validate(binary: str) -> None:
    good = parse(binary, "validate", "--json", "testdata/basic")
    check(good.get("valid") is True, f"testdata/basic should be valid: {good}")
    check(good.get("errors") == [], "a valid target must emit an empty errors array")
    check(good.get("warnings") == [], "a valid target must emit an empty warnings array")
    check(good.get("kind") == "source", f"kind should be 'source', got {good.get('kind')!r}")

    bad = parse(binary, "validate", "--json", "testdata/invalid/remote-resource", expect=3)
    check(bad.get("valid") is False, "an invalid target must report valid=false")
    codes = {e.get("code") for e in bad.get("errors") or []}
    check("REMOTE_RESOURCE" in codes, f"expected REMOTE_RESOURCE, got {codes}")
    for e in bad.get("errors") or []:
        for key in ("code", "message"):
            check(e.get(key), f"diagnostic is missing {key!r}: {e}")

    print("  validate --json: valid and invalid targets both parse")


def check_inspect(binary: str, packed: Path) -> None:
    report = parse(binary, "inspect", "--json", str(packed))
    check(report.get("format") == "slidepack", "inspect --json format is wrong")
    check(report.get("version") == 1, "inspect --json version is wrong")
    check(report.get("entrypoint") == "index.html",
          f"inspect --json entrypoint is {report.get('entrypoint')!r}")
    files = report.get("files") or []
    check(report.get("fileCount") == len(files),
          "inspect --json fileCount disagrees with the files array")
    for f in files:
        for key in ("path", "size", "sha256", "mime", "mode"):
            check(key in f, f"file entry is missing {key!r}: {f}")
        check(len(f.get("sha256", "")) == 64, f"malformed digest for {f.get('path')!r}")
    paths = [f["path"] for f in files]
    check(paths == sorted(paths), "inspect --json files are not sorted by path")

    print(f"  inspect --json: {len(files)} files, all fields present")


def check_stdout_is_unpolluted(binary: str) -> None:
    """--json output must be the JSON document and nothing else."""
    proc = subprocess.run(
        [binary, "validate", "--json", "testdata/invalid/module-script"],
        capture_output=True, text=True, cwd=REPO,
    )
    check(proc.stdout.lstrip().startswith("{"),
          "validate --json stdout does not begin with a JSON object")
    try:
        json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        FAILURES.append(f"validate --json stdout is polluted: {exc}")
    print("  --json stdout carries only the document")


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    binary = sys.argv[1]

    print("Checking the machine-readable interface")
    with tempfile.TemporaryDirectory() as tmp:
        packed = Path(tmp) / "deck.html"
        run(binary, "pack", "testdata/basic", "-o", str(packed), "--quiet")
        check_help(binary)
        check_validate(binary)
        check_inspect(binary, packed)
        check_stdout_is_unpolluted(binary)

    if FAILURES:
        print("\nInterface check FAILED:", file=sys.stderr)
        for f in FAILURES:
            print(f"  - {f}", file=sys.stderr)
        return 1
    print("Interface check passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
