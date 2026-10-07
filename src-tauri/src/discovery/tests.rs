use super::*;
use tempfile::TempDir;
fn fixture() -> (TempDir, Context) {
    let temp = tempfile::tempdir().unwrap();
    let home = temp.path().join("用户 中文");
    std::fs::create_dir_all(&home).unwrap();
    let context = Context {
        home: home.clone(),
        local: Some(home.join("Local")),
        roaming: Some(home.join("Roaming")),
        hints: vec![],
        roots: vec![home],
        notes: vec![],
    };
    (temp, context)
}
fn put(path: &Path, text: &str) {
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, text).unwrap();
}
#[test]
fn unique_config_is_selected_without_creation_or_credential_output() {
    let (_temp, c) = fixture();
    let actual = c.home.join("tools/Codex 自定义");
    put(
        &actual.join("config.toml"),
        "model='test'\nsecret_key='never-expose-this-key'\n",
    );
    let current = c.default_path(Target::Codex);
    let r = run(&c, Target::Codex, &current, false, None).unwrap();
    assert!(same_directory(
        Path::new(r.recommended_directory.as_deref().unwrap()),
        &actual
    ));
    assert!(r.can_auto_select);
    assert!(!current.exists());
    assert!(!serde_json::to_string(&r)
        .unwrap()
        .contains("never-expose-this-key"));
}

#[test]
fn readiness_blocks_ambiguous_and_invalid_paths_but_allows_empty_default() {
    let (_temp, c) = fixture();
    let current = c.default_path(Target::Codex);
    let empty = run(&c, Target::Codex, &current, false, None).unwrap();
    assert!(directory_ready(&empty, false));
    put(&current.join("config.toml"), "model='one'");
    put(&c.home.join("portable/.codex/config.toml"), "model='two'");
    let multiple = run(&c, Target::Codex, &current, false, None).unwrap();
    assert!(!directory_ready(&multiple, false));
    assert!(directory_ready(&multiple, true));
    put(&current.join("config.toml"), "invalid TOML[");
    let invalid = run(&c, Target::Codex, &current, true, None).unwrap();
    assert!(!directory_ready(&invalid, true));
}
#[test]
fn newer_backup_and_project_config_do_not_override_global() {
    let (_temp, c) = fixture();
    let current = c.default_path(Target::Codex);
    put(&current.join("config.toml"), "model='global'");
    let backup = c.home.join(".codex-backup");
    put(&backup.join("config.toml"), "model='backup'");
    let project = c.home.join("work/project");
    put(&project.join("package.json"), "{}");
    put(&project.join(".codex/config.toml"), "model='project'");
    let r = run(&c, Target::Codex, &current, false, None).unwrap();
    assert_eq!(
        r.recommended_directory,
        Some(current.to_string_lossy().into_owned())
    );
    assert!(r.candidates.iter().any(|a| a.kind == "backup" && !a.usable));
    assert!(r
        .candidates
        .iter()
        .any(|a| a.kind == "project" && !a.usable));
}
#[test]
fn multiple_readable_globals_are_not_resolved_by_mtime() {
    let (_temp, c) = fixture();
    let current = c.default_path(Target::ClaudeCli);
    put(&current.join("settings.json"), "{}");
    put(
        &c.home.join("portable/claude-cli/settings.json"),
        "{\"model\":\"other\"}",
    );
    let r = run(&c, Target::ClaudeCli, &current, false, None).unwrap();
    assert!(!r.can_auto_select);
    assert!(r.notes.iter().any(|n| n.contains("多个可读取")));
}
#[test]
fn running_process_directory_wins_but_conflicting_processes_require_choice() {
    let (_temp, mut c) = fixture();
    let current = c.default_path(Target::Codex);
    put(&current.join("config.toml"), "model='default'");
    let active = c.home.join("custom");
    put(&active.join("config.toml"), "model='active'");
    c.hints.push(Hint {
        target: Target::Codex,
        directory: active.clone(),
        evidence: "运行中的 Codex（PID 123）".into(),
        running: true,
    });
    let r = run(&c, Target::Codex, &current, false, None).unwrap();
    assert_eq!(
        r.recommended_directory,
        Some(active.to_string_lossy().into_owned())
    );
    assert!(r.can_auto_select);
    c.hints.push(Hint {
        target: Target::Codex,
        directory: current.clone(),
        evidence: "另一个 Codex 进程".into(),
        running: true,
    });
    let r = run(&c, Target::Codex, &current, false, None).unwrap();
    assert!(r.recommended_directory.is_none());
    assert!(!r.can_auto_select);
}
#[test]
fn manual_binding_is_preserved_and_invalid_configs_are_not_auto_selected() {
    let (_temp, c) = fixture();
    let current = c.home.join("manually-chosen");
    put(&current.join("config.toml"), "model='manual'");
    put(
        &c.default_path(Target::Codex).join("config.toml"),
        "model='default'",
    );
    let r = run(&c, Target::Codex, &current, true, None).unwrap();
    assert_eq!(
        r.recommended_directory,
        Some(current.to_string_lossy().into_owned())
    );
    assert!(!r.can_auto_select);
    put(&current.join("config.toml"), "broken = [");
    let r = run(&c, Target::Codex, &current, true, None).unwrap();
    assert!(!r.candidates.iter().find(|c| c.current).unwrap().usable);
}
#[test]
fn inherited_environment_is_a_hint_not_proof_and_directories_deduplicate() {
    let (_temp, mut c) = fixture();
    let actual = c.home.join("custom");
    put(&actual.join("settings.json"), "{}");
    for evidence in [
        "Inherited CLAUDE_CONFIG_DIR",
        "Windows user CLAUDE_CONFIG_DIR",
    ] {
        c.hints.push(Hint {
            target: Target::ClaudeCli,
            directory: actual.clone(),
            evidence: evidence.into(),
            running: false,
        });
    }
    let r = run(
        &c,
        Target::ClaudeCli,
        &c.default_path(Target::ClaudeCli),
        false,
        None,
    )
    .unwrap();
    assert_eq!(
        r.candidates
            .iter()
            .filter(|a| same_directory(Path::new(&a.directory), &actual))
            .count(),
        1
    );
    assert!(!r.can_auto_select);
    assert_eq!(
        r.candidates
            .iter()
            .find(|a| same_directory(Path::new(&a.directory), &actual))
            .unwrap()
            .confidence,
        "explicit"
    );
}
#[test]
fn desktop_pairs_profiles_and_distinguishes_other_installations() {
    let (_temp, c) = fixture();
    let root = c.local.as_ref().unwrap();
    put(&root.join("Claude/claude_desktop_config.json"), "{}");
    put(&root.join("Claude-3p/claude_desktop_config.json"), "{}");
    put(
        &root.join("Claude-3p/configLibrary/_meta.json"),
        "{\"appliedId\":\"other-profile\"}",
    );
    put(
        &root.join("Claude-3p/configLibrary/other-profile.json"),
        "{}",
    );
    put(
        &root.join("Claude-Beta-3p/claude_desktop_config.json"),
        "{}",
    );
    let r = run(&c, Target::ClaudeDesktop, root, false, None).unwrap();
    assert_eq!(
        r.candidates
            .iter()
            .filter(|a| same_directory(Path::new(&a.directory), root))
            .count(),
        1
    );
    assert!(r
        .candidates
        .iter()
        .any(|a| a.directory.ends_with("Claude-Beta-3p")));
    assert!(r
        .candidates
        .iter()
        .find(|a| a.current)
        .unwrap()
        .files
        .iter()
        .any(|f| f.ends_with("other-profile.json")));
    assert!(!r.can_auto_select);
    let variant = root.join("Claude-Beta-3p");
    let files = crate::adapters::specifications(Target::ClaudeDesktop, &variant);
    assert!(files.iter().any(|f| f.0
        == variant.join(format!(
            "configLibrary/{}.json",
            crate::adapters::PROFILE_UUID
        ))));
    assert!(!files
        .iter()
        .any(|f| f.0.starts_with(root.join("Claude-3p"))));
}
#[test]
fn expanded_search_finds_unrelated_custom_home_and_skips_arbitrary_toml() {
    let (temp, c) = fixture();
    let other = temp.path().join("different-disk");
    put(
        &other.join("nested/custom-home/config.toml"),
        "model='test'",
    );
    put(&other.join("nested/custom-home/auth.json"), "{}");
    put(&other.join("arbitrary/config.toml"), "name='unrelated'");
    let r = run(
        &c,
        Target::Codex,
        &c.default_path(Target::Codex),
        false,
        Some(&other),
    )
    .unwrap();
    assert!(r
        .candidates
        .iter()
        .any(|a| a.directory.ends_with("custom-home")));
    assert!(!r
        .candidates
        .iter()
        .any(|a| a.directory.ends_with("arbitrary")));
}

#[test]
fn broken_active_desktop_profile_and_traversal_are_rejected() {
    let (_temp, c) = fixture();
    let root = c.home.join("custom-desktop");
    let meta = root.join("configLibrary/_meta.json");
    put(&meta, "{\"appliedId\":\"selected\"}");
    put(&root.join("configLibrary/selected.json"), "{broken");
    let r = run(&c, Target::ClaudeDesktop, &root, false, None).unwrap();
    assert!(!r.candidates.iter().find(|c| c.current).unwrap().usable);
    put(&meta, "{\"appliedId\":\"../outside\"}");
    let r = run(&c, Target::ClaudeDesktop, &root, false, None).unwrap();
    assert!(r
        .candidates
        .iter()
        .find(|c| c.current)
        .unwrap()
        .issue
        .as_ref()
        .unwrap()
        .contains("ID 无效"));
    let specs = crate::adapters::specifications(Target::ClaudeDesktop, &root);
    assert_eq!(
        specs.len(),
        3,
        "custom user-data root must not write a file twice"
    );
}

#[test]
fn invalid_environment_hint_does_not_fall_back_to_unrelated_global() {
    let (_temp, mut c) = fixture();
    let current = c.default_path(Target::Codex);
    put(&current.join("config.toml"), "model='default'");
    let bad = c.home.join("Codex invalid");
    put(&bad.join("config.toml"), "model=[");
    c.hints.push(Hint {
        target: Target::Codex,
        directory: bad,
        evidence: "CODEX_HOME".into(),
        running: false,
    });
    let r = run(&c, Target::Codex, &current, false, None).unwrap();
    assert!(!r.can_auto_select);
    assert!(r.recommended_directory.is_none());
}

#[test]
fn only_desktop_variant_is_not_duplicated_as_default_parent() {
    let (_temp, c) = fixture();
    let root = c.local.as_ref().unwrap();
    let variant = root.join("Claude-Beta-3p");
    put(&variant.join("claude_desktop_config.json"), "{}");
    let r = run(&c, Target::ClaudeDesktop, root, false, None).unwrap();
    assert!(r.can_auto_select);
    assert!(same_directory(
        Path::new(r.recommended_directory.as_deref().unwrap()),
        &variant
    ));
    assert_eq!(
        r.candidates
            .iter()
            .filter(|c| !c.files.is_empty() && c.usable)
            .count(),
        1
    );
}
