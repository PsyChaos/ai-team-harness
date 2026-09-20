// Review-only tool; Playwright must be provisioned in the validation environment.
// The shipped dashboard has no JavaScript dependencies or build step.
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

const shell = process.env.DASHBOARD_URL || 'http://127.0.0.1:8080';
const reference = process.env.REFERENCE_URL || 'http://127.0.0.1:8081/theme/Factory%20Floor.dc.html';
const output = process.env.SCREENSHOT_DIR || '/tmp/issue-13-screenshots';
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
    const attack = '<img src=x onerror="window.pwned=1"><script>window.pwned=1</script>';
    const now = Date.now() / 1000;
    const task = { repo: 'owner/repo', project: 'owner/7', issue: 13, title: attack, body: attack,
      pr: 4, pr_title: attack, pr_body: attack, url: 'javascript:window.pwned=1', status: 'IN_PROGRESS',
      provider: 'codex', role: 'implementer', observed_at: now };
    const evidence = { requested: { model: 'requested-model', effort: 'high' }, effective: { model: 'effective-model', effort: 'medium' }, decision_source: 'rules', reason: 'Deterministic policy', policy_version: 'policy-v1' };
    let live = { version: 1, cursor: 3, events: [
      { id: 1, type: 'pybridge.task.observed', data: task },
      { id: 2, type: 'dashboard.agent.observed', data: { ...task, agent_id: 'agent-13', runtime_state: 'running', activity: attack, routing: evidence } },
      { id: 3, type: 'routing.decided', data: { ...task, selected: { model: 'requested-model', effort: 'high' }, effective: { model: 'effective-model', effort: 'medium' }, decision_source: 'rules', reason: 'Deterministic policy', policy_version: 'policy-v1' } },
    ] };
    let snapshotReads = 0;
    await context.route(`${shell}/snapshot`, route => { snapshotReads++; return route.fulfill({ json: live }); });
    await context.route(`${shell}/identity`, route => route.fulfill({ json: { repo: 'owner/repo', project: 'owner/7' } }));
    await context.addInitScript(() => {
      window.streams = [];
      window.EventSource = class extends EventTarget {
        constructor(url) { super(); this.url = url; this.closed = false; window.streams.push(this); setTimeout(() => this.onopen?.(), 0); }
        close() { this.closed = true; }
      };
      window.observation = event => window.streams.at(-1).dispatchEvent(new MessageEvent('observation', { data: JSON.stringify(event) }));
    });
    await page.goto(shell);
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    assert.equal(await page.locator('#demo').isChecked(), false);
    assert.match(await page.locator('#identity').textContent(), /owner\/repo.*owner\/7/);
    assert.match(await page.locator('#decision').textContent(), /requested-model.*effective-model.*rules.*policy-v1/s);
    assert.match(await page.locator('#factory').textContent(), /running/);
    assert.equal(await page.evaluate(() => window.streams.at(-1).url), '/events?cursor=3');
    assert.equal(await page.locator('#factory img, #factory script').count(), 0);
    // Keyboard tab navigation remains available in the live views.
    await page.getByRole('tab', { name: 'Factory', exact: true }).focus();
    await page.keyboard.press('ArrowRight');
    assert.equal(await page.locator('#agents').isVisible(), true);
    assert.match(await page.locator('#agents').textContent(), /effective-model.*medium/s);
    await page.screenshot({ path: `${output}/${name}-agents.png` });
    await page.keyboard.press('ArrowRight');
    assert.equal(await page.locator('#events').isVisible(), true);
    assert.match(await page.locator('#events').textContent(), /<img src=x/);
    await page.getByRole('button', { name: 'CI', exact: true }).click();
    assert.equal(await page.locator('#events-empty').isVisible(), true);
    await page.getByRole('button', { name: 'ALL', exact: true }).click();
    const update = { id: 4, type: 'dashboard.agent.observed', data: { ...task, agent_id: 'agent-13', runtime_state: 'waiting', activity: 'Live SSE update', observed_at: now - 180 } };
    live = { ...live, cursor: 4, events: [...live.events, update] };
    await page.evaluate(event => window.observation(event), update);
    assert.match(await page.locator('#events').textContent(), /Live SSE update/);
    await page.screenshot({ path: `${output}/${name}-events.png` });
    await page.getByRole('tab', { name: 'Agents', exact: true }).click();
    assert.match(await page.locator('#agents').textContent(), /stale.*waiting/s);
    await page.getByRole('tab', { name: 'Factory', exact: true }).click();
    await page.locator('#task-flow button').click();
    assert.equal(await page.locator('#task-detail').isVisible(), true);
    assert.match(await page.locator('#task-detail').textContent(), /<script>window.pwned=1<\/script>/);
    assert.equal(await page.locator('#task-detail img, #task-detail script').count(), 0);
    assert.deepEqual(await page.locator('#task-detail a').evaluateAll(nodes => nodes.map(n => n.href)),
      ['https://github.com/owner/repo/issues/13', 'https://github.com/owner/repo/pull/4']);
    assert.equal(await page.evaluate(() => window.pwned), undefined);
    await page.locator('#task-detail').screenshot({ path: `${output}/${name}-detail.png` });
    await page.getByRole('button', { name: 'Close task detail' }).click();
    await page.evaluate(() => window.streams.at(-1).onerror());
    assert.match(await page.locator('#connection').textContent(), /Reconnecting/);
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    assert.ok(snapshotReads >= 2);
    // A reset (e.g. retained cursor expired) fetches a fresh snapshot.
    const beforeReset = snapshotReads;
    await page.evaluate(() => window.streams.at(-1).dispatchEvent(new Event('reset')));
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    assert.ok(snapshotReads > beforeReset);
    await context.setOffline(true);
    await page.evaluate(() => window.dispatchEvent(new Event('offline')));
    assert.match(await page.locator('#connection').textContent(), /Offline/);
    await context.setOffline(false);
    await page.evaluate(() => window.dispatchEvent(new Event('online')));
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    await page.locator('#demo').check();
    assert.match(await page.locator('#mode-notice').textContent(), /all displayed observations are simulated/);
    assert.equal(await page.evaluate(() => window.streams.at(-1).closed), true);
    assert.doesNotMatch(await page.locator('#factory').textContent(), /owner\/repo/);
    await page.locator('#demo').uncheck();
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    assert.doesNotMatch(await page.locator('#factory').textContent(), /DEMO-01/);
    // Snapshot failure stays visibly unavailable and never falls back to Demo.
    await context.route(`${shell}/snapshot`, route => route.fulfill({ status: 503, body: 'unavailable' }));
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#connection').textContent.includes('Reconnecting'));
    assert.equal(await page.locator('#demo').isChecked(), false);
    assert.doesNotMatch(await page.locator('#factory').textContent(), /DEMO-01|simulated running/);
    await context.unroute(`${shell}/snapshot`);
    await context.route(`${shell}/snapshot`, route => route.fulfill({ json: live }));
    await page.reload();
    await page.waitForFunction(() => document.querySelector('#connection').textContent === 'Live');
    for (const target of ['Agents', 'Events', 'Factory']) {
      await page.getByRole('tab', { name: target, exact: true }).click();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${name}: ${target} overflow`);
    }
    assert.equal(await page.evaluate(() => document.getAnimations().length), 0);
    const actual = await page.screenshot({ path: `${output}/${name}-shell.png` });
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
    for (const view of ['Agents', 'Events']) {
      await refPage.getByRole('button', { name: view, exact: true }).click();
      await refPage.screenshot({ path: `${output}/${name}-${view.toLowerCase()}-reference.png` });
    }
    // The original theme has no task-detail panel. Review the detail capture
    // against its panel typography, colors and mobile spacing, not pixel equality.
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
