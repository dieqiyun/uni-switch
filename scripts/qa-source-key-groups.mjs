// Real compiled core, isolated clients, local synthetic upstream, both engines.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium, webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';

const root = resolve(import.meta.dirname, '..');
const requests = [];
const upstream = createServer(async (req, res) => {
  const key = (req.headers.authorization || '').replace(/^Bearer /, '');
  res.setHeader('content-type', 'application/json');
  if (!/^qa-(basic|premium)-[a-z-]+$/.test(key)) {
    res.writeHead(401); res.end('{"error":{"message":"invalid key"}}'); return;
  }
  if (req.url.endsWith('/models')) {
    const models = key.startsWith('qa-basic-') ? ['qa-shared', 'qa-basic-only'] : ['qa-shared', 'qa-premium-only'];
    res.end(JSON.stringify({data: models.map(id => ({id, name: id}))}));
    return;
  }
  if (!req.url.endsWith('/chat/completions')) { res.writeHead(404); res.end('{}'); return; }
  let raw = '';
  for await (const chunk of req) raw += chunk;
  const body = JSON.parse(raw);
  requests.push({key, body});
  const content = key.startsWith('qa-basic-') ? 'from basic group' : 'from premium group';
  if (body.stream) {
    res.setHeader('content-type', 'text/event-stream');
    const chunk = {id: 'qa-group-response', object: 'chat.completion.chunk', model: body.model, choices: [{index: 0, delta: {role: 'assistant', content}, finish_reason: null}]};
    res.write(`data: ${JSON.stringify(chunk)}\n\n`);
    chunk.choices = [{index: 0, delta: {}, finish_reason: 'stop'}];
    res.end(`data: ${JSON.stringify(chunk)}\n\ndata: [DONE]\n\n`);
  } else {
    res.end(JSON.stringify({id: 'qa-group-response', object: 'chat.completion', model: body.model, choices: [{index: 0, message: {role: 'assistant', content}, finish_reason: 'stop'}], usage: {prompt_tokens: 3, completion_tokens: 4, total_tokens: 7}}));
  }
});
await new Promise(r => upstream.listen(0, '127.0.0.1', r));
const platform = {win32: 'pc-windows-msvc', linux: 'unknown-linux-gnu', darwin: 'apple-darwin'}[process.platform];
const arch = process.arch === 'arm64' ? 'aarch64' : 'x86_64';
const app = await startIsolated(resolve(root, `src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform === 'win32' ? '.exe' : ''}`));
const checks = [];
try {
  await app.wait(() => !!app.ready());
  for (const [engine, launcher] of [['chromium', chromium], ['webkit', webkit]]) {
    const browser = await launcher.launch(engine === 'chromium' ? {channel: 'chromium'} : {});
    try {
      for (const language of ['en', 'zh', 'zh-TW', 'ja', 'de']) {
        const context = await browser.newContext({viewport: {width: 1120, height: 780}, reducedMotion: 'reduce'});
        try {
          const page = await context.newPage(); page.setDefaultTimeout(10000);
          const errors = []; page.on('pageerror', e => errors.push(e.message));
          await page.route('**/api/upstream', route => route.fulfill({json: {providers: {}, vendors: []}}));
          const url = new URL(app.ready().url); url.searchParams.set('locale', language);
          const post = async (path, data) => {
            const response = await context.request.post(new URL(`/api/${path}`, url).href, {data});
            assert.ok(response.ok(), `${path}: ${await response.text()}`);
            return response.json();
          };
          const id = `qa-groups-${engine}-${language.toLowerCase()}`;
          const providerName = `蝶祈云API ${engine} ${language}`;
          const basicKey = `qa-basic-${engine}-${language.toLowerCase()}`;
          const premiumKey = `qa-premium-${engine}-${language.toLowerCase()}`;
          await context.request.get(url.href);
          await post('provider/save', {new: true, id, name: providerName, chat: `http://127.0.0.1:${upstream.address().port}/v1`, key: basicKey, keyName: '普通组', keyGroups: true, models: ['qa-shared', 'qa-basic-only']});
          await page.goto(url.href); await page.waitForFunction(() => !!providers);
          await page.locator('[data-uni-client="codex"]').click();
          const card = page.locator(`.provider[data-id="${id}"]`);
          await card.locator('.uni-edit').click();
          await page.locator('.editor .keys').waitFor();
          await page.getByRole('button', {name: await page.evaluate(() => t('Add API key group')), exact: true}).click();
          const group = page.getByRole('textbox', {name: await page.evaluate(() => t('API key group name')), exact: true});
          await group.fill('高级组');
          await page.locator('.keys .adding input[type="password"]').fill(premiumKey);
          const accessibility = await new AxeBuilder({page}).include('.keys .adding').withRules(['button-name', 'label', 'aria-allowed-attr', 'aria-required-attr']).analyze();
          assert.deepEqual(accessibility.violations.map(v => v.id), []);
          await page.locator('.keys .adding .primary').click();
          await page.waitForFunction(pid => providers.providers.find(p => p.id === pid)?.keyList.length === 2 && state.agents.find(a => a.id === 'codex').fields.find(f => f.key === 'model').options.filter(o => o.value.startsWith(pid + '/')).length === 4, id);
          const options = await page.evaluate(pid => state.agents.find(a => a.id === 'codex').fields.find(f => f.key === 'model').options.filter(o => o.value.startsWith(pid + '/')), id);
          assert.equal(options.length, 4, JSON.stringify(options));
          assert.ok(options.some(o => o.label === `qa-shared · 普通组 · ${providerName}`), JSON.stringify(options));
          assert.ok(options.some(o => o.label === `qa-shared · 高级组 · ${providerName}`));
          assert.ok(!options.some(o => o.label.includes('qa-basic-only · 高级组') || o.label.includes('qa-premium-only · 普通组')));
          const target = options.find(o => o.label === `qa-shared · 高级组 · ${providerName}`);
          await post('set', {agent: 'codex', field: 'model', value: target.value});
          const config = readFileSync(resolve(app.home, '.codex/config.toml'), 'utf8');
          assert.ok(config.includes(target.value), 'actual isolated Codex configuration selects the qualified model');
          const catalog = JSON.parse(readFileSync(resolve(app.home, '.codex/uni-switch-models.json'), 'utf8'));
          assert.ok(catalog.models.some(m => m.slug === target.value && m.display_name === target.label), 'Codex receives the same display name and route');
          assert.ok(!JSON.stringify(catalog).includes(premiumKey) && !JSON.stringify(catalog).includes(basicKey), 'client catalog contains no API key');
          const gateway = config.match(/^base_url\s*=\s*"([^"]+)"/m)?.[1];
          assert.ok(gateway, 'isolated Codex config includes the real gateway endpoint');
          const marker = `qa-group-inference-${id}`;
          const reply = await context.request.post(gateway.replace(/\/$/, '') + '/chat/completions', {headers: {Authorization: 'Bearer uni-switch-codex'}, data: {model: target.value, messages: [{role: 'user', content: marker}], stream: false}});
          assert.equal(reply.status(), 200, await reply.text());
          assert.equal((await reply.json()).choices[0].message.content, 'from premium group');
          const routed = requests.filter(r => JSON.stringify(r.body.messages).includes(marker));
          assert.equal(routed.length, 1, 'real compiled gateway makes exactly one upstream request');
          assert.equal(routed[0].key, premiumKey, 'real compiled gateway uses the selected group credential');
          assert.equal(routed[0].body.model, 'qa-shared', 'only the upstream model ID is sent to the provider');
          if (engine === 'chromium' && language === 'zh') await page.screenshot({path: resolve(root, '.qa/key-groups-zh.png')});
          assert.deepEqual(errors, []);
          checks.push({engine, language, independentModels: true, namedGroups: true, accessible: true, codexConfig: true, compiledGatewayInference: true});
        } finally { await context.close(); }
      }
    } finally { await browser.close(); }
  }
} finally { await app.stop(); await new Promise(r => upstream.close(r)); }
writeFileSync(resolve(root, '.qa/key-groups-ui.json'), JSON.stringify(checks, null, 2) + '\n');
console.log(JSON.stringify(checks, null, 2));
