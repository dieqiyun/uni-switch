import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, desktopRuntime, errorMessage } from "../lib/api";
import {
  platformCall,
  type SubscriptionAccount,
  type Login,
  type GatewaySource,
} from "../lib/platformApi";
const kinds = [
  ["chatgpt", "ChatGPT / Codex"],
  ["claude", "Claude Code"],
  ["google", "Google / Gemini CLI"],
  ["antigravity", "Google / Antigravity"],
  ["copilot", "GitHub Copilot"],
];
const blank: GatewaySource = {
  id: "",
  name: "",
  baseUrl: "",
  protocol: "responses",
  authMode: "bearer",
  apiKey: "",
  account: "",
  vendor: "auto",
  models: [],
  enabled: true,
};
export function PlatformPanel() {
  const query = useQueryClient();
  const [accounts, setAccounts] = useState<SubscriptionAccount[]>([]);
  const [sources, setSources] = useState<GatewaySource[]>([]);
  const [login, setLogin] = useState<Login | null>(null);
  const [kind, setKind] = useState("chatgpt");
  const [name, setName] = useState("");
  const [project, setProject] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [callback, setCallback] = useState("");
  const [editing, setEditing] = useState<GatewaySource | null>(null);
  const [models, setModels] = useState("");
  const [route, setRoute] = useState<{ port: number; token: string } | null>(
    null,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const alive = useRef(false);
  const editHeading = useRef<HTMLHeadingElement>(null);
  async function refresh() {
    const [a, s] = await Promise.all([
      platformCall<SubscriptionAccount[]>("accounts"),
      platformCall<GatewaySource[]>("sources"),
    ]);
    if (alive.current) {
      setAccounts(a);
      setSources(s);
    }
  }
  useEffect(() => {
    alive.current = true;
    void refresh().catch((e) => {
      if (alive.current) setError(errorMessage(e));
    });
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (!login || login.state !== "waiting") return;
    let pending = false;
    const timer = window.setInterval(() => {
      if (pending) return;
      pending = true;
      void platformCall<Login>("loginStatus", { id: login.id })
        .then((next) => {
          if (alive.current) {
            setLogin(next);
            if (next.state === "done") void refresh();
          }
        })
        .catch((e) => {
          if (alive.current) setError(errorMessage(e));
        })
        .finally(() => {
          pending = false;
        });
    }, 1500);
    return () => window.clearInterval(timer);
  }, [login?.id, login?.state]);
  async function run(work: () => Promise<void>) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await work();
    } catch (e) {
      if (alive.current) setError(errorMessage(e));
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  async function accountAction(
    action: string,
    account: SubscriptionAccount,
    value?: string,
  ) {
    await platformCall(action, { id: account.id, value });
    await refresh();
  }
  function edit(source: GatewaySource) {
    setEditing({ ...source, apiKey: "" });
    setModels(source.models.map((m) => `${m.kind} ${m.id}`).join("\n"));
    window.setTimeout(() => editHeading.current?.focus(), 0);
  }
  async function saveSource(event: React.FormEvent) {
    event.preventDefault();
    if (!editing) return;
    await run(async () => {
      const rows = models
        .split(/\r?\n/)
        .filter((s) => s.trim())
        .map((line) => {
          const [kind, ...id] = line.trim().split(/\s+/);
          return { kind, id: id.join(" ") };
        });
      await platformCall("save", { profile: { ...editing, models: rows } });
      setEditing(null);
      await refresh();
      setMessage("网关来源已保存。");
    });
  }
  async function register(source: GatewaySource) {
    await run(async () => {
      const info = await platformCall<{ port: number; token: string }>("info");
      setRoute(info);
      const chat = source.models.filter((m) => m.kind === "chat");
      if (!chat.length) throw new Error("此来源没有聊天模型。");
      await api.save({
        id: null,
        family: "codex",
        name: `${source.name} · 统一网关`,
        baseUrl: `http://127.0.0.1:${info.port}/v1`,
        apiKey: info.token,
        model: `${source.id}/${chat[0].id}`,
        authMode: "bearer",
        reasoningEffort: null,
        codexOptions: {
          protocol: "openai",
          upstreamProtocol: "openai",
          protocolPreference: "openai",
          authPreference: "bearer",
          fastMode: null,
          contextWindow: null,
          autoCompactTokenLimit: null,
          models: chat.map((m) => ({
            id: `${source.id}/${m.id}`,
            enabled: true,
            contextWindow: null,
            reasoningEfforts: [],
          })),
          modelsSyncedAt: null,
          balanceQuery: null,
        },
      });
      await query.invalidateQueries();
      setMessage("已添加到供应商列表，可选择客户端并应用。");
    });
  }
  return (
    <section
      className="settings-section platform-panel"
      aria-labelledby="platform-heading"
      aria-busy={busy}
    >
      <h3 id="platform-heading">订阅账号与统一网关</h3>
      <p>
        统一提供 Chat Completions、Responses、Messages、Gemini
        原生接口，以及图片、视频和厂商原生透传。
      </p>
      {!desktopRuntime && <p>请在桌面应用中添加账号与网关来源。</p>}
      <details>
        <summary>订阅账号管理（{accounts.length}）</summary>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              setLogin(
                await platformCall<Login>("login", {
                  kind,
                  name,
                  project,
                  ...(kind === "google" || kind === "antigravity"
                    ? { oauth: { clientId, clientSecret } }
                    : {}),
                }),
              );
              setClientSecret("");
            });
          }}
        >
          <label>
            订阅服务
            <select
              aria-label="订阅服务"
              value={kind}
              onChange={(e) => {
                setKind(e.target.value);
                setClientId("");
                setClientSecret("");
              }}
            >
              {kinds.map(([id, label]) => (
                <option key={id} value={id}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label>
            账号备注
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          {(kind === "google" || kind === "antigravity") && (
            <>
              <p id="google-oauth-help">
                填写已获此服务授权的桌面应用 OAuth
                客户端信息；本程序不内置第三方注册信息。
                登录后保存在本机账号目录用于续期。普通自建客户端可能没有 Code
                Assist 权限。
              </p>
              <label>
                Google OAuth Client ID
                <input
                  required
                  autoComplete="off"
                  aria-describedby="google-oauth-help"
                  value={clientId}
                  onChange={(e) => setClientId(e.target.value)}
                />
              </label>
              <label>
                Google OAuth Client Secret
                <input
                  required
                  type="password"
                  autoComplete="off"
                  aria-describedby="google-oauth-help"
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                />
              </label>
              <label>
                Google Cloud 项目 ID（可选）
                <input
                  value={project}
                  onChange={(e) => setProject(e.target.value)}
                />
              </label>
            </>
          )}
          <button
            className="button secondary"
            disabled={busy || !desktopRuntime || login?.state === "waiting"}
          >
            添加并登录
          </button>
        </form>
        {login && (
          <div aria-live="polite">
            <p>
              登录状态：
              {(
                {
                  waiting: "等待授权",
                  done: "成功",
                  failed: "失败",
                  cancelled: "已取消",
                } as Record<string, string>
              )[login.state] || login.state}
              。{login.message}
            </p>
            {login.code && (
              <p>
                设备验证码：<code>{login.code}</code>
              </p>
            )}
            {login.state === "waiting" && (
              <>
                {login.url && (
                  <button
                    type="button"
                    className="button secondary"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await platformCall("openLogin", { id: login.id });
                      })
                    }
                  >
                    打开授权页面
                  </button>
                )}
                <button
                  type="button"
                  className="button secondary"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      await platformCall("loginCancel", { id: login.id });
                      setLogin(
                        await platformCall<Login>("loginStatus", {
                          id: login.id,
                        }),
                      );
                    })
                  }
                >
                  取消登录
                </button>
                <form
                  onSubmit={(event) => {
                    event.preventDefault();
                    void run(async () => {
                      await platformCall("loginCallback", {
                        id: login.id,
                        value: callback,
                      });
                      setCallback("");
                    });
                  }}
                >
                  <label>
                    回调地址 / Claude 登录代码
                    <input
                      value={callback}
                      onChange={(e) => setCallback(e.target.value)}
                      autoComplete="off"
                    />
                  </label>
                  <button
                    className="button secondary"
                    disabled={busy || !callback}
                  >
                    提交回调
                  </button>
                </form>
              </>
            )}
          </div>
        )}
        <ul className="platform-list">
          {accounts.map((a) => (
            <li key={a.id}>
              <strong>{a.name}</strong> · {a.kind} · {a.user}
              {a.active ? " · 当前账号" : ""}
              {!a.enabled ? " · 已停用" : ""}
              {a.expiry ? (
                <span>
                  {" "}
                  · 有效期至 {new Date(a.expiry * 1000).toLocaleString()}
                </span>
              ) : null}
              <div className="platform-actions">
                <button
                  type="button"
                  disabled={busy || !a.enabled || a.active}
                  onClick={() =>
                    void run(() => accountAction("accountSwitch", a))
                  }
                >
                  设为当前
                </button>
                <button
                  type="button"
                  disabled={busy || a.kind === "claude"}
                  onClick={() =>
                    void run(() => accountAction("accountRefresh", a))
                  }
                >
                  刷新登录
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void run(() =>
                      accountAction("accountEnable", a, String(!a.enabled)),
                    )
                  }
                >
                  {a.enabled ? "停用" : "启用"}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    edit({
                      ...blank,
                      name: a.name,
                      account: a.id,
                      protocol:
                        a.kind === "google" || a.kind === "antigravity"
                          ? "gemini"
                          : a.kind === "claude"
                            ? "messages"
                            : "responses",
                    })
                  }
                >
                  添加网关来源
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void run(() => accountAction("accountDelete", a))
                  }
                >
                  移除账号
                </button>
              </div>
              {(a.kind === "google" || a.kind === "antigravity") && (
                <form
                  onSubmit={(event) => {
                    event.preventDefault();
                    const data = new FormData(event.currentTarget);
                    void run(() =>
                      accountAction(
                        "accountProject",
                        a,
                        String(data.get("project") || ""),
                      ),
                    );
                  }}
                >
                  <label>
                    Cloud 项目 ID
                    <input
                      key={a.project}
                      name="project"
                      defaultValue={a.project || ""}
                    />
                  </label>
                  <button disabled={busy}>保存项目</button>
                </form>
              )}
            </li>
          ))}
        </ul>
        <p>
          Claude 使用独立目录运行已安装的 Claude Code，并由它自动续期。Google
          CLI 的可用权限取决于 Code Assist 订阅和 Cloud 项目。
        </p>
      </details>
      <details>
        <summary>网关来源（{sources.length}）</summary>
        <p>
          现有 API 供应商自动接入；Gemini 原生 API
          或订阅账号可在此添加。模型每行填写“chat 模型ID”“image 模型ID”或“video
          模型ID”。
        </p>
        <button
          type="button"
          className="button secondary"
          disabled={busy || !desktopRuntime}
          onClick={() => edit({ ...blank })}
        >
          添加网关来源
        </button>
        <ul className="platform-list">
          {sources.map((source) => (
            <li key={source.id}>
              <strong>{source.name}</strong> · {source.protocol} ·{" "}
              {source.models.length} 个模型
              <code>{source.id}</code>
              <div className="platform-actions">
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void register(source)}
                >
                  添加到供应商列表
                </button>
                {!source.builtin && (
                  <>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => edit(source)}
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          await platformCall("delete", { id: source.id });
                          await refresh();
                        })
                      }
                    >
                      移除来源
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      </details>
      {editing && (
        <form
          className="platform-editor"
          onSubmit={(event) => void saveSource(event)}
        >
          <h4 ref={editHeading} tabIndex={-1}>
            编辑网关来源
          </h4>
          <label>
            来源名称
            <input
              required
              value={editing.name}
              onChange={(e) => setEditing({ ...editing, name: e.target.value })}
            />
          </label>
          <label>
            认证来源
            <select
              aria-label="认证来源"
              value={editing.account || ""}
              onChange={(e) =>
                setEditing({ ...editing, account: e.target.value })
              }
            >
              <option value="">API Key</option>
              {kinds.map(([id, label]) => (
                <option key={id} value={id}>
                  {label} · 当前账号
                </option>
              ))}
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name} · {a.user}
                </option>
              ))}
            </select>
          </label>
          {!editing.account && (
            <>
              <label>
                API 地址
                <input
                  type="url"
                  required
                  value={editing.baseUrl}
                  onChange={(e) =>
                    setEditing({ ...editing, baseUrl: e.target.value })
                  }
                  placeholder="https://generativelanguage.googleapis.com/v1beta"
                />
              </label>
              <label>
                API Key{editing.id ? "（留空保留）" : ""}
                <input
                  type="password"
                  required={!editing.id}
                  value={editing.apiKey || ""}
                  onChange={(e) =>
                    setEditing({ ...editing, apiKey: e.target.value })
                  }
                  autoComplete="new-password"
                />
              </label>
              <label>
                认证方式
                <select
                  aria-label="认证方式"
                  value={editing.authMode}
                  onChange={(e) =>
                    setEditing({ ...editing, authMode: e.target.value })
                  }
                >
                  <option value="bearer">Bearer / Gemini API Key</option>
                  <option value="x-api-key">x-api-key</option>
                </select>
              </label>
            </>
          )}
          <label>
            上游协议
            <select
              aria-label="上游协议"
              value={editing.protocol}
              onChange={(e) =>
                setEditing({
                  ...editing,
                  protocol: e.target.value as GatewaySource["protocol"],
                })
              }
            >
              {[
                ["responses", "Responses"],
                ["chat", "Chat Completions"],
                ["messages", "Anthropic Messages"],
                ["gemini", "Gemini 原生"],
              ].map(([id, name]) => (
                <option key={id} value={id}>
                  {name}
                </option>
              ))}
            </select>
          </label>
          <label>
            媒体厂商
            <select
              aria-label="媒体厂商"
              value={editing.vendor}
              onChange={(e) =>
                setEditing({ ...editing, vendor: e.target.value })
              }
            >
              {[
                "auto",
                "openai",
                "gemini",
                "ark",
                "grok",
                "minimax",
                "dashscope",
                "modelscope",
                "openrouter",
              ].map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label>
            模型列表
            <textarea
              required
              rows={5}
              value={models}
              onChange={(e) => setModels(e.target.value)}
              placeholder={
                "chat gemini-3-pro\nimage gemini-3-pro-image\nvideo sora-2"
              }
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={editing.enabled}
              onChange={(e) =>
                setEditing({ ...editing, enabled: e.target.checked })
              }
            />
            启用来源
          </label>
          <div className="platform-actions">
            <button className="button primary" disabled={busy}>
              保存来源
            </button>
            <button
              type="button"
              className="button secondary"
              disabled={busy}
              onClick={() => setEditing(null)}
            >
              取消编辑
            </button>
          </div>
        </form>
      )}
      <button
        type="button"
        className="button secondary"
        disabled={busy || !desktopRuntime}
        onClick={() =>
          void run(async () => {
            setRoute(await platformCall("info"));
          })
        }
      >
        显示本地网关连接
      </button>
      {route && (
        <div>
          <p>
            API 地址：<code>http://127.0.0.1:{route.port}/v1</code>
          </p>
          <label>
            本地网关 Key
            <input
              type="password"
              readOnly
              value={route.token}
              onFocus={(e) => e.currentTarget.select()}
            />
          </label>
          <p>
            Gemini 地址使用 /v1beta；模型名从 /v1/models 获取。厂商透传使用
            /v1/native/来源ID/厂商路径。
          </p>
        </div>
      )}
      {error && <p role="alert">{error}</p>}
      {message && <p role="status">{message}</p>}
    </section>
  );
}
