import { invoke } from "@tauri-apps/api/core";

const message = document.querySelector<HTMLElement>("#status")!;
if ("__TAURI_INTERNALS__" in window) {
  invoke<string>("core_endpoint").then((url) => {
    const endpoint = new URL(url);
    if (endpoint.protocol !== "http:" || endpoint.hostname !== "127.0.0.1") throw new Error("本地入口无效");
    location.replace(endpoint.href);
  }).catch((error: unknown) => { message.textContent = `无法连接后台：${String(error)}`; });
} else {
  message.textContent = "请使用 pnpm dev 启动桌面应用；完整界面由本项目编译的本地 Go 后端提供。";
}
