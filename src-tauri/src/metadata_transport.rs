//! Bounded synchronous adapter for metadata at existing synchronous Store
//! boundaries. Business rules live in Go; this module only moves JSON over pipes.
use crate::error::{AppError, Result};
use serde_json::{json, Value};
use std::io::{BufRead, BufReader, Read, Write};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::sync::{
    atomic::{AtomicBool, AtomicUsize, Ordering},
    Arc, Mutex, OnceLock,
};
use std::time::{Duration, Instant};

static STOPPING: AtomicBool = AtomicBool::new(false);
static ACTIVE: AtomicUsize = AtomicUsize::new(0);
fn payload() -> &'static Mutex<Option<Arc<tempfile::TempDir>>> {
    static PAYLOAD: OnceLock<Mutex<Option<Arc<tempfile::TempDir>>>> = OnceLock::new();
    PAYLOAD.get_or_init(|| Mutex::new(None))
}
struct Worker {
    child: Child,
    input: Option<ChildStdin>,
    output: Option<BufReader<ChildStdout>>,
}
impl Drop for Worker {
    fn drop(&mut self) {
        self.input.take();
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
fn worker() -> &'static Mutex<Option<Worker>> {
    static WORKER: OnceLock<Mutex<Option<Worker>>> = OnceLock::new();
    WORKER.get_or_init(|| Mutex::new(None))
}
fn failure() -> AppError {
    AppError::new("go_metadata", "Go 模型资料服务失败，未使用旧解析规则回退")
}
struct Active;
impl Drop for Active {
    fn drop(&mut self) {
        ACTIVE.fetch_sub(1, Ordering::SeqCst);
    }
}
pub fn call(method: &str, params: Value) -> Result<Value> {
    if STOPPING.load(Ordering::SeqCst) {
        return Err(failure());
    }
    ACTIVE.fetch_add(1, Ordering::SeqCst);
    let _active = Active;
    let directory = {
        let mut guard = payload().lock().map_err(|_| failure())?;
        if STOPPING.load(Ordering::SeqCst) {
            return Err(failure());
        }
        if guard.is_none() {
            let dir = tempfile::Builder::new()
                .prefix("uni-switch-metadata-")
                .tempdir()
                .map_err(|_| failure())?;
            let path = dir
                .path()
                .join(if cfg!(windows) { "core.exe" } else { "core" });
            let mut options = std::fs::OpenOptions::new();
            options.create_new(true).write(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                options.mode(0o700);
            }
            let mut file = options.open(path).map_err(|_| failure())?;
            file.write_all(include_bytes!(env!("UNI_SWITCH_GO_CORE_PATH")))
                .map_err(|_| failure())?;
            file.sync_all().map_err(|_| failure())?;
            *guard = Some(Arc::new(dir));
        }
        guard.as_ref().ok_or_else(failure)?.clone()
    };
    let bytes = serde_json::to_vec(&json!({"version":1,"id":1,"method":method,"params":params}))
        .map_err(|_| failure())?;
    if bytes.len() > 32 * 1024 * 1024 {
        return Err(AppError::new("go_core_request", "模型资料请求超过大小限制"));
    }
    let executable = directory
        .path()
        .join(if cfg!(windows) { "core.exe" } else { "core" });
    // Reuse one serialized local worker. Starting a Go process for every
    // SQLite row was both wasteful and noticeably delayed client switching.
    // Network inference uses its separate cancellable request workers.
    let mut guard = worker().lock().map_err(|_| failure())?;
    if STOPPING.load(Ordering::SeqCst) {
        return Err(failure());
    }
    if guard.is_none() {
        let mut command = Command::new(executable);
        command
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null());
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command.spawn().map_err(|_| failure())?;
        let input = child.stdin.take();
        let output = child.stdout.take().map(BufReader::new);
        *guard = Some(Worker {
            child,
            input,
            output,
        });
    }
    let current = guard.as_mut().ok_or_else(failure)?;
    let input = current.input.take().ok_or_else(failure)?;
    let output = current.output.take().ok_or_else(failure)?;
    let exchange = std::thread::spawn(
        move || -> std::io::Result<(Vec<u8>, ChildStdin, BufReader<ChildStdout>)> {
            let mut input = input;
            let mut output = output;
            input.write_all(&bytes)?;
            input.write_all(b"\n")?;
            input.flush()?;
            let mut result = Vec::new();
            (&mut output)
                .take(4 * 1024 * 1024 + 1)
                .read_until(b'\n', &mut result)?;
            if result.last() != Some(&b'\n') || result.len() > 4 * 1024 * 1024 {
                return Err(std::io::Error::other("invalid core response frame"));
            }
            Ok((result, input, output))
        },
    );
    let deadline = Instant::now() + Duration::from_secs(35);
    let status = loop {
        if exchange.is_finished() {
            break Ok(());
        }
        match current.child.try_wait() {
            Ok(Some(_)) => break Err(failure()),
            Err(_) => break Err(failure()),
            _ => {}
        }
        if STOPPING.load(Ordering::SeqCst) || Instant::now() >= deadline {
            break Err(failure());
        }
        std::thread::sleep(Duration::from_millis(5));
    };
    if status.is_err() {
        let _ = current.child.kill();
    }
    let result = exchange
        .join()
        .map_err(|_| failure())
        .and_then(|v| v.map_err(|_| failure()));
    if status.is_err() || result.is_err() {
        guard.take();
        return Err(failure());
    }
    let (output, input, reader) = result?;
    current.input = Some(input);
    current.output = Some(reader);
    let frame: Value = serde_json::from_slice(&output).map_err(|_| failure())?;
    if frame["version"] != 1 || frame["id"] != 1 {
        return Err(failure());
    }
    if !frame["error"].is_null() {
        return Err(serde_json::from_value(frame["error"].clone()).map_err(|_| failure())?);
    }
    frame.get("result").cloned().ok_or_else(failure)
}
pub fn shutdown() {
    STOPPING.store(true, Ordering::SeqCst);
    let until = Instant::now() + Duration::from_secs(3);
    while ACTIVE.load(Ordering::SeqCst) > 0 && Instant::now() < until {
        std::thread::sleep(Duration::from_millis(5));
    }
    if ACTIVE.load(Ordering::SeqCst) == 0 {
        if let Ok(mut guard) = worker().lock() {
            guard.take();
        }
        if let Ok(mut guard) = payload().lock() {
            guard.take();
        }
    }
}
