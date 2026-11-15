// The window's plugin kernel (docs/subsystems/plugin-runtime.md, P1). A
// plugin's page is a part it adds to the "page" slot: its markup, put in the
// page where index.html's <template data-slot="page"> is, and its hooks,
// which the shell calls through uni-switch.call. A hook that throws breaks only
// its own page: the page says so, with a Try again, and the other pages go
// on (the error boundary). ctx.ui is the kit a plugin draws with, and
// ui.form draws a plugin's settings from the schema the Go side made of
// them (pluginrt.SchemaOf).
//
// Loaded after i18n.js and before the plugins' scripts and app.js. A
// plugin's words come with it (its i18n.js, uni-switch.strings). Its scripts
// register at once, so its page is in the page before app.js looks for
// it, but run nothing of app.js's then: mount runs at uni-switch.start, which
// app.js calls once it is set up.
(() => {
  const plugins = new Map(); // id → { id, config, onConfig: [], disposers: [] }
  const pages = new Map(); // key → { plugin, part, main, mounted, broken }
  let started = false;

  function context(p) {
    return {
      id: p.id,
      get config() { return p.config; },
      // a change of the plugin's settings (Plugins › Built-in), after
      // ctx.config has it
      onConfig(fn) { p.onConfig.push(fn); },
      onDispose(fn) { p.disposers.push(fn); },
      // a page of the plugin's that broke after its hook returned (a
      // frame's own error, one that never answered): it shows what broke,
      // with a Try again, as a hook that throws does
      broke(key, err) {
        const page = pages.get(key);
        if (page && page.plugin === p && !page.broken) fail(page, "open", [], err);
      },
      slots: { add: (slot, part) => add(p, slot, part) },
      ui,
    };
  }

  function add(p, slot, part) {
    if (slot !== "page") throw new Error(`no slot ${slot}`);
    if (!/^[a-z][a-z0-9-]*$/.test(part.key || "") || pages.has(part.key) || document.getElementById("view-" + part.key)) {
      throw new Error(`page ${part.key} is taken`);
    }
    const main = document.createElement("main");
    main.className = "view";
    main.id = "view-" + part.key;
    main.hidden = true;
    main.dataset.feature = p.id;
    main.innerHTML = part.html || "";
    window.uniSwitch?.fillSlots?.(main);
    const anchor = document.querySelector('template[data-slot="page"]');
    if (anchor) anchor.before(main);
    else document.body.append(main);
    const page = { plugin: p, part, main, mounted: false, broken: null };
    pages.set(part.key, page);
    if (started) run(page, "mount", [main]);
  }

  // The error boundary: a hook that throws (or whose promise fails) is
  // caught here, and the page shows what broke instead of what it had.
  function fail(page, hook, args, err) {
    console.error(`[uni-switch] ${page.plugin.id}: ${page.part.key}.${hook}`, err);
    page.broken = { hook, args };
    const card = document.createElement("div");
    card.className = "list slot-error";
    card.setAttribute("role", "alert");
    const row = document.createElement("div");
    row.className = "row";
    const who = document.createElement("div");
    who.className = "who";
    const name = document.createElement("div");
    name.className = "name";
    name.textContent = t("This page stopped working");
    const sub = document.createElement("div");
    sub.className = "sub";
    sub.textContent = String(err?.message || err);
    who.append(name, sub);
    const again = document.createElement("button");
    again.type = "button";
    again.className = "text slot-again";
    again.textContent = t("Try again");
    again.onclick = () => retry(page);
    row.append(who, again);
    card.append(row);
    page.main.classList.remove("loading");
    page.main.removeAttribute("aria-busy");
    page.main.replaceChildren(card);
  }

  // Try again: the page's markup as it came, mounted again, and the hook
  // that broke run again with what it was given.
  function retry(page) {
    const { hook, args } = page.broken;
    page.broken = null;
    page.mounted = false;
    page.main.innerHTML = page.part.html || "";
    window.uniSwitch?.fillSlots?.(page.main);
    window.translateStatic?.();
    if (hook !== "mount") run(page, "mount", [page.main]);
    run(page, hook, args);
  }

  function run(page, hook, args) {
    if (page.broken) return undefined;
    if (hook === "mount") {
      if (page.mounted) return undefined;
      page.mounted = true;
    } else if (!page.mounted && started) {
      run(page, "mount", [page.main]);
      if (page.broken) return undefined;
    }
    const fn = page.part[hook];
    if (typeof fn !== "function") return undefined;
    try {
      const out = fn.apply(page.part, args);
      if (out && typeof out.then === "function") {
        return out.catch((err) => { if (!page.broken) fail(page, hook, args, err); });
      }
      return out;
    } catch (err) {
      fail(page, hook, args, err);
      return undefined;
    }
  }

  // ---------- the kit ----------
  // What a plugin draws with, as the rest of the window draws: el, the app
  // menu (never a native <select>), the switch, the settings row.
  const ui = {
    el: (tag, cls, text) => el(tag, cls, text),
    button(label, onClick, cls = "text") {
      const b = el("button", cls, label);
      b.type = "button";
      b.onclick = onClick;
      return b;
    },
    toggle(on, label, onChange) {
      const b = el("button", "lib-switch use-credits" + (on ? " on" : ""));
      b.type = "button";
      b.setAttribute("role", "switch");
      b.setAttribute("aria-checked", String(on));
      b.setAttribute("aria-label", label);
      b.append(el("i"));
      b.onclick = () => {
        on = !on;
        b.classList.toggle("on", on);
        b.setAttribute("aria-checked", String(on));
        onChange(on);
      };
      return b;
    },
    // a pick of one of opts ([{ v, name }]), in the app's menu
    menu(opts, value, head, onPick) {
      const b = el("button", "proto pick");
      b.type = "button";
      const paint = () => {
        const o = opts.find((x) => x.v === value);
        b.replaceChildren(el("span", "", o ? o.name : String(value)), svg(CHEV, 11, 1.6));
      };
      paint();
      b.onclick = (ev) => {
        ev.stopPropagation();
        openProtoMenu(b, opts.map((o) => ({ ...o, literalName: true, note: "" })), value, (v) => {
          value = v;
          paint();
          onPick(v);
        }, head, "", "right");
      };
      return b;
    },
    // a settings row: what it is, and its control at the end
    row(title, description, control) {
      const r = el("div", "row pref");
      const who = el("div", "who");
      who.append(el("div", "name", title));
      if (description) who.append(el("div", "sub", description));
      r.append(who, control);
      return r;
    },
    // The rows of a plugin's settings, drawn from its schema: true or false
    // a switch, one of a few a menu, a number a field kept to its range,
    // text a field. onChange gets { key: value } for the one changed.
    form(schema, values, onChange) {
      const rows = [];
      for (const key of schema?.["x-order"] || Object.keys(schema?.properties || {})) {
        const p = schema.properties[key];
        if (!p) continue;
        const value = values?.[key] ?? p.default;
        const title = t(p.title), say = p.description ? t(p.description) : "";
        let control;
        if (p.type === "boolean") control = ui.toggle(!!value, title, (v) => onChange({ [key]: v }));
        else if (p.enum?.length) {
          control = ui.menu(p.enum.map((v) => ({ v, name: typeof v === "number" ? v.toLocaleString(locale) : t(String(v)) })), value, p.title, (v) => onChange({ [key]: v }));
        } else {
          const number = p.type === "integer" || p.type === "number";
          control = el("input", "set-field");
          control.type = number ? "number" : "text";
          control.value = value ?? "";
          control.setAttribute("aria-label", title);
          if (number) {
            if (p.minimum !== undefined) control.min = p.minimum;
            if (p.maximum !== undefined) control.max = p.maximum;
            if (p.type === "integer") control.step = 1;
          }
          control.onchange = () => {
            let v = control.value;
            if (number) {
              v = Number(v);
              const bad = control.value === "" || !Number.isFinite(v) || (p.type === "integer" && !Number.isInteger(v))
                || (p.minimum !== undefined && v < p.minimum) || (p.maximum !== undefined && v > p.maximum);
              control.setAttribute("aria-invalid", String(bad));
              if (bad) return;
            }
            onChange({ [key]: v });
          };
        }
        const r = ui.row(title, say, control);
        r.dataset.setting = key;
        rows.push(r);
      }
      return rows;
    },
  };

  window.uniSwitch = {
    ui,
    plugin(id, setup) {
      if (plugins.has(id)) throw new Error(`plugin ${id} is registered twice`);
      const p = { id, config: { ...(window.bootPrefs?.pluginConfig?.[id] || {}) }, onConfig: [], disposers: [] };
      plugins.set(id, p);
      try {
        setup(context(p));
      } catch (err) {
        console.error(`[uni-switch] ${id}: setup`, err);
      }
    },
    // app.js, once it is set up: every page is mounted
    start() {
      if (started) return;
      started = true;
      for (const page of pages.values()) run(page, "mount", [page.main]);
    },
    // a page's hook, behind the boundary; nothing for a page no plugin has
    call(key, hook, ...args) {
      const page = pages.get(key);
      return page ? run(page, hook, args) : undefined;
    },
    // a plugin's words in each language ({ zh: { English: 中文 }, … }),
    // added to i18n.js's; one already there is a mistake, said and not
    // taken, as the later one would silently win
    strings(dict) {
      for (const [lang, words] of Object.entries(dict)) {
        const to = (I18N[lang] ||= {});
        for (const [k, v] of Object.entries(words)) {
          if (k in to) console.error(`[uni-switch] ${lang} ${JSON.stringify(k)} is said twice`);
          else to[k] = v;
        }
      }
    },
    page: (key) => pages.get(key)?.part,
    pages: () => [...pages.keys()],
    // the plugins' settings as Plugins › Built-in or boot.js has them
    configure(id, config) {
      const p = plugins.get(id);
      if (!p || !config) return;
      if (JSON.stringify(p.config) === JSON.stringify(config)) return;
      p.config = { ...config };
      for (const fn of p.onConfig) {
        try { fn(p.config); } catch (err) { console.error(`[uni-switch] ${id}: config`, err); }
      }
    },
  };
})();
