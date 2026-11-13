// On Omarchy (omarchy.org) uni-switch's windows take the look of Omarchy's own
// panels: the current theme's palette and font, square corners and the
// active border's colour (omarchy.css, under html.omarchy). The theme comes
// with boot.js before the first paint and is asked again every few seconds
// while the page shows, so one picked in Omarchy's menu is taken at once.
// Everywhere else nothing here does anything.
(() => {
  const root = document.documentElement;
  let stamp = "";
  let style = null;
  function apply(th) {
    if (!th || !th.vars) return;
    root.classList.add("omarchy");
    root.dataset.omarchyMode = th.mode || "dark";
    if (th.stamp === stamp) return;
    stamp = th.stamp;
    const decls = Object.entries(th.vars).map(([k, v]) => `${k}: ${v} !important;`).join(" ");
    if (!style) {
      style = document.createElement("style");
      style.id = "omarchy-theme";
      document.head.append(style);
    }
    // on the root and the body both, so neither the light/dark palettes nor
    // the panel's own tints (set on the body) win over the theme
    style.textContent = `:root.omarchy, :root.omarchy body { ${decls} color-scheme: ${th.mode === "light" ? "light" : "dark"}; }`;
    for (const e of document.querySelectorAll(".om-theme-name")) e.textContent = th.name || "";
    window.omarchyTheme = th;
    window.dispatchEvent(new CustomEvent("omarchy-theme", { detail: th }));
  }
  const boot = window.bootPrefs && window.bootPrefs.omarchy;
  if (!boot) return;
  apply(boot);
  // the settings' theme name is drawn after this script runs
  document.addEventListener("DOMContentLoaded", () => { for (const e of document.querySelectorAll(".om-theme-name")) e.textContent = th().name || ""; });
  const th = () => window.omarchyTheme || boot;
  async function poll() {
    if (document.visibilityState === "visible") {
      try {
        const r = await fetch("/api/omarchy", { cache: "no-store" });
        if (r.ok) apply(await r.json());
      } catch {}
    }
    setTimeout(poll, 2500);
  }
  setTimeout(poll, 2500);
})();
