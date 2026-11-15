//! Native lifecycle only. The application business implementation is Go source
//! compiled from backend/, not an installed service or executable on PATH.
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    io::{BufRead, BufReader, Write},
    path::Path,
    process::{Child, ChildStdin, Command, Stdio},
    sync::{mpsc, Arc, Mutex},
    time::{Duration, Instant},
};
use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::DialogExt;

pub struct Core {
    pub endpoint: String,
    input: Arc<Mutex<ChildStdin>>,
    child: Mutex<Child>,
}

impl Core {
    pub fn start(directory: &Path, app: AppHandle) -> Result<Self, Box<dyn std::error::Error>> {
        let bytes = include_bytes!(env!("UNI_SWITCH_GO_CORE_PATH"));
        let hash = format!("{:x}", Sha256::digest(bytes));
        let runtime = directory.join("runtime").join(&hash);
        std::fs::create_dir_all(&runtime)?;
        let executable = runtime.join(if cfg!(windows) {
            "uni-switch-core.exe"
        } else {
            "uni-switch-core"
        });
        // Persistent location: client config hooks must keep working after the
        // window exits. Verify the bytes before ever executing an existing file.
        if std::fs::read(&executable).ok().as_deref() != Some(bytes.as_slice()) {
            let mut file = tempfile::NamedTempFile::new_in(&runtime)?;
            file.write_all(bytes)?;
            file.as_file().sync_all()?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                file.as_file()
                    .set_permissions(std::fs::Permissions::from_mode(0o700))?;
            }
            file.persist(&executable)?;
        }
        std::fs::write(
            runtime.join("data-root"),
            directory.to_string_lossy().as_bytes(),
        )?;
        let log = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(directory.join("core-stderr.log"))?;
        let mut command = Command::new(&executable);
        command
            .arg("__uni_switch_desktop")
            .env("UNI_SWITCH_DATA_DIR", directory)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(log);
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command.spawn()?;
        let Some(stdin) = child.stdin.take() else {
            let _ = child.kill();
            let _ = child.wait();
            return Err("后端输入管道不可用".into());
        };
        let input = Arc::new(Mutex::new(stdin));
        let Some(output) = child.stdout.take() else {
            let _ = child.kill();
            let _ = child.wait();
            return Err("后端输出管道不可用".into());
        };
        let (ready_tx, ready_rx) = mpsc::channel();
        let replies = input.clone();
        std::thread::spawn(move || {
            for line in BufReader::new(output).lines() {
                let Ok(line) = line else { break };
                let Ok(message) = serde_json::from_str::<Value>(&line) else {
                    continue;
                };
                match message["type"].as_str() {
                    Some("ready") => {
                        let _ = ready_tx.send(message["url"].as_str().unwrap_or("").to_owned());
                    }
                    Some("host") => match message["method"].as_str() {
                        Some("hide") => {
                            if let Some(w) = app.get_webview_window("main") {
                                let _ = w.hide();
                            }
                        }
                        Some("show") => {
                            if let Some(w) = app.get_webview_window("main") {
                                let _ = w.show();
                                let _ = w.unminimize();
                                let _ = w.set_focus();
                            }
                        }
                        Some("chooseFolder") => {
                            let replies = replies.clone();
                            let id = message["id"].clone();
                            let title = message["title"].as_str().unwrap_or("选择文件夹");
                            app.dialog()
                                .file()
                                .set_title(title)
                                .pick_folder(move |path| {
                                    let result = path
                                        .and_then(|p| p.into_path().ok())
                                        .map(|p| p.to_string_lossy().into_owned())
                                        .unwrap_or_default();
                                    if let Ok(mut pipe) = replies.lock() {
                                        let _ =
                                            writeln!(pipe, "{}", json!({"id":id,"result":result}));
                                    }
                                });
                        }
                        _ => {}
                    },
                    _ => {}
                }
            }
            // An ended core cannot leave an apparently working settings window.
            app.exit(0);
        });
        let endpoint = match ready_rx.recv_timeout(Duration::from_secs(90)) {
            Ok(endpoint) => endpoint,
            Err(error) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!("Go 后端未能启动：{error}；请查看 core-stderr.log").into());
            }
        };
        let valid = url::Url::parse(&endpoint).is_ok_and(|parsed| {
            parsed.scheme() == "http"
                && parsed.host_str() == Some("127.0.0.1")
                && parsed.port().is_some()
                && parsed.query_pairs().any(|(k, v)| k == "k" && !v.is_empty())
        });
        if !valid {
            let _ = child.kill();
            let _ = child.wait();
            return Err("后端返回了无效的本地入口".into());
        }
        Ok(Self {
            endpoint,
            input,
            child: Mutex::new(child),
        })
    }

    pub fn shutdown(&self) {
        if let Ok(mut pipe) = self.input.lock() {
            let _ = writeln!(pipe, "quit");
        }
        if let Ok(mut child) = self.child.lock() {
            let end = Instant::now() + Duration::from_secs(8);
            while Instant::now() < end {
                match child.try_wait() {
                    Ok(Some(_)) => return,
                    Err(_) => break,
                    _ => std::thread::sleep(Duration::from_millis(50)),
                }
            }
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}

impl Drop for Core {
    fn drop(&mut self) {
        self.shutdown();
    }
}
