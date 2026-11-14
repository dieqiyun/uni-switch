const assert = require("node:assert/strict");
const http = require("node:http");
const { execFile, execFileSync } = require("node:child_process");
const run = require("node:util").promisify(execFile);
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

// FourLeafTec on #1514: at home on Windows, but mostly running things on a
// Linux server over ssh. Connect's Shell and curl snippets start as the
// shell of the machine uni-switch runs on and can be switched to the other:
// PowerShell ($env:, curl.exe, the body in a here-string) or a POSIX shell
// (export, curl, the body in '…'). The pick is kept, a click on it doesn't
// move the page, and the POSIX snippets run in bash and zsh as written, with
// a key of the user's own that holds quotes, $, ! and a backtick.
const SECRET = "k'ey\"$HOME!`x\\y";

// a stand-in for the gateway: answers with what it was sent
async function echoServer(t) {
  const got = [];
  const server = http.createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      got.push({ url: req.url, headers: req.headers, body });
      res.setHeader("Content-Type", "application/json");
      res.end("{}");
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  t.after(() => new Promise((r) => server.close(r)));
  return { base: `http://127.0.0.1:${server.address().port}`, got };
}
// Resolve the executable once: on Windows a Store alias may precede Git's bash.
const shells = ["bash", "zsh"].flatMap((sh) => {
  try {
    const paths = execFileSync(process.platform === "win32" ? "where.exe" : "which", [sh],
      { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim().split(/\r?\n/);
    for (const binary of paths) {
      if (process.platform === "win32" && /\\(?:WindowsApps|System32)\\/i.test(binary)) continue;
      try {
        execFileSync(binary, ["-c", ":"], { stdio: "ignore" });
        return [binary];
      } catch {}
    }
  } catch {}
  return [];
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 390]) {
      test(`${engine} ${lang} ${width}: the Shell and curl snippets are for Windows or macOS · Linux, as picked`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const open = async (platform) => {
          const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
          await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
          const page = await context.newPage();
          await page.addInitScript(() => { try { localStorage.setItem("uni-switch.gwConnectFolded", "0"); } catch {} }); // Connect opened, as a user who reads it
          page.setDefaultTimeout(6000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          const serve = fixture(lang, "light", []);
          // the gateway key picked is one of the user's own, quotes and all
          await page.route("**/*", (route) => serve(new Proxy(route, { get: (r, k) => k !== "fulfill" ? r[k].bind?.(r) ?? r[k]
            : (o) => r.fulfill(new URL(r.request().url()).pathname.endsWith("/copy-key") ? { ...o, json: { ...o.json, secret: SECRET } } : o) })));
          return { page, errors };
        };
        const snip = (page) => page.locator("#connect pre.snip").textContent();
        const tab = (page, name) => page.locator("#connect .segs:not(.snip-os) .opt", { hasText: new RegExp(`^${name}$`) });
        const os = (page, name) => page.locator("#connect .snip-os .opt", { hasText: name });
        const until = (page, fn, arg) => page.waitForFunction(fn, arg);
        const snipIs = (page, re) => until(page, (src) => new RegExp(src).test(document.querySelector("#connect pre.snip")?.textContent || ""), re.source);

        // On Windows the local gateway uses PowerShell, with a selected named key.
        const { page, errors } = await open("Win32");
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#connectKey").click();
        await page.locator(".proto-menu .pm-item", { hasText: "Server" }).click();
        await page.locator(".proto-menu").waitFor({ state: "detached" });
        await tab(page, "PowerShell").click();
        await snipIs(page, /OPENAI_API_KEY=.*k/);
        assert.equal(await snip(page), `$env:OPENAI_BASE_URL="http://127.0.0.1:3999/v1"\n$env:OPENAI_API_KEY='k''ey"$HOME!\`x\\y'`);
        assert.equal(await os(page, "Windows").getAttribute("aria-pressed"), "true");

        // picked macOS · Linux: export, and nothing moves under the click
        const at = async () => page.evaluate(() => [document.querySelector("#connect .snip-os").getBoundingClientRect().top,
          document.scrollingElement.scrollTop, document.querySelector("#view-gateway").scrollTop]);
        const before = await at();
        await os(page, "macOS · Linux").click();
        await snipIs(page, /^export /);
        assert.deepEqual(await at(), before, "the click moved the page");
        assert.equal(await tab(page, "Shell").count(), 1);
        const env = await snip(page);
        assert.equal(env, `export OPENAI_BASE_URL=http://127.0.0.1:3999/v1\nexport OPENAI_API_KEY='k'\\''ey"$HOME!\`x\\y'`);
        for (const sh of shells) {
          const { stdout: out } = await run(sh, ["-c", `${env}\nprintf '%s\\n' "$OPENAI_BASE_URL" "$OPENAI_API_KEY"`]);
          assert.equal(out, `http://127.0.0.1:3999/v1\n${SECRET}\n`, sh);
        }

        // curl, as a POSIX shell runs it: the key and the body arrive whole
        await tab(page, "curl").click();
        await snipIs(page, /^curl http:\/\/127\.0\.0\.1:3999\/v1\/chat\/completions /);
        const curl = await snip(page);
        assert.doesNotMatch(curl, /curl\.exe|@'/);
        for (const sh of shells) {
          const srv = await echoServer(t);
          // async: the stand-in answers on this process's own loop
          await run(sh, ["-c", curl.replace("http://127.0.0.1:3999", srv.base) + " -sS -o /dev/null --max-time 10"]);
          assert.equal(srv.got.length, 1, sh);
          assert.equal(srv.got[0].url, "/v1/chat/completions");
          assert.equal(srv.got[0].headers.authorization, `Bearer ${SECRET}`, sh);
          assert.equal(JSON.parse(srv.got[0].body).messages[0].content, "hi", sh);
        }

        // kept across a reload; then back to Windows: curl.exe, the body on stdin
        await page.reload();
        await snipIs(page, /^curl http/);
        assert.equal(await os(page, "macOS · Linux").getAttribute("aria-pressed"), "true");
        await os(page, "Windows").click();
        await snipIs(page, /^@'\n/);
        const ps = await snip(page);
        // a " in the key: the line goes to curl.exe as written, after --%
        assert.equal(ps.split("\n").at(-1), `'@ | curl.exe --% http://127.0.0.1:3999/v1/chat/completions -H "Authorization: Bearer k'ey\\"$HOME!\`x\\y" -H "Content-Type: application/json" --data-binary "@-"`);

        // Python and Node read the same everywhere: no shell to pick
        await tab(page, "Python").click();
        await snipIs(page, /from openai import OpenAI/);
        assert.equal(await page.locator("#connect .snip-os").count(), 0);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
        assert.deepEqual(errors, []);

        // on a Mac nothing was picked yet: macOS · Linux
        const mac = await open("MacIntel");
        await mac.page.goto("http://uni-switch.test/?view=gateway");
        await tab(mac.page, "curl").click();
        await snipIs(mac.page, /^curl http/);
        assert.equal(await os(mac.page, "macOS · Linux").getAttribute("aria-pressed"), "true");
        // this computer's key, with nothing to quote: one -H a line
        await os(mac.page, "Windows").click();
        await snipIs(mac.page, /^@'\n/);
        assert.match(await snip(mac.page), /\n'@ \| curl\.exe http:\/\/127\.0\.0\.1:3999\/v1\/chat\/completions `\n {2}-H "Authorization: Bearer uni-switch" `\n {2}-H "Content-Type: application\/json" `\n {2}--data-binary "@-"$/);
        assert.equal(await mac.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
        assert.deepEqual(mac.errors, []);
      });
    }
  }
}
