// Progressive settings and account automation; values continue to use the core API.
function connectionReason(message, status) {
  const msg = String(message || "");
  const code = Number(status) || Number(msg.match(/(?:^|\b)(401|403|404|429|5\d\d)(?:\b)/)?.[1]);
  if (code === 401 || /invalid[_ -]?(api[_ -]?)?key|incorrect api key|unauthorized|密钥无效/i.test(msg)) return t("API key is invalid. Check or replace it.");
  if (/insufficient_quota|out of (?:quota|credit)|insufficient (?:balance|credit)|余额不足/i.test(msg)) return t("This account has no remaining allowance.");
  if (code === 403) return t("This account cannot access this service. Check its permissions.");
  if (code === 404) return t("The address or model was not found. Check the provider's address.");
  if (code === 429) return t("Too many requests. Try again shortly.");
  if (/certificate|x509|tls handshake/i.test(msg)) return t("The server's certificate could not be verified.");
  if (/timeout|timed out|deadline exceeded/i.test(msg)) return t("The connection timed out. Check the address and proxy.");
  if (/connection refused|econnrefused|enotfound|no such host|dial tcp|failed to fetch|networkerror|无法连接|actively refused/i.test(msg)) return t("The address cannot be reached. Check the address and network.");
  if (code >= 500) return t("The provider is temporarily unavailable. Try again later.");
  return t("Connection failed. Check the address and login information.");
}
function connectionFailure(box, message, status) {
  const raw = String(message || ""), reason = connectionReason(raw, status);
  box.classList.add("connection-failure");
  box.replaceChildren(el("span", "connection-reason", reason));
  box.setAttribute("aria-live", "polite");
  if (raw && raw !== reason) {
    const details = el("details", "connection-details");
    details.append(el("summary", "", t("View details")), el("pre", "", raw));
    box.append(details);
  }
}

// Per-account controls never save the provider draft or another account's settings.
function accountAutomation(agent, user, quota) {
  const s = prefs || state?.settings || {}, rows = [];
  const key = agent + "|" + user.toLowerCase();
  const row = (label, controls) => {
    const r = el("div", "account-automation-row");
    r.append(el("span", "", t(label)), controls);
    rows.push(r);
  };
  const error = el("div", "account-automation-error");
  error.setAttribute("role", "status");
  const save = async (path, body) => {
    const focused=document.activeElement;
    const focusRow=rows.findIndex(r=>r.contains(focused));
    const focusLabel=focused?.textContent,focusTag=focused?.tagName;
    for (const r of rows) for (const b of r.querySelectorAll("button,input")) b.disabled = true;
    try {
      prefs = await writingPrefs(api(path, body));
      if (state) state.settings = prefs;
      // Replace only these controls: preserve the provider's unsaved model picks.
      const fresh = accountAutomation(agent, user, quota);
      const anchor = error.parentElement;
      if (anchor) { rows.forEach(r => r.remove()); error.replaceWith(...fresh);
        if(focusRow>=0) [...fresh[focusRow].querySelectorAll("button,input")].find(n=>n.tagName===focusTag&&n.textContent===focusLabel)?.focus(NO_SCROLL);
      }
    } catch (e) {
      connectionFailure(error, e.message);
      for (const r of rows) for (const b of r.querySelectorAll("button,input")) b.disabled = false;
    }
  };
  if (agent === "codex" || agent === "claude") {
    const legacy = agent === "codex" ? s.codexWarmAtOf?.[user.toLowerCase()] : undefined;
    const own = s.accountWarmups?.[key] || {
      reset: s[agent + "Warmup"] || "",
      times: legacy === "off" ? [] : legacy ? [legacy] : warmTimes(s[agent + "WarmAts"], s[agent + "WarmAt"]),
    };
    const update = changes => save("settings/account-warmup", { agent, user, ...own, ...changes });
    const reset = segs([["", t("Off")], ["week", t("Weekly")], ["all", t("Weekly and 5-hour")]], own.reset || "", v => update({ reset: v }));
    reset.setAttribute("aria-label", t("Warm up on reset") + " · " + user);
    row("Warm up on reset", reset);
    const box = el("div", "warm-at"), sub = el("span");
    renderWarmAt(box, sub, own.times || [], own.reset, "", times => update({ times }));
    box.setAttribute("aria-label", t("Daily warm-up") + " · " + user);
    row("Daily warm-up", box);
    const go = el("button", "text", t("Warm up now")); go.type = "button";
    go.onclick = async () => {
      go.disabled = true;
      try { await api("warm-now", { agent, user }); status(t("Warmed up {who}", { who:user }), "ok"); }
      catch (e) { connectionFailure(error, e.message); }
      finally { go.disabled = false; }
    };
    row("Warm up", go);
  }
  const acct = (s.checkinAccounts || []).find(a => a.user?.toLowerCase() === user.toLowerCase() &&
    a.key?.startsWith(agent + "|"));
  if (acct) {
    const master = s[(acct.by || "workbuddy") + "Checkin"];
    const on = s.accountCheckins?.[acct.key] ?? (!!master && acct.on);
    const daily = segs([["off", t("Off")], ["on", t("On")]], on ? "on" : "off", v => save("settings/account-checkin", { key: acct.key, on: v === "on" }));
    daily.setAttribute("aria-label", t("Daily check-in") + " · " + user);
    row("Daily check-in", daily);
    const slot = el("span");
    const fill = () => { if (slot.isConnected) ciAcctFill(slot, {checkinKey:acct.key,checkinBy:acct.by}, agent, user); };
    queueMicrotask(() => window.ciAcctFill ? fill() : import("./checkinnow.js").then(fill, () => {}));
    row("Check in now", slot);
  }
  return rows.length ? [...rows, error] : [];
}

(() => {
  function fold(parent, id, title, nodes) {
    const d = el("details", "newcomer-fold"); d.id = id;
    const sum = el("summary", ""); sum.dataset.en = title; sum.textContent = t(title);
    const body = el("div", "newcomer-fold-body"); body.append(...nodes);
    d.append(sum, body); parent.append(d); return d;
  }
  const general = $("#setPage-general"), list = general.querySelector(".list");
  const basics = el("div", "list prefs newcomer-basics");
  for (const id of ["langPick", "themeSegs", "textSizeRow", "loginRow"]) basics.append($("#"+id).closest(".row"));
  general.prepend(basics);
  fold(general, "generalAdvanced", "Advanced configuration", [list]);
  const usage = $("#setPage-usage");
  const note = el("p", "hint newcomer-note"); note.dataset.en = "Remaining allowances are shown on each account's card.";
  note.textContent = t(note.dataset.en);
  const allowanceList = usage.querySelector(".list");
  usage.prepend(note);
  fold(usage, "allowanceAdvanced", "Allowance display and alerts", [allowanceList]);
  const network = $("#setPage-network"), children = [...network.children];
  const netNote = el("p", "hint newcomer-note"); netNote.id = "networkAutomatic"; network.prepend(netNote);
  fold(network, "networkProxy", "Manual proxy", children.slice(0,1));
  fold(network, "networkGitHub", "GitHub token", children.slice(1,3));
  fold(network, "networkWebPages", "Web page permissions", children.slice(3,5));
  fold(network, "networkBrowser", "Browser access", children.slice(5));
  const models = $("#setPage-models"), modelParts = [...models.children];
  for (let i=0; i<modelParts.length; i+=2) {
    const title = ["Images", "Web search", "Replies", "Disabled models"][i/2];
    fold(models, "modelOptions"+i/2, title, [modelParts[i],modelParts[i+1]]);
  }
  const modelNote = el("p", "hint newcomer-note");
  modelNote.dataset.en = "Models work with the default settings. Expand a feature only when you need it.";
  modelNote.textContent = t(modelNote.dataset.en); models.prepend(modelNote);
  // Searches temporarily open advanced settings and restore the user's folds on clear.
  const find = findSettings, search = searchSettings;
  const savedFolds = new Map();
  findSettings = () => {
    for (const d of $$("#view-settings .newcomer-fold")) {
      if (!savedFolds.has(d)) savedFolds.set(d, d.open);
      d.open = true;
    }
    find();
    for (const d of $$("#view-settings .newcomer-fold")) d.classList.toggle("set-miss", ![...d.querySelectorAll(".row")].some(r => !r.hidden && !r.classList.contains("set-miss")));
  };
  searchSettings = q => {
    if (!q.trim()) { for (const [d,open] of savedFolds) d.open = open; savedFolds.clear(); }
    return search(q);
  };
  let loginState, loginBusy=false, loginLoading=false;
  async function loadLogin() {
    if(loginLoading)return;loginLoading=true;
    try {loginState=await window.__TAURI__.core.invoke("plugin:autostart|is_enabled");paintLogin();}
    catch(e) {const box=$("#loginSegs");connectionFailure(box,String(e));}
    finally {loginLoading=false;}
  }
  function paintLogin() {
    if (!window.__TAURI__?.core) return;
    const box=$("#loginSegs");$("#loginRow").hidden=false;
    if (loginState===undefined) {box.textContent=t("Loading…");return;}
    box.replaceChildren(segs([["off",t("Off")],["on",t("On")]],loginState?"on":"off",async v=>{
      if(loginBusy)return;loginBusy=true;for(const b of box.querySelectorAll("button"))b.disabled=true;
      try {await window.__TAURI__.core.invoke("plugin:autostart|"+(v==="on"?"enable":"disable"));loginState=v==="on";}
      catch(e) {statusErr({message:String(e)});}
      finally {loginBusy=false;paintLogin();}
    }));
    for(const b of box.querySelectorAll("button"))b.disabled=loginBusy;
  }
  const render = renderSettings;
  renderSettings = (...args) => {
    render(...args);
    window.refreshClientImageSetup?.();
    const s = prefs || state?.settings || {};
    const automatic = !s.proxy;
    netNote.dataset.en = automatic ? "The system proxy is used automatically. Usually no changes are needed." : "A custom network setting is active. Expand Manual proxy to change it.";
    netNote.textContent = t(netNote.dataset.en);
    // This bundle supports real WebView zoom, independent of the Wails host.
    if (window.__TAURI__?.webview) $("#textSizeRow").hidden = false;
    paintLogin();
    if(window.__TAURI__?.core&&loginState===undefined)loadLogin();
    document.querySelector(".uni-add")?.toggleAttribute("hidden", view !== "providers");
  };
  if (window.bootPrefs?.uniSwitch) {
    if(window.__TAURI__?.webview)document.documentElement.classList.add("uni-native");
    const originalSize=setTextSize;
    setTextSize=async n=>{
      if(!window.__TAURI__?.webview)return originalSize(n);
      if(n===textSize||!TEXT_SIZES.includes(n))return;
      try {
        prefs=await writingPrefs(api("settings/text-size",{size:n}));
        state.settings=prefs;textSize=n;newTextSize();applyZoom(n);
        if(view==="settings")renderSettings();
      } catch(e) {statusErr(e);}
    };
    const zoom = applyZoom;
    applyZoom = n => {
      zoom(n);
      if(window.__TAURI__?.webview){
        document.documentElement.style.setProperty("--zoom",(n||100)/100);
        window.__TAURI__.webview.getCurrentWebview().setZoom((n||100)/100).catch(e=>statusErr({message:String(e)}));
      }
    };
    addEventListener("load",()=>requestAnimationFrame(()=>applyZoom(textSize)),{once:true});
  }
})();
