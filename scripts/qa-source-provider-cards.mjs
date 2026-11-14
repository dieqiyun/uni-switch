// Real compiled core, isolated configuration, synthetic keys and upstream only.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium, webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';

const root = resolve(import.meta.dirname, '..');
const models = ['qa-model', 'gpt-5.5', 'claude-sonnet-4-5'];
const upstream = createServer((req, res) => {
  res.setHeader('content-type', 'application/json');
  res.end(JSON.stringify({data: models.map(id => ({id}))}));
});
await new Promise(r => upstream.listen(0, '127.0.0.1', r));
const endpoint = `http://127.0.0.1:${upstream.address().port}/v1`;
const platform = {win32: 'pc-windows-msvc', linux: 'unknown-linux-gnu', darwin: 'apple-darwin'}[process.platform];
const arch = process.arch === 'arm64' ? 'aarch64' : 'x86_64';
const app = await startIsolated(resolve(root, `src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform === 'win32' ? '.exe' : ''}`));
const checks = [];
let seeded = false;
try {
  await app.wait(() => !!app.ready());
  for (const [engine, launcher] of [['chromium', chromium], ['webkit', webkit]]) {
    const browser = await launcher.launch(engine === 'chromium' ? {channel: 'chromium'} : {});
    try {
      for (const language of ['en', 'zh', 'zh-TW', 'ja', 'de']) {
        const context = await browser.newContext({viewport: {width: 1120, height: 780}, reducedMotion: 'reduce'});
        try {
          const page = await context.newPage();
          page.setDefaultTimeout(8000);
          const errors = [];
          page.on('pageerror', error => errors.push(error.message));
          await page.route('**/api/upstream', route => route.fulfill({json: {providers: {}, vendors: []}}));
          const url = new URL(app.ready().url);
          url.searchParams.set('locale', language);
          const api = path => new URL(`/api/${path}`, url).href;
          await page.goto(url.href);
          await page.waitForFunction(() => !!providers);
          const post = async (path, data) => {
            const result = await context.request.post(api(path), {data});
            assert.ok(result.ok(), await result.text());
            return result.json();
          };
          const reload = async () => {
            await page.evaluate(async () => { await loadProviders(); renderProviders(); });
          };
          if (!seeded) {
            await post('provider/save', {new: true, id: 'qa-cards', name: '蝶祈云API', preset: 'dieqiyun', chat: endpoint, key: 'qa-card-key', models});
            await post('provider/save', {new: true, id: 'qa-secondary', name: '备用供应商', chat: endpoint, key: 'qa-secondary-key', models: ['qa-model']});
            seeded = true;
          }
          await reload();
          await page.locator('[data-uni-client="zcode"]').click();
          const card = page.locator('#providers .provider[data-id="qa-cards"]');
          await card.waitFor();
          const directory = await (await context.request.get(api('providers'))).json();
          const provider = directory.providers.find(p => p.id === 'qa-cards');
          assert.deepEqual(provider.models.filter(m => m.on).map(m => m.context), models.map(() => 256000));
          assert.ok(!provider.contexts || !Object.keys(provider.contexts).length, 'default is not stored as a manual override');
          assert.equal(await card.locator('.uni-context').textContent(), `256K${await page.evaluate(() => t('Adjust'))}`);
          assert.equal(await card.locator('.uni-card-meta dt').first().textContent(), await page.evaluate(() => t('Available models')));
          assert.equal(await card.locator('.key').count(), 1);
          assert.equal(await card.locator('.uni-card-meta .key').count(), 1);
          assert.equal(await card.locator(':scope > .uses:empty').count(), 0, 'empty client shortcuts do not create an extra grid row');
          const bounds = await card.boundingBox();
          assert.ok(bounds.height <= 180, 'compact card height');
          const name = await card.locator('.who').boundingBox();
          const actions = await card.locator('.uni-card-actions').boundingBox();
          assert.ok(Math.abs(name.y + name.height / 2 - actions.y - actions.height / 2) < 2, 'identity and actions align');
          const toolbarAligned = async () => {
            const boxes = await Promise.all(['#uniProviderHint', '#pvQ', '#pvMask'].map(selector => page.locator(selector).boundingBox()));
            const centers = boxes.map(b => b.y + b.height / 2);
            assert.ok(Math.max(...centers) - Math.min(...centers) <= 1, 'provider hint, search and masking share one centered row');
            assert.ok(boxes[0].x + boxes[0].width <= boxes[1].x && boxes[1].x + boxes[1].width <= boxes[2].x, 'toolbar controls do not overlap');
          };
          await page.evaluate(() => {
            const hint = document.querySelector('#uniProviderHint');
            hint.textContent = t('API keys and subscriptions work together. Add a provider, then use it to choose a model.');
            hint.title = hint.textContent;
          });
          await toolbarAligned();
          if (engine === 'chromium' && language === 'zh') await page.screenshot({path: resolve(root, '.qa/provider-cards-zh.png')});

          await card.locator('.uni-context').click();
          const input = page.locator('.editor .contexts');
          assert.equal(await input.evaluate(e => e === document.activeElement), true);
          assert.equal(await input.evaluate(e => e.closest('details').open), true);
          await input.fill('128k, qa-model=512k');
          await page.locator('.editor > .bar .primary').click();
          await page.locator('.editor').waitFor({state: 'detached'});
          await page.waitForFunction(() => providers.providers.find(p => p.id === 'qa-cards').models.some(m => m.id === 'qa-model' && m.context === 512000));
          assert.ok((await card.locator('.uni-context').innerText()).startsWith('128K – 512K'));
          await card.locator('.uni-context').click();
          await input.fill('');
          await page.locator('.editor > .bar .primary').click();
          await page.locator('.editor').waitFor({state: 'detached'});
          await page.waitForFunction(() => providers.providers.find(p => p.id === 'qa-cards').models.every(m => !m.on || m.context === 256000));

          // A chosen model belongs to the selected client, never an arbitrary first model.
          await page.evaluate(() => {
            const p = providers.providers.find(p => p.id === 'qa-cards');
            p.agents = p.agents.filter(a => a.id !== 'zcode').concat({id: 'zcode', name: 'ZCode', icon: 'zcode', current: true, model: 'qa-model'});
            renderProviders();
          });
          assert.equal(await card.locator('.uni-card-meta dt').first().textContent(), await page.evaluate(() => t('Current model')));
          assert.equal(await card.locator('.uni-card-meta dd').first().textContent(), 'qa-model');
          await reload();

          for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
            for (const width of [1120, 760, 600]) {
              await page.setViewportSize({width, height: 780});
              await toolbarAligned();
              assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${theme} ${width}: page does not overflow`);
              const b = await card.boundingBox();
              for (const selector of ['.uni-card-actions', '.uni-context', '.uni-card-connection']) {
                const c = await card.locator(selector).boundingBox();
                assert.ok(c.x >= b.x && c.x + c.width <= b.x + b.width + 1, `${selector} fits card`);
              }
            }
          }
          await page.setViewportSize({width: 1120, height: 780});
          await page.evaluate(() => document.documentElement.dataset.theme = 'light');
          await card.locator('.uni-edit').click();
          await page.locator('.editor').waitFor();
          await page.keyboard.press('Escape');
          await page.locator('.editor').waitFor({state: 'detached'});
          await card.locator('.pswitch').click();
          await page.locator('#offProviders .provider[data-id="qa-cards"]').waitFor({state: 'attached'});
          assert.equal(await page.locator('#offProviders [data-id="qa-cards"] .uni-use').isDisabled(), true);
          await post('provider/on', {id: 'qa-cards'});
          await reload();
          await page.locator('#pvQ').fill('does-not-exist');
          assert.equal(await card.isVisible(), false);
          await page.locator('#pvQ').fill('');
          await card.waitFor();
          if (engine === 'chromium' && language === 'en') {
            await card.locator('.pv-handle').focus();
            await page.keyboard.press('Alt+ArrowDown');
            await page.waitForFunction(() => providers.providers.filter(p => !p.off)[0]?.id === 'qa-secondary');
          }
          const audit = await new AxeBuilder({page}).include('#providers').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
          assert.deepEqual(audit.violations.map(v => ({id: v.id, nodes: v.nodes.map(n => n.target)})), []);
          assert.deepEqual(errors, []);
          checks.push({engine, language, compactCard: true, default256K: true, manualOverride: true, responsive: true, accessible: true});
        } finally { await context.close(); }
      }
    } finally { await browser.close(); }
  }
  writeFileSync(resolve(root, '.qa/provider-cards-results.json'), JSON.stringify(checks, null, 2));
  console.log(JSON.stringify({checks: checks.length, default256K: true, manualOverrides: true, compactCards: true}));
} finally {
  await app.stop();
  await new Promise(r => upstream.close(r));
}
