// Run with Node's test runner and Playwright on the module path; see README.md.
// Antigravity's 429 "Resource has been exhausted" for Claude Desktop's chat
// (Claude Code's and the Agent SDK's system prompt, #666), with quota to
// spare (#1425): the Routing page told it as "429 · quota used up" and the
// account's tally as quota used up, while uni-switch's own note said it isn't
// one. It is told as the agent's prompt turned away, in the request's row,
// its story and the account's tally, with the 400 the agent got; the
// vendor's words are said apart from uni-switch's note, which is in the page's
// language. Codex's own GPT-5 prompt, which a thread begun on a GPT model
// keeps after a switch to Antigravity's (Jodiezzzzzz on X, 10-27), is told
// the same way with its own note.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const hint = "Antigravity answers this 429 to the system prompt of Claude Code and the Claude Agent SDK (Claude Desktop's chats) whatever quota is left, so it is not a quota and waiting won't help; use another provider for these chats, or put one after Antigravity in a routing group";
const codexHint = "Antigravity answers this 429 to Codex's own system prompt for GPT-5 models whatever quota is left, so it is not a quota and waiting won't help; a Codex chat begun on a GPT model keeps that prompt after a model switch or a fork, so start a new chat on this model";
const vendor = "Antigravity: Resource has been exhausted (e.g. check quota).";
const key = { id: "antigravity@u@example.com", provider: "antigravity", name: "Antigravity", kind: "account", who: "u@example.com", model: "gemini-3.8-flash" };
// as the gateway now traces it: the try Antigravity's 429, told as the
// agent's prompt turned away, not resting; the request the 400 the agent got
const routesOf = (agent, note) => {
  const error = vendor + " — " + note;
  return [0, 1].map((i) => ({
    id: 100 - i, seq: 100 - i, time: at(i), agent, model: "antigravity/gemini-3.8-flash", provider: "antigravity",
    order: [key], tries: [{ id: key.id, model: key.model, start: at(i), done: true, status: 429, ms: 400, error, fail: "prompt", effort: "high" }],
    done: true, status: 400, ms: 400, error, effort: "high",
  }));
};

function serve(lang, routes) {
  const state = { agents: [{ id: "claude-desktop", name: "Claude Desktop", path: "/test/config.json", fields: [] }, { id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { said: "It said: " + vendor, hint, codex: codexHint, fail: "agent's prompt turned away", quota: "quota used up" },
  zh: { said: "原话：" + vendor, hint: "Antigravity 对 Claude Code 和 Claude Agent SDK（Claude Desktop 的对话）的系统提示词都会回这个 429，与剩余额度无关，所以这不是额度用尽，等待也不会好；请用其他供应商处理这些对话，或在路由分组中把另一个供应商排在 Antigravity 之后", codex: "Antigravity 对 Codex 给 GPT-5 模型的系统提示词都会回这个 429，与剩余额度无关，所以这不是额度用尽，等待也不会好；在 GPT 模型上开始的 Codex 对话切换模型或分叉后仍带着这段提示词，请在这个模型上新开一个对话", fail: "Agent 的提示词被拒", quota: "额度用尽" },
  "zh-TW": { said: "原話：" + vendor, hint: "Antigravity 對 Claude Code 和 Claude Agent SDK（Claude Desktop 的對話）的系統提示詞都會回這個 429，與剩餘額度無關，所以這不是額度用盡，等待也不會好；請用其他供應商處理這些對話，或在路由分組中把另一個供應商排在 Antigravity 之後", codex: "Antigravity 對 Codex 給 GPT-5 模型的系統提示詞都會回這個 429，與剩餘額度無關，所以這不是額度用盡，等待也不會好；在 GPT 模型上開始的 Codex 對話切換模型或分叉後仍帶著這段提示詞，請在這個模型上新開一個對話", fail: "Agent 的提示詞被拒", quota: "額度用盡" },
  ja: { said: "メッセージ：" + vendor, hint: "Antigravity は Claude Code と Claude Agent SDK（Claude Desktop のチャット）のシステムプロンプトに対し、残りクォータに関係なくこの 429 を返します。クォータ切れではなく、待っても解決しません。これらのチャットには別のプロバイダを使うか、ルーティンググループで Antigravity の後に別のプロバイダを置いてください", codex: "Antigravity は Codex の GPT-5 モデル向けシステムプロンプトに対し、残りクォータに関係なくこの 429 を返します。クォータ切れではなく、待っても解決しません。GPT モデルで始めた Codex のチャットは、モデルを切り替えたりフォークしたりしてもそのプロンプトを持ち続けるので、このモデルで新しいチャットを始めてください", fail: "エージェントのプロンプトを拒否", quota: "クォータ切れ" },
  de: { said: "Meldung: " + vendor, hint: "Antigravity beantwortet den System-Prompt von Claude Code und dem Claude Agent SDK (Chats in Claude Desktop) mit diesem 429, egal wie viel Kontingent übrig ist – das Kontingent ist also nicht aufgebraucht, und Warten hilft nicht; nutzen Sie für diese Chats einen anderen Anbieter oder setzen Sie in einer Routing-Gruppe einen hinter Antigravity", codex: "Antigravity beantwortet den eigenen System-Prompt von Codex für GPT-5-Modelle mit diesem 429, egal wie viel Kontingent übrig ist – das Kontingent ist also nicht aufgebraucht, und Warten hilft nicht; ein Codex-Chat, der auf einem GPT-Modell begonnen wurde, behält diesen Prompt nach einem Modellwechsel oder Fork, also beginnen Sie einen neuen Chat mit diesem Modell", fail: "Prompt des Agenten abgelehnt", quota: "Kontingent aufgebraucht" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) for (const [agent, note] of [["claude-desktop", "hint"], ["codex", "codex"]]) {
    test(`${engine} ${lang}: Antigravity turning ${agent}'s prompt away isn't a quota`, async (t) => {
      const routes = routesOf(agent, note === "codex" ? codexHint : hint);
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, routes));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${agent}-ag-turned-away.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(routes.length - 1).waitFor();
      const w = { ...want[lang], hint: want[lang][note] };

      // the request's row: the 400 the agent got, the prompt turned away
      const row = page.locator(".rt-req").nth(0);
      const rowText = await row.textContent();
      assert(rowText.includes("400 · " + w.fail), rowText);
      assert(!rowText.includes(w.quota), rowText);

      // the account's tally: turned away twice, no quota used up
      const tally = await page.locator(".rt-acts").textContent();
      assert(tally.includes("2 " + w.fail), tally);
      assert(!tally.includes(w.quota), tally);

      await row.click();
      await page.waitForTimeout(400);
      const got = await page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => [l.className, l.textContent]));
      // the vendor's words as they were, without uni-switch's note in them
      assert.deepEqual(got.filter(([c]) => c === "aside said").map(([, s]) => s), [w.said]);
      // and the note on its own line, in the page's language
      assert.deepEqual(got.filter(([, s]) => s === w.hint).map(([c]) => c), ["aside"]);
      assert(!got.some(([, s]) => s !== w.hint && s.includes(w.quota)), JSON.stringify(got));
      assert.deepEqual(errors, []);
    });
  }
}
