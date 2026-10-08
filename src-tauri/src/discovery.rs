//! Bounded, read-only discovery. Evidence is kept separate from file validity:
//! a readable config is not proof that a running client uses that directory.
use crate::{
    error::{AppError, Result},
    types::Target,
};
use serde::{Deserialize, Serialize};
use std::{
    collections::{BTreeMap, HashSet, VecDeque},
    path::{Path, PathBuf},
    time::{Duration, Instant, UNIX_EPOCH},
};

#[cfg(windows)]
pub(crate) mod windows;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DirectoryCandidate {
    pub directory: String,
    pub files: Vec<String>,
    pub evidence: Vec<String>,
    pub confidence: String,
    pub kind: String,
    pub usable: bool,
    pub current: bool,
    pub recommended: bool,
    pub modified_at: Option<u64>,
    pub issue: Option<String>,
    #[serde(skip)]
    score: u32,
    #[serde(skip)]
    explicit: bool,
}
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DirectoryDiscovery {
    pub target: Target,
    pub candidates: Vec<DirectoryCandidate>,
    pub recommended_directory: Option<String>,
    pub can_auto_select: bool,
    pub searched_locations: Vec<String>,
    pub notes: Vec<String>,
    pub limited: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DirectoryReadiness {
    pub ready: bool,
    pub message: String,
    pub discovery: DirectoryDiscovery,
}

/// Only confirmed paths, conclusive discovery, or an empty default location
/// can be used without asking the user. Recency is never evidence of use.
pub fn directory_ready(found: &DirectoryDiscovery, selected: bool) -> bool {
    if selected {
        return found.candidates.iter().any(|c| c.current && c.usable);
    }
    if found.can_auto_select {
        return true;
    }
    !found.limited
        && found.candidates.iter().all(|c| {
            c.confidence != "running"
                && c.confidence != "explicit"
                && (c.kind != "global" || c.files.is_empty())
        })
        && found
            .candidates
            .iter()
            .any(|c| c.current && c.usable && c.recommended)
}
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub(super) struct Hint {
    target: Target,
    directory: PathBuf,
    evidence: String,
    running: bool,
}
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Context {
    home: PathBuf,
    local: Option<PathBuf>,
    roaming: Option<PathBuf>,
    hints: Vec<Hint>,
    roots: Vec<PathBuf>,
    notes: Vec<String>,
}
fn path_key(path: &Path) -> String {
    let path = std::fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf());
    let text = path
        .to_string_lossy()
        .trim_end_matches(['/', '\\'])
        .to_owned();
    #[cfg(windows)]
    {
        text.trim_start_matches("\\\\?\\").to_lowercase()
    }
    #[cfg(not(windows))]
    {
        text
    }
}
pub fn same_directory(a: &Path, b: &Path) -> bool {
    path_key(a) == path_key(b)
}

impl Context {
    fn system() -> Result<Self> {
        // Test-only context permits native QA without scanning real profiles or
        // treating the developer's running client as part of a test fixture.
        #[cfg(feature = "qa-webview")]
        if let Some(file) = std::env::var_os("UNI_SWITCH_QA_DISCOVERY_CONTEXT") {
            return serde_json::from_slice(
                &std::fs::read(&file)
                    .map_err(|_| AppError::new("discovery_fixture", "无法读取搜索测试目录"))?,
            )
            .map_err(|_| AppError::new("discovery_fixture", "搜索测试目录无效"));
        }
        let home = dirs::home_dir()
            .ok_or_else(|| AppError::new("missing_home", "无法确定当前用户目录"))?;
        #[cfg(feature = "qa-webview")]
        let home = std::env::var_os("UNI_SWITCH_DATA_DIR")
            .and_then(|p| PathBuf::from(p).parent().map(Path::to_path_buf))
            .unwrap_or(home);
        let local = std::env::var_os("LOCALAPPDATA")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute());
        let roaming = std::env::var_os("APPDATA")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute());
        let mut hints = Vec::new();
        for (name, target) in [
            ("CODEX_HOME", Target::Codex),
            ("CLAUDE_CONFIG_DIR", Target::ClaudeCli),
        ] {
            if let Some(path) = std::env::var_os(name)
                .map(PathBuf::from)
                .filter(|p| p.is_absolute())
            {
                hints.push(Hint {
                    target,
                    directory: path,
                    evidence: format!("uni-switch 继承的 {name} 环境变量（客户端可能不同）"),
                    running: false,
                });
            }
        }
        #[cfg(windows)]
        #[allow(unused_mut)] // Windows QA excludes native process inspection.
        let mut notes = Vec::new();
        #[cfg(not(windows))]
        let notes = vec!["当前平台使用环境变量与文件线索；尚未读取客户端进程的配置位置".into()];
        #[cfg(all(windows, not(feature = "qa-webview")))]
        {
            let (found, warnings) = windows::hints();
            hints.extend(found);
            notes.extend(warnings);
        }
        let mut roots = vec![home.clone()];
        #[cfg(target_os = "macos")]
        roots.push(home.join("Library/Application Support"));
        #[cfg(all(not(windows), not(target_os = "macos")))]
        if let Some(config) = dirs::config_dir() {
            roots.push(config);
        }
        roots.extend(local.clone());
        roots.extend(roaming.clone());
        Ok(Self {
            home,
            local,
            roaming,
            hints,
            roots,
            notes,
        })
    }
    fn default_path(&self, target: Target) -> PathBuf {
        match target {
            Target::Codex => self.home.join(".codex"),
            Target::ClaudeCli => self.home.join(".claude"),
            Target::ClaudeDesktop => self.local.clone().unwrap_or_else(|| {
                #[cfg(target_os = "macos")]
                {
                    self.home.join("Library/Application Support")
                }
                #[cfg(all(not(windows), not(target_os = "macos")))]
                {
                    self.home.join(".config")
                }
                #[cfg(windows)]
                {
                    self.home.join("AppData/Local")
                }
            }),
        }
    }
}

pub fn client_runtime(target: Target, directory: &Path, applied_at_ms: u64) -> (bool, bool) {
    #[cfg(windows)]
    {
        windows::client_runtime(target, directory, applied_at_ms)
    }
    #[cfg(not(windows))]
    {
        let _ = (target, directory, applied_at_ms);
        (false, false)
    }
}
fn read_bounded(path: &Path) -> std::result::Result<String, String> {
    let info = std::fs::metadata(path).map_err(|_| "文件无法读取或没有访问权限".to_owned())?;
    if info.len() > 2 * 1024 * 1024 {
        return Err("配置文件超过 2 MB，未解析".into());
    }
    let bytes = std::fs::read(path).map_err(|_| "配置文件无法读取".to_owned())?;
    String::from_utf8(bytes)
        .map(|s| s.trim_start_matches('\u{feff}').to_owned())
        .map_err(|_| "配置文件不是 UTF-8".into())
}
fn backup_path(path: &Path) -> bool {
    path.components()
        .filter_map(|p| p.as_os_str().to_str())
        .any(|part| {
            let name = part.to_lowercase();
            [
                "backup", "backups", "archive", "archives", "old", "bak", "备份",
            ]
            .iter()
            .any(|term| {
                name == *term
                    || name.starts_with(&format!("{term}-"))
                    || name.ends_with(&format!("-{term}"))
                    || name.ends_with(&format!("_{term}"))
                    || name.ends_with(&format!(".{term}"))
            })
        })
}
fn desktop_marker(path: &Path) -> bool {
    path.join("claude_desktop_config.json").is_file()
        || path.join("configLibrary/_meta.json").is_file()
}
fn desktop_location(path: &Path) -> PathBuf {
    // Default pair keeps the historical parent-directory representation.
    // Variant/user-data directories are explicit so multiple siblings do not
    // get collapsed into one alphabetically chosen deployment.
    if path
        .file_name()
        .and_then(|s| s.to_str())
        .is_some_and(|s| matches!(s, "Claude" | "Claude-3p"))
    {
        path.parent().unwrap_or(path).to_path_buf()
    } else {
        path.to_path_buf()
    }
}
fn is_project(context: &Context, path: &Path, target: Target) -> bool {
    if target == Target::ClaudeDesktop || same_directory(path, &context.default_path(target)) {
        return false;
    }
    let name = path.file_name().and_then(|s| s.to_str()).unwrap_or("");
    if !matches!(name, ".codex" | ".claude") {
        return false;
    }
    let Some(parent) = path.parent() else {
        return false;
    };
    [
        ".git",
        "package.json",
        "Cargo.toml",
        "pyproject.toml",
        "AGENTS.md",
        "CLAUDE.md",
    ]
    .iter()
    .any(|m| parent.join(m).exists())
}
fn inspect(context: &Context, target: Target, path: &Path, explicit: bool) -> DirectoryCandidate {
    let paths: Vec<PathBuf> = match target {
        Target::Codex => vec![path.join("config.toml")],
        Target::ClaudeCli => vec![path.join("settings.json")],
        Target::ClaudeDesktop
            if !explicit
                && !desktop_marker(path)
                && !desktop_marker(&path.join("Claude"))
                && !desktop_marker(&path.join("Claude-3p")) =>
        {
            vec![]
        }
        Target::ClaudeDesktop => crate::adapters::specifications(target, path)
            .into_iter()
            .map(|f| f.0)
            .filter(|p| {
                p.file_name().and_then(|s| s.to_str())
                    != Some(format!("{}.json", crate::adapters::PROFILE_UUID).as_str())
            })
            .collect(),
    };
    let mut files = Vec::new();
    let mut modified = None;
    let mut issues = Vec::new();
    for file in paths.into_iter().filter(|p| p.is_file()) {
        if let Ok(info) = std::fs::metadata(&file) {
            let time = info
                .modified()
                .ok()
                .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
                .map(|d| d.as_secs());
            modified = modified.into_iter().chain(time).max();
        }
        files.push(file.to_string_lossy().into_owned());
        match read_bounded(&file) {
            Ok(text) => {
                let valid = if target == Target::Codex {
                    text.parse::<toml_edit::DocumentMut>().is_ok()
                } else {
                    serde_json::from_str::<serde_json::Value>(&text).is_ok_and(|v| v.is_object())
                };
                if !valid {
                    issues.push("配置格式无效，需要修复后再应用".to_owned());
                }
                // Inspect only structural metadata; never expose model keys or
                // credential fields to the UI, command line or diagnostic log.
                if target == Target::ClaudeDesktop
                    && file.file_name().and_then(|n| n.to_str()) == Some("_meta.json")
                {
                    if let Ok(value) = serde_json::from_str::<serde_json::Value>(&text) {
                        if let Some(id) = value["appliedId"].as_str().filter(|id| !id.is_empty()) {
                            if id.contains(['/', '\\', ':'])
                                || id == "."
                                || id == ".."
                                || id.len() > 100
                            {
                                issues.push("配置库当前选中的 profile ID 无效".into());
                                continue;
                            }
                            let profile = file.parent().unwrap().join(format!("{id}.json"));
                            if profile.is_file() {
                                files.push(profile.to_string_lossy().into_owned());
                                match read_bounded(&profile) {
                                    Ok(text)
                                        if serde_json::from_str::<serde_json::Value>(&text)
                                            .is_ok_and(|v| v.is_object()) => {}
                                    _ => issues
                                        .push("配置库当前选中的 profile 无法读取或格式无效".into()),
                                }
                            } else {
                                issues.push("配置库当前选中的 profile 文件缺失".into());
                            }
                        }
                    }
                }
            }
            Err(issue) => issues.push(issue),
        }
    }
    let kind = if backup_path(path) {
        "backup"
    } else if !explicit && is_project(context, path, target) {
        "project"
    } else {
        "global"
    };
    let usable = issues.is_empty() && (kind == "global" || explicit);
    DirectoryCandidate {
        directory: path.to_string_lossy().into_owned(),
        files,
        evidence: vec![],
        confidence: "candidate".into(),
        kind: kind.into(),
        usable,
        current: false,
        recommended: false,
        modified_at: modified,
        issue: if issues.is_empty() {
            None
        } else {
            Some(issues.join("；"))
        },
        score: 0,
        explicit,
    }
}
struct Search<'a> {
    context: &'a Context,
    target: Target,
    current: &'a Path,
    candidates: BTreeMap<String, DirectoryCandidate>,
    locations: Vec<String>,
    start: Instant,
    visited: usize,
    limited: bool,
}
impl Search<'_> {
    fn add(&mut self, path: PathBuf, evidence: &str, score: u32, explicit: bool, running: bool) {
        if !path.is_absolute() {
            return;
        }
        let path = if self.target == Target::ClaudeDesktop {
            desktop_location(&path)
        } else {
            path
        };
        let key = path_key(&path);
        let item = self
            .candidates
            .entry(key)
            .or_insert_with(|| inspect(self.context, self.target, &path, explicit));
        if !item.evidence.iter().any(|s| s == evidence) {
            item.evidence.push(evidence.into());
        }
        item.explicit |= explicit;
        if explicit && item.kind == "project" {
            item.kind = "global".into();
            item.usable = item.issue.is_none();
        }
        item.current |= same_directory(&path, self.current);
        if score > item.score {
            item.score = score;
        }
        if running {
            item.confidence = "running".into();
        } else if explicit && item.confidence != "running" {
            item.confidence = "explicit".into();
        }
    }
    fn scan(&mut self, root: PathBuf, depth: usize) {
        if !root.is_absolute() || !root.is_dir() {
            return;
        }
        self.locations.push(root.to_string_lossy().into_owned());
        let mut queue = VecDeque::from([(root, 0)]);
        let mut seen = HashSet::new();
        while let Some((path, level)) = queue.pop_front() {
            if self.visited >= 12_000 || self.start.elapsed() > Duration::from_secs(3) {
                self.limited = true;
                break;
            }
            if !seen.insert(path_key(&path)) {
                continue;
            }
            if self.target == Target::Codex && path.join("config.toml").is_file() {
                let name = path
                    .file_name()
                    .and_then(|s| s.to_str())
                    .unwrap_or("")
                    .to_lowercase();
                if name.contains("codex")
                    || [
                        "auth.json",
                        "models_cache.json",
                        "state_5.sqlite",
                        "sessions",
                    ]
                    .iter()
                    .any(|m| path.join(m).exists())
                {
                    self.add(path.clone(), "搜索到 Codex 配置文件", 100, false, false);
                }
            }
            if self.target == Target::ClaudeCli && path.join("settings.json").is_file() {
                let name = path
                    .file_name()
                    .and_then(|s| s.to_str())
                    .unwrap_or("")
                    .to_lowercase();
                if name.contains("claude") {
                    self.add(
                        path.clone(),
                        "搜索到 Claude CLI 配置文件",
                        100,
                        false,
                        false,
                    );
                }
            }
            if self.target == Target::ClaudeDesktop && desktop_marker(&path) {
                self.add(
                    path.clone(),
                    "搜索到 Claude 桌面配置或配置库",
                    100,
                    false,
                    false,
                );
            }
            if level >= depth {
                continue;
            }
            let Ok(entries) = std::fs::read_dir(&path) else {
                continue;
            };
            for entry in entries.flatten() {
                self.visited += 1;
                if self.visited >= 12_000 {
                    self.limited = true;
                    break;
                }
                let Ok(kind) = entry.file_type() else {
                    continue;
                };
                if !kind.is_dir() || kind.is_symlink() {
                    continue;
                }
                let name = entry.file_name().to_string_lossy().to_lowercase();
                if [
                    "node_modules",
                    ".git",
                    ".qa",
                    "target",
                    "cache",
                    "caches",
                    "code cache",
                    "gpuCache",
                    "webview",
                    "webview2",
                    "windows",
                    "windowsapps",
                ]
                .iter()
                .any(|n| name == n.to_lowercase())
                {
                    continue;
                }
                // AppData has its own focused roots; skip the same large tree
                // when visiting the user home and never follow reparse points.
                if level == 0 && same_directory(&path, &self.context.home) && name == "appdata" {
                    continue;
                }
                #[cfg(windows)]
                {
                    use std::os::windows::fs::MetadataExt;
                    if entry
                        .metadata()
                        .is_ok_and(|m| m.file_attributes() & 0x400 != 0)
                    {
                        continue;
                    }
                }
                queue.push_back((entry.path(), level + 1));
            }
        }
    }
}

pub fn discover(
    target: Target,
    current: &Path,
    selected: bool,
    search_root: Option<&Path>,
) -> Result<DirectoryDiscovery> {
    if search_root.is_some_and(|p| !p.is_absolute() || !p.is_dir()) {
        return Err(AppError::new("discovery_root", "请选择可以访问的绝对目录"));
    }
    run(&Context::system()?, target, current, selected, search_root)
}
fn run(
    context: &Context,
    target: Target,
    current: &Path,
    selected: bool,
    extra: Option<&Path>,
) -> Result<DirectoryDiscovery> {
    let mut search = Search {
        context,
        target,
        current,
        candidates: BTreeMap::new(),
        locations: vec![],
        start: Instant::now(),
        visited: 0,
        limited: false,
    };
    search.add(
        current.to_path_buf(),
        if selected {
            "当前手动选择或已接管的目录"
        } else {
            "当前配置目录"
        },
        if selected { 1000 } else { 120 },
        selected,
        false,
    );
    search.add(
        context.default_path(target),
        "客户端默认位置",
        150,
        false,
        false,
    );
    if target == Target::ClaudeDesktop {
        if let Some(roaming) = &context.roaming {
            search.add(
                roaming.clone(),
                "Windows Roaming 中的 Claude 桌面位置（需核实）",
                90,
                false,
                false,
            );
        }
    }
    for hint in context.hints.iter().filter(|h| h.target == target) {
        search.add(
            hint.directory.clone(),
            &hint.evidence,
            if hint.running { 900 } else { 650 },
            true,
            hint.running,
        );
    }
    let mut roots = context.roots.clone();
    if let Some(extra) = extra {
        roots.insert(0, extra.to_path_buf());
    }
    if let Some(parent) = current.parent() {
        if selected {
            roots.insert(0, parent.to_path_buf());
        }
    }
    for hint in context.hints.iter().filter(|h| h.target == target) {
        if let Some(parent) = hint.directory.parent() {
            roots.insert(0, parent.to_path_buf());
        }
    }
    let mut unique = HashSet::new();
    for root in roots {
        if unique.insert(path_key(&root)) {
            search.scan(root, if extra.is_some() { 5 } else { 3 });
        }
    }
    let mut candidates = search.candidates.into_values().collect::<Vec<_>>();
    candidates.retain(|c| {
        c.current
            || c.explicit
            || !c.files.is_empty()
            || same_directory(Path::new(&c.directory), &context.default_path(target))
    });
    candidates.sort_by(|a, b| {
        b.score
            .cmp(&a.score)
            .then_with(|| b.modified_at.cmp(&a.modified_at))
            .then_with(|| a.directory.cmp(&b.directory))
    });
    let running: Vec<_> = candidates
        .iter()
        .filter(|c| c.confidence == "running")
        .collect();
    let globals: Vec<_> = candidates
        .iter()
        .filter(|c| c.kind == "global" && c.usable && !c.files.is_empty())
        .collect();
    let mut notes = context.notes.clone();
    if selected && running.iter().any(|c| !c.current) {
        notes.push(
            "运行中的客户端使用另一目录；当前手动选择或已接管的目录已保留，可查看来源后切换".into(),
        );
    }
    let mut auto = false;
    let recommended = if selected {
        candidates
            .iter()
            .find(|c| c.current && c.usable)
            .map(|c| c.directory.clone())
    } else if running.len() == 1 {
        let c = running[0];
        auto = c.usable;
        if c.usable {
            Some(c.directory.clone())
        } else {
            notes.push("运行中的客户端配置无法读取或格式无效，请修复后重新查找".into());
            None
        }
    } else if running.len() > 1 {
        notes.push(
            "发现运行中的客户端使用不同目录，可能是桌面端与 CLI 分开配置；请按要使用的客户端选择"
                .into(),
        );
        None
    } else {
        let explicit: Vec<_> = candidates.iter().filter(|c| c.explicit).collect();
        if explicit.len() == 1 {
            notes.push("环境变量是配置线索，尚未由运行中的客户端确认".into());
            explicit[0].usable.then(|| explicit[0].directory.clone())
        } else if explicit.len() > 1 {
            notes.push("多个环境变量指向不同目录，无法自动确定；请核对运行客户端后选择".into());
            None
        } else if globals.len() == 1 {
            auto = true;
            Some(globals[0].directory.clone())
        } else if globals.len() > 1 {
            notes.push(
                "找到多个可读取的全局配置，修改时间不能证明生效目录；请核对来源后选择".into(),
            );
            globals
                .iter()
                .find(|c| same_directory(Path::new(&c.directory), &context.default_path(target)))
                .map(|c| c.directory.clone())
        } else {
            notes.push("尚未找到已有全局配置。可以使用默认位置，应用供应商时再创建文件".into());
            candidates
                .iter()
                .find(|c| {
                    c.usable
                        && same_directory(Path::new(&c.directory), &context.default_path(target))
                })
                .map(|c| c.directory.clone())
        }
    };
    if search.limited {
        auto = false;
        notes.push("搜索达到时间或文件数量上限，可选择其他文件夹继续搜索".into());
    }
    for c in &mut candidates {
        c.recommended = recommended.as_deref() == Some(c.directory.as_str());
        if c.kind == "project" {
            c.issue = Some("项目局部配置会与全局配置叠加，不作为全局供应商写入目录".into());
        }
        if c.kind == "backup" && !c.explicit {
            c.usable = false;
            c.issue = Some("备份或归档目录，不自动选用".into());
        }
    }
    Ok(DirectoryDiscovery {
        target,
        candidates,
        recommended_directory: recommended,
        can_auto_select: auto && !selected,
        searched_locations: search.locations,
        notes,
        limited: search.limited,
    })
}

#[cfg(test)]
mod tests;
