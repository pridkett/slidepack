import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
export const artifacts = join(here, '.artifacts');
export const repoRoot = resolve(here, '..', '..');

/** file:// URL of an artifact produced by global-setup. */
export function artifactURL(name) {
  return pathToFileURL(join(artifacts, name)).href;
}

/**
 * Attaches listeners that record every condition the console-health assertion
 * cares about, plus every http(s) request the page attempts.
 *
 * Returns an object whose arrays fill in as the page runs; assert on them after
 * the presentation has settled.
 */
export function watch(page) {
  const record = { pageErrors: [], consoleErrors: [], failedRequests: [], networkRequests: [] };

  page.on('pageerror', (e) => record.pageErrors.push(String(e.message ?? e)));
  page.on('console', (m) => {
    if (m.type() === 'error') record.consoleErrors.push(m.text());
  });
  page.on('requestfailed', (r) => {
    record.failedRequests.push(`${r.url().slice(0, 120)} :: ${r.failure()?.errorText ?? 'unknown'}`);
  });
  page.on('request', (r) => {
    const u = r.url();
    if (u.startsWith('http:') || u.startsWith('https:')) record.networkRequests.push(u);
  });
  return record;
}

/**
 * Makes any http(s) request fail outright.
 *
 * This is stronger than Playwright's offline mode for our purposes: it proves
 * the presentation never even reaches for the network, and if it did, the
 * attempt would surface as a failed request rather than a silent fallback.
 */
export async function denyNetwork(page) {
  await page.route(/^https?:\/\//, (route) => route.abort('blockedbyclient'));
}

/** Waits for the runtime to finish and returns the presentation's frame. */
export async function presentationFrame(page, timeout = 30_000) {
  await page.waitForFunction(
    () => window.slidepack && window.slidepack.diagnostics.stage === 'ready',
    null,
    { timeout }
  );
  const frame = page.frames().find((f) => f !== page.mainFrame());
  if (!frame) throw new Error('the presentation iframe was never created');
  await frame.waitForFunction(() => window.__deck && window.__deck.ready, null, { timeout });
  return frame;
}
