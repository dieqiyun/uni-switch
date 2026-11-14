// The Plugins page's npm registry row: the registry plugins are installed
// from and npm is asked of, and where it came from (the user's .npmrc,
// bunfig or shell, the 「国内镜像」 switch, npm's own); one set here comes
// before all of them (timwangdev on X). plugins.js imports this when
// Discover draws the row, so it weighs nothing on the core. The row is
// plugins.js's box, kept across its redraws, and drawn here alone: a draft
// and its error stay while the page is drawn again.
(() => {
  let box = null, mine = null, askNPM = null;
  let editing = false, draft = "", err = "", saving = false;

  function from(r) {
    switch (r.from) {
      case "uni-switch": return t("set here");
      case "env": return t("from {name}", { name: r.var });
      case "shell": return t("from your shell's {name}", { name: r.var });
      case "bunfig": case "npmrc": return t("from {name}", { name: r.path });
      case "china": return t("Mirrors in China");
    }
    return t("npm's own");
  }

  function button(label, cls, fn) {
    const b = el("button", cls, label);
    b.type = "button";
    b.disabled = saving;
    b.onclick = fn;
    return b;
  }

  async function set(url) {
    saving = true;
    draw();
    try {
      const r = await api("plugins/registry", { url });
      mine.registry = r.registry;
      editing = false;
      draft = err = "";
      // npm's answers again, now from this registry
      askNPM();
    } catch (e) {
      err = t(e.message);
      statusTErr(e);
    }
    saving = false;
    draw();
  }

  function draw() {
    const r = mine.registry;
    box.replaceChildren(el("span", "pm-mlabel", t("npm registry")));
    if (!editing) {
      box.append(el("code", "pm-reg-url", r.url), el("span", "pm-reg-from", from(r)), el("span", "grow"),
        button(t("Change"), "text", () => { editing = true; draft = r.from === "uni-switch" ? r.url : ""; draw(); }));
      if (r.from === "uni-switch") {
        const def = button(t("Use default"), "text", () => set(""));
        def.title = t("Use the registry your .npmrc, .bunfig.toml or shell names, or npm's own");
        box.append(def);
      }
      return;
    }
    const i = el("input", "pm-minput pm-reg-input");
    i.value = draft;
    i.placeholder = "https://registry.npmmirror.com";
    i.spellcheck = false;
    i.autocomplete = "off";
    i.setAttribute("aria-label", t("npm registry"));
    i.oninput = () => { draft = i.value; };
    const save = button(t("Save"), "text primary", () => {
      const v = i.value.trim();
      if (v) set(v); else i.focus(NO_SCROLL);
    });
    const cancel = button(t("Cancel"), "text", () => { editing = false; draft = err = ""; draw(); });
    cancel.disabled = false;
    i.onkeydown = (ev) => { ev.stopPropagation(); if (ev.key === "Enter") save.onclick(); else if (ev.key === "Escape") cancel.onclick(); };
    box.append(i, cancel, save);
    if (err) box.append(el("div", "pm-reg-err", err));
    i.focus(NO_SCROLL);
  }

  // plugins.js calls this at each redraw; an open field is left as it is
  window.pmRegistry = (b, m, ask) => {
    const fresh = box !== b;
    box = b; mine = m; askNPM = ask;
    if (fresh || !editing) draw();
  };
})();
