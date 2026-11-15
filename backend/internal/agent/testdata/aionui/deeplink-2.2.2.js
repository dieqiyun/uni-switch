// AionUi 2.2.2's handling of an aionui:// link, copied from its released
// app.asar (com.aionui.app 2.2.2, macOS arm64 dmg from GitHub releases):
// parseDeepLinkUrl from out/main/index.js byte for byte (its lines 50995-51021), then the
// renderer's add-provider mapping and the add-model form's prefill from
// its renderer bundles, unminified by hand with their logic unchanged.
// TestAionUiLinkParsedAs2_2_2 runs it with the link as argv[2] and reads
// the JSON it prints.
const PROTOCOL_SCHEME = "aionui";
const parseDeepLinkUrl = (url) => {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== `${PROTOCOL_SCHEME}:`) return null;
    const hostname = parsed.hostname || "";
    const pathname = parsed.pathname.replace(/^\/+/, "");
    const action = pathname ? `${hostname}/${pathname}` : hostname;
    const params = {};
    parsed.searchParams.forEach((value, key) => {
      params[key] = value;
    });
    if (params.data) {
      try {
        const json = JSON.parse(Buffer.from(params.data, "base64").toString("utf-8"));
        if (json && typeof json === "object") {
          Object.assign(params, json);
        }
      } catch {
      }
      delete params.data;
    }
    return { action, params };
  } catch {
    return null;
  }
};

const n = parseDeepLinkUrl(process.argv[2]);
let Wf = null; // the pending provider the add-model form opens with
let route = null;
if (n && (n.action === "add-provider" || n.action === "provider/add")) {
  Wf = {
    base_url: n.params.base_url,
    api_key: n.params.api_key || n.params.key,
    name: n.params.name,
    platform: n.params.platform,
  };
  route = "/settings/model";
}
// the add-model modal, opened with Wf
const form = {};
if (Wf && (Wf.base_url || Wf.api_key)) {
  form.platform = Wf.platform || "new-api";
  if (Wf.base_url) form.base_url = Wf.base_url;
  if (Wf.api_key) form.api_key = Wf.api_key;
}
process.stdout.write(JSON.stringify({ parsed: n, route, pending: Wf, form }));
