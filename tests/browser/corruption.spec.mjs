import { expect, test } from '@playwright/test';
import { artifactURL, denyNetwork, watch } from './helpers.mjs';

/**
 * AC-020: a damaged file must fail visibly.
 *
 * The three variants cover the three ways a packed file realistically breaks:
 * bytes altered in transit, the tail lost to a truncated download, and a text
 * editor or mail gateway injecting characters outside the base64 alphabet.
 */
const cases = [
  {
    name: 'payload bytes altered',
    file: 'corrupt-payload.html',
    expect: /integrity check failed|could not be expanded|corrupt archive/i,
  },
  {
    name: 'payload truncated',
    file: 'truncated.html',
    expect: /not valid base64|integrity check failed|could not be expanded|truncated/i,
  },
  {
    name: 'payload contains non-base64 characters',
    file: 'corrupt-base64.html',
    expect: /not valid base64/i,
  },
];

for (const c of cases) {
  test(`AC-020 ${c.name}: a visible error replaces the loading state`, async ({ page }) => {
    const record = watch(page);
    await denyNetwork(page);
    await page.goto(artifactURL(c.file));

    const status = page.locator('#slidepack-status');
    const panel = status.locator('.slidepack-panel');

    // The failure must arrive promptly; a permanent "Loading" is the bug this
    // test exists to prevent.
    await expect(panel).toBeVisible({ timeout: 20_000 });

    // The loading indicator must be gone, not merely covered.
    await expect(status.locator('.slidepack-loading')).toHaveCount(0);

    // innerText reflects text-transform, so the "Reason" label arrives
    // upper-cased; match case-insensitively rather than couple the test to a
    // styling choice.
    const text = await status.innerText();
    expect(text).toContain('This presentation could not be loaded.');
    expect(text).toMatch(/reason/i);
    expect(text).toMatch(c.expect);
    expect(text).toContain('slidepack format v1');

    // No presentation frame should be left behind.
    await expect(page.locator('#slidepack-frame')).toHaveCount(0);

    // A useful diagnostic reached the console...
    expect(record.consoleErrors.join('\n')).toMatch(/\[slidepack\] failed during stage/);

    // ...and the runtime recorded where it gave up, without throwing out of the
    // page (an uncaught error would show up as a pageerror).
    expect(record.pageErrors).toEqual([]);

    const stage = await page.evaluate(() => window.slidepack.diagnostics.stage);
    expect(['base64', 'integrity', 'decompress', 'archive']).toContain(stage);

    // The browser is still healthy and responsive.
    expect(await page.evaluate(() => 1 + 1)).toBe(2);
  });
}
