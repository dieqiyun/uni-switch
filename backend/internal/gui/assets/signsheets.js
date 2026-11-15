// Sign-ins that ask one value before they start. app.js imports this only
// when one is asked for (signAsk), so it weighs nothing on the core.

// Copilot's enterprise on GHE.com: its host, asked before its sign-in starts
window.copilotGHESheet = function () {
  const box = el("div", "signing copilot-ghe");
  const tt = el("span", "tt");
  box.append(tt);
  const flow = signing;
  const label = t("Your enterprise on GHE.com");
  tt.append(el("span", "n", label),
    el("span", "s", t("The address you sign in to GitHub at, like acme.ghe.com. An account on github.com doesn't need it.")));
  const form = el("form", "callback-form");
  const inp = input(flow.host || "", "acme.ghe.com");
  inp.setAttribute("aria-label", label);
  const go = el("button", "text primary", t("Sign in"));
  go.type = "submit";
  go.disabled = !GHE_HOST.test(inp.value.trim());
  const why = el("span", "s why", "");
  inp.oninput = () => { flow.host = inp.value; go.disabled = !GHE_HOST.test(inp.value.trim()); why.textContent = ""; };
  const submit = (e) => {
    e.preventDefault();
    if (signing !== flow) return;
    const host = inp.value.trim().replace(/^https?:\/\//i, "").replace(/\/$/, "").toLowerCase();
    if (!GHE_HOST.test(host)) { why.textContent = t("Type your enterprise's <name>.ghe.com"); return; }
    startSignIn("copilot", true, host);
  };
  form.onsubmit = submit;
  go.onclick = submit;
  form.append(inp, go);
  tt.append(form, why);
  const close = el("button", "text", t("Cancel"));
  close.onclick = cancelSignIn;
  const back = el("button", "text", "github.com");
  back.title = t("Sign in on github.com instead");
  back.onclick = () => startSignIn("copilot", true);
  box.append(close, back);
  setTimeout(() => { if (inp.isConnected) inp.focus({ preventScroll: true }); });
  return box;
};

// a Claude account from the token `claude setup-token` prints (sfss5362 on
// X; claude_setup_token.go)
window.claudeTokenSheet = function () {
  const box = el("div", "signing claude-token");
  const tt = el("span", "tt");
  box.append(tt);
  const flow = signing;
  const label = t("Add a Claude account by setup-token");
  tt.append(el("span", "n", label),
    el("span", "s", t("Paste the token {cmd} printed. uni-switch gives it to Claude Code for this account's requests only; Claude Code itself stays signed in as it is.", { cmd: "claude setup-token" })),
    el("span", "s", t("A setup-token can't tell its email or allowance: the account is named by the token, and its allowance shows once a reply tells it.")));
  const form = el("form", "callback-form");
  const inp = input(flow.token || "", "sk-ant-oat01-…", "password");
  inp.setAttribute("aria-label", label);
  const go = el("button", "text primary", t("Add"));
  go.type = "submit";
  go.disabled = !inp.value.trim() || !!flow.busy;
  const why = el("span", "s why", flow.error || "");
  inp.oninput = () => { flow.token = inp.value; go.disabled = !inp.value.trim(); why.textContent = ""; };
  const submit = async (e) => {
    e.preventDefault();
    if (signing !== flow || flow.busy || !inp.value.trim()) return;
    flow.busy = true;
    go.disabled = true;
    try {
      const r = await api("signin/claude-token", { token: inp.value });
      if (signing !== flow) return;
      await signedIn({ agent: "claude", user: r.user, token: true });
    } catch (err) {
      if (signing !== flow) return;
      flow.busy = false;
      flow.error = err.message;
      renderProviders();
    }
  };
  form.onsubmit = submit;
  go.onclick = submit;
  form.append(inp, go);
  tt.append(form, why);
  const close = el("button", "text", t("Cancel"));
  close.onclick = cancelSignIn;
  box.append(close);
  setTimeout(() => { if (inp.isConnected) inp.focus({ preventScroll: true }); });
  return box;
};
