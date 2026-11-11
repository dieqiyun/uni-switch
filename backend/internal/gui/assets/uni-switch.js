// Local product UI. No provider, model, account, protocol or file-write rules
// are implemented here: every action calls the original page's API/picker.
if (window.bootPrefs?.uniSwitch) (() => {
  const words = {
    "Sent with that event, by built-in ids and counts; a provider you added yourself is only “custom”. No names, addresses, accounts, keys or usage":["随该事件发送内置 ID 和数量；自行添加的供应商仅记为 custom。不含名称、地址、账号、密钥或用量。","隨該事件傳送內建 ID 與數量；自行新增的供應商僅記為 custom。不含名稱、位址、帳號、金鑰或用量。","組み込み ID と件数を送信します。独自のプロバイダーは custom として記録されます。名前、アドレス、アカウント、キー、使用量は含まれません。","Sendet integrierte IDs und Anzahlen. Eigene Anbieter werden als custom erfasst. Keine Namen, Adressen, Konten, Schlüssel oder Nutzungsdaten."],
    "View releases":["查看发布页","查看發布頁","リリースを見る","Versionen ansehen"],
    "New version":["有新版本","有新版本","新バージョンあり","Neue Version verfügbar"],
    "uni-switch recommended providers":["uni-switch推荐供应商","uni-switch推薦供應商","uni-switchおすすめプロバイダー","Von uni-switch empfohlene Anbieter"],
    "Connect this client above, then choose a model.":["请先在上方接入当前客户端，再选择模型。","請先在上方連接目前用戶端，再選擇模型。","上でクライアントを接続してからモデルを選択してください。","Diesen Client oben verbinden, dann ein Modell wählen."],
    "Client not detected. Add a provider to prepare its configuration.":["未检测到此客户端。可先添加供应商准备配置。","未偵測到此用戶端。可先新增供應商準備設定。","クライアント未検出。プロバイダーを追加して設定を準備できます。","Client nicht erkannt. Einen Anbieter hinzufügen, um die Einrichtung vorzubereiten."],
    "Choose client":["选择客户端","選擇用戶端","クライアントを選択","Client wählen"],
    "Desktop and CLI":["桌面端与 CLI","桌面版與 CLI","デスクトップと CLI","Desktop und CLI"],
    "Model configuration":["模型配置","模型設定","モデル設定","Modelleinstellungen"],
    "Use →":["使用 →","使用 →","使用 →","Verwenden →"],
    "Edit":["编辑","編輯","編集","Bearbeiten"],
    "Current model":["当前模型","目前模型","現在のモデル","Aktuelles Modell"],
    "Available models":["可用模型","可用模型","利用可能なモデル","Verfügbare Modelle"],
    "Adjust":["调整","調整","調整","Anpassen"],
    "Adjust context window for {provider}":["调整 {provider} 的上下文长度","調整 {provider} 的上下文長度","{provider} のコンテキスト長を調整","Kontextfenster für {provider} anpassen"],
    "Current / available model":["当前模型 / 可用模型","目前模型 / 可用模型","現在 / 利用可能なモデル","Aktuelles / verfügbares Modell"],
    "Choose models":["尚未选择模型","尚未選擇模型","モデルを選択","Modelle wählen"],
    "{n} enabled · Edit to manage":["已启用 {n} 个 · 编辑管理模型","已啟用 {n} 個 · 編輯管理模型","{n} 個有効 · 編集で管理","{n} aktiv · Unter Bearbeiten verwalten"],
    "Context window":["上下文长度","上下文長度","コンテキスト長","Kontextfenster"],
    "Defaults to 256K for all models. Enter another size, or model=size for individual models.":["所有模型默认 256K。可填写其他长度，或用 模型=长度 单独调整。","所有模型預設 256K。可填寫其他長度，或用 模型=長度 個別調整。","全モデルの既定値は 256K です。別の長さ、または model=size で個別に調整できます。","Alle Modelle verwenden standardmäßig 256K. Eine andere Größe oder model=size für einzelne Modelle eingeben."],
    "From model metadata":["由模型资料确定","依模型資料決定","モデル情報から取得","Aus den Modelldaten"],
    "Connection":["接入方式","連接方式","接続方法","Verbindung"],
    "Subscription account":["订阅账号","訂閱帳號","サブスクリプション","Abonnementkonto"],
    "Manage sign-in, refresh and accounts in Edit":["登录、续期和多账号在编辑中管理","在編輯中管理登入、續期與多帳號","ログインと更新は編集で管理","Anmeldung, Erneuerung und Konten unter Bearbeiten"],
    "API keys and subscriptions work together. Add a provider, then use it to choose a model.":["API Key 与订阅账号可同时使用。添加供应商后，点「使用」选择模型。","API Key 與訂閱帳號可同時使用。新增供應商後，按「使用」選擇模型。","API キーとサブスクリプションを併用できます。追加してモデルを選択。","API-Schlüssel und Abos sind kombinierbar. Anbieter hinzufügen und Modell wählen."],
    "＋ Add provider":["＋ 添加供应商","＋ 新增供應商","＋ プロバイダー追加","＋ Anbieter hinzufügen"],
    "Add Dieqiyun provider":["添加蝶祈云供应商","新增蝶祈雲供應商","蝶祈雲プロバイダーを追加","Dieqiyun-Anbieter hinzufügen"],
    "Add custom provider":["添加自定义供应商","新增自訂供應商","カスタムプロバイダーを追加","Eigenen Anbieter hinzufügen"],
    "Enter your API key, then choose a model to start.":["填写 API Key，选择模型即可开始使用。","填寫 API Key，選擇模型即可開始使用。","API キーを入力し、モデルを選ぶだけで始められます。","API-Schlüssel eingeben, Modell wählen und loslegen."],
    "Use {provider} in {client}":["在 {client} 使用 {provider}","在 {client} 使用 {provider}","{client} で {provider} を使用","{provider} in {client} verwenden"],
    "Minimize":["最小化","最小化","最小化","Minimieren"],
    "Maximize or restore":["最大化或还原","最大化或還原","最大化 / 元に戻す","Maximieren oder wiederherstellen"],
    "Hide to tray":["关闭到后台","關閉至背景","トレイに隠す","Im Hintergrund weiterlaufen"],
  };
  for(const [key,values] of Object.entries(words))for(const [i,lang] of ["zh","zh-TW","ja","de"].entries())I18N[lang][key]=values[i];
  const sourceT = t;
  t = (s, vars) => {
    if(s.includes("~/.config/uni-switch/providers.json"))return locale.startsWith("zh")?"保存在 uni-switch 的本机数据目录内，仅你本人可读。不从 shell 读取密钥。":"Stored in uni-switch's local data directory, readable by you alone. Keys are not read from the shell.";
    return sourceT(s, vars);
  };
  translateStatic();
  const zh = () => locale.startsWith("zh");
  const say = (_cn, en) => t(en);
  let selected = "codex", clientNav;
  const primary = [
    ["codex","Codex","codex-color","桌面端与 CLI","Desktop and CLI"],
    ["claude","Claude Code","claude-color","桌面端与 CLI","Desktop and CLI"],
    ["dsh","DSH","deepseek-color","DeepSeek Harness","DeepSeek Harness"],
    ["workbuddy","WorkBuddy","workbuddy-color","模型配置","Model configuration"],
    ["zcode","ZCode","zcode","模型配置","Model configuration"],
    ["pi","Pi","pi","模型配置","Model configuration"],
    ["opencode","OpenCode","opencode","模型配置","Model configuration"],
    ["gemini","Gemini CLI","geminicli-color","模型配置","Model configuration"],
    ["kimi","Kimi Code","kimi","模型配置","Model configuration"],
    ["qoder","Qoder","qoder","模型配置","Model configuration"],
    ["qoder-cn","Qoder CN","qoder","模型配置","Model configuration"],
  ];
  const button = (text, cls, action) => {
    const b = el("button",cls,text); b.type="button"; b.onclick=action; return b;
  };
  const openReleases = () => api("open",{url:"https://github.com/dieqiyun/uni-switch/releases"}).catch(statusErr);
  const updateWrap = $("#update").closest(".update-wrap");
  updateWrap.classList.add("uni-sidebar-update");
  function layoutSidebarInfo() {
    if(mode!=="window")return;
    if(sideNav()){
      const nav=$("#nav"), prefs=$("#prefs");
      // Keep the real Settings control and its expanding tabs in the page list.
      const sub=$("#side .nav-sub[data-view='settings']:not(.leaving)");
      const tail=[prefs,...(sub?[sub]:[]),updateWrap];
      const pages=[...nav.children].filter(n=>!tail.includes(n));
      replaceKeeping(nav,[...pages,...tail]);
    }else{
      $(".actions").insertBefore(updateWrap,$("#prefs"));
    }
  }
  const sourceLayoutNav=layoutNav;
  layoutNav=(...args)=>{sourceLayoutNav(...args);layoutSidebarInfo();};
  renderUpdateBadge=async()=>{
    const u=await api(updatePath("update")).catch(()=>null);
    if(servedAnew(u))return;
    if(u)updateLast=u,u.at=Date.now();
    const current=u||updateLast;
    const available=!!current?.latest && current.latest!==pageVersion && ["available","found","ready"].includes(current.state);
    const b=$("#update"), label=b.querySelector("span");
    b.hidden=false;
    b.classList.toggle("uni-update-available",available);
    label.textContent=available ? `v${pageVersion} · ${t("New version")}` : `${t("Version")} v${pageVersion}`;
    b.title=available ? `${t("New version")} ${current.latest} · ${t("View releases")}` : `${t("Version")} v${pageVersion}`;
    b.onclick=available ? openReleases : e=>openSettingsAt(e,"about");
    b.oncontextmenu=null;
    $("#updateHide").hidden=true;
  };
  const sourceRenderAgents = renderAgents;
  renderAgents = (...args) => {
    sourceRenderAgents(...args);
    renderClientNav(); enhanceProviders();
  };
  const sourceRenderAdd = renderAdd;
  renderAdd = () => sourceRenderAdd({ subscriptionClient:selected, hideLocalPresets:true, hideCustomProviders:true, relayTitle:"uni-switch recommended providers" });
  const sourceRenderProviders = renderProviders;
  renderProviders = (...args) => {
    sourceRenderProviders(...args); enhanceProviders(); renderClientNav();
  };
  const sourceShow = show;
  show = async (...args) => {
    if(args[0]==="agents")args[0]="providers";
    const went = await sourceShow(...args);
    if (went) { renderClientNav(); renderHeading(); }
    return went;
  };
  function renderClientNav() {
    if (!clientNav) {
      clientNav = el("nav","uni-clients"); clientNav.id="uniClients";
      clientNav.setAttribute("aria-label",say("选择客户端","Choose client"));
      $(".side-scroll").prepend(clientNav);
      const brand = $(".brand");
      brand.replaceChildren(Object.assign(document.createElement("img"), {src:"uni-switch-logo.png",alt:"",className:"uni-logo"}),el("span","","uniswitch"));
    }
    // Keep focused sidebar controls across the periodic state refresh.
    const focused = document.activeElement?.dataset.uniClient;
    const scrollTop = clientNav.scrollTop;
    clientNav.replaceChildren(el("p","uni-nav-label",say("选择客户端","Choose client")));
    for (const [id,name,logo,cn,en] of primary) {
      const b = button("","uni-client",async () => {
        if(id!==selected && (editing!==null || importing) && !await cancelEdit(true))return;
        const before=selected; selected=id;
        adding=false; presetQuery="";
        if (!await show("providers")) { selected=before; renderClientNav(); return; }
        enhanceProviders(); renderHeading();
      });
      b.dataset.uniClient=id;
      if (view==="providers" && selected===id) b.setAttribute("aria-current","page");
      const label=el("span",""); label.append(el("strong","",name),el("small","",say(cn,en)));
      b.append(icon(logo),label); clientNav.append(b);
    }
    if(focused) clientNav.querySelector(`[data-uni-client="${focused}"]`)?.focus(NO_SCROLL);
    clientNav.scrollTop = scrollTop;
  }
  function renderHeading() {
    const add = document.querySelector(".uni-add");
    if (add) {
      add.toggleAttribute("hidden", view!=="providers");
      add.setAttribute("aria-haspopup", "dialog");
      add.textContent = "+ " + t("Add Dieqiyun provider");
      add.disabled = !!providers?.fileError;
    }
    if(view!=="providers")return;
    const title=primary.find(([id])=>id===selected)?.[1]||"Codex";
    const crumb=$("#crumb");crumb.replaceChildren(el("b","",title),el("span","uni-count",say("供应商","Providers")+` ${providers?.providers?.length??0}`));
    document.title=`${title} · uni-switch`;
  }
  function enhanceProviders() {
    if(!providers?.providers)return;
    document.querySelectorAll("input.detect-model").forEach(n=>n.setAttribute("aria-label",say("测试用模型","Model for protocol detection")));
    const agent=state.agents.find(a=>a.id===selected);
    for(const row of $$(".row.provider")) {
      const p=providers.providers.find(p=>p.id===row.dataset.id); if(!p)continue;
      const key=row.querySelector(".key"), uses=row.querySelector(".uses");
      row.querySelector(".uni-card-meta")?.remove();
      let actions=row.querySelector(".uni-card-actions");
      if(!actions){
      actions=el("div","uni-card-actions");
      const use=button(say("使用 →","Use →"),"uni-use",e=>{
        e.stopPropagation();
        const agent=state.agents.find(a=>a.id===selected);
        if(!agent?.wired){status(t("Connect this client above, then choose a model."));$("#uniClientControls .ag-conn")?.focus(NO_SCROLL);return;}
        const a=p.agents.find(a=>a.id===selected)||agent;
        if(modelField(a))pickForAgent(a,p,use,e);
        else if(agent.models)openAgentModels(agent,use,e);
      });
      use.disabled=!!p.off;
      use.setAttribute("aria-label",t("Use {provider} in {client}",{provider:p.name,client:agent?.name||primary.find(([id])=>id===selected)?.[1]}));
      const edit=button(say("编辑","Edit"),"uni-edit",e=>{e.stopPropagation();row.onclick(e);});
      edit.setAttribute("aria-label",t("Edit")+" "+p.name);
      actions.append(edit,use);row.append(actions);
      }
      actions.querySelector(".uni-use").setAttribute("aria-label",t("Use {provider} in {client}",{provider:p.name,client:agent?.name||primary.find(([id])=>id===selected)?.[1]}));
      actions.querySelector(".uni-use").disabled=!!p.off;
      const modelFieldHere=agent?.fields?.find(f=>f.key==="model");
      const chosen=p.agents.find(a=>a.id===selected&&a.current)?.model;
      const active=p.models.filter(m=>m.on);
      const model=active.find(m=>chosen&&(m.id===chosen||m.name===chosen||p.id+"/"+m.id===chosen))
        ||active.find(m=>modelFieldHere?.value===p.id+"/"+m.id||modelFieldHere?.value==="uni-switch/"+p.id+"/"+m.id);
      if(!p.account){const sub=row.querySelector(".who > .sub");if(sub)sub.textContent=p.host||"";}
      const meta=el("dl","uni-card-meta");
      const item=(label,value)=>{const d=el("div","");d.append(el("dt","",label),el("dd","",value));return d;};
      const modelInfo=item(t(model||chosen?"Current model":"Available models"),model?.name||model?.id||chosen||t(active.length===1?"{n} model":"{n} models",{n:active.length}));
      modelInfo.querySelector("dd").title=active.map(m=>m.name||m.id).join("\n");
      meta.append(modelInfo);
      const windows=[...new Set((model?[model]:active).map(m=>m.context).filter(n=>n>0))].sort((a,b)=>a-b);
      const context=windows.length?tokensText(windows[0])+(windows.length>1?" – "+tokensText(windows.at(-1)):""):"256K";
      const contextInfo=item(t("Context window"),"");
      const adjust=button("","uni-context",async e=>{
        e.stopPropagation();
        try{
          await row.onclick(e);
          const input=document.querySelector(".editor .contexts");
          if(input){const details=input.closest("details");if(details)details.open=true;input.scrollIntoView({block:"center"});input.focus(NO_SCROLL);}
        }catch(error){statusErr(error);}
      });
      adjust.append(el("span","",context),el("span","uni-context-edit",t("Adjust")));
      adjust.setAttribute("aria-label",t("Adjust context window for {provider}",{provider:p.name})+" · "+context);
      adjust.setAttribute("aria-haspopup","dialog");
      contextInfo.querySelector("dd").append(adjust);meta.append(contextInfo);
      const connection=item(p.account?t("Subscription account"):"API Key","");
      connection.className="uni-card-connection";
      if(key)connection.querySelector("dd").append(key);
      if(uses?.children.length)connection.querySelector("dd").append(uses);
      else uses?.remove();
      meta.append(connection);
      row.append(meta);
    }
    let hint=$("#uniProviderHint");
    if(!hint){hint=el("div","uni-hint");hint.id="uniProviderHint";$("#pvHead").prepend(hint);}
    hint.replaceChildren();
    if(!agent)hint.textContent=t("Client not detected. Add a provider to prepare its configuration.");
    else hint.textContent=say("API Key 与订阅账号可同时使用。添加供应商后，点「使用」选择模型。","API keys and subscriptions work together. Add a provider, then use it to choose a model.");
    hint.title=hint.textContent;
    renderClientControls(agent);
    window.renderClientImageSetup?.(selected, primary.find(([id])=>id===selected)?.[1] || selected);
    renderProviderEntry();
    renderHeading();
  }
  let openingProvider = false;
  async function openProviderSetup(custom = false, trigger = null) {
    if (openingProvider) return;
    openingProvider = true;
    try {
      if ((editing !== null || importing) && !await cancelEdit(true)) return;
      if (!await show("providers")) return;
      if (!providers) await loadProviders();
      if (!providers || providers.fileError) return;
      trigger?.focus(NO_SCROLL);
      adding = false; presetQuery = "";
      if (custom) {
        editing = { custom: true }; draft = null; renderProviders();
      } else {
        const preset = providers.presets.find(p => p.id === "dieqiyun");
        if (preset) tile(preset).onclick();
      }
    } catch (error) { statusErr(error); }
    finally { openingProvider = false; }
  }
  function renderProviderEntry() {
    const page = $("#view-providers");
    let empty = $("#uniProviderEmpty");
    if (!empty) {
      empty = el("section", "uni-provider-empty"); empty.id = "uniProviderEmpty";
      const add = button("+ " + t("Add Dieqiyun provider"), "uni-provider-primary", e => openProviderSetup(false, e.currentTarget));
      add.setAttribute("aria-haspopup", "dialog");
      empty.append(add, el("p", "", t("Enter your API key, then choose a model to start.")));
      $("#providers").before(empty);
    }
    empty.hidden = !!providers.fileError || providers.providers.length > 0 || adding;
    let footer = $("#uniProviderFooter");
    if (!footer) {
      footer = el("div", "uni-provider-footer"); footer.id = "uniProviderFooter";
      const subscriptions = button(t("Subscriptions"), "text uni-subscription-add", async () => {
        if ((editing !== null || importing) && !await cancelEdit(true)) return;
        if (adding) { closeAddSheet(); return; }
        $("#addProvider").click();
        requestAnimationFrame(() => $("#addSheet .find")?.focus(NO_SCROLL));
      });
      subscriptions.id = "uniSubscriptionAdd";
      subscriptions.setAttribute("aria-controls", "addSheet");
      const custom = button(t("Add custom provider"), "text uni-custom-add", e => openProviderSetup(true, e.currentTarget));
      custom.setAttribute("aria-haspopup", "dialog");
      footer.append(subscriptions, custom); page.append(footer);
    }
    footer.hidden = !!providers.fileError;
    const subscriptions = $("#uniSubscriptionAdd");
    subscriptions.hidden = !SUBS.some(x => x.agent === selected && !(providers.movable || []).includes(x.agent));
    subscriptions.setAttribute("aria-expanded", String(adding));
    subscriptions.textContent = t(adding ? "Collapse provider choices" : "Subscriptions");
    $("#view-providers .after-list").classList.add("uni-provider-actions-replaced");
    $("#uniProviderHint").hidden = providers.providers.length === 0;
  }
  function renderClientControls(agent) {
    let panel=$("#uniClientControls");
    if(!panel){panel=el("div","uni-client-controls");panel.id="uniClientControls";$("#view-providers").prepend(panel);}
    const focus=panel.contains(document.activeElement)?document.activeElement.dataset.uniControl:null;
    panel.replaceChildren();
    if(!agent)return;
    panel.append(el("strong","",agent.name));
    if(connectable(agent)){const sw=connectSwitch(agent);sw.dataset.uniControl="connect";panel.append(sw);}
    if(agent.wired){
      const f=modelField(agent);
      if(f){const pick=button(optionFor(f,f.value)?.label||f.value||t("Choose models"),"field uni-model",ev=>openPicker(agent,f,pick,ev));pick.dataset.uniControl="model";pick.setAttribute("aria-label",t("Current / available model"));panel.append(pick);}
      else if(agent.models){const pick=menuButton(agent);pick.dataset.uniControl="model";panel.append(pick);}
    }
    if(focus)panel.querySelector('[data-uni-control="'+focus+'"]')?.focus(NO_SCROLL);
  }
  function tokensText(value) {return value>=1e6?`${+(value/1e6).toFixed(2)}M`:`${+(value/1000).toFixed(1)}K`;}
  // Native titlebar integration; no backend business calls are routed to Rust.
  const native=window.__TAURI__?.window?.getCurrentWindow?.();
  const bar=el("div","uni-window");
  for(const [label,text,method] of [["Minimize","−","minimize"],["Maximize or restore","□","toggleMaximize"],["Hide to tray","×","hide"]]){
    const b=button(text,"",()=>native?.[method]?.().catch(statusErr));b.setAttribute("aria-label",t(label));bar.append(b);
  }
  if(native)$(".top").append(bar);
  $(".top").addEventListener("pointerdown",e=>{if(e.button===0&&!e.target.closest("button,input,a,nav"))native?.startDragging?.();});
  const add=button("+ " + t("Add Dieqiyun provider"),"uni-add",e=>openProviderSetup(false,e.currentTarget));
  $(".top .actions").prepend(add);
  window.uniSwitchQuit=()=>askQuit();
  // The distribution belongs to this desktop bundle, not the source project's
  // installer. Keep a real release-page action instead of an inert updater.
  renderUpdate = async (row) => {
    const value=row.querySelector(".val");
    const release=button(say("查看发布页","View releases"),"text",openReleases);
    value.replaceChildren(el("code","",pageVersion),release);
    row.querySelector(".who .sub")?.remove();
  };
  const originalSettings=renderSettings;
  renderSettings=(...args)=>{
    originalSettings(...args);
    // Wails-only shell preferences have no effect on this Tauri window.
    // Keep their DOM for the source page's contracts, but expose no no-op UI.
    for(const id of ["panelKeyRow","dockRow","lightweightRow","barIconRow","trayUsageRow","trayWindowsRow","trayEveryRow","trayLogosRow","trayBirdRow","trayPaceRow","trayNumberRow","usageAlertRow","balanceAlertRow","resetReminderRow","turnDoneAlertRow"]){
      const row=document.getElementById(id);if(row)row.hidden=true;
    }
    $("#traySegs").parentElement.hidden=true;
    for(const row of $$("#about [class*='update-'],#about .whatsnew-row"))row.hidden=true;
    for(const row of $$("#about .row")){
      const value=row.querySelector(".val");
      if(value?.textContent.includes("io.uni-switch.desktop")){
        const open=button(t("Open"),"text",()=>api("open",{url:"https://github.com/dieqiyun/uni-switch"}).catch(statusErr));
        value.replaceChildren(el("code","","github.com/dieqiyun/uni-switch"),open);
      }
      const repo=row.querySelector("button.github");
      if(repo){
        repo.title="github.com/dieqiyun/uni-switch";
        repo.onclick=()=>api("open",{url:"https://github.com/dieqiyun/uni-switch/issues"}).catch(statusErr);
        row.querySelector("button.discord")?.remove();
        row.querySelector(".who .sub")?.remove();
      }
    }
  };
  layoutSidebarInfo();
  renderClientNav();
})();
