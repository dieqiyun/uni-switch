// Run with Node's test runner and Playwright on the module path; see README.md.
// What a model makes, said by the user (shen-joe, #1399: a relay's
// agnes-video-2.5-flash was turned away as a model that can't make videos,
// its /models marking nothing, and there was nowhere to say it does). A
// model chip's right-click has "Makes: Auto…" for a provider set up with a
// base URL (marksKinds): it opens the app's menu (never a native <select>),
// and the pick is staged as a model's API is — the chip tagged, the page
// not moved, nothing saved — and sent with the Save as modelPrefs' kind.
// One the user said already shows its tag and its pick; a provider whose
// models can't be marked (a subscription's) has no such item. In English,
// Chinese, Japanese and German, at a wide and a narrow window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = { icon: "generic", catalog: "", agents: [], fallback: [], headers: {}, keyList: [], balanceToken: { takes: false, set: false }, proxy: "" };
const relay = {
  ...base, id: "relay", name: "Relay", chat: "https://relay.example.com/v1", responses: "", anthropic: "", marksKinds: true,
  models: [{ id: "agnes-video-2.5-flash", name: "agnes-video-2.5-flash", on: true }, { id: "pic-9", name: "pic-9", on: true, kind: "image" }],
  key: { set: true, masked: "sk-…one" },
};
const sub = {
  ...base, id: "copilot", name: "GitHub Copilot", icon: "githubcopilot",
  chat: "https://api.githubcopilot.com", responses: "", anthropic: "",
  account: { agent: "copilot", user: "octo", accounts: [] },
  models: [{ id: "gpt-9", name: "GPT-9", on: true }],
  key: { set: false, masked: "" },
};

function serve(lang, posts) {
  const providers = { providers: [relay, sub], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/provider/save") { posts.push(route.request().postDataJSON()); return json(providers); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { auto: "Makes: Auto…", video: "Video model", image: "Makes: Image model…", head: "What this model makes", tagVideo: "video", tagImage: "image", save: "Save", staged: "agnes-video-2.5-flash makes videos once saved" },
  zh: { auto: "类型：自动…", video: "视频模型", image: "类型：生图模型…", head: "这个模型生成什么", tagVideo: "视频", tagImage: "图片", save: "保存", staged: "保存后 agnes-video-2.5-flash 将作为视频模型" },
  ja: { auto: "種類：自動…", video: "動画モデル", image: "種類：画像モデル…", head: "このモデルが生成するもの", tagVideo: "動画", tagImage: "画像", save: "保存", staged: "保存すると agnes-video-2.5-flash は動画モデルになります" },
  de: { auto: "Erzeugt: Automatisch…", video: "Videomodell", image: "Erzeugt: Bildmodell…", head: "Was dieses Modell erzeugt", tagVideo: "Video", tagImage: "Bild", save: "Speichern", staged: "agnes-video-2.5-flash erzeugt nach dem Speichern Videos" },
};

// scroll the chip in and let the frame pass before the right-click, or
// Chromium's late scroll event closes the menu it opened
async function rclick(loc) {
  await loc.click({ button: "right", trial: true });
  await loc.page().evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  await loc.click({ button: "right" });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [900, 420]) {
      const w = words[lang];
      test(`${engine} ${lang} ${width}: a model marked a video model, staged and saved`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-model-kind.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posts = [];
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://uni-switch.test/?view=providers");
        const open = async (name) => {
          await page.locator(".row.provider", { hasText: name }).first().click();
          await page.locator(".editor .mchips .mchip").first().waitFor();
        };
        const chip = (id) => page.locator(".editor .mchips .mchip", { hasText: id });
        const rowMenu = page.locator(".pop.row-menu");
        const kindMenu = page.locator(".pop.proto-menu.model-kind-menu");

        await open("Relay");
        // one said before: its tag, and its pick in the menu
        assert.equal(await chip("pic-9").locator(".mkind-tag").textContent(), w.tagImage);
        await rclick(chip("pic-9"));
        await rowMenu.getByRole("menuitem", { name: w.image }).waitFor();
        await page.keyboard.press("Escape");
        await rowMenu.waitFor({ state: "detached" });

        const agnes = chip("agnes-video-2.5-flash");
        assert.equal(await agnes.locator(".mkind-tag").count(), 0);
        const top = () => page.evaluate(() => [scrollY, ...[...document.querySelectorAll(".editor, .editor *")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join());
        const before = await top();
        const picked = await page.locator(".editor .mchips .mchip.on").count();
        await rclick(agnes);
        await rowMenu.getByRole("menuitem", { name: w.auto }).click();
        await kindMenu.waitFor();
        assert.equal(await kindMenu.locator(".pm-head").textContent(), w.head);
        assert.equal(await kindMenu.locator(".pm-item").count(), 3, "Auto, image and video");
        assert.equal(await page.locator(".editor select").count(), 0, "no native select");
        const box = await kindMenu.boundingBox();
        assert(box.x >= 0 && box.x + box.width <= width + 0.5, `the menu fits the window: ${JSON.stringify(box)}`);
        const border = await page.evaluate(() => [...document.querySelectorAll(".pop.proto-menu, .pop.proto-menu *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
        assert.deepEqual(border, [], "no left-border accent");
        await kindMenu.locator(".pm-item", { hasText: w.video }).click();
        await kindMenu.waitFor({ state: "detached" });
        await chip("agnes-video-2.5-flash").locator(".mkind-tag").waitFor();
        assert.equal(await chip("agnes-video-2.5-flash").locator(".mkind-tag").textContent(), w.tagVideo);
        assert.equal(await page.locator("#status").textContent(), w.staged);
        assert.equal(await top(), before, "the page didn't move");
        assert.equal(await page.locator(".editor .mchips .mchip.on").count(), picked, "nothing picked or unpicked");
        assert.equal(posts.length, 0, "staged, not saved");
        await page.locator(".editor").getByRole("button", { name: w.save, exact: true }).click();
        for (let i = 0; i < 40 && !posts.length; i++) await page.waitForTimeout(50);
        assert.deepEqual(posts[0]?.modelPrefs, { "agnes-video-2.5-flash": { kind: "video" } });

        // a subscription's models can't be marked: on a page loaded
        // afresh, the Save's own reply left aside
        await page.goto("http://uni-switch.test/?view=providers");
        await open("GitHub Copilot");
        await rclick(chip("GPT-9"));
        await rowMenu.waitFor();
        assert.equal(await rowMenu.getByRole("menuitem", { name: w.auto }).count(), 0, "nothing to mark");
        assert.deepEqual(errors, []);
      });
    }
  }
}
