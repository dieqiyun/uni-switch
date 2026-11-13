// The Repair dialog for a folder on the way to uni-switch's own that isn't the
// user's, most often a ~/.config a command run with sudo made root's
// (YouWhat on Discord: adding a provider said "mkdir ~/.config/uni-switch:
// permission denied"). app.js imports this only when /api/state names such
// a folder or a save fails for it (configBlockedAsk), so it weighs nothing
// on the core. Repair makes the folder the user's again, after the system's
// password prompt; asked once a load, and again over whatever save failed,
// which "Not now" goes back to as it was left.
window.cfgBlockedAsk = function (path) {
  if (confirmAsk?.classList.contains("cfg-blocked")) return;
  configBlockedAsked = true;
  const m = $("#modal"), under = !m.hidden && !m.classList.contains("out") && !confirmAsk && m.firstElementChild.firstElementChild;
  const ed = el("div", "editor cfg-blocked"), head = el("div", "ehead"), bar = el("div", "bar");
  head.append(el("b", "", t("uni-switch can't save its settings")));
  ed.append(head, el("p", "lib-confirm", t("{path} belongs to another user, often after a command was run with sudo, so uni-switch can't save providers or settings there. Repair makes it yours again; the system may ask for your password.", { path })));
  const later = el("button", "text", t("Not now")), go = el("button", "text primary", t("Repair"));
  later.onclick = cancelAsk;
  go.onclick = async (e) => {
    e.stopPropagation();
    go.disabled = true;
    try {
      if ((await api("config/repair", {}))?.canceled) return;
      if (state) state.configBlocked = "";
      closeConfirmAsk();
      status(t("Repaired: uni-switch can save again"), "ok");
    } catch (err) {
      status(err.message, "err", 12000);
    } finally {
      go.disabled = false;
    }
  };
  bar.append(el("span", "grow"), later, go);
  ed.append(bar);
  if (under) ed.back = () => openModal(under); // the editor whose save failed, as it was left
  confirmAsk = ed;
  openModal(ed);
  m.classList.add("lib");
  go.focus({ preventScroll: true });
};
