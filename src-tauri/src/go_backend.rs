//! Thin desktop transport for our Go business core. Credentials stay on inherited
//! pipes; no local management port, shell command, or legacy request retry exists.
use crate::error::{AppError, Result};
use crate::types::{BalanceQuery, BalanceResult, CodexProtocol, ModelSyncResult};
use serde::de::DeserializeOwned;
use serde::Deserialize;
use serde_json::{json, Value};
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex as StdMutex};
use std::time::Duration;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin, ChildStdout, Command};
use tokio::sync::{watch, Mutex, Semaphore};

const VERSION: u8 = 1;
const MAX_REQUEST: usize = 32 * 1024 * 1024;
const MAX_RESPONSE: usize = 4 * 1024 * 1024;

pub struct GoBackend {
    executable: PathBuf,
    idle: Mutex<Vec<Worker>>,
    slots: Semaphore,
    sequence: AtomicU64,
    payload: StdMutex<Option<Arc<tempfile::TempDir>>>,
    stopping: watch::Sender<bool>,
    gateway_slots: Arc<Semaphore>,
}

struct Worker {
    child: Child,
    input: Option<ChildStdin>,
    output: BufReader<ChildStdout>,
    _payload: Option<Arc<tempfile::TempDir>>,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Response {
    version: u8,
    id: u64,
    result: Option<Value>,
    error: Option<AppError>,
}

fn transport_error() -> AppError {
    AppError::new(
        "go_core",
        "Go 后端通信失败，请重新打开应用；不会自动重复此操作",
    )
}

impl GoBackend {
    pub fn new(executable: PathBuf) -> Self {
        Self {
            executable,
            idle: Mutex::new(Vec::new()),
            slots: Semaphore::new(4),
            sequence: AtomicU64::new(1),
            payload: StdMutex::new(None),
            stopping: watch::channel(false).0,
            gateway_slots: Arc::new(Semaphore::new(64)),
        }
    }

    pub fn bundled() -> Result<Self> {
        use std::io::Write;
        // A private, unpredictable directory keeps the existing single-file
        // portable distribution intact. Never execute a user-writable sibling
        // file or an executable found on PATH.
        let directory = tempfile::Builder::new()
            .prefix("uni-switch-core-")
            .tempdir()
            .map_err(|_| transport_error())?;
        let filename = if cfg!(windows) {
            "uni-switch-core.exe"
        } else {
            "uni-switch-core"
        };
        let executable = directory.path().join(filename);
        let mut options = std::fs::OpenOptions::new();
        options.write(true).create_new(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o700);
        }
        let mut file = options.open(&executable).map_err(|_| transport_error())?;
        file.write_all(include_bytes!(env!("UNI_SWITCH_GO_CORE_PATH")))
            .map_err(|_| transport_error())?;
        file.sync_all().map_err(|_| transport_error())?;
        drop(file);
        let mut core = Self::new(executable);
        core.payload = StdMutex::new(Some(Arc::new(directory)));
        Ok(core)
    }

    async fn spawn(&self) -> Result<Worker> {
        let mut command = Command::new(&self.executable);
        command
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .kill_on_drop(true);
        #[cfg(windows)]
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW
        let mut child = command.spawn().map_err(|_| {
            AppError::new(
                "go_core_missing",
                "无法启动 Go 后端，请重新安装包含 uni-switch-core 的完整应用",
            )
        })?;
        let input = child.stdin.take().ok_or_else(transport_error)?;
        let output = BufReader::new(child.stdout.take().ok_or_else(transport_error)?);
        let mut worker = Worker {
            child,
            input: Some(input),
            output,
            _payload: self.payload.lock().map_err(|_| transport_error())?.clone(),
        };
        let response = worker.exchange(0, "health", json!({})).await?;
        let health = response.result.ok_or_else(transport_error)?;
        if response.error.is_some()
            || health.get("protocolVersion").and_then(Value::as_u64) != Some(VERSION.into())
            || health.get("implementation").and_then(Value::as_str) != Some("go")
            || health.get("service").and_then(Value::as_str) != Some("uni-switch-core")
        {
            return Err(AppError::new(
                "go_core_version",
                "Go 后端版本不匹配，请重新安装完整应用",
            ));
        }
        Ok(worker)
    }

    async fn call<T: DeserializeOwned>(&self, method: &str, params: Value) -> Result<T> {
        let mut stopping = self.stopping.subscribe();
        if *stopping.borrow() {
            return Err(stopping_error());
        }
        let _permit = tokio::select! {
            biased;
            _ = stopping.changed() => return Err(stopping_error()),
            permit = self.slots.acquire() => permit.map_err(|_| stopping_error())?,
        };
        // Ownership leaves the pool for the entire request. Cancellation, timeout,
        // malformed frames and process failure drop/kill only this owned worker,
        // so its late response can never be consumed by the next caller.
        let idle = self.idle.lock().await.pop();
        let operation = async {
            let mut worker = match idle {
                Some(worker) => worker,
                None => self.spawn().await?,
            };
            let id = self.sequence.fetch_add(1, Ordering::Relaxed);
            let response = worker.exchange(id, method, params).await?;
            let parsed = match (response.result, response.error) {
                (Some(value), None) => serde_json::from_value(value).map_err(|_| transport_error()),
                (None, Some(error)) => Err(error),
                _ => return Err(transport_error()),
            };
            // Application errors are complete protocol frames. They can reuse a
            // worker; transport/schema errors must discard it instead.
            if parsed.as_ref().err().is_none_or(|e| e.code != "go_core") {
                self.idle.lock().await.push(worker);
            }
            parsed
        };
        tokio::select! {
            biased;
            _ = stopping.changed() => Err(stopping_error()),
            result = tokio::time::timeout(Duration::from_secs(35), operation) => {
                result.map_err(|_| AppError::new("go_core_timeout", "Go 后端操作超时，请稍后重试"))?
            }
        }
    }

    /// Verify the embedded worker before startup can report a ready application.
    pub async fn check(&self) -> Result<()> {
        let _: Value = self.call("health", json!({})).await?;
        Ok(())
    }

    /// Tauri exits the process directly, so Drop alone is not an exit hook.
    /// Cancel calls, wait for their owned workers to drop, then close our payload.
    pub async fn shutdown(&self) -> Result<()> {
        self.stopping.send_replace(true);
        let permits = self
            .slots
            .acquire_many(4)
            .await
            .map_err(|_| stopping_error())?;
        let gateway_permits = self
            .gateway_slots
            .acquire_many(64)
            .await
            .map_err(|_| stopping_error())?;
        self.idle.lock().await.clear();
        self.slots.close();
        self.gateway_slots.close();
        drop(permits);
        drop(gateway_permits);
        let payload = self.payload.lock().map_err(|_| transport_error())?.take();
        if let Some(payload) = payload {
            Arc::try_unwrap(payload)
                .map_err(|_| transport_error())?
                .close()
                .map_err(|_| transport_error())?;
        }
        Ok(())
    }

    pub async fn discover(
        &self,
        base: &str,
        key: &str,
        protocol: Option<CodexProtocol>,
        auth: Option<&str>,
    ) -> Result<ModelSyncResult> {
        let mut result: ModelSyncResult = self
            .call(
                "supplier.discover",
                json!({
                    "baseUrl": base, "apiKey": key, "protocol": protocol, "authMode": auth,
                }),
            )
            .await?;
        crate::model_registry::resolve_models(&mut result.models, &result.base_url, None)?;
        Ok(result)
    }

    pub async fn balance(
        &self,
        base: &str,
        key: &str,
        query: Option<BalanceQuery>,
        token: Option<&str>,
    ) -> Result<BalanceResult> {
        self.call(
            "supplier.balance.auto",
            json!({
                "baseUrl": base, "apiKey": key, "query": query, "accountToken": token,
            }),
        )
        .await
    }

    pub async fn test_model(
        &self,
        base: &str,
        key: &str,
        auth: &str,
        model: &str,
        endpoint: crate::model_verification::Endpoint,
        consent: bool,
    ) -> Result<crate::model_verification::Observation> {
        self.call("gateway.test",json!({"baseUrl":base,"apiKey":key,"authMode":auth,"model":model,"endpoint":endpoint,"consent":consent})).await
    }

    /// The Go gateway owns upstream HTTP, conversion and SSE. Rust only moves
    /// bounded framed bytes between the local listener and our embedded core.
    pub async fn gateway(self: Arc<Self>, params: Value) -> Result<axum::response::Response> {
        use axum::{
            body::Body,
            http::{HeaderName, HeaderValue, StatusCode},
            response::IntoResponse,
        };
        use base64::Engine;
        let mut stopping = self.stopping.subscribe();
        if *stopping.borrow() {
            return Err(stopping_error());
        }
        let permit = tokio::select! {
            biased;
            _ = stopping.changed() => return Err(stopping_error()),
            permit = self.gateway_slots.clone().acquire_owned() => permit.map_err(|_| stopping_error())?,
        };
        let payload = self.payload.lock().map_err(|_| transport_error())?.clone();
        let mut command = Command::new(&self.executable);
        command
            .arg("--gateway")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .kill_on_drop(true);
        #[cfg(windows)]
        command.creation_flags(0x08000000);
        let mut child = command.spawn().map_err(|_| transport_error())?;
        let mut input = child.stdin.take().ok_or_else(transport_error)?;
        let output = BufReader::new(child.stdout.take().ok_or_else(transport_error)?);
        let bytes = serde_json::to_vec(&params).map_err(|_| transport_error())?;
        if bytes.len() > 16 * 1024 * 1024 {
            return Err(AppError::new("go_gateway_limit", "推理请求超过大小限制"));
        }
        input
            .write_all(&bytes)
            .await
            .map_err(|_| transport_error())?;
        drop(input); // Real EOF for the one-request gateway pipe.
        let mut worker = Worker {
            child,
            input: None,
            output,
            _payload: payload,
        };
        let head = tokio::select! {
            biased;
            _ = stopping.changed() => return Err(stopping_error()),
            packet = tokio::time::timeout(Duration::from_secs(150), worker.gateway_packet()) => packet.map_err(|_| transport_error())??,
        };
        if head.kind != "head" {
            return Err(transport_error());
        }
        let status = StatusCode::from_u16(head.status).map_err(|_| transport_error())?;
        let mut headers = axum::http::HeaderMap::new();
        for (key, value) in head.headers {
            if matches!(
                key.as_str(),
                "content-type" | "cache-control" | "x-accel-buffering"
            ) {
                headers.insert(
                    HeaderName::from_bytes(key.as_bytes()).map_err(|_| transport_error())?,
                    HeaderValue::from_str(&value).map_err(|_| transport_error())?,
                );
            }
        }
        let body = async_stream::stream! {
            let _permit = permit;
            // Retain the core/extracted payload until this stream's worker dies.
            let _core = self;
            loop {
                let packet = tokio::select! {
                    biased;
                    _ = stopping.changed() => Err(stopping_error()),
                    packet = tokio::time::timeout(Duration::from_secs(150), worker.gateway_packet()) => packet.map_err(|_| transport_error()).and_then(|v|v),
                };
                match packet {
                    Ok(packet) if packet.kind == "end" => break,
                    Ok(packet) if packet.kind == "data" => match base64::engine::general_purpose::STANDARD.decode(packet.data) {
                        Ok(bytes) => yield Ok::<_, std::io::Error>(axum::body::Bytes::from(bytes)),
                        Err(_) => { yield Err(std::io::Error::other("Go gateway frame invalid")); break; }
                    },
                    _ => { yield Err(std::io::Error::other("Go gateway connection ended")); break; }
                }
            }
        };
        Ok((status, headers, Body::from_stream(body)).into_response())
    }
}

#[derive(Deserialize)]
struct GatewayPacket {
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    status: u16,
    #[serde(default)]
    headers: std::collections::BTreeMap<String, String>,
    #[serde(default)]
    data: String,
}

fn stopping_error() -> AppError {
    AppError::new("go_core_stopping", "Go 后端正在退出，操作已取消")
}

impl Drop for Worker {
    fn drop(&mut self) {
        // Reap our own process before the last private-directory handle drops.
        // Windows otherwise refuses to remove an executable still mapped by it.
        let _ = self.child.start_kill();
        for _ in 0..50 {
            match self.child.try_wait() {
                Ok(None) => std::thread::sleep(Duration::from_millis(2)),
                _ => break,
            }
        }
    }
}

impl Worker {
    async fn gateway_packet(&mut self) -> Result<GatewayPacket> {
        let mut bytes = Vec::new();
        loop {
            let buffer = self
                .output
                .fill_buf()
                .await
                .map_err(|_| transport_error())?;
            if buffer.is_empty() {
                return Err(transport_error());
            }
            let newline = buffer.iter().position(|b| *b == b'\n');
            let count = newline.map_or(buffer.len(), |p| p + 1);
            if bytes.len() + count > 24 * 1024 * 1024 {
                return Err(transport_error());
            }
            bytes.extend_from_slice(&buffer[..count]);
            self.output.consume(count);
            if newline.is_some() {
                break;
            }
        }
        serde_json::from_slice(&bytes).map_err(|_| transport_error())
    }
    async fn exchange(&mut self, id: u64, method: &str, params: Value) -> Result<Response> {
        if self
            .child
            .try_wait()
            .map_err(|_| transport_error())?
            .is_some()
        {
            return Err(transport_error());
        }
        let request = serde_json::to_vec(
            &json!({ "version": VERSION, "id": id, "method": method, "params": params }),
        )
        .map_err(|_| transport_error())?;
        if request.len() > MAX_REQUEST {
            return Err(AppError::new("go_core_request", "Go 后端请求超过大小限制"));
        }
        let input = self.input.as_mut().ok_or_else(transport_error)?;
        input
            .write_all(&request)
            .await
            .map_err(|_| transport_error())?;
        input
            .write_all(b"\n")
            .await
            .map_err(|_| transport_error())?;
        input.flush().await.map_err(|_| transport_error())?;
        let mut bytes = Vec::new();
        loop {
            let buffer = self
                .output
                .fill_buf()
                .await
                .map_err(|_| transport_error())?;
            if buffer.is_empty() {
                return Err(transport_error());
            }
            let newline = buffer.iter().position(|b| *b == b'\n');
            let count = newline.map_or(buffer.len(), |p| p + 1);
            if bytes.len() + count > MAX_RESPONSE + 1 {
                return Err(transport_error());
            }
            bytes.extend_from_slice(&buffer[..count]);
            self.output.consume(count);
            if newline.is_some() {
                break;
            }
        }
        let response: Response = serde_json::from_slice(&bytes).map_err(|_| transport_error())?;
        if response.version != VERSION || response.id != id {
            return Err(transport_error());
        }
        Ok(response)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn missing_worker_is_explicit_and_has_no_legacy_fallback() {
        let core = GoBackend::new(std::env::temp_dir().join("uni-switch-nonexistent-core-fixture"));
        let error = core
            .discover("http://127.0.0.1:1", "synthetic-key", None, None)
            .await
            .unwrap_err();
        assert_eq!(error.code, "go_core_missing");
        assert!(!error.message.contains("synthetic-key"));
    }

    #[tokio::test]
    async fn shutdown_rejects_new_requests_without_starting_processes() {
        let core = GoBackend::new(std::env::temp_dir().join("uni-switch-nonexistent-core-fixture"));
        core.shutdown().await.unwrap();
        assert_eq!(core.check().await.unwrap_err().code, "go_core_stopping");
    }
}
