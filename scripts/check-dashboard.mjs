// Review-only tool; Playwright must be provisioned in the validation environment.
// The shipped dashboard has no JavaScript dependencies or build step.
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

const shell = process.env.DASHBOARD_URL || 'http://127.0.0.1:8080';
const reference = process.env.REFERENCE_URL || 'http://127.0.0.1:8081/theme/Factory%20Floor.dc.html';
const output = process.env.SCREENSHOT_DIR || '/tmp/issue-4-screenshots';
await mkdir(output, { recursive: true });
const browser = await chromium.launch();
try {
  const metrics = [];
  for (const [name, width, height] of [['desktop', 1440, 1000], ['mobile', 390, 844]]) {
    const context = await browser.newContext({ viewport: { width, height }, reducedMotion: 'reduce' });
    const requests = [];
    const errors = [];
    await context.route('**/*', route => {
      const url = route.request().url();
      requests.push(url);
      return new URL(url).origin === new URL(shell).origin ? route.continue() : route.abort();
    });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(shell);
    await page.waitForFunction(() => document.querySelector('#events [role="status"]'));
    assert.deepEqual(requests.filter(url => new URL(url).origin !== new URL(shell).origin), []);
    assert.deepEqual(errors, []);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
    const actual = await page.screenshot({ path: `${output}/${name}-shell.png` });
    // Keyboard-only interaction, including all tabs and every event filter.
    await page.keyboard.press('Tab');
    assert.equal(await page.locator('.skip').evaluate(el => el === document.activeElement), true);
    await page.keyboard.press('Tab');
    const checkView = async target => {
      const tab = page.getByRole('tab', { name: target, exact: true });
      assert.equal(await tab.getAttribute('aria-selected'), 'true');
      assert.equal(await tab.evaluate(el => el === document.activeElement), true);
      assert.equal(await page.locator(`#${target.toLowerCase()}`).isVisible(), true);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true,
        `${name}: ${target} must fit the viewport`);
      assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
      assert.equal(await tab.evaluate(el => getComputedStyle(el).outlineStyle !== 'none'), true);
    };
    await checkView('Factory');
    for (const target of ['Agents', 'Events']) {
      await page.keyboard.press('ArrowRight');
      await checkView(target);
    }
    for (const [key, target] of [['ArrowRight', 'Factory'], ['ArrowLeft', 'Events'],
      ['Home', 'Factory'], ['End', 'Events']]) {
      await page.keyboard.press(key);
      await checkView(target);
    }
    await page.keyboard.press('Tab'); // Focusable Events panel.
    assert.equal(await page.locator('#events').evaluate(el => el === document.activeElement), true);
    for (const label of ['ALL', 'ASSIGN', 'REVIEW', 'CI', 'MERGE', 'HUMAN']) {
      await page.keyboard.press('Tab');
      assert.equal(await page.getByRole('button', { name: label, exact: true })
        .evaluate(el => el === document.activeElement), true);
      await page.keyboard.press('Enter');
      assert.equal(await page.getByRole('button', { name: label, exact: true }).getAttribute('aria-pressed'), 'true');
    }
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Space');
    assert.equal(await page.getByRole('button', { name: 'MERGE', exact: true }).getAttribute('aria-pressed'), 'true');
    assert.equal(await page.locator('[data-filter][aria-pressed="true"]').count(), 1);
    // Once the local files have loaded, controls also work fully offline.
    await context.setOffline(true);
    await page.getByRole('tab', { name: 'Factory', exact: true }).click();
    assert.equal(await page.locator('#factory').isVisible(), true);
    assert.deepEqual(errors, []);
    assert.deepEqual(requests.filter(url => new URL(url).origin !== new URL(shell).origin), []);
    await context.close();

    const refContext = await browser.newContext({ viewport: { width, height }, reducedMotion: 'reduce' });
    // Freeze the original simulator and use the same system font fallback.
    // Reference support.js still needs its React CDN resources provisioned.
    await refContext.addInitScript(() => { window.setInterval = () => 0; });
    await refContext.route('https://fonts.**/*', route => route.abort());
    const refPage = await refContext.newPage();
    await refPage.goto(reference);
    await refPage.waitForSelector('#dc-root button');
    await refPage.addStyleTag({ content: '* { animation:none !important; transition:none !important; }' });
    const expected = await refPage.screenshot({ path: `${output}/${name}-reference.png` });
    // Pixel diff is diagnostic: fixtures, Demo labels and mobile reflow intentionally differ.
    const diff = await refPage.evaluate(async ({ actual, expected, width, height }) => {
      const load = async base64 => {
        const image = new Image();
        image.src = `data:image/png;base64,${base64}`;
        await image.decode();
        const canvas = document.createElement('canvas');
        canvas.width = width; canvas.height = height;
        const ctx = canvas.getContext('2d'); ctx.drawImage(image, 0, 0);
        return ctx.getImageData(0, 0, width, height);
      };
      const a = await load(actual), b = await load(expected);
      let changed = 0;
      for (let i = 0; i < a.data.length; i += 4) {
        const delta = Math.max(...[0, 1, 2].map(c => Math.abs(a.data[i + c] - b.data[i + c])));
        if (delta > 20) changed++;
        a.data[i] = delta; a.data[i + 1] = 0; a.data[i + 2] = 0; a.data[i + 3] = 255;
      }
      const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
      canvas.getContext('2d').putImageData(a, 0, 0);
      return { changedFraction: changed / (width * height), image: canvas.toDataURL().split(',')[1] };
    }, { actual: actual.toString('base64'), expected: expected.toString('base64'), width, height });
    await writeFile(`${output}/${name}-diff.png`, Buffer.from(diff.image, 'base64'));
    metrics.push({ viewport: name, changedFraction: diff.changedFraction });
    await refContext.close();
  }
  await writeFile(`${output}/results.json`, JSON.stringify(metrics, null, 2));
  console.log('Browser checks passed; review screenshot differences in', output);
} finally {
  await browser.close();
}
