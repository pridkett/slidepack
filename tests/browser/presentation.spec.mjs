import { expect, test } from '@playwright/test';
import { artifactURL, denyNetwork, presentationFrame, watch } from './helpers.mjs';

/**
 * Every assertion here runs against a packed .html file opened directly from
 * the filesystem, with the network blocked. No server, no extension, no
 * companion directory.
 */
test.describe('packed presentation renders from file://', () => {
  /** @type {ReturnType<typeof watch>} */
  let record;
  /** @type {import('@playwright/test').Frame} */
  let frame;

  test.beforeEach(async ({ page }) => {
    record = watch(page);
    await denyNetwork(page);
    await page.goto(artifactURL('fixture.html'));
    frame = await presentationFrame(page);
    await frame.evaluate(() => document.fonts.ready);
  });

  test('AC-003/AC-004 the presentation mounts and the outer title is set', async ({ page }) => {
    await expect(page).toHaveTitle('Slidepack Browser Fixture');
    // The loading indicator must be gone once the frame is up.
    await expect(page.locator('#slidepack-status')).toBeHidden();
    await expect(page.locator('#slidepack-frame')).toBeVisible();

    // The frame fills the viewport, so the presentation looks like the page.
    const box = await page.locator('#slidepack-frame').boundingBox();
    const viewport = page.viewportSize();
    expect(box.width).toBeCloseTo(viewport.width, 0);
    expect(box.height).toBeCloseTo(viewport.height, 0);
  });

  test('AC-005 nothing is fetched over http or https', async () => {
    expect(record.networkRequests).toEqual([]);
    expect(record.failedRequests).toEqual([]);
  });

  test('AC-006 the separately authored stylesheet is applied', async () => {
    const styles = await frame.evaluate(() => {
      const cs = (sel) => getComputedStyle(document.querySelector(sel));
      return {
        headline: cs('.headline').fontSize,
        body: cs('body').color,
        // From css/theme/theme.css, reached through @import.
        h2color: cs('h2').color,
        h2size: cs('h2').fontSize,
      };
    });
    expect(styles.headline).toBe('48px');
    expect(styles.body).toBe('rgb(17, 34, 51)');
    // Proves the @import resolved: these rules live only in the nested file.
    expect(styles.h2color).toBe('rgb(43, 108, 176)');
    expect(styles.h2size).toBe('32px');
  });

  test('AC-007 the separately authored classic script executed', async () => {
    const deck = await frame.evaluate(() => ({
      ready: window.__deck?.ready === true,
      slideCount: window.__deck?.slideCount,
      attr: document.body.getAttribute('data-deck-ready'),
    }));
    expect(deck.ready).toBe(true);
    expect(deck.slideCount).toBe(4);
    expect(deck.attr).toBe('true');
  });

  test('AC-008 packaged raster images load', async () => {
    const imgs = await frame.evaluate(() =>
      ['img-png', 'img-root', 'img-unicode', 'img-srcset'].map((id) => {
        const el = document.getElementById(id);
        return { id, complete: el.complete, width: el.naturalWidth, height: el.naturalHeight };
      })
    );
    for (const img of imgs) {
      expect(img.complete, `${img.id} complete`).toBe(true);
      expect(img.width, `${img.id} naturalWidth`).toBeGreaterThan(0);
      expect(img.height, `${img.id} naturalHeight`).toBeGreaterThan(0);
    }
  });

  test('AC-009 a packaged SVG loads, and its fragment survives rewriting', async () => {
    const svg = await frame.evaluate(() => {
      const el = document.getElementById('img-svg');
      return { complete: el.complete, width: el.naturalWidth, src: el.getAttribute('src') };
    });
    expect(svg.complete).toBe(true);
    expect(svg.width).toBeGreaterThan(0);
    expect(svg.src).toMatch(/^blob:/);
    expect(svg.src.endsWith('#detail')).toBe(true);
  });

  test('AC-010 CSS url() references resolve from nested, inline and attribute styles', async () => {
    const bgs = await frame.evaluate(() =>
      ['css-bg', 'inline-bg', 'attr-bg'].map((id) => ({
        id,
        image: getComputedStyle(document.getElementById(id)).backgroundImage,
      }))
    );
    for (const bg of bgs) {
      // css-bg comes from css/theme/theme.css via url(../../assets/texture.png),
      // which only resolves if the reference was taken relative to the nested
      // stylesheet rather than to the document.
      expect(bg.image, `${bg.id} background-image`).toMatch(/^url\("blob:/);
    }

    // And the resolved background actually decodes to a real image.
    const decoded = await frame.evaluate(async () => {
      const url = getComputedStyle(document.getElementById('css-bg')).backgroundImage.slice(5, -2);
      const img = new Image();
      img.src = url;
      await img.decode();
      return { w: img.naturalWidth, h: img.naturalHeight };
    });
    expect(decoded.w).toBe(32);
    expect(decoded.h).toBe(32);
  });

  test('AC-011 the packaged @font-face font loads and is actually applied', async () => {
    const fonts = await frame.evaluate(() =>
      Array.from(document.fonts).map((f) => ({
        family: f.family.replace(/^"|"$/g, ''),
        status: f.status,
      }))
    );
    const custom = fonts.find((f) => f.family === 'Slidepack Test');
    expect(custom, `document.fonts contained ${JSON.stringify(fonts)}`).toBeTruthy();
    expect(custom.status).toBe('loaded');

    // document.fonts reporting "loaded" is necessary but not sufficient: the
    // test font has a deliberately wide 1.2em advance, so if it is really in
    // use the probe must be far wider than the monospace fallback.
    const widths = await frame.evaluate(() => ({
      probe: document.getElementById('font-probe').getBoundingClientRect().width,
      fallback: document.getElementById('font-fallback').getBoundingClientRect().width,
    }));
    expect(widths.probe).toBeGreaterThan(widths.fallback * 1.5);
  });

  test('AC-012 paths with spaces and non-ASCII characters resolve', async () => {
    const img = await frame.evaluate(() => {
      const el = document.getElementById('img-unicode');
      return { complete: el.complete, width: el.naturalWidth, src: el.getAttribute('src') };
    });
    expect(img.complete).toBe(true);
    expect(img.width).toBe(160);
    expect(img.src).toMatch(/^blob:/);
  });

  test('AC-041 authored data: URLs are left untouched', async () => {
    const data = await frame.evaluate(() => {
      const el = document.getElementById('img-data');
      return { src: el.getAttribute('src'), complete: el.complete, width: el.naturalWidth };
    });
    expect(data.src.startsWith('data:image/svg+xml,')).toBe(true);
    expect(data.complete).toBe(true);
    expect(data.width).toBeGreaterThan(0);
  });

  test('a CSS string that merely looks like url() is not rewritten', async () => {
    const content = await frame.evaluate(
      () => getComputedStyle(document.getElementById('decoy'), '::after').content
    );
    expect(content).toContain('url(assets/not-a-real-file.png)');
  });

  test('external hyperlinks survive as ordinary links', async () => {
    const href = await frame.evaluate(() => document.getElementById('external').getAttribute('href'));
    expect(href).toBe('https://example.com/documentation');
  });

  test('AC-038 keyboard and hash navigation both work inside the frame', async ({ page }) => {
    // The runtime focuses the frame on load, so keys reach the presentation.
    await page.keyboard.press('ArrowRight');
    await expect
      .poll(() => frame.evaluate(() => document.body.getAttribute('data-slide')))
      .toBe('slide-2');
    expect(await frame.evaluate(() => location.hash)).toBe('#slide-2');
    expect(await frame.evaluate(() => document.getElementById('status').textContent))
      .toBe('slide 2 of 4');

    await page.keyboard.press('ArrowRight');
    await expect
      .poll(() => frame.evaluate(() => document.body.getAttribute('data-slide')))
      .toBe('slide-3');

    await page.keyboard.press('ArrowLeft');
    await expect
      .poll(() => frame.evaluate(() => document.body.getAttribute('data-slide')))
      .toBe('slide-2');

    // Fragment links navigate and scroll, as they would on an ordinary page.
    await frame.click('a.jump[href="#slide-3"]');
    await expect
      .poll(() => frame.evaluate(() => location.hash))
      .toBe('#slide-3');
    expect(await frame.evaluate(() => window.scrollY)).toBeGreaterThan(0);
  });

  test('AC-039 no unexpected page errors, console errors or failed loads', async () => {
    expect(record.pageErrors).toEqual([]);
    expect(record.consoleErrors).toEqual([]);
    expect(record.failedRequests).toEqual([]);
  });
});

test('AC-005 renders with the browser context fully offline', async ({ browser }) => {
  const context = await browser.newContext({ offline: true });
  const page = await context.newPage();
  const record = watch(page);
  await page.goto(artifactURL('fixture.html'));
  const frame = await presentationFrame(page);

  expect(await frame.evaluate(() => window.__deck.slideCount)).toBe(4);
  expect(
    await frame.evaluate(() => {
      const el = document.getElementById('img-png');
      return el.complete && el.naturalWidth > 0;
    })
  ).toBe(true);
  expect(record.networkRequests).toEqual([]);
  expect(record.pageErrors).toEqual([]);
  expect(record.consoleErrors).toEqual([]);
  await context.close();
});
