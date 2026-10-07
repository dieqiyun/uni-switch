//! Read only the relevant paths from native client processes. Entire command
//! lines/environments are never serialized or logged (they can contain keys).
use super::Hint;
use crate::types::Target;
use std::{
    collections::BTreeMap,
    ffi::c_void,
    mem::{size_of, zeroed},
    path::PathBuf,
};
use windows_sys::Win32::{
    Foundation::{CloseHandle, HANDLE, INVALID_HANDLE_VALUE},
    System::{
        Diagnostics::{
            Debug::ReadProcessMemory,
            ToolHelp::{
                CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
                TH32CS_SNAPPROCESS,
            },
        },
        Environment::ExpandEnvironmentStringsW,
        Memory::{VirtualQueryEx, MEMORY_BASIC_INFORMATION},
        Registry::{
            RegGetValueW, HKEY, HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE, RRF_RT_REG_EXPAND_SZ,
            RRF_RT_REG_SZ,
        },
        Threading::{
            GetProcessTimes, IsWow64Process, OpenProcess, QueryFullProcessImageNameW,
            PROCESS_QUERY_INFORMATION, PROCESS_VM_READ,
        },
    },
};
pub(super) fn client_runtime(
    target: Target,
    directory: &std::path::Path,
    applied_at_ms: u64,
) -> (bool, bool) {
    let (hints, _) = hints();
    let matching = hints.iter().filter(|h| {
        h.target == target
            && (h.running
                || (target == Target::ClaudeDesktop && h.evidence.contains("进程 LOCALAPPDATA")))
            && super::same_directory(
                &if target == Target::ClaudeDesktop {
                    super::desktop_location(&h.directory)
                } else {
                    h.directory.clone()
                },
                &if target == Target::ClaudeDesktop {
                    super::desktop_location(directory)
                } else {
                    directory.to_path_buf()
                },
            )
    });
    let mut running = false;
    let mut needs_restart = false;
    for hint in matching {
        running = true;
        let pid = hint.evidence.split("PID ").nth(1).and_then(|s| {
            s.chars()
                .take_while(char::is_ascii_digit)
                .collect::<String>()
                .parse::<u32>()
                .ok()
        });
        let Some(pid) = pid else {
            continue;
        };
        let handle = unsafe { OpenProcess(PROCESS_QUERY_INFORMATION, 0, pid) };
        if handle.is_null() {
            continue;
        }
        let handle = Handle(handle);
        let mut creation = unsafe { zeroed() };
        let mut exit = unsafe { zeroed() };
        let mut kernel = unsafe { zeroed() };
        let mut user = unsafe { zeroed() };
        if unsafe { GetProcessTimes(handle.0, &mut creation, &mut exit, &mut kernel, &mut user) }
            != 0
        {
            let ticks = ((creation.dwHighDateTime as u64) << 32) | creation.dwLowDateTime as u64;
            let started = ticks.saturating_sub(116_444_736_000_000_000) / 10_000;
            needs_restart |= applied_at_ms > 0 && started < applied_at_ms;
        }
    }
    (running, needs_restart)
}
#[link(name = "ntdll")]
unsafe extern "system" {
    fn NtQueryInformationProcess(
        process: HANDLE,
        class: u32,
        buffer: *mut c_void,
        length: u32,
        returned: *mut u32,
    ) -> i32;
}
#[repr(C)]
struct BasicInfo {
    reserved: usize,
    peb: usize,
    reserved2: [usize; 2],
    pid: usize,
    reserved3: usize,
}
struct Handle(HANDLE);
impl Drop for Handle {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.0);
        }
    }
}
fn wide(value: &str) -> Vec<u16> {
    value.encode_utf16().chain(Some(0)).collect()
}
fn remote(process: HANDLE, address: usize, len: usize) -> Option<Vec<u8>> {
    if address < 4096 || len == 0 || len > 128 * 1024 {
        return None;
    }
    let mut bytes = vec![0; len];
    let mut read = 0;
    let ok = unsafe {
        ReadProcessMemory(
            process,
            address as *const c_void,
            bytes.as_mut_ptr().cast(),
            len,
            &mut read,
        )
    };
    (ok != 0 && read == len).then_some(bytes)
}
fn pointer(process: HANDLE, address: usize) -> Option<usize> {
    let bytes = remote(process, address, size_of::<usize>())?;
    Some(usize::from_ne_bytes(bytes.try_into().ok()?))
}
fn decode(bytes: &[u8]) -> String {
    String::from_utf16_lossy(
        &bytes
            .as_chunks::<2>()
            .0
            .iter()
            .map(|b| u16::from_le_bytes([b[0], b[1]]))
            .collect::<Vec<_>>(),
    )
}
pub(crate) struct ProcessContext {
    pub command: String,
    pub env: BTreeMap<String, String>,
    pub current_directory: Option<PathBuf>,
}
pub(crate) fn process_data(process: HANDLE) -> Option<(String, BTreeMap<String, String>)> {
    process_context(process).map(|context| (context.command, context.env))
}
pub(crate) fn process_context(process: HANDLE) -> Option<ProcessContext> {
    // The release is x64. Do not guess offsets for a WOW64 client.
    if size_of::<usize>() != 8 {
        return None;
    }
    let mut wow = 0;
    if unsafe { IsWow64Process(process, &mut wow) } == 0 || wow != 0 {
        return None;
    }
    let mut basic: BasicInfo = unsafe { zeroed() };
    if unsafe {
        NtQueryInformationProcess(
            process,
            0,
            (&mut basic as *mut BasicInfo).cast(),
            size_of::<BasicInfo>() as u32,
            std::ptr::null_mut(),
        )
    } < 0
    {
        return None;
    }
    let params = pointer(process, basic.peb + 0x20)?;
    // RTL_USER_PROCESS_PARAMETERS.CurrentDirectory.DosPath on x64.
    let current_directory = remote(process, params + 0x38, 16).and_then(|info| {
        let len = u16::from_le_bytes([info[0], info[1]]) as usize;
        let address = usize::from_ne_bytes(info[8..16].try_into().ok()?);
        let path = PathBuf::from(decode(&remote(process, address, len)?));
        path.is_absolute().then_some(path)
    });
    let command_info = remote(process, params + 0x70, 16)?;
    let len = u16::from_le_bytes([command_info[0], command_info[1]]) as usize;
    let address = usize::from_ne_bytes(command_info[8..16].try_into().ok()?);
    let command = if len == 0 {
        String::new()
    } else {
        decode(&remote(process, address, len.min(32 * 1024))?)
    };
    let environment = pointer(process, params + 0x80)?;
    let mut region: MEMORY_BASIC_INFORMATION = unsafe { zeroed() };
    if unsafe {
        VirtualQueryEx(
            process,
            environment as *const c_void,
            &mut region,
            size_of::<MEMORY_BASIC_INFORMATION>(),
        )
    } == 0
    {
        return None;
    }
    let available = (region.BaseAddress as usize)
        .checked_add(region.RegionSize)?
        .checked_sub(environment)?;
    let bytes = remote(process, environment, available.min(128 * 1024))?;
    let mut env = BTreeMap::new();
    for field in decode(&bytes).split('\0').take_while(|s| !s.is_empty()) {
        if let Some((key, value)) = field.split_once('=') {
            let key = key.to_uppercase();
            if [
                "CODEX_HOME",
                "CLAUDE_CONFIG_DIR",
                "USERPROFILE",
                "LOCALAPPDATA",
            ]
            .contains(&key.as_str())
            {
                env.insert(key, value.to_owned());
            }
        }
    }
    Some(ProcessContext {
        command,
        env,
        current_directory,
    })
}
fn argument(command: &str, name: &str) -> Option<PathBuf> {
    // Path arguments support spaces/quotes. This parser is intentionally
    // limited to Electron's user-data directory option, not shell syntax.
    let parts = split_command(command);
    let prefix = format!("{name}=");
    for (i, part) in parts.iter().enumerate() {
        let value = if part == name {
            parts.get(i + 1).map(String::as_str)
        } else {
            part.strip_prefix(&prefix)
        };
        if let Some(value) = value {
            let path = PathBuf::from(value);
            if path.is_absolute() {
                return Some(path);
            }
        }
    }
    None
}
fn split_command(command: &str) -> Vec<String> {
    let mut output = Vec::new();
    let mut current = String::new();
    let mut quoted = false;
    for ch in command.chars() {
        match ch {
            '"' => quoted = !quoted,
            c if c.is_whitespace() && !quoted => {
                if !current.is_empty() {
                    output.push(std::mem::take(&mut current));
                }
            }
            c => current.push(c),
        }
    }
    if !current.is_empty() {
        output.push(current);
    }
    output
}
fn registry_path(key: HKEY, subkey: &str, name: &str) -> Option<PathBuf> {
    let mut buffer = vec![0u16; 4096];
    let mut size = (buffer.len() * 2) as u32;
    if unsafe {
        RegGetValueW(
            key,
            wide(subkey).as_ptr(),
            wide(name).as_ptr(),
            RRF_RT_REG_SZ | RRF_RT_REG_EXPAND_SZ,
            std::ptr::null_mut(),
            buffer.as_mut_ptr().cast(),
            &mut size,
        )
    } != 0
    {
        return None;
    }
    let text = String::from_utf16_lossy(
        &buffer[..buffer.iter().position(|c| *c == 0).unwrap_or(buffer.len())],
    );
    let mut expanded = vec![0u16; 4096];
    let len = unsafe {
        ExpandEnvironmentStringsW(
            wide(&text).as_ptr(),
            expanded.as_mut_ptr(),
            expanded.len() as u32,
        )
    };
    if len == 0 || len as usize > expanded.len() {
        return None;
    }
    let path = PathBuf::from(String::from_utf16_lossy(&expanded[..len as usize - 1]));
    path.is_absolute().then_some(path)
}
pub(super) fn hints() -> (Vec<Hint>, Vec<String>) {
    let mut hints = Vec::new();
    let mut notes = Vec::new();
    for (name, target) in [
        ("CODEX_HOME", Target::Codex),
        ("CLAUDE_CONFIG_DIR", Target::ClaudeCli),
    ] {
        for (key, subkey, source) in [
            (HKEY_CURRENT_USER, "Environment", "Windows 用户"),
            (
                HKEY_LOCAL_MACHINE,
                "SYSTEM\\CurrentControlSet\\Control\\Session Manager\\Environment",
                "Windows 系统",
            ),
        ] {
            if let Some(directory) = registry_path(key, subkey, name) {
                hints.push(Hint {
                    target,
                    directory,
                    evidence: format!("{source}环境变量 {name}（新进程通常使用）"),
                    running: false,
                });
            }
        }
    }
    let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) };
    if snapshot == INVALID_HANDLE_VALUE {
        notes.push("无法读取运行中的客户端，使用环境变量和文件线索".into());
        return (hints, notes);
    }
    let snapshot = Handle(snapshot);
    let mut entry: PROCESSENTRY32W = unsafe { zeroed() };
    entry.dwSize = size_of::<PROCESSENTRY32W>() as u32;
    let mut found = unsafe { Process32FirstW(snapshot.0, &mut entry) } != 0;
    let mut inaccessible = false;
    while found {
        let name = String::from_utf16_lossy(
            &entry.szExeFile[..entry
                .szExeFile
                .iter()
                .position(|c| *c == 0)
                .unwrap_or(entry.szExeFile.len())],
        )
        .to_lowercase();
        if ["codex.exe", "claude.exe", "node.exe"].contains(&name.as_str()) {
            let handle = unsafe {
                OpenProcess(
                    PROCESS_QUERY_INFORMATION | PROCESS_VM_READ,
                    0,
                    entry.th32ProcessID,
                )
            };
            if !handle.is_null() {
                let handle = Handle(handle);
                let mut image = vec![0u16; 4096];
                let mut len = image.len() as u32;
                if unsafe { QueryFullProcessImageNameW(handle.0, 0, image.as_mut_ptr(), &mut len) }
                    != 0
                {
                    let image = String::from_utf16_lossy(&image[..len as usize]).to_lowercase();
                    if let Some((command, env)) = process_data(handle.0) {
                        let cmd = command.to_lowercase();
                        let desktop = image.contains("windowsapps\\claude_")
                            || image.contains("\\claude\\app-")
                            || image.contains("\\claude\\claude.exe");
                        let codex = name == "codex.exe"
                            && (image.contains("\\bin\\")
                                || cmd.contains("app-server")
                                || cmd.contains(" exec "))
                            || name == "node.exe"
                                && cmd.contains("@openai")
                                && cmd.contains("codex");
                        let cli = !desktop
                            && (name == "claude.exe"
                                || name == "node.exe"
                                    && cmd.contains("@anthropic-ai")
                                    && cmd.contains("claude-code"));
                        let profile = env
                            .get("USERPROFILE")
                            .map(PathBuf::from)
                            .filter(|p| p.is_absolute());
                        let hint = if codex {
                            env.get("CODEX_HOME")
                                .map(PathBuf::from)
                                .or_else(|| profile.as_ref().map(|p| p.join(".codex")))
                                .map(|p| (Target::Codex, p, "CODEX_HOME / 用户默认目录", true))
                        } else if cli {
                            env.get("CLAUDE_CONFIG_DIR")
                                .map(PathBuf::from)
                                .or_else(|| profile.as_ref().map(|p| p.join(".claude")))
                                .map(|p| {
                                    (
                                        Target::ClaudeCli,
                                        p,
                                        "CLAUDE_CONFIG_DIR / 用户默认目录",
                                        true,
                                    )
                                })
                        } else if desktop && !cmd.contains("--type=") {
                            argument(&command, "--user-data-dir")
                                .map(|p| {
                                    (Target::ClaudeDesktop, p, "--user-data-dir 启动参数", true)
                                })
                                .or_else(|| {
                                    env.get("LOCALAPPDATA").map(|p| {
                                        (
                                            Target::ClaudeDesktop,
                                            PathBuf::from(p),
                                            "进程 LOCALAPPDATA（安装实例的子目录仍需核实）",
                                            false,
                                        )
                                    })
                                })
                        } else {
                            None
                        };
                        if let Some((target, directory, source, confirmed)) =
                            hint.filter(|h| h.1.is_absolute())
                        {
                            hints.push(Hint {
                                target,
                                directory,
                                evidence: format!(
                                    "运行中的 {}（PID {}）：{source}",
                                    if target == Target::Codex {
                                        "Codex"
                                    } else if target == Target::ClaudeCli {
                                        "Claude CLI"
                                    } else {
                                        "Claude 桌面端"
                                    },
                                    entry.th32ProcessID
                                ),
                                running: confirmed,
                            });
                        }
                    } else if name != "node.exe" {
                        inaccessible = true;
                    }
                }
            } else if name != "node.exe" {
                inaccessible = true;
            }
        }
        found = unsafe { Process32NextW(snapshot.0, &mut entry) } != 0;
    }
    if inaccessible {
        notes.push("部分客户端进程无法读取（权限或进程架构限制），未将其目录标记为已确认".into());
    }
    (hints, notes)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    #[ignore = "helper process for native environment inspection"]
    fn environment_child() {
        let marker = std::env::var_os("UNI_SWITCH_PROCESS_MARKER").unwrap();
        std::fs::write(marker, "ready").unwrap();
        std::thread::sleep(std::time::Duration::from_secs(20));
    }
    #[test]
    fn reads_native_client_environment_without_exposing_other_variables() {
        let temp = tempfile::tempdir().unwrap();
        let bin = temp.path().join("bin");
        std::fs::create_dir_all(&bin).unwrap();
        let exe = bin.join("codex.exe");
        std::fs::copy(std::env::current_exe().unwrap(), &exe).unwrap();
        let directory = temp.path().join("Codex 中文 配置");
        let marker = temp.path().join("ready");
        let mut child = std::process::Command::new(exe)
            .args([
                "--exact",
                "discovery::windows::tests::environment_child",
                "--ignored",
            ])
            .env("CODEX_HOME", &directory)
            .env("OPENAI_API_KEY", "do-not-serialize-private-test-key")
            .env("UNI_SWITCH_PROCESS_MARKER", &marker)
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .spawn()
            .unwrap();
        let pid = child.id();
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        while !marker.exists() && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(40));
        }
        let (found, _) = hints();
        let running = client_runtime(Target::Codex, &directory, 0);
        let restart = client_runtime(Target::Codex, &directory, u64::MAX);
        let other = client_runtime(Target::Codex, &temp.path().join("other"), u64::MAX);
        let _ = child.kill();
        let output = child.wait_with_output().unwrap();
        assert!(
            marker.exists(),
            "helper failed: {} {} {}",
            output.status,
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        let actual = found
            .iter()
            .find(|h| h.evidence.contains(&format!("PID {pid}）")))
            .unwrap();
        assert!(actual.running);
        assert_eq!(running, (true, false));
        assert_eq!(restart, (true, true));
        assert_eq!(other, (false, false));
        assert_eq!(actual.target, Target::Codex);
        assert!(super::super::same_directory(&actual.directory, &directory));
        assert!(!serde_json::to_string(actual)
            .unwrap()
            .contains("private-test-key"));
    }
    #[test]
    fn detects_running_desktop_without_promoting_its_directory_hint_to_confirmed() {
        let temp = tempfile::tempdir().unwrap();
        let bin = temp.path().join("Claude");
        std::fs::create_dir_all(&bin).unwrap();
        let exe = bin.join("claude.exe");
        std::fs::copy(std::env::current_exe().unwrap(), &exe).unwrap();
        let directory = temp.path().join("Local");
        let marker = temp.path().join("ready");
        let mut child = std::process::Command::new(exe)
            .args([
                "--exact",
                "discovery::windows::tests::environment_child",
                "--ignored",
            ])
            .env("LOCALAPPDATA", &directory)
            .env("UNI_SWITCH_PROCESS_MARKER", &marker)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let pid = child.id();
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
        while !marker.exists() && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(40));
        }
        let found = hints().0;
        let runtime = client_runtime(Target::ClaudeDesktop, &directory, u64::MAX);
        let _ = child.kill();
        let _ = child.wait();
        assert!(marker.exists());
        assert_eq!(runtime, (true, true));
        let hint = found
            .iter()
            .find(|h| h.evidence.contains(&format!("PID {pid}）")))
            .unwrap();
        assert_eq!(hint.target, Target::ClaudeDesktop);
        assert!(
            !hint.running,
            "restart guidance must not promote an inferred directory to automatic binding proof"
        );
    }
    #[test]
    fn only_extracts_absolute_quoted_user_data_path() {
        assert_eq!(
            argument(
                r#""C:\Apps\Claude.exe" --user-data-dir="D:\Claude Data\Claude-3p" --token secret"#,
                "--user-data-dir"
            ),
            Some(PathBuf::from("D:\\Claude Data\\Claude-3p"))
        );
        assert!(argument("claude --user-data-dir relative", "--user-data-dir").is_none());
    }
}
