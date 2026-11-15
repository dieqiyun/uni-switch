// A direct, client-scoped entry to the retained image model and Library APIs.
(() => {
  let host, client, expanded = false, library, loading = false, busy = false, error = "", generation = 0, libraryRevision = 0, stamp = "";
  let imageSource, keyField, editingKey = false, keyValue = "", keyError = "", keyPhase = "", keyRun = 0, keyTimer, keyRequest;
  const serverName = "uni-switch-image";
  const server = () => library?.servers?.find(s => s.name === serverName);
  const currentAgent = () => library?.agents?.find(a => a.id === client.id);
  const enabled = () => !!server()?.agents?.includes(client.id);
  const problems = () => (library?.problems || []).filter(p => p.agent === client.id && p.what === "mcp:" + serverName);
  const control = (name, cls, run) => {
    const b = el("button", cls, t(name)); b.type = "button"; b.onclick = run; return b;
  };
  const focusedDraw = (fallback) => {
    const focused = host?.contains(document.activeElement) && document.activeElement.dataset.imageControl
      || (document.activeElement === document.body ? fallback : null);
    const selection = focused === "key" ? [keyField.selectionStart, keyField.selectionEnd] : null;
    stamp = ""; draw();
    if (focused) host.querySelector('[data-image-control="' + focused + '"]')?.focus(NO_SCROLL);
    if (selection) keyField.setSelectionRange(...selection);
  };
  function cancelKey(clear = false) {
    clearTimeout(keyTimer); keyRun++; keyRequest?.abort(); keyRequest = null;
    keyPhase = "";
    if (clear) { keyValue = ""; if (keyField) keyField.value = ""; keyError = ""; editingKey = false; }
  }
  async function fetchModels() {
    const value = keyValue.trim();
    if (!value && !imageSource?.configured) return;
    cancelKey();
    const run = keyRun, focus = host.contains(document.activeElement) ? document.activeElement.dataset.imageControl : null;
    const request = keyRequest = new AbortController();
    keyPhase = "loading"; keyError = ""; focusedDraw();
    try {
      const result = await api("image-provider", value ? {key:value} : {}, undefined, request.signal);
      if (run !== keyRun) return;
      imageSource = result; keyValue = ""; keyField.value = "";
      const settings = await api("settings", undefined, undefined, request.signal);
      if (run !== keyRun) return;
      prefs = settings; if (state) state.settings = prefs;
      // New setups use the first returned image model; explicit picks and Off survive.
      if ((!prefs.imageGen || prefs.imageGenMissing) && result.models?.length) {
        await savePrefs({...prefsKeep(prefs), imageGen:result.models[0].id});
      }
      if (run !== keyRun) return;
      keyPhase = "saved"; editingKey = false; renderSettings();
    } catch (e) {
      if (run !== keyRun || request.signal.aborted) return;
      keyError = e.message || String(e); keyPhase = "";
    } finally {
      if (run === keyRun) { keyRequest = null; focusedDraw(keyPhase === "saved" ? "edit" : focus); }
    }
  }
  function sourceForm() {
    if (imageSource?.configured && !editingKey) {
      const summary = el("div", "uni-image-source uni-image-configured"), copy = el("div", "");
      copy.append(el("strong", "", t("Image service configured")), el("p", "hint", t("API key configured. Image models are ready to select.")));
      const edit = control("Edit", "text", () => {
        cancelKey(true); editingKey = true; focusedDraw(); keyField.focus(NO_SCROLL);
      });
      edit.dataset.imageControl = "edit";
      edit.setAttribute("aria-label", t("Edit image settings"));
      summary.append(copy, edit); return summary;
    }
    const form = el("form", "uni-image-source");
    form.id = "uniImageCredentials";
    form.onsubmit = e => { e.preventDefault(); fetchModels(); };
    const label = el("label", "name", "API Key"); label.htmlFor = "uniImageKey";
    const hint = el("p", "hint", t("Uses DieqiYun's image service. Enter an API key to load image models automatically."));
    hint.id = "uniImageKeyHint";
    hint.append(el("span", "uni-image-url", imageSource?.baseUrl || "https://cfimg.dieqiyun.top/v1"));
    if (!keyField) {
      keyField = el("input", ""); keyField.id = "uniImageKey"; keyField.type = "password";
      keyField.autocomplete = "new-password"; keyField.spellcheck = false;
      keyField.setAttribute("autocapitalize", "none"); keyField.dataset.imageControl = "key";
      keyField.oninput = () => {
        keyValue = keyField.value; cancelKey(); keyError = "";
        if (keyValue.trim()) { keyPhase = "waiting"; keyTimer = setTimeout(fetchModels, 700); }
        focusedDraw();
      };
    }
    keyField.placeholder = imageSource?.configured ? t("Configured. Enter a new key to replace it.") : t("Enter API key");
    keyField.setAttribute("aria-describedby", "uniImageKeyHint uniImageKeyStatus");
    keyField.setAttribute("aria-invalid", String(!!keyError));
    const row = el("div", "uni-image-key-row");
    const refresh = control(imageSource?.configured ? "Refresh image models" : "Get image models", "text", fetchModels);
    refresh.dataset.imageControl = "fetch";
    refresh.disabled = keyPhase === "loading" || (!keyValue.trim() && !imageSource?.configured);
    row.append(keyField, refresh);
    if (imageSource?.configured) {
      const cancel = control("Cancel", "text", () => {
        cancelKey(true); focusedDraw(); host.querySelector('[data-image-control="edit"]')?.focus(NO_SCROLL);
      });
      cancel.dataset.imageControl = "cancel-key"; row.append(cancel);
    }
    const info = el("div", "hint uni-image-key-status"); info.id = "uniImageKeyStatus";
    info.setAttribute("role", keyError ? "alert" : "status");
    if (keyError) {
      const reason = /image_models_empty|no models listed/.test(keyError) ? t("No image models are available for this key. Check its image service permissions.")
        : /image_provider_conflict/.test(keyError) ? t("The image provider configuration conflicts with an existing provider. Check it before retrying.")
        : /^image_models_failed:/.test(keyError) ? connectionReason(keyError)
        : t("Image setup could not be applied. Check the details and retry.");
      info.append(el("span", "", reason));
      const details = el("details", "connection-details");
      details.append(el("summary", "", t("View details")), el("pre", "", keyError)); info.append(details);
    } else info.textContent = keyPhase === "loading" ? t("Loading image models…")
      : keyPhase === "waiting" ? t("Image models will load after you finish typing.")
      : imageSource?.configured ? t("API key configured. Image models are ready to select.") : "";
    form.setAttribute("aria-busy", String(keyPhase === "loading"));
    form.append(label, hint, row, info); return form;
  }
  async function load() {
    const run = ++generation;
    loading = true; error = ""; editingKey = false; focusedDraw();
    const results = await Promise.allSettled([api("settings"), api("library?glance=1"), api("image-provider")]);
    if (run !== generation) return;
    loading = false;
    if (results[0].status === "fulfilled") {
      prefs = results[0].value;
      if (state) state.settings = prefs;
      renderSettings();
    }
    if (results[1].status === "fulfilled") library = results[1].value;
    else library = null;
    imageSource = results[2].status === "fulfilled" ? results[2].value : null;
    libraryRevision++;
    error = results.filter(r => r.status === "rejected").map(r => r.reason.message || String(r.reason)).join("\n");
    focusedDraw();
  }
  async function toggle() {
    if (busy || loading || !library) return;
    const id = client.id, next = !enabled(), run = generation;
    const focus = host.contains(document.activeElement) ? document.activeElement.dataset.imageControl : null;
    busy = true; error = ""; focusedDraw();
    try {
      // Re-read before changing one client so other clients' selections survive.
      const fresh = await api("library?glance=1");
      const existing = fresh.servers?.find(s => s.name === serverName);
      const agents = [...new Set([...(existing?.agents || []).filter(a => a !== id), ...(next ? [id] : [])])];
      const result = existing
        ? await api("library/servers/agents", {name:serverName, agents})
        : next ? await api("library/market/server", {id:serverName, values:{}, agents}) : fresh;
      if (run !== generation) return;
      library = result;
      libraryRevision++;
      if (!problems().length) status(t("Saved"), "ok", 1500);
    } catch (e) {
      if (run === generation) error = e.message || String(e);
    } finally {
      busy = false;
      focusedDraw(run === generation ? focus : null);
    }
  }
  function draw() {
    if (!host || !client) return;
    const s = prefs || state?.settings || {};
    const signature = JSON.stringify([client, locale, expanded, loading, busy, error, editingKey, keyRun, keyPhase, keyError, imageSource?.configured, s.imageGen, s.imageGenShown, s.imageGenAuto, s.imageGenModels, s.imageGenMissing, s.imageTimeout, libraryRevision]);
    if (signature === stamp) return;
    stamp = signature;
    const advancedOpen = !!host.querySelector(".uni-image-advanced")?.open;
    const words = el("span", "uni-disclosure-copy");
    words.append(el("strong", "", t("Image generation")), el("span", "hint", t("Choose an image model and enable the image tool for this client.")));
    const entry = control("", "uni-image-heading uni-image-open", () => {
      expanded = !expanded;
      if (!expanded) cancelKey(true);
      focusedDraw();
      if (expanded) load();
    });
    const action = el("span", "uni-disclosure-action", t(expanded ? "Collapse" : "Image setup"));
    const arrow = svg("m4 6 4 4 4-4", 16, 1.8); arrow.setAttribute("aria-hidden", "true");
    action.append(arrow);
    entry.append(words, action);
    entry.dataset.imageControl = "open";
    entry.setAttribute("aria-expanded", String(expanded));
    entry.setAttribute("aria-controls", "uniImageSetupBody");
    const body = el("div", "uni-image-body"); body.id = "uniImageSetupBody"; body.hidden = !expanded;
    host.replaceChildren(entry, body);
    if (!expanded) return;
    if (loading) {
      const wait = el("p", "hint", t("Loading…")); wait.setAttribute("role", "status"); body.append(wait); return;
    }
    body.append(sourceForm());
    const models = el("div", "list prefs uni-image-models");
    renderImageGen(s, prefsKeep(s), models);
    models.querySelector(".lib-way")?.remove();
    const row = models.querySelector("#imageGenRow");
    row.id = "uniImageModelRow";
    const timeout = models.querySelector("#imageTimeoutRow"); timeout.id = "uniImageTimeoutRow";
    timeout.querySelectorAll("button").forEach((b, i) => { b.dataset.imageControl = "timeout-" + i; });
    // This is the same shared model used by Settings; injection is per client.
    row.querySelector(".name").textContent = t("Image model (shared by clients)");
    row.querySelector(".sub.wraps").textContent = t("The selected model generates images for every enabled client. Images are saved in the project.");
    row.querySelector(".rt-cond").setAttribute("aria-label", t("Image model (shared by clients)"));
    row.querySelector(".rt-cond").dataset.imageControl = "model";
    body.append(models);
    const injection = el("div", "row pref uni-image-injection"), who = el("div", "who");
    const on = enabled(), agent = currentAgent();
    const ready = !!(s.imageGen !== "off" && (s.imageGenAuto || s.imageGenModels?.length));
    let explanation = t("Enable to add the image tool to {client}. Reopen the client or start a new session after changing it.", {client:client.name});
    if (!library) explanation = t("Client configuration could not be read. Retry before enabling the image tool.");
    else if (!agent?.mcp || agent.gone || agent.asleep) explanation = t("This client is unavailable or does not support automatic image tool setup.");
    else if (!ready && !on) explanation = t("Enter an API key above to load image models, then select one.");
    who.append(el("div", "name", t("Image tool for {client}", {client:client.name})), el("div", "sub wraps", explanation));
    const sw = control(on ? "On" : "Off", "text uni-image-toggle", toggle);
    const track = el("span", "uni-switch-track"); track.setAttribute("aria-hidden", "true");
    track.append(el("span", "uni-switch-thumb")); sw.prepend(track);
    sw.dataset.imageControl = "inject";
    sw.setAttribute("role", "switch"); sw.setAttribute("aria-checked", String(on));
    sw.setAttribute("aria-label", t("Image tool for {client}", {client:client.name}));
    sw.disabled = busy || !library || (!on && (!agent?.mcp || agent.gone || agent.asleep || !ready));
    sw.setAttribute("aria-describedby", "uniImageInjectionHint"); who.lastChild.id = "uniImageInjectionHint";
    if (busy) sw.setAttribute("aria-busy", "true");
    const val = el("div", "val"); val.append(sw); injection.append(who, val); body.append(injection);
    const issues = problems();
    if (error || issues.length) {
      const failed = el("div", "uni-image-error"); failed.setAttribute("role", "alert");
      failed.append(el("span", "", t("Image setup could not be applied. Check the details and retry.")));
      const details = el("details", "connection-details");
      details.append(el("summary", "", t("View details")), el("pre", "", error || issues.map(p => p.error).join("\n")));
      failed.append(details);
      const retry = control("Retry", "text", load); retry.disabled = busy; failed.append(retry); body.append(failed);
    }
    const advanced = el("details", "newcomer-fold uni-image-advanced");
    advanced.open = advancedOpen;
    const summary = el("summary", "", t("Advanced configuration")); summary.dataset.imageControl = "advanced";
    advanced.append(summary, timeout);
    body.append(advanced);
  }
  window.renderClientImageSetup = (id, name) => {
    if (!host) {
      host = el("section", "uni-image-setup"); host.id = "uniImageSetup";
      $("#uniClientControls").after(host);
    }
    if (client?.id !== id) {
      cancelKey(true);
      generation++; client = {id,name}; library = null; libraryRevision++; error = ""; loading = false;
      if (expanded) { load(); return; }
    }
    draw();
  };
  window.refreshClientImageSetup = () => { if (expanded) focusedDraw(); };
})();
