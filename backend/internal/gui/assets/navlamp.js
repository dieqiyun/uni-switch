// The sidebar lights the row under the pointer as a torch would, in its
// pages (Providers, Routing…, Settings), their sub-lists and the
// gateway's address at its foot alike:
// brightest where it points, the rest of that row a step up from the
// others, which keep their own ink (app.css, span.lit). The light
// follows the pointer with a little lag and fades out as it moves on; with
// reduced motion it is where the pointer is at once. Imported by app.js
// when the pointer first comes over the sidebar (layoutNav), not at start,
// where it held up the open page's row; a touch never lights a row.
document.documentElement.classList.add("navlamp");
const lit = new Map(); // a row's word -> how lit it is, 0 to 1
let at = null, tx = 0, x = 0, raf = 0;

function frame() {
  raf = 0;
  const still = calm(), row = at && at.isConnected && !at.parentElement.matches(".cur, .on") ? at : null;
  if (row && !lit.has(row)) lit.set(row, 0);
  x = still ? tx : x + (tx - x) * 0.22;
  let busy = Math.abs(tx - x) > 0.3;
  for (const [s, v] of lit) {
    const to = s === row ? 1 : 0, now = still ? to : v + (to - v) * (to > v ? 0.22 : 0.1);
    if (!to && (now < 0.01 || !s.isConnected)) {
      lit.delete(s);
      s.classList.remove("lit");
      s.style.removeProperty("--si");
      s.style.removeProperty("--lx");
      continue;
    }
    if (Math.abs(to - now) > 0.002) busy = true;
    lit.set(s, now);
    s.classList.add("lit");
    s.style.setProperty("--si", now.toFixed(3));
    s.style.setProperty("--lx", (x - s.getBoundingClientRect().left).toFixed(1) + "px");
  }
  if (busy) raf = requestAnimationFrame(frame);
}
const go = () => { if (!raf) raf = requestAnimationFrame(frame); };

document.addEventListener("pointermove", (e) => {
  const row = e.pointerType === "touch" || !e.target.closest ? null : e.target.closest(".nav-sub > .nav-s, body.side-nav :is(#nav > button[data-view], #prefs, #sideGw)");
  const word = row && row.querySelector(":scope > span");
  if (word && !at) x = e.clientX;
  tx = e.clientX;
  if (word !== at) at = word;
  if (at || lit.size) go();
}, { passive: true });
document.documentElement.addEventListener("pointerleave", () => { at = null; go(); });
