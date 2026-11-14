// Opt-in real Codex CLI check, with no user config, login or paid upstream.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {createServer} from 'node:http';
import {existsSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {startIsolated} from './qa-source-core.mjs';

const root = resolve(import.meta.dirname, '..');
const at = process.argv.indexOf('--codex');
assert.ok(at >= 0 && process.argv[at + 1], 'Pass --codex <absolute Codex CLI executable>');
const codex = resolve(process.argv[at + 1]);
assert.ok(existsSync(codex), 'Codex executable is missing');
const platform = {win32: 'pc-windows-msvc', linux: 'unknown-linux-gnu', darwin: 'apple-darwin'}[process.platform];
const arch = process.arch === 'arm64' ? 'aarch64' : 'x86_64';
const core = resolve(root, `src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform === 'win32' ? '.exe' : ''}`);
const calls = [];
const upstream = createServer(async (req, res) => {
  res.setHeader('content-type', 'application/json');
  if (req.url.endsWith('/models')) {
    res.end(JSON.stringify({data: [{id: 'qa-cli'}]}));
    return;
  }
  if (!req.url.endsWith('/chat/completions')) { res.writeHead(404); res.end('{}'); return; }
  let raw = '';
  for await (const b of req) raw += b;
  const body = JSON.parse(raw);
  const key = (req.headers.authorization || '').replace(/^Bearer /, '');
  calls.push({key, model: body.model});
  if (key !== 'qa-cli-premium' || body.model !== 'qa-cli') {
    res.writeHead(403); res.end('{"error":{"message":"wrong QA group or model"}}'); return;
  }
  const base = {id: 'qa-cli-reply', object: 'chat.completion.chunk', model: 'qa-cli'};
  res.setHeader('content-type', 'text/event-stream');
  res.write(`data: ${JSON.stringify({...base, choices: [{index: 0, delta: {role: 'assistant', content: 'QA_GROUP_OK'}, finish_reason: null}]})}\n\n`);
  res.end(`data: ${JSON.stringify({...base, choices: [{index: 0, delta: {}, finish_reason: 'stop'}]})}\n\ndata: [DONE]\n\n`);
});
await new Promise(r => upstream.listen(0, '127.0.0.1', r));
let app;
try {
  app = await startIsolated(core);
  await app.wait(() => !!app.ready());
  const link = app.ready().url;
  const base = new URL(link).origin;
  const login = await fetch(link, {redirect: 'manual'});
  const cookie = login.headers.getSetCookie().map(x => x.split(';')[0]).join('; ');
  assert.ok(cookie);
  const request = (path, options = {}) => fetch(base + '/api/' + path, {...options, headers: {Cookie: cookie, 'Sec-Fetch-Site': 'same-origin', ...options.headers}});
  const post = async (path, data) => {
    const r = await request(path, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(data)});
    assert.equal(r.status, 200, await r.clone().text());
    return r.json();
  };
  await post('provider/save', {new: true, id: 'qa-cli-groups', name: 'QA Relay', chat: `http://127.0.0.1:${upstream.address().port}/v1`, key: 'qa-cli-basic', keyName: 'Basic', keyGroups: true, keys: [{name: 'Premium', key: 'qa-cli-premium'}], models: ['qa-cli']});
  const state = await (await request('state')).json();
  const options = state.agents.find(a => a.id === 'codex').fields.find(f => f.key === 'model').options;
  const pick = options.find(o => o.label === 'qa-cli · Premium · QA Relay');
  assert.ok(pick, 'The real client needs a named qualified group option');
  await post('set', {agent: 'codex', field: 'model', value: pick.value});
  const dir = resolve(app.home, '.codex');
  const configPath = resolve(dir, 'config.toml');
  const config = readFileSync(configPath, 'utf8');
  assert.match(config, /^requires_openai_auth\s*=\s*false$/m);
  assert.ok(!existsSync(resolve(dir, 'auth.json')), 'QA must not hold a ChatGPT login');
  writeFileSync(configPath, 'cli_auth_credentials_store = "file"\n' + config);
  const cwd = resolve(app.home, 'empty-project');
  const temp = resolve(app.home, 'tmp');
  mkdirSync(cwd, {recursive: true}); mkdirSync(temp, {recursive: true});
  // Construct an allowlisted environment; never inherit API keys or client dirs.
  const env = {};
  for (const k of ['SystemRoot', 'SystemDrive', 'WINDIR', 'COMSPEC', 'PATHEXT', 'PATH', 'Path']) if (process.env[k]) env[k] = process.env[k];
  Object.assign(env, {HOME: app.home, USERPROFILE: app.home, APPDATA: resolve(app.home, 'roaming'), LOCALAPPDATA: resolve(app.home, 'local'), XDG_CONFIG_HOME: resolve(app.home, '.config'), XDG_CACHE_HOME: resolve(app.home, '.cache'), XDG_DATA_HOME: resolve(app.home, '.local/share'), XDG_STATE_HOME: resolve(app.home, '.local/state'), TMP: temp, TEMP: temp, TMPDIR: temp, CODEX_HOME: dir, HTTP_PROXY: 'http://127.0.0.1:9', HTTPS_PROXY: 'http://127.0.0.1:9', ALL_PROXY: 'http://127.0.0.1:9', NO_PROXY: '127.0.0.1,localhost', OTEL_SDK_DISABLED: 'true'});
  const output = resolve(app.home, 'last-message.txt');
  const child = spawn(codex, ['exec', '--skip-git-repo-check', '--sandbox', 'read-only', '--json', '--output-last-message', output, 'Reply with QA_GROUP_OK. Do not run any tools.'], {env, cwd, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe']});
  let stdout = '', stderr = '', timedOut = false;
  child.stdout.on('data', b => { stdout += b; }); child.stderr.on('data', b => { stderr += b; });
  const timer = setTimeout(() => { timedOut = true; child.kill(); }, 60000);
  const code = await new Promise((ok, fail) => { child.once('error', fail); child.once('exit', ok); }).finally(() => clearTimeout(timer));
  writeFileSync(resolve(root, '.qa/codex-key-groups-cli.log'), stdout + '\n' + stderr);
  assert.ok(!timedOut, 'Isolated Codex CLI timed out');
  assert.equal(code, 0, stderr);
  assert.equal(readFileSync(output, 'utf8').trim(), 'QA_GROUP_OK');
  assert.ok(calls.length > 0, 'Actual Codex must contact the local upstream');
  assert.ok(calls.every(c => c.key === 'qa-cli-premium' && c.model === 'qa-cli'), 'Actual Codex must never cross key groups');
  assert.ok(!existsSync(resolve(dir, 'auth.json')), 'The request must not create a ChatGPT login');
  const result = {actualCodexCLI: true, noLogin: true, isolatedHome: true, externalNetworkBlocked: true, selectedGroupKey: true, upstreamModel: 'qa-cli', calls: calls.length};
  writeFileSync(resolve(root, '.qa/codex-key-groups-cli.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result));
} finally {
  if (app) await app.stop();
  await new Promise(r => upstream.close(r));
}
