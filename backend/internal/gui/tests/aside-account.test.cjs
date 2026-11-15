// #1499 (fjrtdk): Aside keeps one folder per account, and uni-switch configures
// the one Aside has active unless an account is picked on the row. The
// account field shows only with more than one account, says in the page's
// language which one Aside has active, and a pick is sent as the field.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { test } = require('node:test');
const { chromium, webkit } = require('playwright');
const assets = path.resolve(__dirname, '../assets');

const said = {
  en: { def: 'the account active in Aside', active: 'active in Aside', local: 'Local Account' },
  zh: { def: 'Aside 当前使用的账号', active: 'Aside 正在使用', local: '本地账号' },
  'zh-TW': { def: 'Aside 目前使用的帳號', active: 'Aside 正在使用', local: '本機帳號' },
  ja: { def: 'Aside で使用中のアカウント', active: 'Aside で使用中', local: 'ローカルアカウント' },
  de: { def: 'das in Aside aktive Konto', active: 'in Aside aktiv', local: 'Lokales Konto' },
};

for (const engine of ['chromium', 'webkit']) {
  for (const width of [980, 360]) {
    for (const lang of Object.keys(said)) {
      test(`${engine} ${width}px ${lang}: Aside's account is picked on its row`, async (t) => {
        const browser = await (engine === 'webkit' ? webkit.launch() : chromium.launch({ channel: 'chromium' }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 760 }, reducedMotion: 'reduce' });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const posts = [], errors = [];
        page.on('pageerror', (e) => errors.push(e.message));
        const accounts = [
          { value: 'u0', label: 'u0', note: 'Local Account' },
          { value: 'u1', label: 'u1', note: '... · active in Aside' },
          { value: 'u2', label: 'u2', note: 'Local Account' },
        ];
        const a = {
          id: 'aside', name: 'Aside', icon: 'aside', wired: true, path: '~/.aside/u/1/settings.json',
          native: { provider: 'connected', runtime: 'applied', fields: {} },
          fields: [
            { key: 'model', label: 'model', value: 'uni-switch/group/spacebunny', options: [{ value: 'uni-switch/group/spacebunny', ref: 'group/spacebunny', label: 'spacebunny' }] },
            { key: 'account', label: 'account', value: '', options: accounts },
          ],
        };
        const state = () => ({ agents: [a], profiles: [], settings: { lang, theme: 'light' } });
        await page.route('**/*', async (route) => {
          const req = route.request(), p = new URL(req.url()).pathname;
          if (req.method() === 'POST') posts.push({ path: p, body: req.postDataJSON() });
          if (p === '/boot.js') return route.fulfill({ contentType: 'text/javascript', body: `window.bootPrefs=${JSON.stringify({ lang, theme: 'light', web: true })}` });
          if (p === '/api/state') return route.fulfill({ json: state() });
          if (p === '/api/set') { a.fields[1].value = req.postDataJSON().value; a.path = '~/.aside/u/2/settings.json'; return route.fulfill({ json: state() }); }
          if (p === '/api/providers') return route.fulfill({ json: { providers: [], presets: [], gateway: { running: true } } });
          if (p === '/api/groups') return route.fulfill({ json: { groups: [] } });
          if (p === '/api/plugins') return route.fulfill({ json: { plugins: [] } });
          if (p === '/api/usage/quotas') return route.fulfill({ json: [] });
          if (p === '/api/agents/cli') return route.fulfill({ json: { agents: {}, pending: false } });
          if (p.startsWith('/api/')) return route.fulfill({ json: {} });
          const file = path.join(assets, p === '/' ? 'index.html' : p);
          return route.fulfill({ body: await fs.readFile(file), contentType: { '.js': 'text/javascript', '.html': 'text/html', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] });
        });
        await page.goto('http://uni-switch.test/?view=agents');
        const row = page.locator('.row.agent[data-id="aside"]');
        await row.waitFor();
        // its settings beyond the model are under Details
        await row.locator('.ag-link').click();
        const field = row.locator('.field[data-key="account"]');
        await field.waitFor();
        assert.ok(await field.isVisible(), 'account field shows with three accounts');
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no sideways scroll');
        await field.click();
        const items = page.locator('#pop:not([hidden]) #list li');
        await items.first().waitFor();
        const texts = await items.allTextContents();
        assert.match(texts[0], new RegExp(said[lang].def), 'Default says it follows Aside');
        const u1 = texts.find((s) => s.includes('u1'));
        assert.ok(u1.includes(said[lang].active), `active account named: ${u1}`);
        assert.ok(u1.includes('...'), 'the e-mail stays as is');
        assert.ok(texts.find((s) => s.includes('u0')).includes(said[lang].local));
        await items.filter({ hasText: 'u2' }).first().click();
        await page.waitForFunction(() => document.querySelector('.row.agent[data-id="aside"] .field[data-key="account"]')?.textContent.includes('u2'));
        assert.deepEqual(posts.filter((p) => p.path === '/api/set').map((p) => p.body), [{ agent: 'aside', field: 'account', value: 'u2' }]);
        // one account only: nothing to pick, so no field
        a.fields[1] = { key: 'account', label: 'account', value: '', options: [] };
        await page.evaluate(async () => { state = await api('state'); renderAgents(); });
        await page.waitForFunction(() => !document.querySelector('.row.agent[data-id="aside"] .field[data-key="account"]'));
        assert.deepEqual(errors, []);
      });
    }
  }
}
