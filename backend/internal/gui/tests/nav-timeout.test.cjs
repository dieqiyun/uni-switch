// Run with Node's test runner; no browser. A page's default timeout is the
// bar for its clicks and waits (5s in most files), and Playwright gives a
// page.goto or reload the same bar unless the page has a navigation timeout
// of its own. Loading app.js, i18n.js and the plugins takes longer than 5s
// on a busy machine, so a whole-suite run went red at the first load
// (claude-switch-only, #4470). Every page given a default timeout here is
// given a navigation timeout of at least a minute too.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const LOAD = 60000;
const num = (s) => Number(s.replace(/_/g, ""));

test("every page with a short default timeout loads with a minute's room", () => {
  const short = [];
  for (const f of fs.readdirSync(__dirname).filter((f) => f.endsWith(".cjs")).sort()) {
    const src = fs.readFileSync(path.join(__dirname, f), "utf8");
    for (const m of src.matchAll(/(\w+)\.setDefaultTimeout\(([\d_]+)\)/g)) {
      if (num(m[2]) >= LOAD) continue;
      const nav = [...src.matchAll(new RegExp(`\\b${m[1]}\\.setDefaultNavigationTimeout\\(([\\d_]+)\\)`, "g"))];
      if (!nav.length || nav.some((n) => num(n[1]) < LOAD)) short.push(`${f}: ${m[1]}.setDefaultTimeout(${m[2]})`);
    }
  }
  assert.deepEqual(short, [], "a page.goto under a short default times out on a busy machine's first load");
});
