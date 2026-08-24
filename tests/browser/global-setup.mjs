import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
export const repoRoot = resolve(here, '..', '..');
export const artifacts = join(here, '.artifacts');

/**
 * Builds the slidepack binary and packs the browser fixture before any spec
 * runs, so that the tests always exercise the current source rather than a
 * stale artifact someone forgot to rebuild.
 *
 * Corrupt variants are produced here too, by editing the packed file's base64
 * payload directly. That is exactly the damage a truncated download or a
 * well-meaning text editor would do, which is the failure the loader has to
 * survive visibly.
 */
export default function globalSetup() {
  rmSync(artifacts, { recursive: true, force: true });
  mkdirSync(artifacts, { recursive: true });

  const bin = join(artifacts, 'slidepack');
  execFileSync('go', ['build', '-o', bin, './cmd/slidepack'], {
    cwd: repoRoot,
    stdio: 'inherit',
  });

  const fixture = join(artifacts, 'fixture.html');
  execFileSync(bin, ['pack', join(repoRoot, 'testdata', 'browser'), '-o', fixture, '--force'], {
    cwd: repoRoot,
    stdio: 'inherit',
  });

  const html = readFileSync(fixture, 'utf8');
  writeFileSync(join(artifacts, 'corrupt-payload.html'), corruptPayload(html));
  writeFileSync(join(artifacts, 'truncated.html'), truncatePayload(html));
  writeFileSync(join(artifacts, 'corrupt-base64.html'), corruptBase64(html));
}

const PAYLOAD_OPEN = 'id="slidepack-payload"';

/** Returns [start, end) offsets of the payload element's text content. */
function payloadSpan(html) {
  const idAt = html.indexOf(PAYLOAD_OPEN);
  if (idAt < 0) throw new Error('packed fixture has no payload element');
  const start = html.indexOf('>', idAt) + 2; // skip ">" and the newline
  const end = html.indexOf('</script', start);
  if (end < 0) throw new Error('packed fixture payload element is unterminated');
  return [start, end];
}

/**
 * Rewrites a run of characters in the middle of the payload using valid base64
 * characters, so the payload still decodes but no longer matches its digest.
 */
function corruptPayload(html) {
  const [start, end] = payloadSpan(html);
  const at = Math.floor((start + end) / 2);
  const original = html.slice(at, at + 64);
  const replacement = original
    .split('')
    .map((c) => (c === 'A' ? 'B' : 'A'))
    .join('');
  return html.slice(0, at) + replacement + html.slice(at + 64);
}

/** Removes the second half of the payload, as a truncated download would. */
function truncatePayload(html) {
  const [start, end] = payloadSpan(html);
  const cut = Math.floor((start + end) / 2);
  return html.slice(0, cut) + html.slice(end);
}

/** Inserts characters outside the base64 alphabet. */
function corruptBase64(html) {
  const [start, end] = payloadSpan(html);
  const at = Math.floor((start + end) / 2);
  return html.slice(0, at) + '!!!!' + html.slice(at + 4);
}
