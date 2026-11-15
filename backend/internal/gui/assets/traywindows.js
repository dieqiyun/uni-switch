// Settings › Menu bar windows, and Windows' Tray icon number. app.js imports this only when the menu bar
// shows a card (renderTrayUsage), so it weighs nothing on the core.

// which of a subscription's windows its menu bar card shows, by kind
// (settings.TrayWindows, StrikingRaw on X): a pill each for those ticked
// with two or more, every window unless some are picked. A window the card shows only when picked
// (trayExtra: money aside, one model's, Cursor's Grok Bot, CrazyJal on X)
// is offered too; its usual windows ticked alone are no pick at all.
window.trayWindowsRow = function trayWindowsRow(s, keep, ids, mac) {
  // Windows' tray shows no text: the first card's figure is its icon, or
  // the bird (Benjaminliang_1 on X)
  $("#trayNumberRow").hidden = !document.documentElement.classList.contains("win");
  $("#trayNumberSegs").replaceChildren(segs([["off", t("Off")], ["on", t("On")]], s.trayNoNumber ? "off" : "on",
    (v) => savePrefs({ ...keep, trayNoNumber: v === "off" })));
  const pick = s.trayWindows || {}, box = $("#trayWindowsPick"), subs = [];
  for (const pid of new Set(ids.filter((id) => !id.startsWith("usage:")).map((id) => id.split("|")[0]))) {
    const ws = [], q = (quotas || []).find((q) => q.provider === pid);
    for (const w of q?.windows || []) if (w.trayKind && !ws.some((x) => x.v === w.trayKind)) ws.push({ v: w.trayKind, name: w.name, note: "", extra: !!w.trayExtra });
    if (ws.length > 1) subs.push([pid, q.name, ws]);
  }
  // Windows' tray icon draws the first one's figure (traynumber.go), so
  // which window it is matters there too
  $("#trayWindowsRow").hidden = !subs.length;
  if (!quotas && ids.some((id) => !id.startsWith("usage:"))) loadQuotas().then(() => box.isConnected && trayWindowsRow(s, keep, ids, mac));
  box.replaceChildren(...subs.map(([pid, name, ws]) => {
    const on = (pick[pid] || []).filter((k) => ws.some((w) => w.v === k)), b = el("button", "proto pick" + (on.length ? " set" : ""));
    b.type = "button";
    b.append(el("span", "", name + " · " + (on.length ? on.map((k) => t(ws.find((w) => w.v === k).name)).join(" · ") : t("All windows"))), svg(CHEV, 11, 1.6));
    b.onclick = (e) => {
      e.stopPropagation();
      if (b.classList.contains("open")) return closeProtoMenu();
      openProtoMenu(b, [{ v: "", name: "All windows", note: "" }, ...ws], on, (v) => {
        const next = { ...pick };
        const usual = ws.filter((w) => !w.extra);
        if (v.length && !(v.length === usual.length && usual.every((w) => v.includes(w.v)))) next[pid] = v; else delete next[pid];
        savePrefs({ ...keep, trayWindows: next });
      }, "Shown in the menu bar");
    };
    return b;
  }));
};
