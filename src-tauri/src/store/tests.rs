use super::*;
use serde_json::{json, Value};
use std::path::Path;
use tempfile::TempDir;

fn fixture() -> (TempDir, Store) {
    let temp = tempfile::tempdir().unwrap();
    let mut store = Store::open(temp.path().join("数据 目录")).unwrap();
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        store
            .set_directory(
                target,
                temp.path().join(target.id()).to_string_lossy().into(),
            )
            .unwrap();
    }
    (temp, store)
}

fn quick_input(provider: Provider, target: Target) -> QuickModelInput {
    QuickModelInput {
        model: provider.model.clone(),
        models: vec![ProviderModel {
            id: provider.model.clone(),
            context_window: Some(256_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        }],
        expected: provider,
        target,
        synced_at: None,
        repair_reasoning_levels: None,
    }
}

#[test]
fn conversion_preferences_are_per_target_and_block_incompatible_writes() {
    let (temp, mut store) = fixture();
    let value = input(Family::Codex, "Shared OpenAI");
    let original = store.save(value).unwrap();
    assert!(original.conversion_enabled(Target::ClaudeCli));
    let saved = store
        .set_protocol_conversion(original.clone(), Target::ClaudeCli, false)
        .unwrap();
    assert!(!saved.restored);
    assert!(!saved.provider.conversion_enabled(Target::ClaudeCli));
    assert!(saved.provider.conversion_enabled(Target::ClaudeDesktop));
    assert!(store.apply(Target::Codex, &original.id).is_ok());
    let config_before = adapters::read(&temp.path().join("claude_cli/settings.json")).unwrap();
    assert_eq!(
        store
            .apply(Target::ClaudeCli, &original.id)
            .unwrap_err()
            .code,
        "conversion_required"
    );
    assert_eq!(
        adapters::read(&temp.path().join("claude_cli/settings.json")).unwrap(),
        config_before
    );
    let enabled = store
        .set_protocol_conversion(saved.provider, Target::ClaudeCli, true)
        .unwrap();
    assert!(enabled.provider.conversion_enabled(Target::ClaudeCli));
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        store
            .set_protocol_conversion(enabled.provider, Target::Codex, false)
            .unwrap_err()
            .code,
        "conversion_not_required"
    );
}

#[test]
fn disabling_applied_conversion_restores_only_that_client_and_persists() {
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        let (temp, mut store) = fixture();
        let (route, _listener) = crate::bridge::Route::bind(&store.data_directory).unwrap();
        store.set_bridge_route(route);
        let mut value = input(
            if target == Target::Codex {
                Family::Claude
            } else {
                Family::Codex
            },
            "Converted",
        );
        let other = if target == Target::Codex {
            Target::ClaudeCli
        } else {
            Target::Codex
        };
        value.codex_options.upstream_protocol = Some(if target == Target::Codex {
            CodexProtocol::Anthropic
        } else {
            CodexProtocol::Openai
        });
        let provider = store.save(value).unwrap();
        let directory = temp.path().join(target.id());
        let originals: Vec<_> = adapters::specifications(target, &directory)
            .iter()
            .map(|(p, _, _)| (p.clone(), adapters::read(p).unwrap()))
            .collect();
        store.apply(target, &provider.id).unwrap();
        store.apply(other, &provider.id).unwrap();
        let before_revision = store.status(target).unwrap().configuration_revision;
        let other_before: Vec<_> = adapters::specifications(other, &temp.path().join(other.id()))
            .iter()
            .map(|(p, _, _)| (p.clone(), adapters::read(p).unwrap()))
            .collect();
        let saved = store
            .set_protocol_conversion(provider, target, false)
            .unwrap();
        assert!(saved.restored);
        assert!(store.status(target).unwrap().active_provider_id.is_none());
        assert!(store.status(target).unwrap().configuration_revision > before_revision);
        assert!(store.bridge_record(target).unwrap().is_none());
        for (path, original) in originals {
            assert_eq!(adapters::read(&path).unwrap(), original);
        }
        for (path, original) in other_before {
            assert_eq!(adapters::read(&path).unwrap(), original);
        }
        assert_eq!(store.status(other).unwrap().state, "applied");
        drop(store);
        let mut store = Store::open(temp.path().join("数据 目录")).unwrap();
        assert_eq!(
            store.apply(target, &saved.provider.id).unwrap_err().code,
            "conversion_required"
        );
        let enabled = store
            .set_protocol_conversion(saved.provider, target, true)
            .unwrap();
        assert!(enabled.provider.conversion_enabled(target));
        assert!(store.status(target).unwrap().active_provider_id.is_none());
    }
}

#[test]
fn conversion_toggle_rejects_stale_supplier_and_external_changes_without_mutation() {
    let (temp, mut store) = fixture();
    let (route, _listener) = crate::bridge::Route::bind(&store.data_directory).unwrap();
    store.set_bridge_route(route);
    let p = store
        .save(input(Family::Claude, "Claude upstream"))
        .unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let path = temp.path().join("codex/config.toml");
    let mut config = adapters::read(&path)
        .unwrap()
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    config["model"] = toml_edit::value("externally-changed");
    put(&path, &config.to_string());
    assert_eq!(
        store
            .set_protocol_conversion(p.clone(), Target::Codex, false)
            .unwrap_err()
            .code,
        "configuration_changed"
    );
    assert_eq!(adapters::read(&path).unwrap().unwrap(), config.to_string());
    assert!(store
        .provider(&p.id)
        .unwrap()
        .summary
        .conversion_enabled(Target::Codex));
    let mut stale = p.clone();
    stale.name = "stale supplier".into();
    assert_eq!(
        store
            .set_protocol_conversion(stale, Target::Codex, false)
            .unwrap_err()
            .code,
        "provider_changed"
    );
}

#[cfg(windows)]
#[test]
fn failed_conversion_restore_keeps_switch_and_active_snapshot_enabled() {
    use std::os::windows::fs::OpenOptionsExt;
    use windows_sys::Win32::Storage::FileSystem::FILE_SHARE_READ;
    let (temp, mut store) = fixture();
    let (route, _listener) = crate::bridge::Route::bind(&store.data_directory).unwrap();
    store.set_bridge_route(route);
    let path = temp.path().join("codex/config.toml");
    put(&path, "model='original'\n");
    let provider = store.save(input(Family::Claude, "Converted")).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let before: Vec<_> = adapters::specifications(Target::Codex, &temp.path().join("codex"))
        .iter()
        .map(|(p, _, _)| (p.clone(), adapters::read(p).unwrap()))
        .collect();
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    let held = std::fs::OpenOptions::new()
        .read(true)
        .share_mode(FILE_SHARE_READ)
        .open(&path)
        .unwrap();
    assert!(store
        .set_protocol_conversion(provider.clone(), Target::Codex, false)
        .is_err());
    drop(held);
    for (path, content) in before {
        assert_eq!(adapters::read(&path).unwrap(), content);
    }
    assert!(store
        .provider(&provider.id)
        .unwrap()
        .summary
        .conversion_enabled(Target::Codex));
    assert_eq!(
        store
            .status(Target::Codex)
            .unwrap()
            .active_provider_id
            .as_deref(),
        Some(provider.id.as_str())
    );
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        revision
    );
    assert!(store.bridge_record(Target::Codex).unwrap().is_some());
    assert!(
        store
            .set_protocol_conversion(provider, Target::Codex, false)
            .unwrap()
            .restored
    );
    assert_eq!(
        adapters::read(&path)
            .unwrap()
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap()["model"]
            .as_str(),
        Some("original")
    );
}

#[test]
fn protocol_detection_uses_saved_key_and_retains_models_preferences_and_live_files() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Detected connection");
    value.codex_options.conversion_disabled_targets = vec![Target::ClaudeCli];
    let p = store.save(value).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let path = temp.path().join("codex/config.toml");
    let before = adapters::read(&path).unwrap();
    let (base, key, protocol, auth) = store.protocol_probe_connection(&p).unwrap();
    assert_eq!(base, p.base_url);
    assert_eq!(key, "test-key-1234");
    assert!(protocol.is_none() && auth.is_none());
    let result = ModelSyncResult {
        models: vec![],
        synced_at: 1234,
        protocol: CodexProtocol::Openai,
        auth_mode: p.auth_mode.clone(),
        base_url: p.base_url.clone(),
    };
    let detected = store.accept_protocol_detection(p.clone(), result).unwrap();
    assert_eq!(detected.codex_options.protocol_detected_at, Some(1234));
    assert_eq!(
        detected.codex_options.conversion_disabled_targets,
        vec![Target::ClaudeCli]
    );
    assert_eq!(
        json(&detected.codex_options.models).unwrap(),
        json(&p.codex_options.models).unwrap()
    );
    assert_eq!(before, adapters::read(&path).unwrap());
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        store.protocol_probe_connection(&p).unwrap_err().code,
        "provider_changed"
    );
}

fn assert_automatic_reasoning(directory: &Path) {
    let config = std::fs::read_to_string(directory.join("config.toml"))
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    let display = config["desktop"]["enabled-reasoning-efforts"]
        .as_array()
        .unwrap();
    let catalog = read_json(&directory.join("uni-switch-models.json"));
    assert!(!catalog["models"].as_array().unwrap().is_empty());
    for effort in [
        "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra",
    ] {
        assert!(display.iter().any(|v| v.as_str() == Some(effort)));
        for model in catalog["models"].as_array().unwrap() {
            assert_eq!(
                model["supported_reasoning_levels"]
                    .as_array()
                    .unwrap()
                    .iter()
                    .filter(|v| v["effort"] == effort)
                    .count(),
                1
            );
        }
    }
}

fn hide_max(directory: &Path) {
    let config = directory.join("config.toml");
    let mut doc = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    let mut display = toml_edit::Array::new();
    display.push("high");
    doc["desktop"]["enabled-reasoning-efforts"] = toml_edit::value(display);
    put(&config, &doc.to_string());
    let path = directory.join("uni-switch-models.json");
    let mut catalog = read_json(&path);
    for model in catalog["models"].as_array_mut().unwrap() {
        model["supported_reasoning_levels"]
            .as_array_mut()
            .unwrap()
            .retain(|entry| entry["effort"] != "max");
    }
    put(&path, &serde_json::to_string_pretty(&catalog).unwrap());
}

#[test]
fn automatic_reasoning_applies_to_legacy_and_shared_providers_without_opt_in() {
    for family in [Family::Codex, Family::Claude] {
        let (temp, mut store) = fixture();
        store.set_bridge_route(crate::bridge::Route {
            port: 19881,
            token: "d".repeat(64),
        });
        let provider = store.save(input(family, "Automatic")).unwrap();
        assert!(!provider.codex_options.repair_reasoning_levels);
        let summary = json(&provider).unwrap();
        store.apply(Target::Codex, &provider.id).unwrap();
        assert_automatic_reasoning(&temp.path().join("codex"));
        assert_eq!(
            json(&store.provider(&provider.id).unwrap().summary).unwrap(),
            summary
        );
        let revision = store.status(Target::Codex).unwrap().configuration_revision;
        store.apply(Target::Codex, &provider.id).unwrap();
        assert_eq!(
            store.status(Target::Codex).unwrap().configuration_revision,
            revision
        );
        hide_max(&temp.path().join("codex"));
        store.restore(Target::Codex).unwrap();
        assert!(!temp.path().join("codex/config.toml").exists());
        assert!(!temp.path().join("codex/uni-switch-models.json").exists());
    }
}

#[test]
fn automatic_reasoning_rechecks_model_context_sync_and_fast_writes_and_noop_confirmation() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("codex");
    let mut value = input(Family::Codex, "Automatic updates");
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        enabled: true,
        reasoning_efforts: vec![],
        context_window: Some(256_000),
        ..Default::default()
    }];
    let mut provider = store.commit_provider(value, Target::Codex, true).unwrap();
    for action in ["context", "default", "sync", "fast", "same"] {
        hide_max(&dir);
        assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
        let revision = store.status(Target::Codex).unwrap().configuration_revision;
        match action {
            "fast" => {
                provider = store
                    .set_provider_fast_mode(&provider.id, true)
                    .unwrap()
                    .provider
            }
            "sync" => {
                store.sync_connection(Target::Codex, &provider.id).unwrap();
            }
            _ => {
                let mut request = quick_input(provider.clone(), Target::Codex);
                request.models = provider.codex_options.models.clone();
                if action == "context" {
                    request.models[0].context_window = Some(192_000);
                }
                if action == "default" {
                    request.model = "gpt-next".into();
                    request.models.push(ProviderModel {
                        id: request.model.clone(),
                        enabled: true,
                        reasoning_efforts: vec![],
                        context_window: Some(256_000),
                        ..Default::default()
                    });
                }
                provider = store.quick_model_settings(request).unwrap().provider;
            }
        }
        assert_automatic_reasoning(&dir);
        assert!(store.status(Target::Codex).unwrap().configuration_revision > revision);
    }
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    let mut request = quick_input(provider.clone(), Target::Codex);
    request.models = provider.codex_options.models.clone();
    store.quick_model_settings(request).unwrap();
    store.set_provider_fast_mode(&provider.id, true).unwrap();
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        revision
    );
}

#[test]
fn automatic_reasoning_rejects_other_catalog_changes_without_partial_writes() {
    for field in ["context_window", "description", "reasoning_description"] {
        let (temp, mut store) = fixture();
        let provider = store
            .commit_provider(input(Family::Codex, "Guard"), Target::Codex, true)
            .unwrap();
        let dir = temp.path().join("codex");
        hide_max(&dir);
        let path = dir.join("uni-switch-models.json");
        let mut catalog = read_json(&path);
        if field == "reasoning_description" {
            catalog["models"][0]["supported_reasoning_levels"][0]["description"] =
                json!("External");
        } else {
            catalog["models"][0][field] = json!("External");
        }
        put(&path, &serde_json::to_string_pretty(&catalog).unwrap());
        let before = std::fs::read(&path).unwrap();
        let config_before = std::fs::read(dir.join("config.toml")).unwrap();
        assert_eq!(
            store
                .set_provider_fast_mode(&provider.id, true)
                .unwrap_err()
                .code,
            "external_change"
        );
        assert_eq!(std::fs::read(&path).unwrap(), before);
        assert_eq!(
            std::fs::read(dir.join("config.toml")).unwrap(),
            config_before
        );
        assert_eq!(
            json(&store.provider(&provider.id).unwrap().summary).unwrap(),
            json(&provider).unwrap()
        );
    }
}

#[test]
fn automatic_fast_repair_upgrades_old_baseline_and_restores_first_owned_preferences() {
    let (temp, mut store) = fixture();
    let provider = store
        .commit_provider(input(Family::Codex, "Old baseline"), Target::Codex, true)
        .unwrap();
    let dir = temp.path().join("codex");
    let (_, _, baseline) = store.target_record(Target::Codex).unwrap();
    let mut baseline = baseline.unwrap();
    // A 0.5.8 provider without the optional repair had no catalog and did not
    // own desktop display preferences. Capture them on the first new write.
    for file in &mut baseline {
        if file.format == "toml" {
            let mut legacy = file
                .expected
                .as_ref()
                .unwrap()
                .parse::<toml_edit::DocumentMut>()
                .unwrap();
            legacy.as_table_mut().remove("model_catalog_json");
            let mut choices = toml_edit::Array::new();
            choices.push("high");
            legacy["desktop"]["enabled-reasoning-efforts"] = toml_edit::value(choices);
            legacy["desktop"]["theme"] = toml_edit::value("dark");
            file.expected = Some(legacy.to_string());
            file.keys.retain(|k| k != adapters::REASONING_DISPLAY_KEY);
            put(&file.path, file.expected.as_ref().unwrap());
        }
        if file.format == "catalog" {
            file.expected = None;
            file.original = None;
            std::fs::remove_file(&file.path).unwrap();
        }
    }
    baseline.retain(|file| file.format != "catalog");
    store
        .conn
        .execute(
            "UPDATE targets SET baseline=?1 WHERE id='codex'",
            [json(&baseline).unwrap()],
        )
        .unwrap();
    let mut pending = input(Family::Codex, "Old baseline");
    pending.id = Some(provider.id.clone());
    pending.api_key = Some("unconfirmed-key".into());
    pending.model = "unconfirmed-model".into();
    store.save(pending).unwrap();
    store.set_provider_fast_mode(&provider.id, true).unwrap();
    assert_automatic_reasoning(&dir);
    let text = std::fs::read_to_string(dir.join("config.toml")).unwrap();
    assert!(!text.contains("unconfirmed"));
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    store.restore(Target::Codex).unwrap();
    let restored = std::fs::read_to_string(dir.join("config.toml"))
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    let display = restored["desktop"]["enabled-reasoning-efforts"]
        .as_array()
        .unwrap();
    assert_eq!(display.len(), 1);
    assert_eq!(display.get(0).unwrap().as_str(), Some("high"));
    assert_eq!(restored["desktop"]["theme"].as_str(), Some("dark"));
    assert!(!dir.join("uni-switch-models.json").exists());
}

#[test]
fn inline_rename_preserves_credentials_files_revision_and_active_status() {
    let (temp, mut store) = fixture();
    let provider = store
        .commit_provider(input(Family::Codex, "Before"), Target::Codex, true)
        .unwrap();
    let before = std::fs::read(temp.path().join("codex/config.toml")).unwrap();
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    let renamed = store
        .rename_provider(provider.clone(), " 新名称 ".into())
        .unwrap();
    assert_eq!(renamed.name, "新名称");
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        revision
    );
    assert_eq!(
        std::fs::read(temp.path().join("codex/config.toml")).unwrap(),
        before
    );
    assert_eq!(
        store.provider(&renamed.id).unwrap().api_key,
        "test-key-1234"
    );
    assert_eq!(
        store
            .rename_provider(provider, "Stale".into())
            .unwrap_err()
            .code,
        "provider_changed"
    );
}

#[test]
fn inline_models_keep_other_shared_targets_independent_and_work_with_claude_on_codex() {
    let (temp, mut store) = fixture();
    store.set_bridge_route(crate::bridge::Route {
        port: 19881,
        token: "d".repeat(64),
    });
    let provider = store.save(input(Family::Claude, "Shared Claude")).unwrap();
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        store.apply(target, &provider.id).unwrap();
    }
    let cli_before = std::fs::read(temp.path().join("claude_cli/settings.json")).unwrap();
    let desktop_files = store.status(Target::ClaudeDesktop).unwrap().files;
    let desktop_before: Vec<_> = desktop_files
        .iter()
        .map(|path| std::fs::read(path).ok())
        .collect();
    let mut request = quick_input(provider.clone(), Target::Codex);
    request.models[0].context_window = Some(192_000);
    request.repair_reasoning_levels = Some(true);
    let saved = store.quick_model_settings(request).unwrap();
    assert!(saved.applied);
    assert_eq!(saved.provider.family, Family::Claude);
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"][0]["context_window"], 192_000);
    assert_eq!(
        std::fs::read(temp.path().join("claude_cli/settings.json")).unwrap(),
        cli_before
    );
    assert_eq!(
        desktop_files
            .iter()
            .map(|path| std::fs::read(path).ok())
            .collect::<Vec<_>>(),
        desktop_before
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        store.status(Target::ClaudeCli).unwrap().applied_model,
        Some(provider.model)
    );
}

#[test]
fn model_refresh_replaces_catalog_and_removed_client_selection_without_reintroducing_ids() {
    // Cover both a retired default and a retired model selected inside Codex.
    for retire_default in [false, true] {
        let (temp, mut store) = fixture();
        let mut value = input(Family::Codex, "Refreshed catalog");
        let initial = ["gpt-kept", "gpt-retired", "gpt-off"];
        value.model = if retire_default {
            "gpt-retired"
        } else {
            "gpt-kept"
        }
        .into();
        value.codex_options.fast_mode = Some(true);
        value.codex_options.models = initial
            .iter()
            .map(|id| ProviderModel {
                id: (*id).into(),
                context_window: Some(512_000),
                reasoning_efforts: vec![],
                enabled: *id != "gpt-off",
                ..Default::default()
            })
            .collect();
        let provider = store.commit_provider(value, Target::Codex, true).unwrap();
        let config_path = temp.path().join("codex/config.toml");
        let mut config = std::fs::read_to_string(&config_path)
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap();
        config["model"] = toml_edit::value("gpt-retired");
        config["model_reasoning_effort"] = toml_edit::value("max");
        std::fs::write(&config_path, config.to_string()).unwrap();
        let mut request = quick_input(provider.clone(), Target::Codex);
        request.model = "gpt-kept".into();
        request.models = vec![
            ProviderModel {
                id: "gpt-kept".into(),
                context_window: Some(512_000),
                reasoning_efforts: vec![],
                enabled: true,
                ..Default::default()
            },
            ProviderModel {
                id: "gpt-new".into(),
                context_window: Some(256_000),
                reasoning_efforts: vec![],
                enabled: true,
                ..Default::default()
            },
            ProviderModel {
                id: "gpt-off".into(),
                context_window: Some(512_000),
                reasoning_efforts: vec![],
                enabled: false,
                ..Default::default()
            },
        ];
        request.synced_at = Some(456);
        let saved = store.quick_model_settings(request).unwrap();
        assert!(saved.applied);
        assert!(!saved
            .provider
            .codex_options
            .models
            .iter()
            .any(|m| m.id == "gpt-retired"));
        let catalog_path = temp.path().join("codex/uni-switch-models.json");
        let catalog = read_json(&catalog_path);
        assert_eq!(
            catalog["models"]
                .as_array()
                .unwrap()
                .iter()
                .map(|m| m["slug"].as_str().unwrap())
                .collect::<Vec<_>>(),
            vec!["gpt-kept", "gpt-new"]
        );
        assert_eq!(catalog["models"][0]["context_window"], 512_000);
        let config = std::fs::read_to_string(&config_path)
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap();
        assert_eq!(config["model"].as_str(), Some("gpt-kept"));
        assert_eq!(config["model_reasoning_effort"].as_str(), Some("max"));
        assert_eq!(config["service_tier"].as_str(), Some("priority"));
        assert_eq!(
            Path::new(config["model_catalog_json"].as_str().unwrap())
                .canonicalize()
                .unwrap(),
            catalog_path.canonicalize().unwrap()
        );
        assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
        let data_dir = store.data_directory.clone();
        drop(store);
        let mut store = Store::open(data_dir).unwrap();
        let provider = store.provider(&provider.id).unwrap().summary;
        let mut repeat = quick_input(provider.clone(), Target::Codex);
        repeat.models = provider.codex_options.models;
        store.quick_model_settings(repeat).unwrap();
        assert!(!std::fs::read_to_string(catalog_path)
            .unwrap()
            .contains("gpt-retired"));
    }
}

#[test]
fn inline_model_settings_apply_to_each_active_client_and_preserve_other_settings() {
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        let (temp, mut store) = fixture();
        let mut value = input(target.family(), "Inline");
        value.codex_options.models = vec![ProviderModel {
            id: value.model.clone(),
            context_window: Some(256_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        }];
        let provider = store.commit_provider(value, target, true).unwrap();
        let before = store.status(target).unwrap().configuration_revision;
        let mut request = quick_input(provider.clone(), target);
        request.model = if target == Target::Codex {
            "gpt-next".into()
        } else {
            "claude-opus-4-6".into()
        };
        request.models.push(ProviderModel {
            id: request.model.clone(),
            context_window: Some(512_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        });
        let saved = store.quick_model_settings(request).unwrap();
        assert!(saved.applied);
        assert!(store.status(target).unwrap().configuration_revision > before);
        assert_eq!(
            store.status(target).unwrap().applied_model.as_deref(),
            Some(saved.provider.model.as_str())
        );
        assert_eq!(
            store.provider(&provider.id).unwrap().api_key,
            "test-key-1234"
        );
        assert_eq!(saved.provider.base_url, provider.base_url);
        if target == Target::Codex {
            let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
            assert_eq!(catalog["models"][1]["context_window"], 512_000);
        }
    }
}

#[test]
fn inline_inactive_selection_does_not_switch_or_write_client_and_applies_later() {
    let (temp, mut store) = fixture();
    let active = store
        .commit_provider(input(Family::Codex, "Active"), Target::Codex, true)
        .unwrap();
    let other = store.save(input(Family::Codex, "Other")).unwrap();
    let before = std::fs::read(temp.path().join("codex/config.toml")).unwrap();
    let mut request = quick_input(other.clone(), Target::Codex);
    request.models[0].context_window = Some(128_000);
    request.repair_reasoning_levels = Some(true);
    let saved = store.quick_model_settings(request).unwrap();
    assert!(!saved.applied);
    assert_eq!(
        std::fs::read(temp.path().join("codex/config.toml")).unwrap(),
        before
    );
    assert_eq!(
        store.status(Target::Codex).unwrap().active_provider_id,
        Some(active.id)
    );
    store.apply(Target::Codex, &other.id).unwrap();
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"][0]["context_window"], 128_000);
}

#[test]
fn inline_rejects_stale_provider_empty_selection_and_external_changes_atomically() {
    let (temp, mut store) = fixture();
    let provider = store
        .commit_provider(input(Family::Codex, "Guard"), Target::Codex, true)
        .unwrap();
    let mut stale = quick_input(provider.clone(), Target::Codex);
    stale.expected.name = "Old view".into();
    assert_eq!(
        store.quick_model_settings(stale).unwrap_err().code,
        "provider_changed"
    );
    let mut invalid = quick_input(provider.clone(), Target::Codex);
    invalid.models[0].enabled = false;
    assert_eq!(
        store.quick_model_settings(invalid).unwrap_err().code,
        "no_enabled_models"
    );
    let config = temp.path().join("codex/config.toml");
    let external = std::fs::read_to_string(&config)
        .unwrap()
        .replace("test-model", "external-model");
    std::fs::write(&config, &external).unwrap();
    assert_eq!(
        store
            .quick_model_settings(quick_input(provider.clone(), Target::Codex))
            .unwrap_err()
            .code,
        "configuration_changed"
    );
    assert_eq!(std::fs::read_to_string(config).unwrap(), external);
    assert_eq!(
        json(&store.provider(&provider.id).unwrap().summary).unwrap(),
        json(&provider).unwrap()
    );
}

#[test]
fn inline_context_and_repair_preserve_client_model_effort_and_fast_without_noop_prompt() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Choices");
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        context_window: Some(256_000),
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let provider = store.commit_provider(value, Target::Codex, true).unwrap();
    let config = temp.path().join("codex/config.toml");
    let mut doc = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    doc["model_reasoning_effort"] = toml_edit::value("max");
    doc["service_tier"] = toml_edit::value("flex");
    std::fs::write(&config, doc.to_string()).unwrap();
    let mut request = quick_input(provider, Target::Codex);
    request.models[0].context_window = Some(128_000);
    request.repair_reasoning_levels = Some(true);
    let saved = store.quick_model_settings(request).unwrap().provider;
    let after = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    assert_eq!(after["model_reasoning_effort"].as_str(), Some("max"));
    assert_eq!(after["service_tier"].as_str(), Some("flex"));
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    let mut same = quick_input(saved, Target::Codex);
    same.models[0].context_window = Some(128_000);
    same.repair_reasoning_levels = Some(true);
    store.quick_model_settings(same).unwrap();
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        revision
    );
}
#[test]
fn overview_exposes_actual_configuration_write_revision_without_process_inspection() {
    let (_temp, mut store) = fixture();
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        0
    );
    let p = store
        .save(input(Family::Codex, "write notification"))
        .unwrap();
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        0
    );
    store.apply(Target::Codex, &p.id).unwrap();
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    assert!(revision > 0);
    assert_eq!(revision, store.runtime_context(Target::Codex).unwrap().1);
    store.apply(Target::Codex, &p.id).unwrap();
    assert_eq!(
        store.status(Target::Codex).unwrap().configuration_revision,
        revision
    );
    store.set_provider_fast_mode(&p.id, true).unwrap();
    assert!(store.status(Target::Codex).unwrap().configuration_revision > revision);
    let revision = store.status(Target::Codex).unwrap().configuration_revision;
    store.restore(Target::Codex).unwrap();
    let codex = store
        .overview()
        .unwrap()
        .targets
        .into_iter()
        .find(|s| s.target == Target::Codex)
        .unwrap();
    assert!(codex.configuration_revision > revision);
    assert!(codex.active_provider_id.is_none());
}
#[test]
fn claude_restart_revisions_follow_real_writes_and_restore_independently() {
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        let (temp, mut store) = fixture();
        let p = store.save(input(Family::Claude, "Claude restart")).unwrap();
        assert_eq!(store.runtime_context(target).unwrap().1, 0);
        store.apply(target, &p.id).unwrap();
        let revision = store.runtime_context(target).unwrap().1;
        assert!(revision > 0);
        store.apply(target, &p.id).unwrap();
        assert_eq!(store.runtime_context(target).unwrap().1, revision);
        assert_eq!(
            store
                .runtime_context(if target == Target::ClaudeCli {
                    Target::ClaudeDesktop
                } else {
                    Target::ClaudeCli
                })
                .unwrap()
                .1,
            0
        );
        store.restore(target).unwrap();
        assert!(store.runtime_context(target).unwrap().1 > revision);
        store
            .set_directory(
                target,
                temp.path()
                    .join("another-directory")
                    .to_string_lossy()
                    .into(),
            )
            .unwrap();
        assert_eq!(store.runtime_context(target).unwrap().1, 0);
    }
}
fn input(family: Family, name: &str) -> ProviderInput {
    ProviderInput {
        id: None,
        family,
        name: name.into(),
        base_url: "https://gateway.example.test/v1/".into(),
        api_key: Some("test-key-1234".into()),
        balance_access_token: None,
        model: if family == Family::Claude {
            "claude-sonnet-4-6".into()
        } else {
            "test-model".into()
        },
        auth_mode: "bearer".into(),
        reasoning_effort: None,
        codex_options: CodexOptions::default(),
    }
}
fn put(path: &Path, text: &str) {
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, text).unwrap();
}
fn read_json(path: &Path) -> Value {
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

#[test]
fn fast_toggle_preserves_client_choices_catalog_and_restores_original() {
    let (temp, mut store) = fixture();
    let config = temp.path().join("codex/config.toml");
    let original = "# original\nmodel='original'\nservice_tier='flex'\n[features]\nfast_mode=false\nother=true\n";
    put(&config, original);
    let mut value = input(Family::Codex, "Fast test");
    value.codex_options.repair_reasoning_levels = true;
    value.codex_options.models = ["test-model", "other-model"]
        .iter()
        .map(|id| ProviderModel {
            id: (*id).into(),
            context_window: Some(128_000),
            reasoning_efforts: vec!["low".into()],
            enabled: true,
            ..Default::default()
        })
        .collect();
    let p = store.commit_provider(value, Target::Codex, true).unwrap();
    let mut current: toml_edit::DocumentMut =
        std::fs::read_to_string(&config).unwrap().parse().unwrap();
    current["model"] = toml_edit::value("other-model");
    let text = current.to_string();
    put(
        &config,
        &format!("model_reasoning_effort = 'max'\n{text}\n[extra]\nkeep=true\n"),
    );
    let catalog_path = temp.path().join("codex/uni-switch-models.json");
    let before = read_json(&catalog_path);
    for enabled in [true, false, true] {
        let result = store.set_provider_fast_mode(&p.id, enabled).unwrap();
        assert!(result.applied);
        assert_eq!(result.provider.codex_options.fast_mode, Some(enabled));
        let doc: toml_edit::DocumentMut =
            std::fs::read_to_string(&config).unwrap().parse().unwrap();
        assert_eq!(
            doc["service_tier"].as_str(),
            Some(if enabled { "priority" } else { "default" })
        );
        assert_eq!(doc["features"]["fast_mode"].as_bool(), Some(true));
        assert_eq!(doc["features"]["other"].as_bool(), Some(true));
        assert_eq!(doc["model"].as_str(), Some("other-model"));
        assert_eq!(doc["model_reasoning_effort"].as_str(), Some("max"));
        assert_eq!(doc["extra"]["keep"].as_bool(), Some(true));
        let catalog = read_json(&catalog_path);
        for (i, model) in catalog["models"].as_array().unwrap().iter().enumerate() {
            let mut unchanged = model.clone();
            unchanged["service_tiers"] = before["models"][i]["service_tiers"].clone();
            unchanged["additional_speed_tiers"] =
                before["models"][i]["additional_speed_tiers"].clone();
            assert_eq!(unchanged, before["models"][i]);
            assert_eq!(
                model["service_tiers"].as_array().unwrap().len(),
                usize::from(enabled)
            );
            assert_eq!(
                model["additional_speed_tiers"],
                if enabled { json!(["fast"]) } else { json!([]) }
            );
        }
        assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    }
    let data = store.data_directory.clone();
    drop(store);
    let mut reopened = Store::open(data).unwrap();
    assert_eq!(
        reopened
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .fast_mode,
        Some(true)
    );
    assert_eq!(reopened.status(Target::Codex).unwrap().state, "applied");
    reopened.restore(Target::Codex).unwrap();
    let doc: toml_edit::DocumentMut = std::fs::read_to_string(config).unwrap().parse().unwrap();
    assert_eq!(doc["service_tier"].as_str(), Some("flex"));
    assert_eq!(doc["features"]["fast_mode"].as_bool(), Some(false));
    assert!(!catalog_path.exists());
}

#[test]
fn inactive_fast_toggle_saves_without_switching_then_applies_on_use() {
    let (temp, mut store) = fixture();
    let active = store
        .commit_provider(input(Family::Codex, "Active"), Target::Codex, true)
        .unwrap();
    let other = store.save(input(Family::Codex, "Other")).unwrap();
    let config = temp.path().join("codex/config.toml");
    let before = std::fs::read(&config).unwrap();
    let result = store.set_provider_fast_mode(&other.id, true).unwrap();
    assert!(!result.applied);
    assert_eq!(std::fs::read(&config).unwrap(), before);
    assert_eq!(
        store
            .status(Target::Codex)
            .unwrap()
            .active_provider_id
            .as_deref(),
        Some(active.id.as_str())
    );
    store.apply(Target::Codex, &other.id).unwrap();
    let doc: toml_edit::DocumentMut = std::fs::read_to_string(config).unwrap().parse().unwrap();
    assert_eq!(doc["service_tier"].as_str(), Some("priority"));
}

#[test]
fn fast_toggle_does_not_apply_pending_connection_or_model_edits() {
    let (temp, mut store) = fixture();
    let p = store
        .commit_provider(input(Family::Codex, "Original"), Target::Codex, true)
        .unwrap();
    let mut edited = input(Family::Codex, "Edited");
    edited.id = Some(p.id.clone());
    edited.model = "pending-model".into();
    edited.api_key = Some("pending-key".into());
    store.save(edited).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    store.set_provider_fast_mode(&p.id, true).unwrap();
    let doc: toml_edit::DocumentMut =
        std::fs::read_to_string(temp.path().join("codex/config.toml"))
            .unwrap()
            .parse()
            .unwrap();
    assert_eq!(doc["service_tier"].as_str(), Some("priority"));
    assert_eq!(doc["model"].as_str(), Some("test-model"));
    assert_eq!(
        doc["model_providers"]["uni_switch"]["experimental_bearer_token"].as_str(),
        Some("test-key-1234")
    );
    assert_eq!(
        store.provider(&p.id).unwrap().summary.model,
        "pending-model"
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
}

#[test]
fn fast_toggle_rejects_external_changes_without_partial_save() {
    for corrupt_catalog in [false, true] {
        let (temp, mut store) = fixture();
        let mut value = input(Family::Codex, "Protected");
        value.codex_options.models = vec![ProviderModel {
            id: "test-model".into(),
            context_window: Some(256_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        }];
        let p = store.commit_provider(value, Target::Codex, true).unwrap();
        let config = temp.path().join("codex/config.toml");
        let catalog = temp.path().join("codex/uni-switch-models.json");
        if corrupt_catalog {
            let mut changed = read_json(&catalog);
            changed["models"][0]["context_window"] = json!(512_000);
            put(&catalog, &changed.to_string());
        } else {
            let changed = std::fs::read_to_string(&config)
                .unwrap()
                .replace("gateway.example.test", "external.example.test");
            put(&config, &changed);
        }
        let before = (
            std::fs::read(&config).unwrap(),
            std::fs::read(&catalog).unwrap(),
        );
        assert_eq!(
            store.set_provider_fast_mode(&p.id, true).unwrap_err().code,
            "external_change"
        );
        assert_eq!(
            (
                std::fs::read(&config).unwrap(),
                std::fs::read(&catalog).unwrap()
            ),
            before
        );
        assert_eq!(
            store
                .provider(&p.id)
                .unwrap()
                .summary
                .codex_options
                .fast_mode,
            None
        );
    }
}

#[test]
fn fast_toggle_rejects_anthropic_upstream_and_supports_shared_openai_provider() {
    let (temp, mut store) = fixture();
    let native = store.save(input(Family::Claude, "Native Claude")).unwrap();
    assert_eq!(
        store
            .set_provider_fast_mode(&native.id, true)
            .unwrap_err()
            .code,
        "anthropic_fast"
    );
    assert_eq!(
        store
            .provider(&native.id)
            .unwrap()
            .summary
            .codex_options
            .fast_mode,
        None
    );
    let mut value = input(Family::Claude, "Shared GPT");
    value.model = "gpt-5.5".into();
    value.codex_options.upstream_protocol = Some(CodexProtocol::Openai);
    value.codex_options.claude_protocol = ClaudeProtocol::Openai;
    let shared = store.save(value).unwrap();
    store.apply(Target::Codex, &shared.id).unwrap();
    let config = temp.path().join("codex/config.toml");
    let original = std::fs::read_to_string(&config).unwrap();
    put(
        &config,
        &format!("model_reasoning_effort='max'\n{original}"),
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert!(
        store
            .set_provider_fast_mode(&shared.id, true)
            .unwrap()
            .applied
    );
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"][0]["slug"], "gpt-5.5");
    assert_eq!(
        catalog["models"][0]["additional_speed_tiers"],
        json!(["fast"])
    );
    let doc: toml_edit::DocumentMut = std::fs::read_to_string(config).unwrap().parse().unwrap();
    assert_eq!(doc["model_reasoning_effort"].as_str(), Some("max"));
}

#[test]
fn fast_toggle_keeps_shared_claude_snapshots_current_without_writing_them() {
    let (temp, mut store) = fixture();
    let (route, _listener) = crate::bridge::Route::start(&temp.path().join("bridge")).unwrap();
    store.set_bridge_route(route);
    let p = store.save(input(Family::Codex, "Shared")).unwrap();
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        store.apply(target, &p.id).unwrap();
    }
    let before: Vec<_> = [Target::ClaudeDesktop, Target::ClaudeCli]
        .into_iter()
        .flat_map(|target| store.target_record(target).unwrap().2.unwrap())
        .map(|file| {
            let content = std::fs::read(&file.path).unwrap();
            (file.path, content)
        })
        .collect();
    for enabled in [true, false] {
        store.set_provider_fast_mode(&p.id, enabled).unwrap();
        for (path, bytes) in &before {
            assert_eq!(&std::fs::read(path).unwrap(), bytes);
        }
        for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
            assert_eq!(store.status(target).unwrap().state, "applied");
        }
    }
    store.restore(Target::Codex).unwrap();
    assert!(!store.set_provider_fast_mode(&p.id, true).unwrap().applied);
    for (path, bytes) in &before {
        assert_eq!(&std::fs::read(path).unwrap(), bytes);
    }
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        assert_eq!(store.status(target).unwrap().state, "applied");
    }
    let mut pending = input(Family::Codex, "Pending connection");
    pending.id = Some(p.id.clone());
    pending.api_key = Some("pending-key".into());
    store.save(pending).unwrap();
    store.set_provider_fast_mode(&p.id, false).unwrap();
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        assert_eq!(store.status(target).unwrap().state, "saved_changes");
    }
}

#[test]
fn configuration_revision_changes_only_after_real_client_writes_and_restore() {
    let (temp, mut store) = fixture();
    assert_eq!(store.runtime_context(Target::Codex).unwrap().1, 0);
    let p = store.save(input(Family::Codex, "Saved only")).unwrap();
    assert_eq!(store.runtime_context(Target::Codex).unwrap().1, 0);
    store.apply(Target::Codex, &p.id).unwrap();
    let first = store.runtime_context(Target::Codex).unwrap().1;
    assert!(first > 0);
    store.apply(Target::Codex, &p.id).unwrap();
    assert_eq!(store.runtime_context(Target::Codex).unwrap().1, first);
    store.set_provider_fast_mode(&p.id, true).unwrap();
    let fast = store.runtime_context(Target::Codex).unwrap().1;
    assert!(fast > first);
    store.set_provider_fast_mode(&p.id, true).unwrap();
    assert_eq!(store.runtime_context(Target::Codex).unwrap().1, fast);
    store.restore(Target::Codex).unwrap();
    assert!(store.runtime_context(Target::Codex).unwrap().1 > fast);
    assert_eq!(store.status(Target::Codex).unwrap().state, "unmanaged");
    store
        .set_directory(
            Target::Codex,
            temp.path()
                .join("another-codex-directory")
                .to_string_lossy()
                .into(),
        )
        .unwrap();
    assert_eq!(
        store.runtime_context(Target::Codex).unwrap().1,
        0,
        "old directory writes must not require restarting a different directory"
    );
}

#[test]
fn configured_directory_ignores_nested_config_copies() {
    let (temp, mut store) = fixture();
    let current = temp.path().join("codex");
    let duplicate = current.join("browser/config.toml");
    put(&duplicate, "model='copy-model'\n");
    let provider = store
        .commit_provider(
            input(Family::Codex, "Current directory"),
            Target::Codex,
            true,
        )
        .unwrap();
    assert_eq!(
        PathBuf::from(store.status(Target::Codex).unwrap().directory),
        current
    );
    assert_eq!(
        std::fs::read_to_string(&duplicate).unwrap(),
        "model='copy-model'\n"
    );
    store.apply(Target::Codex, &provider.id).unwrap();
    let data = store.data_directory.clone();
    drop(store);
    let reopened = Store::open(data).unwrap();
    assert_eq!(
        PathBuf::from(reopened.status(Target::Codex).unwrap().directory),
        current
    );
    assert_eq!(reopened.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        std::fs::read_to_string(duplicate).unwrap(),
        "model='copy-model'\n"
    );
}

#[test]
fn duplicate_connection_reuses_record_without_overwriting_model_choices() {
    let (_temp, mut store) = fixture();
    let first = store
        .commit_provider(input(Family::Codex, "First"), Target::Codex, true)
        .unwrap();
    let duplicate = input(Family::Codex, "Duplicate");
    let (reused, matched) = store
        .commit_unique_provider(duplicate, Target::Codex, true)
        .unwrap();
    assert!(matched);
    assert_eq!(reused.id, first.id);
    assert_eq!(reused.model, first.model);
    assert_eq!(store.list().unwrap().len(), 1);
    let mut model_variant = input(Family::Codex, "Different model settings");
    model_variant.model = "other-model".into();
    assert!(
        !store
            .commit_unique_provider(model_variant, Target::Codex, false)
            .unwrap()
            .1
    );
    let mut different = input(Family::Codex, "Another key");
    different.api_key = Some("another-private-key".into());
    assert!(
        !store
            .commit_unique_provider(different, Target::Codex, false)
            .unwrap()
            .1
    );
    assert_eq!(store.list().unwrap().len(), 3);
}

#[test]
fn syncing_connection_preserves_target_model_and_survives_restart() {
    let (temp, mut store) = fixture();
    let first = store
        .commit_provider(input(Family::Codex, "Original"), Target::Codex, true)
        .unwrap();
    let mut edit = input(Family::Codex, "Edited");
    edit.id = Some(first.id.clone());
    edit.api_key = Some("changed-key-1234".into());
    edit.model = "new-shared-model".into();
    edit.codex_options.fast_mode = Some(true);
    store.save(edit).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    store.sync_connection(Target::Codex, &first.id).unwrap();
    let doc: toml_edit::DocumentMut =
        std::fs::read_to_string(temp.path().join("codex/config.toml"))
            .unwrap()
            .parse()
            .unwrap();
    assert_eq!(doc["model"].as_str(), Some("test-model"));
    assert_eq!(
        doc["model_providers"]["uni_switch"]["experimental_bearer_token"].as_str(),
        Some("changed-key-1234")
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    let data = store.data_directory.clone();
    drop(store);
    let reopened = Store::open(data).unwrap();
    assert_eq!(reopened.status(Target::Codex).unwrap().state, "applied");
}

#[test]
fn syncing_upgraded_direct_configuration_recovers_model_snapshot_from_client() {
    let (temp, mut store) = fixture();
    let first = store
        .commit_provider(input(Family::Codex, "Legacy"), Target::Codex, true)
        .unwrap();
    store
        .conn
        .execute(
            "UPDATE targets SET applied_summary=NULL,accepted_source=NULL WHERE id='codex'",
            [],
        )
        .unwrap();
    let mut updated = input(Family::Codex, "Updated");
    updated.id = Some(first.id.clone());
    updated.model = "different-new-model".into();
    updated.api_key = Some("new-key".into());
    store.save(updated).unwrap();
    store.sync_connection(Target::Codex, &first.id).unwrap();
    let doc: toml_edit::DocumentMut =
        std::fs::read_to_string(temp.path().join("codex/config.toml"))
            .unwrap()
            .parse()
            .unwrap();
    assert_eq!(doc["model"].as_str(), Some("test-model"));
    assert_eq!(
        store
            .status(Target::Codex)
            .unwrap()
            .applied_model
            .as_deref(),
        Some("test-model")
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
}

#[test]
fn confirm_failure_keeps_supplier_and_client_unchanged_and_retry_inserts_once() {
    let (temp, mut store) = fixture();
    let original = store
        .commit_provider(input(Family::Codex, "Original"), Target::Codex, true)
        .unwrap();
    let config = temp.path().join("codex/config.toml");
    let before = std::fs::read_to_string(&config).unwrap();
    let changed = before.replace("gateway.example.test", "outside.example.test");
    put(&config, &changed);
    let mut edit = input(Family::Codex, "Unconfirmed");
    edit.id = Some(original.id.clone());
    edit.api_key = Some("new-unconfirmed-key".into());
    assert_eq!(
        store
            .commit_provider(edit, Target::Codex, true)
            .unwrap_err()
            .code,
        "external_change"
    );
    assert_eq!(
        json(&store.provider(&original.id).unwrap().summary).unwrap(),
        json(&original).unwrap()
    );
    assert_eq!(
        store.provider(&original.id).unwrap().api_key,
        "test-key-1234"
    );
    assert_eq!(std::fs::read_to_string(&config).unwrap(), changed);
    assert_eq!(
        store
            .commit_provider(input(Family::Codex, "New"), Target::Codex, true)
            .unwrap_err()
            .code,
        "external_change"
    );
    assert_eq!(store.list().unwrap().len(), 1);
    put(&config, &before);
    let added = store
        .commit_provider(input(Family::Codex, "New"), Target::Codex, true)
        .unwrap();
    assert_eq!(store.list().unwrap().len(), 2);
    assert_eq!(
        store.status(Target::Codex).unwrap().active_provider_id,
        Some(added.id)
    );
}

#[test]
fn one_shared_supplier_has_independent_client_snapshots_and_pending_edits() {
    let (temp, mut store) = fixture();
    store.set_bridge_route(crate::bridge::Route {
        port: 19880,
        token: "c".repeat(64),
    });
    let mut value = input(Family::Codex, "Shared GPT");
    value.model = "gpt-5.4".into();
    value.codex_options.upstream_protocol = Some(CodexProtocol::Openai);
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        context_window: Some(256000),
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let p = store
        .commit_provider(value.clone(), Target::Codex, true)
        .unwrap();
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        assert_eq!(store.apply(target, &p.id).unwrap().state, "applied");
        let snapshot = store.claude_bridge_provider(target, &p.id).unwrap();
        assert_eq!(snapshot.api_key, "test-key-1234");
        assert_eq!(snapshot.summary.model, "gpt-5.4");
    }
    assert_eq!(store.list().unwrap().len(), 1);
    value.id = Some(p.id.clone());
    value.api_key = Some("changed-shared-key".into());
    value.name = "Edited from Claude".into();
    store
        .commit_provider(value, Target::ClaudeCli, true)
        .unwrap();
    assert_eq!(store.status(Target::ClaudeCli).unwrap().state, "applied");
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    assert_eq!(
        store.status(Target::ClaudeDesktop).unwrap().state,
        "saved_changes"
    );
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeDesktop, &p.id)
            .unwrap()
            .api_key,
        "test-key-1234"
    );
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeCli, &p.id)
            .unwrap()
            .api_key,
        "changed-shared-key"
    );
    assert!(!read_json(&temp.path().join("claude_cli/settings.json"))
        .to_string()
        .contains("changed-shared-key"));
    assert!(store.runtime_context(Target::Codex).unwrap().1 > 0);
    store.restore(Target::ClaudeCli).unwrap();
    assert!(store.runtime_context(Target::ClaudeCli).unwrap().1 > 0);
    assert_eq!(
        store.status(Target::Codex).unwrap().active_provider_id,
        Some(p.id)
    );
}

#[test]
fn legacy_claude_supplier_reuses_native_targets_and_creates_codex_catalog() {
    let (temp, mut store) = fixture();
    store.set_bridge_route(crate::bridge::Route {
        port: 19881,
        token: "d".repeat(64),
    });
    let p = store.save(input(Family::Claude, "Legacy Claude")).unwrap();
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        assert_eq!(store.apply(target, &p.id).unwrap().state, "applied");
    }
    assert_eq!(store.list().unwrap().len(), 1);
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"][0]["slug"], "claude-sonnet-4-6");
    assert_eq!(catalog["models"][0]["context_window"], 256000);
    assert_eq!(
        store
            .bridge_provider(&p.id)
            .unwrap()
            .summary
            .upstream_protocol(),
        CodexProtocol::Anthropic
    );
    assert_eq!(
        read_json(&temp.path().join("claude_cli/settings.json"))["env"]["ANTHROPIC_AUTH_TOKEN"],
        "test-key-1234"
    );
}

#[test]
fn completed_confirm_journal_recovers_a_new_provider_only_after_all_files_exist() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Recovered new supplier");
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        context_window: Some(256000),
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let stored = store.prepare_provider(value).unwrap();
    let files = store
        .plan(Target::Codex, &temp.path().join("codex"), &stored)
        .unwrap();
    let pending = Pending {
        target: Target::Codex,
        changes: files
            .iter()
            .map(|f| Change {
                path: f.path.clone(),
                before: f.original.clone(),
                after: f.expected.clone(),
            })
            .collect(),
        baseline: Some(files),
        active: Some(stored.summary.id.clone()),
        provider_update: Some(stored.clone()),
        applied_summary: None,
        accepted_source: None,
    };
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('new-confirm','pending',?1,0)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    writer::write(
        &pending.changes[0].path,
        pending.changes[0].after.as_deref(),
    )
    .unwrap();
    store.recover().unwrap();
    assert!(store.list().unwrap().is_empty());
    assert!(!pending.changes[0].path.exists());
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('completed-confirm','pending',?1,0)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    for change in &pending.changes {
        writer::write(&change.path, change.after.as_deref()).unwrap();
    }
    store.recover().unwrap();
    assert_eq!(store.list().unwrap().len(), 1);
    assert_eq!(
        store.provider(&stored.summary.id).unwrap().api_key,
        stored.api_key
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
}

#[test]
fn codex_x_api_key_configuration_preserves_shared_authentication() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Header gateway");
    value.auth_mode = "x-api-key".into();
    value.codex_options.upstream_protocol = Some(CodexProtocol::Openai);
    let p = store.commit_provider(value, Target::Codex, true).unwrap();
    let config = std::fs::read_to_string(temp.path().join("codex/config.toml")).unwrap();
    assert!(config.contains("x-api-key"));
    assert!(!config.contains("experimental_bearer_token"));
    let doc: toml_edit::DocumentMut = config.parse().unwrap();
    let id = doc["model_provider"].as_str().unwrap();
    assert_eq!(
        doc["model_providers"][id]["http_headers"]["x-api-key"].as_str(),
        Some(store.provider(&p.id).unwrap().api_key.as_str())
    );
    assert_eq!(
        store.provider(&p.id).unwrap().summary.auth_mode,
        "x-api-key"
    );
    assert_eq!(store.list().unwrap().len(), 1);
}

#[test]
fn directory_binding_persists_and_rejects_stale_or_managed_changes() {
    let temp = tempfile::tempdir().unwrap();
    let data = temp.path().join("data");
    let mut store = Store::open(data.clone()).unwrap();
    let target = Target::Codex;
    let (initial, chosen) = store.directory_discovery_context(target).unwrap();
    assert!(!chosen);
    let new = temp
        .path()
        .join("Codex 中文")
        .to_string_lossy()
        .into_owned();
    store
        .select_detected_directory(target, new.clone(), &initial.to_string_lossy(), true)
        .unwrap();
    assert!(!store.directory_discovery_context(target).unwrap().1);
    assert!(
        !Path::new(&new).exists(),
        "binding cannot create client files"
    );
    assert_eq!(
        store
            .select_detected_directory(target, new.clone(), &initial.to_string_lossy(), false)
            .unwrap_err()
            .code,
        "directory_changed"
    );
    store.set_directory(target, new.clone()).unwrap();
    drop(store);
    let mut store = Store::open(data).unwrap();
    assert!(store.directory_discovery_context(target).unwrap().1);
    assert_eq!(
        store
            .select_detected_directory(target, new.clone(), &new, true)
            .unwrap_err()
            .code,
        "directory_selected"
    );
    let provider = store.save(input(Family::Codex, "test")).unwrap();
    store.apply(target, &provider.id).unwrap();
    assert_eq!(
        store
            .select_detected_directory(
                target,
                temp.path().join("other").to_string_lossy().into_owned(),
                &new,
                false
            )
            .unwrap_err()
            .code,
        "managed_directory"
    );
}

#[test]
fn old_database_custom_directories_migrate_as_manually_selected() {
    let temp = tempfile::tempdir().unwrap();
    let data = temp.path().join("data");
    std::fs::create_dir_all(&data).unwrap();
    let conn = Connection::open(data.join("uni-switch.db")).unwrap();
    conn.execute_batch("CREATE TABLE targets (id TEXT PRIMARY KEY, directory TEXT NOT NULL, active TEXT, baseline TEXT);").unwrap();
    let custom = temp.path().join("custom");
    conn.execute(
        "INSERT INTO targets(id,directory) VALUES('codex',?1)",
        [custom.to_string_lossy().as_ref()],
    )
    .unwrap();
    drop(conn);
    let store = Store::open(data).unwrap();
    let (path, selected) = store.directory_discovery_context(Target::Codex).unwrap();
    assert!(crate::discovery::same_directory(&path, &custom));
    assert!(selected);
    assert!(
        !store
            .directory_discovery_context(Target::ClaudeCli)
            .unwrap()
            .1
    );
}

#[test]
fn bridge_routes_use_private_applied_snapshot_and_restore_transactionally() {
    let (temp, mut store) = fixture();
    let route = crate::bridge::Route {
        port: 19871,
        token: "a".repeat(64),
    };
    store.set_bridge_route(route.clone());
    let mut value = input(Family::Codex, "Claude in Codex");
    value.codex_options.protocol = CodexProtocol::Anthropic;
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        context_window: Some(256_000),
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    value.auth_mode = "x-api-key".into();
    let provider = store.save(value.clone()).unwrap();
    let status = store.apply(Target::Codex, &provider.id).unwrap();
    assert_eq!(status.state, "applied");
    let config = std::fs::read_to_string(temp.path().join("codex/config.toml")).unwrap();
    assert!(config.contains(&route.base_url(&provider.id)));
    assert!(config.contains("web_search = \"disabled\""));
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"][0]["apply_patch_tool_type"], "freeform");
    assert!(!config.contains("test-key-1234"));
    assert_eq!(
        store.bridge_provider(&provider.id).unwrap().api_key,
        "test-key-1234"
    );
    assert!(store.bridge_required());
    assert_eq!(
        store
            .bridge_provider(&provider.id)
            .unwrap()
            .summary
            .base_url,
        "https://gateway.example.test/v1"
    );
    store.repair_reasoning_levels(&provider.id).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert!(
        store
            .bridge_provider(&provider.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    value.id = Some(provider.id.clone());
    value.api_key = Some("new-private-key".into());
    store.save(value).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    assert_eq!(
        store.bridge_provider(&provider.id).unwrap().api_key,
        "test-key-1234",
        "unapplied key changes cannot affect active requests"
    );
    store.repair_reasoning_levels(&provider.id).unwrap();
    assert_eq!(
        store.bridge_provider(&provider.id).unwrap().api_key,
        "test-key-1234",
        "repair cannot activate a pending credential edit"
    );
    store.apply(Target::Codex, &provider.id).unwrap();
    assert_eq!(
        store.bridge_provider(&provider.id).unwrap().api_key,
        "new-private-key"
    );
    let direct = store.save(input(Family::Codex, "direct")).unwrap();
    store.apply(Target::Codex, &direct.id).unwrap();
    assert!(
        !std::fs::read_to_string(temp.path().join("codex/config.toml"))
            .unwrap()
            .contains("web_search")
    );
    assert!(!store.bridge_required());
    assert!(store.bridge_provider(&provider.id).is_err());
    store.apply(Target::Codex, &provider.id).unwrap();
    store.restore(Target::Codex).unwrap();
    assert!(!store.data_directory.join("bridge-active.json").exists());
    assert!(!store.bridge_required());
    assert!(!temp.path().join("codex/config.toml").exists());
}

#[test]
fn reverse_bridges_have_independent_snapshots_aliases_and_restore_cli_environment() {
    let (temp, mut store) = fixture();
    let route = crate::bridge::Route {
        port: 19872,
        token: "b".repeat(64),
    };
    store.set_bridge_route(route.clone());
    let cli = temp.path().join("claude_cli/settings.json");
    put(
        &cli,
        "{\"env\":{\"ENABLE_TOOL_SEARCH\":\"auto:10\",\"CUSTOM\":\"preserve\"},\"hooks\":{}}",
    );
    let mut value = input(Family::Claude, "GPT in Claude");
    value.model = "gpt-5.4".into();
    value.codex_options.claude_protocol = ClaudeProtocol::Openai;
    value.codex_options.models = ["gpt-5.4", "gpt-4.1"]
        .iter()
        .map(|id| ProviderModel {
            id: (*id).into(),
            context_window: Some(256000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        })
        .collect();
    let first = store.save(value.clone()).unwrap();
    assert_eq!(
        store.apply(Target::ClaudeDesktop, &first.id).unwrap().state,
        "applied"
    );
    assert_eq!(
        store.apply(Target::ClaudeCli, &first.id).unwrap().state,
        "applied"
    );
    let settings = read_json(&cli);
    assert_eq!(settings["env"]["ANTHROPIC_MODEL"], "gpt-5.4");
    assert_eq!(settings["env"]["ENABLE_TOOL_SEARCH"], "false");
    assert!(!settings.to_string().contains("test-key-1234"));
    let desktop_dir = temp.path().join("claude_desktop");
    let profile = adapters::plan(
        Target::ClaudeDesktop,
        &desktop_dir,
        &store.provider(&first.id).unwrap(),
    );
    assert!(profile.is_err(), "Direct adapter rejects unaliased GPT IDs");
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeDesktop, &first.id)
            .unwrap()
            .summary
            .model,
        "gpt-5.4"
    );
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeCli, &first.id)
            .unwrap()
            .summary
            .base_url,
        "https://gateway.example.test/v1"
    );
    value.id = None;
    value.model = "gpt-4.1".into();
    value.api_key = Some("second-upstream-key".into());
    let second = store.save(value).unwrap();
    assert_eq!(
        store.apply(Target::ClaudeCli, &second.id).unwrap().state,
        "applied"
    );
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeDesktop, &first.id)
            .unwrap()
            .api_key,
        "test-key-1234"
    );
    assert_eq!(
        store
            .claude_bridge_provider(Target::ClaudeCli, &second.id)
            .unwrap()
            .api_key,
        "second-upstream-key"
    );
    assert!(store
        .claude_bridge_provider(Target::ClaudeCli, &first.id)
        .is_err());
    let native = store.save(input(Family::Claude, "native")).unwrap();
    store.apply(Target::ClaudeCli, &native.id).unwrap();
    assert_eq!(read_json(&cli)["env"]["ENABLE_TOOL_SEARCH"], "auto:10");
    assert!(store.bridge_required(), "Desktop route remains active");
    store.restore(Target::ClaudeDesktop).unwrap();
    assert!(!store.bridge_required());
    store.restore(Target::ClaudeCli).unwrap();
    assert_eq!(read_json(&cli)["env"]["ENABLE_TOOL_SEARCH"], "auto:10");
    assert_eq!(read_json(&cli)["env"]["CUSTOM"], "preserve");
}

#[test]
fn reasoning_repair_preserves_client_choices_and_survives_sync_and_restore() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("codex");
    let config = dir.join("config.toml");
    let original = "# original\nmodel = \"original-model\"\nmodel_reasoning_effort = \"medium\"\n";
    put(&config, original);
    let mut value = input(Family::Codex, "修复测试");
    value.codex_options.models = ["test-model", "other-model"]
        .iter()
        .map(|id| ProviderModel {
            id: (*id).into(),
            context_window: Some(128000),
            reasoning_efforts: vec!["low".into()],
            enabled: true,
            ..Default::default()
        })
        .collect();
    let p = store.save(value.clone()).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let text = std::fs::read_to_string(&config)
        .unwrap()
        .replace("model = \"test-model\"", "model = \"other-model\"");
    let mut edited = text.parse::<toml_edit::DocumentMut>().unwrap();
    edited["model_reasoning_effort"] = toml_edit::value("max");
    edited["service_tier"] = toml_edit::value("priority");
    let text = format!("{edited}\n[extra]\nkeep = true\n");
    put(&config, &text);
    let result = store.repair_reasoning_levels(&p.id).unwrap();
    assert!(result.applied);
    let after = std::fs::read_to_string(&config).unwrap();
    assert!(after.starts_with(&text));
    let repaired_config = after.parse::<toml_edit::DocumentMut>().unwrap();
    assert!(repaired_config["desktop"]["enabled-reasoning-efforts"]
        .as_array()
        .unwrap()
        .iter()
        .any(|v| v.as_str() == Some("max")));
    let catalog_path = dir.join("uni-switch-models.json");
    let repaired = read_json(&catalog_path);
    for model in repaired["models"].as_array().unwrap() {
        assert_eq!(model["default_reasoning_level"], "low");
        assert_eq!(model["context_window"], 128000);
        assert_eq!(
            model["supported_reasoning_levels"]
                .as_array()
                .unwrap()
                .len(),
            8
        );
        assert!(model["supported_reasoning_levels"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["effort"] == "max"));
    }
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    store.repair_reasoning_levels(&p.id).unwrap();
    assert_eq!(read_json(&catalog_path), repaired);
    value.id = Some(p.id.clone());
    value.api_key = None;
    value.codex_options.models[0].reasoning_efforts.clear();
    store.save(value).unwrap();
    assert!(
        store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    store.apply(Target::Codex, &p.id).unwrap();
    assert!(
        read_json(&catalog_path)["models"][0]["supported_reasoning_levels"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["effort"] == "max")
    );
    store.restore(Target::Codex).unwrap();
    let restored = std::fs::read_to_string(&config).unwrap();
    assert!(restored.contains("model_reasoning_effort = \"medium\""));
    assert!(restored.contains("keep = true"));
    assert!(!catalog_path.exists());
}

#[test]
fn reasoning_repair_inactive_does_not_switch_provider_and_legacy_catalog_is_created() {
    let (temp, mut store) = fixture();
    let active = store.save(input(Family::Codex, "正在使用")).unwrap();
    store.apply(Target::Codex, &active.id).unwrap();
    let other = store.save(input(Family::Codex, "其他配置")).unwrap();
    let config = temp.path().join("codex/config.toml");
    let text = std::fs::read_to_string(&config).unwrap();
    assert!(!store.repair_reasoning_levels(&other.id).unwrap().applied);
    assert_eq!(std::fs::read_to_string(&config).unwrap(), text);
    assert_eq!(
        store.status(Target::Codex).unwrap().active_provider_id,
        Some(active.id.clone())
    );
    assert!(store.repair_reasoning_levels(&active.id).unwrap().applied);
    let repaired = std::fs::read_to_string(&config).unwrap();
    assert!(repaired.contains("model_catalog_json"));
    assert!(repaired.contains("model = \"test-model\""));
    assert!(
        read_json(&temp.path().join("codex/uni-switch-models.json"))["models"][0]
            ["supported_reasoning_levels"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["effort"] == "max")
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    store.restore(Target::Codex).unwrap();
    assert!(!config.exists());
}

#[test]
fn reasoning_repair_rejects_external_changes_without_persisting_success() {
    let (temp, mut store) = fixture();
    let p = store.save(input(Family::Codex, "外部修改")).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let config = temp.path().join("codex/config.toml");
    let text = std::fs::read_to_string(&config)
        .unwrap()
        .replace("gateway.example.test", "changed.example.test");
    put(&config, &text);
    assert_eq!(
        store.repair_reasoning_levels(&p.id).unwrap_err().code,
        "external_change"
    );
    assert_eq!(std::fs::read_to_string(&config).unwrap(), text);
    assert!(
        !store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    let clean = text.replace("changed.example.test", "gateway.example.test");
    let profile = format!(
        "profile = \"override\"\n{clean}\n[profiles.override]\nmodel = \"override-model\"\n"
    );
    put(&config, &profile);
    assert_eq!(
        store.repair_reasoning_levels(&p.id).unwrap_err().code,
        "profile_override"
    );
    assert_eq!(std::fs::read_to_string(&config).unwrap(), profile);
    assert!(
        !store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    let claude = store.save(input(Family::Claude, "Claude")).unwrap();
    assert!(store.repair_reasoning_levels(&claude.id).is_err());
}

#[test]
fn reasoning_catalog_repair_retains_custom_presets_and_default() {
    let mut no_default =
        json!({"models": [{"slug": "no-default", "default_reasoning_level": null}]});
    adapters::repair_reasoning_catalog(&mut no_default).unwrap();
    assert!(no_default["models"][0]["default_reasoning_level"].is_null());
    let mut catalog = json!({"models": [{"slug": "custom", "default_reasoning_level": "persistent", "supported_reasoning_levels": [{"effort": "persistent", "description": "keep"}, {"effort": "max", "description": "Original max"}], "extra": 42}]});
    adapters::repair_reasoning_catalog(&mut catalog).unwrap();
    let once = catalog.clone();
    adapters::repair_reasoning_catalog(&mut catalog).unwrap();
    assert_eq!(catalog, once);
    assert_eq!(
        catalog["models"][0]["default_reasoning_level"],
        "persistent"
    );
    assert_eq!(
        catalog["models"][0]["supported_reasoning_levels"][1]["description"],
        "Original max"
    );
    assert_eq!(catalog["models"][0]["extra"], 42);
}

#[test]
fn interrupted_reasoning_repair_commits_provider_only_with_all_files() {
    let (temp, mut store) = fixture();
    let p = store.save(input(Family::Codex, "修复事务")).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let dir = temp.path().join("codex");
    // Simulate the pre-automatic-repair files so both writes are required.
    let mut legacy = std::fs::read_to_string(dir.join("config.toml"))
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    legacy.as_table_mut().remove("desktop");
    legacy.as_table_mut().remove("model_catalog_json");
    put(&dir.join("config.toml"), &legacy.to_string());
    std::fs::remove_file(dir.join("uni-switch-models.json")).unwrap();
    let mut updated = store.provider(&p.id).unwrap();
    updated.summary.codex_options.repair_reasoning_levels = true;
    updated.summary.codex_options.models = vec![ProviderModel {
        id: "test-model".into(),
        context_window: None,
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let files = adapters::reasoning_repair_files(&dir, &updated).unwrap();
    assert_eq!(files.len(), 2);
    let pending = Pending {
        target: Target::Codex,
        changes: files
            .iter()
            .map(|f| Change {
                path: f.path.clone(),
                before: f.original.clone(),
                after: f.expected.clone(),
            })
            .collect(),
        baseline: Some(files),
        active: Some(p.id.clone()),
        provider_update: Some(updated),
        applied_summary: None,
        accepted_source: None,
    };
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('repair-crash','pending',?1,0)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    writer::write(
        &pending.changes[0].path,
        pending.changes[0].after.as_deref(),
    )
    .unwrap();
    store.recover().unwrap();
    assert!(
        !store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    assert!(!dir.join("uni-switch-models.json").exists());
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('repair-complete','pending',?1,1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    for change in &pending.changes {
        writer::write(&change.path, change.after.as_deref()).unwrap();
    }
    store.recover().unwrap();
    assert!(
        store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
}

#[test]
fn desktop_display_repair_captures_original_preferences_preserves_other_settings_and_restores() {
    let (temp, mut store) = fixture();
    let config = temp.path().join("codex/config.toml");
    put(&config, "model = \"original\"\nmodel_reasoning_effort = \"medium\"\n[desktop]\nenabled-reasoning-efforts = [\"low\", \"medium\", \"high\", \"xhigh\"]\ntheme = \"dark\"\n");
    let mut value = input(Family::Codex, "菜单修复");
    value.codex_options.models = vec![ProviderModel {
        id: "test-model".into(),
        context_window: None,
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let p = store.save(value).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let mut before = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    let mut choices = toml_edit::Array::new();
    choices.push("high");
    before["desktop"]["enabled-reasoning-efforts"] = toml_edit::value(choices);
    before["model_reasoning_effort"] = toml_edit::value("max");
    put(&config, &before.to_string());
    store.repair_reasoning_levels(&p.id).unwrap();
    let after = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    assert_eq!(after["model_reasoning_effort"].as_str(), Some("max"));
    assert_eq!(after["desktop"]["theme"].as_str(), Some("dark"));
    assert!(after["desktop"]["enabled-reasoning-efforts"]
        .as_array()
        .unwrap()
        .iter()
        .any(|v| v.as_str() == Some("max")));
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    // Codex may hide max again through its UI. The repair must remain usable.
    let mut hidden = after;
    let mut array = toml_edit::Array::new();
    array.push("high");
    hidden["desktop"]["enabled-reasoning-efforts"] = toml_edit::value(array);
    put(&config, &hidden.to_string());
    store.repair_reasoning_levels(&p.id).unwrap();
    let other = store.save(input(Family::Codex, "另一供应商")).unwrap();
    store.apply(Target::Codex, &other.id).unwrap();
    store.restore(Target::Codex).unwrap();
    let restored = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    assert_eq!(restored["model"].as_str(), Some("original"));
    assert_eq!(restored["model_reasoning_effort"].as_str(), Some("medium"));
    assert_eq!(restored["desktop"]["theme"].as_str(), Some("dark"));
    let restored_efforts = restored["desktop"]["enabled-reasoning-efforts"]
        .as_array()
        .unwrap();
    assert_eq!(restored_efforts.len(), 4);
    assert_eq!(
        restored_efforts.get(0).and_then(toml_edit::Value::as_str),
        Some("low")
    );
}

#[test]
fn malformed_desktop_display_preferences_prevent_all_repair_writes() {
    let (temp, mut store) = fixture();
    let p = store.save(input(Family::Codex, "异常设置")).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let config = temp.path().join("codex/config.toml");
    let catalog_before = std::fs::read(temp.path().join("codex/uni-switch-models.json")).unwrap();
    let mut doc = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    doc["desktop"]["enabled-reasoning-efforts"] = toml_edit::value(false);
    let before = doc.to_string();
    put(&config, &before);
    assert_eq!(
        store.repair_reasoning_levels(&p.id).unwrap_err().code,
        "invalid_reasoning_display"
    );
    assert_eq!(std::fs::read_to_string(&config).unwrap(), before);
    assert_eq!(
        std::fs::read(temp.path().join("codex/uni-switch-models.json")).unwrap(),
        catalog_before
    );
    assert!(
        !store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .repair_reasoning_levels
    );
}

#[test]
fn codex_model_refresh_writes_per_model_context_without_changing_client_choices_and_restores() {
    let (temp, mut store) = fixture();
    let config = temp.path().join("codex/config.toml");
    put(&config, "model = \"original\"\nmodel_context_window = 64000\nmodel_auto_compact_token_limit = 50000\n");
    let mut value = input(Family::Codex, "自动模型");
    value.codex_options.models = vec![
        ProviderModel {
            id: "test-model".into(),
            context_window: Some(256_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        },
        ProviderModel {
            id: "second".into(),
            context_window: Some(512_000),
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        },
    ];
    let p = store.save(value).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let original_applied = std::fs::read_to_string(&config).unwrap();
    put(
        &config,
        &original_applied.replace("model = \"test-model\"", "model = \"second\""),
    );
    put(
        &config,
        &format!(
            "model_reasoning_effort = \"max\"\nservice_tier = \"flex\"\n{}\n[other]\nkeep = true\n",
            std::fs::read_to_string(&config).unwrap()
        ),
    );
    let mut models = store.provider(&p.id).unwrap().summary.codex_options.models;
    models[0].context_window = Some(128_000);
    models.push(ProviderModel {
        id: "disabled".into(),
        context_window: Some(1_000_000),
        reasoning_efforts: vec![],
        enabled: false,
        ..Default::default()
    });
    let result = store
        .update_provider_models(ModelWriteInput {
            connection: ConnectionInput {
                provider_id: Some(p.id.clone()),
                base_url: p.base_url.clone(),
                api_key: None,
                balance_access_token: None,
            },
            auth_mode: "bearer".into(),
            model: p.model.clone(),
            models,
            synced_at: 42,
        })
        .unwrap();
    assert!(result.applied);
    let after = std::fs::read_to_string(&config)
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    assert_eq!(after["model"].as_str(), Some("second"));
    assert_eq!(after["model_reasoning_effort"].as_str(), Some("max"));
    assert_eq!(after["service_tier"].as_str(), Some("flex"));
    assert!(after.get("model_context_window").is_none());
    assert!(after.get("model_auto_compact_token_limit").is_none());
    let catalog = read_json(&temp.path().join("codex/uni-switch-models.json"));
    assert_eq!(catalog["models"].as_array().unwrap().len(), 2);
    assert_eq!(catalog["models"][0]["context_window"], 128_000);
    assert_eq!(catalog["models"][0]["auto_compact_token_limit"], 128_000);
    assert_eq!(catalog["models"][1]["context_window"], 512_000);
    assert_eq!(catalog["models"][1]["auto_compact_token_limit"], 512_000);
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_eq!(
        store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .models_synced_at,
        Some(42)
    );
    store.restore(Target::Codex).unwrap();
    let restored = std::fs::read_to_string(config).unwrap();
    assert!(restored.contains("model_context_window = 64000"));
    assert!(restored.contains("model_auto_compact_token_limit = 50000"));
    assert!(restored.contains("keep = true"));
}

#[test]
fn automatic_model_write_rejects_other_connections_invalid_values_and_external_files() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "保护写入");
    value.codex_options.models = vec![ProviderModel {
        id: "test-model".into(),
        context_window: Some(256_000),
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let p = store.save(value.clone()).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let config = temp.path().join("codex/config.toml");
    let before = std::fs::read_to_string(&config).unwrap();
    let request = || ModelWriteInput {
        connection: ConnectionInput {
            provider_id: Some(p.id.clone()),
            base_url: p.base_url.clone(),
            api_key: None,
            balance_access_token: None,
        },
        auth_mode: "bearer".into(),
        model: p.model.clone(),
        models: p.codex_options.models.clone(),
        synced_at: 42,
    };
    let mut changed = request();
    changed.connection.api_key = Some("another-key".into());
    assert_eq!(
        store.update_provider_models(changed).unwrap_err().code,
        "connection_changed"
    );
    let mut invalid = request();
    invalid.models[0].context_window = Some(0);
    assert_eq!(
        store.update_provider_models(invalid).unwrap_err().code,
        "invalid_models"
    );
    assert_eq!(std::fs::read_to_string(&config).unwrap(), before);
    let catalog = temp.path().join("codex/uni-switch-models.json");
    let external = std::fs::read_to_string(&catalog)
        .unwrap()
        .replace("供应商模型", "外部模型");
    put(&catalog, &external);
    assert_eq!(
        store.update_provider_models(request()).unwrap_err().code,
        "configuration_changed"
    );
    assert_eq!(std::fs::read_to_string(&catalog).unwrap(), external);
    assert_ne!(
        store
            .provider(&p.id)
            .unwrap()
            .summary
            .codex_options
            .models_synced_at,
        Some(42)
    );
    value.name = "未应用供应商".into();
    let other = store.save(value).unwrap();
    let mut inactive = request();
    inactive.connection.provider_id = Some(other.id);
    assert!(!store.update_provider_models(inactive).unwrap().applied);
    assert_eq!(std::fs::read_to_string(config).unwrap(), before);
}

#[test]
fn balance_account_credentials_are_private_retained_and_bound_to_site() {
    let (_temp, mut store) = fixture();
    let mut value = input(Family::Codex, "账户余额");
    value.codex_options.balance_query = Some(BalanceQuery {
        adapter: BalanceAdapter::NewapiAccount,
        user_id: Some("42".into()),
        divisor: 500_000.0,
        ..BalanceQuery::default()
    });
    value.balance_access_token = Some("private-console-token".into());
    let provider = store.save(value.clone()).unwrap();
    assert!(provider.has_balance_token);
    assert!(!serde_json::to_string(&store.overview().unwrap())
        .unwrap()
        .contains("private-console-token"));
    value.id = Some(provider.id.clone());
    value.api_key = None;
    value.balance_access_token = None;
    store.save(value.clone()).unwrap();
    let q = value.codex_options.balance_query.as_ref().unwrap();
    let connection = ConnectionInput {
        provider_id: Some(provider.id),
        base_url: value.base_url.clone(),
        api_key: None,
        balance_access_token: None,
    };
    assert_eq!(
        store.balance_connection(connection.clone(), q).unwrap().1,
        "private-console-token"
    );
    assert_eq!(
        store.connection(connection.clone()).unwrap().1,
        "test-key-1234"
    );
    let auto = store
        .auto_balance_connection(connection.clone(), None)
        .unwrap();
    assert_eq!(auto.1, "test-key-1234");
    assert_eq!(auto.3.as_deref(), Some("private-console-token"));
    let auto = store
        .auto_balance_connection(
            connection.clone(),
            Some(BalanceQuery {
                adapter: BalanceAdapter::Auto,
                ..BalanceQuery::default()
            }),
        )
        .unwrap();
    assert!(auto.3.is_none());
    let mut other = connection;
    other.base_url = "https://other.example.test/v1".into();
    assert!(store.balance_connection(other.clone(), q).is_err());
    assert!(store
        .auto_balance_connection(other, None)
        .unwrap()
        .3
        .is_none());
    value.base_url = "https://other.example.test/v1".into();
    assert!(store.save(value.clone()).is_err());
    value.balance_access_token = Some("new-console-token".into());
    assert!(store.save(value.clone()).is_ok());
    value.codex_options.balance_query = None;
    store.save(value.clone()).unwrap();
    assert!(store
        .provider(value.id.as_deref().unwrap())
        .unwrap()
        .balance_access_token
        .is_none());
}

#[test]
fn legacy_balance_presets_migrate_without_changing_custom_queries() {
    let mut value = input(Family::Codex, "旧版 New API");
    value.codex_options.balance_query = Some(BalanceQuery {
        path: "/api/usage/token".into(),
        json_path: "data.total_available".into(),
        unit: "额度".into(),
        ..BalanceQuery::default()
    });
    let (_temp, mut store) = fixture();
    let provider = store.save(value).unwrap();
    let mut stored = serde_json::to_value(store.provider(&provider.id).unwrap()).unwrap();
    stored
        .pointer_mut("/summary/codexOptions/balanceQuery")
        .unwrap()
        .as_object_mut()
        .unwrap()
        .remove("adapter");
    stored
        .as_object_mut()
        .unwrap()
        .remove("balance_access_token");
    stored
        .pointer_mut("/summary")
        .unwrap()
        .as_object_mut()
        .unwrap()
        .remove("hasBalanceToken");
    let migrated = decode_provider(&stored.to_string()).unwrap();
    let q = migrated.summary.codex_options.balance_query.unwrap();
    assert_eq!(q.adapter, BalanceAdapter::NewapiToken);
    assert_eq!(q.divisor, 500_000.0);
    stored
        .pointer_mut("/summary/codexOptions/balanceQuery/path")
        .unwrap()
        .clone_from(&json!("/custom"));
    assert_eq!(
        decode_provider(&stored.to_string())
            .unwrap()
            .summary
            .codex_options
            .balance_query
            .unwrap()
            .adapter,
        BalanceAdapter::Custom
    );
}

#[test]
fn codex_options_catalog_switch_and_restore_are_transactional() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("codex");
    let config = dir.join("config.toml");
    put(&config, "# original\nmodel = \"old\"\nservice_tier = \"flex\"\nmodel_context_window = 32000\n[features]\nkeep_feature = true\nfast_mode = false\n");
    let mut value = input(Family::Codex, "模型供应商");
    value.reasoning_effort = Some("high".into());
    value.codex_options = CodexOptions {
        fast_mode: Some(true),
        context_window: Some(128000),
        auto_compact_token_limit: Some(100000),
        models: vec![
            ProviderModel {
                id: "test-model".into(),
                context_window: Some(128000),
                reasoning_efforts: vec!["low".into(), "high".into()],
                enabled: true,
                ..Default::default()
            },
            ProviderModel {
                id: "backup-model".into(),
                context_window: None,
                reasoning_efforts: vec![],
                enabled: true,
                ..Default::default()
            },
        ],
        ..Default::default()
    };
    let provider = store.save(value.clone()).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let text = std::fs::read_to_string(&config).unwrap();
    assert!(text.contains("service_tier = \"priority\""));
    assert!(text.contains("model_context_window = 128000"));
    assert!(text.contains("model_auto_compact_token_limit = 100000"));
    assert!(text.contains("keep_feature = true"));
    let catalog_path = dir.join("uni-switch-models.json");
    let catalog = read_json(&catalog_path);
    assert_eq!(catalog["models"].as_array().unwrap().len(), 2);
    assert_eq!(catalog["models"][0]["visibility"], "list");
    assert_eq!(catalog["models"][0]["slug"], "test-model");
    let preferences = adapters::read_model_preferences(Target::Codex, &dir).unwrap();
    assert_eq!(preferences.codex_options.fast_mode, Some(true));
    assert_eq!(preferences.codex_options.models.len(), 2);
    assert_eq!(store.list().unwrap().len(), 1);
    value.id = Some(provider.id.clone());
    value.codex_options.fast_mode = Some(false);
    value.codex_options.models[1].enabled = false;
    store.save(value).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    store.apply(Target::Codex, &provider.id).unwrap();
    assert_eq!(
        read_json(&catalog_path)["models"].as_array().unwrap().len(),
        1
    );
    store.restore(Target::Codex).unwrap();
    assert!(!catalog_path.exists());
    let restored = std::fs::read_to_string(config).unwrap();
    assert!(restored.contains("service_tier = \"flex\""));
    assert!(restored.contains("model_context_window = 32000"));
    assert!(restored.contains("fast_mode = false"));
    assert!(!restored.contains("model_catalog_json"));
}

#[test]
fn older_saved_providers_and_baselines_migrate_without_losing_new_user_settings() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("codex");
    let config = dir.join("config.toml");
    put(&config, "model_context_window = 32000\n");
    let provider = store.save(input(Family::Codex, "旧版配置")).unwrap();
    let mut old = serde_json::to_value(store.provider(&provider.id).unwrap()).unwrap();
    old["summary"]
        .as_object_mut()
        .unwrap()
        .remove("codexOptions");
    store
        .conn
        .execute(
            "UPDATE providers SET data=?1 WHERE id=?2",
            params![old.to_string(), provider.id],
        )
        .unwrap();
    assert!(store.list().unwrap()[0].codex_options.models.is_empty());
    store.apply(Target::Codex, &provider.id).unwrap();
    let (_, _, baseline) = store.target_record(Target::Codex).unwrap();
    let mut legacy = baseline.unwrap();
    legacy.retain(|f| f.format == "toml");
    legacy[0].keys.retain(|k| {
        [
            "model",
            "model_provider",
            "model_reasoning_effort",
            "model_providers.uni_switch",
        ]
        .contains(&k.as_str())
    });
    store
        .conn
        .execute(
            "UPDATE targets SET baseline=?1 WHERE id='codex'",
            [json(&legacy).unwrap()],
        )
        .unwrap();
    let text = std::fs::read_to_string(&config).unwrap();
    put(&config, &format!("model_context_window = 64000\n{text}"));
    // The first application removed the field; this simulates an independent user change after an older release.
    let mut value = input(Family::Codex, "旧版配置");
    value.id = Some(provider.id.clone());
    value.codex_options.context_window = Some(128000);
    store.save(value).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    store.restore(Target::Codex).unwrap();
    assert!(std::fs::read_to_string(config)
        .unwrap()
        .contains("model_context_window = 64000"));
}

#[test]
fn modified_catalog_is_preserved_and_invalid_options_are_rejected() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "模型供应商");
    value.codex_options.models = vec![ProviderModel {
        id: "test-model".into(),
        context_window: None,
        reasoning_efforts: vec![],
        enabled: true,
        ..Default::default()
    }];
    let provider = store.save(value.clone()).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let catalog = temp.path().join("codex/uni-switch-models.json");
    put(&catalog, r#"{"models":[],"external":true}"#);
    assert_eq!(
        store.status(Target::Codex).unwrap().state,
        "external_change"
    );
    assert!(store.restore(Target::Codex).is_err());
    assert!(store.apply(Target::Codex, &provider.id).is_err());
    assert_eq!(read_json(&catalog)["external"], true);
    value.codex_options.context_window = Some(0);
    assert!(store.save(value.clone()).is_err());
    value.codex_options.context_window = Some(10);
    value.codex_options.auto_compact_token_limit = Some(20);
    assert!(store.save(value.clone()).is_err());
    value.codex_options.auto_compact_token_limit = None;
    value.model = "unselected-model".into();
    assert!(store.save(value).is_err());
}

#[test]
fn codex_model_menu_choices_do_not_block_switch_or_restore_but_routing_changes_do() {
    let (temp, mut store) = fixture();
    let config = temp.path().join("codex/config.toml");
    put(&config, "model = \"original\"\n");
    let mut value = input(Family::Codex, "多模型");
    value.codex_options.models = ["test-model", "other-model"]
        .into_iter()
        .map(|id| ProviderModel {
            id: id.into(),
            context_window: None,
            reasoning_efforts: vec![],
            enabled: true,
            ..Default::default()
        })
        .collect();
    let provider = store.save(value.clone()).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let text = std::fs::read_to_string(&config)
        .unwrap()
        .replace("model = \"test-model\"", "model = \"other-model\"");
    put(
        &config,
        &format!("model_reasoning_effort = \"high\"\nservice_tier = \"priority\"\n{text}"),
    );
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    store.restore(Target::Codex).unwrap();
    assert!(std::fs::read_to_string(&config)
        .unwrap()
        .contains("original"));
    store.apply(Target::Codex, &provider.id).unwrap();
    let text = std::fs::read_to_string(&config)
        .unwrap()
        .replace("model = \"test-model\"", "model = \"other-model\"");
    put(&config, &text);
    value.id = Some(provider.id.clone());
    value.codex_options.fast_mode = Some(true);
    value.codex_options.models[1].enabled = false;
    store.save(value).unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    store.apply(Target::Codex, &provider.id).unwrap();
    let text = std::fs::read_to_string(&config)
        .unwrap()
        .replace("test-key-1234", "externally-edited-key");
    put(&config, &text);
    assert_eq!(
        store.status(Target::Codex).unwrap().state,
        "external_change"
    );
    assert!(store.restore(Target::Codex).is_err());
}

#[test]
fn codex_switch_preserves_comments_projects_and_credentials() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("codex");
    let path = dir.join("config.toml");
    put(&path, "# 用户原来的注释\nmodel = \"original\"\n[projects.\"D:/中文 项目\"]\ntrust_level = \"trusted\"\n[mcp_servers.example]\ncommand = \"test\"\n");
    put(
        &dir.join("auth.json"),
        r#"{"tokens":{"access_token":"official-test-token"}}"#,
    );
    let original_auth = std::fs::read(dir.join("auth.json")).unwrap();
    let a = store.save(input(Family::Codex, "配置 A")).unwrap();
    let mut b_input = input(Family::Codex, "配置 B");
    b_input.api_key = Some("test-key-5678".into());
    b_input.model = "another-model".into();
    let b = store.save(b_input).unwrap();
    store.apply(Target::Codex, &a.id).unwrap();
    store.apply(Target::Codex, &b.id).unwrap();
    let text = std::fs::read_to_string(&path).unwrap();
    assert!(text.contains("# 用户原来的注释"));
    assert!(text.contains("trust_level"));
    assert!(text.contains("mcp_servers.example"));
    assert!(text.contains("another-model"));
    assert!(text.contains("test-key-5678"));
    assert!(text.contains("requires_openai_auth = true"));
    assert!(!text.contains("official-test-token"));
    assert_eq!(std::fs::read(dir.join("auth.json")).unwrap(), original_auth);
    put(&path, &format!("{text}\n[features]\nexample = true\n"));
    store.restore(Target::Codex).unwrap();
    let restored = std::fs::read_to_string(path).unwrap();
    assert!(restored.contains("original"));
    assert!(restored.contains("[features]"));
    assert!(!restored.contains("test-key-5678"));
}

#[test]
fn native_claude_client_bases_do_not_duplicate_the_sdk_version_path() {
    for (upstream, expected) in [
        (
            "https://gateway.example.test/v1",
            "https://gateway.example.test",
        ),
        (
            "https://gateway.example.test/v1/",
            "https://gateway.example.test",
        ),
        (
            "https://gateway.example.test",
            "https://gateway.example.test",
        ),
        (
            "https://gateway.example.test/",
            "https://gateway.example.test",
        ),
        (
            "https://gateway.example.test:9443/custom/anthropic/v1/",
            "https://gateway.example.test:9443/custom/anthropic",
        ),
        (
            "https://gateway.example.test/custom/v10",
            "https://gateway.example.test/custom/v10",
        ),
        (
            "https://gateway.example.test/custom/v1/service",
            "https://gateway.example.test/custom/v1/service",
        ),
    ] {
        for auth in ["bearer", "x-api-key"] {
            let (temp, mut store) = fixture();
            let mut value = input(Family::Claude, "Native Claude base");
            value.base_url = upstream.into();
            value.auth_mode = auth.into();
            value.model = "claude-opus-5-5".into();
            value.codex_options.models = ["claude-opus-5-5", "claude-fable-5", "claude-haiku-5-5"]
                .iter()
                .map(|id| ProviderModel {
                    id: (*id).into(),
                    enabled: true,
                    ..Default::default()
                })
                .collect();
            let provider = store.save(value).unwrap();
            for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
                assert_eq!(store.apply(target, &provider.id).unwrap().state, "applied");
                assert_eq!(store.status(target).unwrap().state, "applied");
            }
            let cli = read_json(&temp.path().join("claude_cli/settings.json"));
            assert_eq!(cli["env"]["ANTHROPIC_BASE_URL"], expected, "{upstream}");
            assert_eq!(cli["env"]["ANTHROPIC_MODEL"], "claude-opus-5-5");
            let auth_key = if auth == "bearer" {
                "ANTHROPIC_AUTH_TOKEN"
            } else {
                "ANTHROPIC_API_KEY"
            };
            assert_eq!(cli["env"][auth_key], "test-key-1234");
            let profile = read_json(&temp.path().join(format!(
                "claude_desktop/Claude-3p/configLibrary/{}.json",
                adapters::PROFILE_UUID
            )));
            assert_eq!(profile["inferenceGatewayBaseUrl"], expected, "{upstream}");
            assert_eq!(profile["inferenceGatewayAuthScheme"], auth);
            assert_eq!(
                profile["inferenceModels"],
                json!(["claude-opus-5-5", "claude-fable-5", "claude-haiku-5-5"])
            );
            assert_eq!(
                store.provider(&provider.id).unwrap().summary.base_url,
                upstream.trim_end_matches('/')
            );
            assert!(!store.bridge_required());
        }
    }
}

#[test]
fn native_claude_base_normalization_restores_the_original_client_values() {
    let (temp, mut store) = fixture();
    let cli_path = temp.path().join("claude_cli/settings.json");
    let profile_path = temp.path().join(format!(
        "claude_desktop/Claude-3p/configLibrary/{}.json",
        adapters::PROFILE_UUID
    ));
    let original_cli = json!({
        "env": {"ANTHROPIC_BASE_URL": "https://original.example.test/route/v1", "KEEP": "yes"},
        "hooks": {"Stop": []}
    });
    let original_profile = json!({
        "inferenceGatewayBaseUrl": "https://original.example.test/route/v1",
        "permissionMode": "default"
    });
    put(&cli_path, &original_cli.to_string());
    put(&profile_path, &original_profile.to_string());
    let provider = store
        .save(input(Family::Claude, "Native Claude base restore"))
        .unwrap();
    for target in [Target::ClaudeCli, Target::ClaudeDesktop] {
        store.apply(target, &provider.id).unwrap();
    }
    assert_eq!(
        read_json(&cli_path)["env"]["ANTHROPIC_BASE_URL"],
        "https://gateway.example.test"
    );
    assert_eq!(
        read_json(&profile_path)["inferenceGatewayBaseUrl"],
        "https://gateway.example.test"
    );
    for target in [Target::ClaudeCli, Target::ClaudeDesktop] {
        store.restore(target).unwrap();
    }
    assert_eq!(read_json(&cli_path), original_cli);
    assert_eq!(read_json(&profile_path), original_profile);
}

#[test]
fn legacy_claude_client_base_is_pending_without_overwriting_external_changes() {
    let (temp, mut store) = fixture();
    let provider = store
        .save(input(Family::Claude, "Legacy native base"))
        .unwrap();
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        store.apply(target, &provider.id).unwrap();
        let (_, _, baseline) = store.target_record(target).unwrap();
        let mut baseline = baseline.unwrap();
        let file = baseline
            .iter_mut()
            .find(|file| {
                file.keys.iter().any(|key| {
                    matches!(
                        key.as_str(),
                        "env.ANTHROPIC_BASE_URL" | "inferenceGatewayBaseUrl"
                    )
                })
            })
            .unwrap();
        let mut doc: Value = serde_json::from_str(file.expected.as_deref().unwrap()).unwrap();
        let base = if target == Target::ClaudeCli {
            &mut doc["env"]["ANTHROPIC_BASE_URL"]
        } else {
            &mut doc["inferenceGatewayBaseUrl"]
        };
        *base = json!(provider.base_url);
        let legacy = doc.to_string();
        put(&file.path, &legacy);
        file.expected = Some(legacy.clone());
        let path = file.path.clone();
        store
            .conn
            .execute(
                "UPDATE targets SET baseline=?1 WHERE id=?2",
                params![json(&baseline).unwrap(), target.id()],
            )
            .unwrap();
        let revision = store.status(target).unwrap().configuration_revision;
        assert_eq!(store.status(target).unwrap().state, "saved_changes");
        assert_eq!(std::fs::read_to_string(&path).unwrap(), legacy);
        let outside = legacy.replace("gateway.example.test", "outside.example.test");
        put(&path, &outside);
        assert_eq!(store.status(target).unwrap().state, "external_change");
        assert_eq!(
            store.apply(target, &provider.id).unwrap_err().code,
            "external_change"
        );
        assert_eq!(std::fs::read_to_string(&path).unwrap(), outside);
        put(&path, &legacy);
        store.apply(target, &provider.id).unwrap();
        let status = store.status(target).unwrap();
        assert_eq!(status.state, "applied");
        assert!(status.configuration_revision > revision);
        assert!(!std::fs::read_to_string(&path)
            .unwrap()
            .contains("gateway.example.test/v1"));
        assert_eq!(
            store.provider(&provider.id).unwrap().summary.base_url,
            provider.base_url
        );
    }
    assert!(!temp.path().join("codex/config.toml").exists());
}

#[test]
fn desktop_writes_profile_and_library_preserves_mcp_and_restores_new_settings() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("claude_desktop");
    let normal = dir.join("Claude/claude_desktop_config.json");
    let third = dir.join("Claude-3p/claude_desktop_config.json");
    let meta = dir.join("Claude-3p/configLibrary/_meta.json");
    put(
        &normal,
        r#"{"deploymentMode":"1p","mcpServers":{"keep":{"command":"demo"}}}"#,
    );
    put(&third, r#"{"existing":true}"#);
    put(
        &meta,
        r#"{"entries":[{"id":"other-profile","name":"Existing"}],"appliedId":"other-profile","custom":true}"#,
    );
    let mut multi = input(Family::Claude, "Claude 桌面");
    multi.codex_options.models = [
        "claude-opus-4-6",
        "claude-sonnet-4-6",
        "claude-haiku-4-5",
        "ignored-model",
    ]
    .iter()
    .map(|id| crate::types::ProviderModel {
        id: (*id).into(),
        enabled: *id != "ignored-model",
        context_window: None,
        reasoning_efforts: Vec::new(),
        ..Default::default()
    })
    .collect();
    let provider = store.save(multi).unwrap();
    let status = store.apply(Target::ClaudeDesktop, &provider.id).unwrap();
    assert_eq!(status.state, "applied");
    assert_eq!(read_json(&normal)["deploymentMode"], "3p");
    assert!(read_json(&normal)["mcpServers"].is_object());
    let profile_path = dir.join(format!(
        "Claude-3p/configLibrary/{}.json",
        adapters::PROFILE_UUID
    ));
    let profile = read_json(&profile_path);
    assert_eq!(profile["inferenceProvider"], "gateway");
    assert_eq!(profile["inferenceGatewayAuthScheme"], "bearer");
    assert_eq!(profile["inferenceModels"][0], "claude-sonnet-4-6");
    assert_eq!(
        profile["inferenceModels"],
        json!(["claude-sonnet-4-6", "claude-opus-4-6", "claude-haiku-4-5"])
    );
    let preferences = adapters::read_model_preferences(Target::ClaudeDesktop, &dir).unwrap();
    assert_eq!(preferences.codex_options.models.len(), 3);
    store.apply(Target::ClaudeCli, &provider.id).unwrap();
    let cli = read_json(&temp.path().join("claude_cli/settings.json"));
    assert_eq!(
        cli["env"]["ANTHROPIC_DEFAULT_OPUS_MODEL"],
        "claude-opus-4-6"
    );
    assert_eq!(
        cli["env"]["ANTHROPIC_DEFAULT_SONNET_MODEL"],
        "claude-sonnet-4-6"
    );
    assert_eq!(
        cli["env"]["ANTHROPIC_DEFAULT_HAIKU_MODEL"],
        "claude-haiku-4-5"
    );
    assert_eq!(read_json(&meta)["entries"].as_array().unwrap().len(), 2);
    let mut normal_doc = read_json(&normal);
    normal_doc["newUserSetting"] = json!(true);
    put(&normal, &normal_doc.to_string());
    let mut profile_doc = read_json(&profile_path);
    profile_doc["permissionMode"] = json!("default");
    put(&profile_path, &profile_doc.to_string());
    store.restore(Target::ClaudeDesktop).unwrap();
    assert_eq!(read_json(&normal)["deploymentMode"], "1p");
    assert_eq!(read_json(&normal)["newUserSetting"], true);
    assert_eq!(read_json(&meta)["appliedId"], "other-profile");
    assert_eq!(read_json(&meta)["entries"].as_array().unwrap().len(), 1);
    let restored_profile = read_json(&profile_path);
    assert_eq!(restored_profile["permissionMode"], "default");
    assert!(restored_profile.get("inferenceGatewayApiKey").is_none());
}

#[test]
fn cli_switch_clears_old_auth_preserves_hooks_and_does_not_apply_desktop() {
    let (temp, mut store) = fixture();
    let path = temp.path().join("claude_cli/settings.json");
    put(
        &path,
        r#"{"env":{"ANTHROPIC_API_KEY":"old-key","KEEP":"yes"},"hooks":{"Stop":[]},"permissions":{"allow":["Read"]}}"#,
    );
    let provider = store.save(input(Family::Claude, "Claude CLI")).unwrap();
    store.apply(Target::ClaudeCli, &provider.id).unwrap();
    let doc = read_json(&path);
    assert!(doc["env"].get("ANTHROPIC_API_KEY").is_none());
    assert_eq!(doc["env"]["ANTHROPIC_AUTH_TOKEN"], "test-key-1234");
    assert_eq!(doc["env"]["KEEP"], "yes");
    assert!(doc["hooks"].is_object());
    assert_eq!(
        store.status(Target::ClaudeDesktop).unwrap().state,
        "unmanaged"
    );
    store.restore(Target::ClaudeCli).unwrap();
    assert_eq!(read_json(&path)["env"]["ANTHROPIC_API_KEY"], "old-key");
}

#[test]
fn damaged_desktop_file_prevents_all_four_writes() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("claude_desktop");
    let normal = dir.join("Claude/claude_desktop_config.json");
    let third = dir.join("Claude-3p/claude_desktop_config.json");
    put(&normal, r#"{"deploymentMode":"1p"}"#);
    put(&third, "{ broken json");
    let provider = store.save(input(Family::Claude, "Claude")).unwrap();
    assert_eq!(
        store
            .apply(Target::ClaudeDesktop, &provider.id)
            .unwrap_err()
            .code,
        "invalid_json"
    );
    assert_eq!(read_json(&normal)["deploymentMode"], "1p");
    assert!(!dir.join("Claude-3p/configLibrary").exists());
}

#[test]
fn external_changes_remain_protected_until_external_edit_is_reverted() {
    let (temp, mut store) = fixture();
    let p = store.save(input(Family::Claude, "A")).unwrap();
    store.apply(Target::ClaudeCli, &p.id).unwrap();
    let path = temp.path().join("claude_cli/settings.json");
    let before = std::fs::read_to_string(&path).unwrap();
    let mut doc = read_json(&path);
    doc["env"]["ANTHROPIC_BASE_URL"] = json!("https://changed.example.test");
    put(&path, &doc.to_string());
    assert_eq!(
        store.status(Target::ClaudeCli).unwrap().state,
        "external_change"
    );
    assert_eq!(
        store.apply(Target::ClaudeCli, &p.id).unwrap_err().code,
        "external_change"
    );
    assert_eq!(
        store.restore(Target::ClaudeCli).unwrap_err().code,
        "external_change"
    );
    assert_eq!(read_json(&path), doc, "conflicting file is untouched");
    assert_eq!(store.list().unwrap().len(), 1, "no provider is adopted");
    assert_eq!(
        store.provider(&p.id).unwrap().summary.base_url,
        "https://gateway.example.test/v1"
    );
    put(&path, &before);
    store.apply(Target::ClaudeCli, &p.id).unwrap();
    assert_eq!(store.status(Target::ClaudeCli).unwrap().state, "applied");
    store.restore(Target::ClaudeCli).unwrap();
}

#[test]
fn edited_key_is_preserved_when_blank_and_pending_changes_are_visible() {
    let (_temp, mut store) = fixture();
    let p = store.save(input(Family::Codex, "A")).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    let mut update = input(Family::Codex, "Renamed");
    update.id = Some(p.id.clone());
    update.api_key = None;
    update.model = "new-model".into();
    store.save(update).unwrap();
    assert_eq!(store.provider(&p.id).unwrap().api_key, "test-key-1234");
    assert_eq!(store.status(Target::Codex).unwrap().state, "saved_changes");
    assert_eq!(store.delete(&p.id).unwrap_err().code, "in_use");
    assert!(!json(&store.overview().unwrap())
        .unwrap()
        .contains("test-key-1234"));
}

#[test]
fn partial_crash_rolls_back_and_completed_files_finish_database_commit() {
    let (temp, mut store) = fixture();
    let dir = temp.path().join("claude_desktop");
    let p = store.save(input(Family::Claude, "Claude")).unwrap();
    let files =
        adapters::plan(Target::ClaudeDesktop, &dir, &store.provider(&p.id).unwrap()).unwrap();
    let changes: Vec<_> = files
        .iter()
        .map(|f| Change {
            path: f.path.clone(),
            before: f.original.clone(),
            after: f.expected.clone(),
        })
        .collect();
    let pending = Pending {
        target: Target::ClaudeDesktop,
        changes,
        baseline: Some(files),
        active: Some(p.id.clone()),
        provider_update: None,
        applied_summary: None,
        accepted_source: None,
    };
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('crash','pending',?1,0)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    writer::write(
        &pending.changes[0].path,
        pending.changes[0].after.as_deref(),
    )
    .unwrap();
    store.recover().unwrap();
    assert!(!pending.changes[0].path.exists());
    assert_eq!(
        store.status(Target::ClaudeDesktop).unwrap().state,
        "unmanaged"
    );
    store
        .conn
        .execute(
            "INSERT INTO operations VALUES('complete','pending',?1,1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    for change in &pending.changes {
        writer::write(&change.path, change.after.as_deref()).unwrap();
    }
    store.recover().unwrap();
    assert_eq!(
        store
            .status(Target::ClaudeDesktop)
            .unwrap()
            .active_provider_id,
        Some(p.id)
    );
}

#[test]
fn invalid_urls_models_and_changed_directory_are_rejected() {
    let (_temp, mut store) = fixture();
    let mut bad = input(Family::Codex, "A");
    bad.base_url = "file:///tmp/secret".into();
    assert_eq!(store.save(bad).unwrap_err().code, "invalid_url");
    let p = store.save(input(Family::Codex, "A")).unwrap();
    store.apply(Target::Codex, &p.id).unwrap();
    assert_eq!(
        store
            .set_directory(Target::Codex, std::env::temp_dir().to_string_lossy().into())
            .unwrap_err()
            .code,
        "managed_directory"
    );
    let mut other = input(Family::Claude, "Other");
    other.model = "non-claude-model".into();
    let c = store.save(other).unwrap();
    assert_eq!(
        store.apply(Target::ClaudeDesktop, &c.id).unwrap_err().code,
        "desktop_model"
    );
}

#[cfg(windows)]
#[test]
fn windows_locked_file_keeps_original_contents() {
    use std::os::windows::fs::OpenOptionsExt;
    use windows_sys::Win32::Storage::FileSystem::FILE_SHARE_READ;
    let temp = tempfile::tempdir().unwrap();
    let path = temp.path().join("locked.json");
    put(&path, "original");
    let _held = std::fs::OpenOptions::new()
        .read(true)
        .share_mode(FILE_SHARE_READ)
        .open(&path)
        .unwrap();
    assert!(writer::write(&path, Some("replacement")).is_err());
    assert_eq!(std::fs::read_to_string(path).unwrap(), "original");
}

#[test]
fn model_capabilities_write_repair_and_upgrade_preserve_client_choices() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Image capability regression");
    value.model = "gpt-4o".into();
    value.codex_options.models = vec![ProviderModel {
        id: "gpt-4o".into(),
        context_window: Some(256000),
        enabled: true,
        ..Default::default()
    }];
    let provider = store.save(value).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let dir = temp.path().join("codex");
    let catalog_path = dir.join("uni-switch-models.json");
    assert_eq!(
        read_json(&catalog_path)["models"][0]["input_modalities"],
        json!(["text", "image"])
    );
    // Simulate exactly the byte-for-byte owned text-only catalog from 0.5.18.
    let mut legacy = read_json(&catalog_path);
    legacy["models"][0]["input_modalities"] = json!(["text"]);
    legacy["models"][0]["description"] = json!("preserve this metadata");
    put(
        &catalog_path,
        &serde_json::to_string_pretty(&legacy).unwrap(),
    );
    let (_, _, baseline) = store.target_record(Target::Codex).unwrap();
    let mut baseline = baseline.unwrap();
    baseline
        .iter_mut()
        .find(|f| f.format == "catalog")
        .unwrap()
        .expected = adapters::read(&catalog_path).unwrap();
    store
        .conn
        .execute(
            "UPDATE targets SET baseline=?1 WHERE id='codex'",
            [json(&baseline).unwrap()],
        )
        .unwrap();
    let config_before = adapters::read(&dir.join("config.toml")).unwrap();
    let provider_before = json(&store.provider(&provider.id).unwrap()).unwrap();
    let revision_before = store.status(Target::Codex).unwrap().configuration_revision;
    let data = store.data_directory.clone();
    drop(store);
    let mut reopened = Store::open(data.clone()).unwrap();
    assert!(reopened.overview().unwrap().repaired_model_capabilities);
    assert!(
        reopened
            .status(Target::Codex)
            .unwrap()
            .configuration_revision
            > revision_before
    );
    assert_eq!(
        adapters::read(&dir.join("config.toml")).unwrap(),
        config_before
    );
    assert_eq!(
        json(&reopened.provider(&provider.id).unwrap()).unwrap(),
        provider_before
    );
    let repaired = read_json(&catalog_path);
    assert_eq!(
        repaired["models"][0]["input_modalities"],
        json!(["text", "image"])
    );
    assert_eq!(
        repaired["models"][0]["description"],
        "preserve this metadata"
    );
    // Fast and reasoning repairs must retain correct modalities and be reversible.
    reopened.set_provider_fast_mode(&provider.id, true).unwrap();
    reopened.repair_reasoning_levels(&provider.id).unwrap();
    assert_eq!(
        read_json(&catalog_path)["models"][0]["input_modalities"],
        json!(["text", "image"])
    );
    let revision = reopened
        .status(Target::Codex)
        .unwrap()
        .configuration_revision;
    drop(reopened);
    let mut final_store = Store::open(data).unwrap();
    assert!(!final_store.overview().unwrap().repaired_model_capabilities);
    assert_eq!(
        final_store
            .status(Target::Codex)
            .unwrap()
            .configuration_revision,
        revision
    );
    final_store.restore(Target::Codex).unwrap();
    assert!(!catalog_path.exists());
}

#[test]
fn manual_image_capability_applies_to_unknown_models_and_survives_fast() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Custom image model");
    value.codex_options.models = vec![ProviderModel {
        id: value.model.clone(),
        enabled: true,
        capability_overrides: ModelCapabilities {
            image_input: Some(true),
            parallel_tool_calls: Some(true),
        },
        ..Default::default()
    }];
    let provider = store.save(value).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    store.set_provider_fast_mode(&provider.id, true).unwrap();
    let path = temp.path().join("codex/uni-switch-models.json");
    assert_eq!(
        read_json(&path)["models"][0]["input_modalities"],
        json!(["text", "image"])
    );
    assert_eq!(
        read_json(&path)["models"][0]["supports_parallel_tool_calls"],
        true
    );
    let mut input = quick_input(store.provider(&provider.id).unwrap().summary, Target::Codex);
    input.models[0].capability_overrides.image_input = Some(false);
    store.quick_model_settings(input).unwrap();
    assert_eq!(
        read_json(&path)["models"][0]["input_modalities"],
        json!(["text"])
    );
}

#[test]
fn capability_upgrade_does_not_overwrite_external_catalog_edits() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Conflict safe upgrade");
    value.model = "gpt-4o".into();
    let provider = store.save(value).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let path = temp.path().join("codex/uni-switch-models.json");
    let mut catalog = read_json(&path);
    catalog["models"][0]["input_modalities"] = json!(["text"]);
    put(&path, &serde_json::to_string_pretty(&catalog).unwrap());
    let before = adapters::read(&path).unwrap();
    let data = store.data_directory.clone();
    drop(store);
    let reopened = Store::open(data).unwrap();
    assert!(!reopened.overview().unwrap().repaired_model_capabilities);
    assert_eq!(adapters::read(&path).unwrap(), before);
    assert_eq!(
        reopened.status(Target::Codex).unwrap().state,
        "external_change"
    );
}

#[test]
fn confirmed_overwrite_applies_all_targets_and_backs_up_external_files() {
    for target in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli] {
        let (temp, mut store) = fixture();
        let dir = temp.path().join(target.id());
        let path = match target {
            Target::Codex => dir.join("config.toml"),
            Target::ClaudeCli => dir.join("settings.json"),
            Target::ClaudeDesktop => dir.join(format!(
                "Claude-3p/configLibrary/{}.json",
                adapters::PROFILE_UUID
            )),
        };
        let original = if target == Target::Codex {
            "# original before takeover\nmodel = 'original-model'\nuser_setting = 'original'\n"
        } else {
            r#"{"userSetting":"original"}"#
        };
        put(&path, original);
        let a = store.save(input(target.family(), "A")).unwrap();
        let mut b_input = input(target.family(), "B");
        b_input.base_url = "https://new-supplier.example.test/v1".into();
        b_input.api_key = Some("test-overwrite-new-key".into());
        let b = store.save(b_input).unwrap();
        store.apply(target, &a.id).unwrap();
        let old_revision = store.status(target).unwrap().configuration_revision;
        let changed = if target == Target::Codex {
            let mut doc = std::fs::read_to_string(&path)
                .unwrap()
                .parse::<toml_edit::DocumentMut>()
                .unwrap();
            doc["model_providers"]["uni_switch"]["base_url"] =
                toml_edit::value("https://outside.example.test/v1");
            doc["model_providers"]["uni_switch"]["experimental_bearer_token"] =
                toml_edit::value("test-external-key");
            doc["user_setting"] = toml_edit::value("outside-kept");
            doc.to_string()
        } else {
            let mut doc = read_json(&path);
            if target == Target::ClaudeCli {
                doc["env"]["ANTHROPIC_BASE_URL"] = json!("https://outside.example.test/v1");
            } else {
                doc["inferenceGatewayBaseUrl"] = json!("https://outside.example.test/v1");
            }
            doc["userSetting"] = json!("outside-kept");
            doc.to_string()
        };
        put(&path, &changed);
        assert_eq!(
            store.apply(target, &b.id).unwrap_err().code,
            "external_change"
        );
        let confirmation = store.prepare_apply_overwrite(target, &b.id).unwrap();
        assert_eq!(
            adapters::read(&path).unwrap().as_deref(),
            Some(changed.as_str()),
            "asking/cancelling does not write"
        );
        assert!(confirmation
            .files
            .iter()
            .any(|file| Path::new(file) == path));
        assert!(!json(&confirmation)
            .unwrap()
            .contains("test-overwrite-new-key"));
        assert!(!json(&confirmation).unwrap().contains("test-external-key"));
        assert_eq!(
            store.status(target).unwrap().active_provider_id.as_deref(),
            Some(a.id.as_str())
        );
        let result = store
            .apply_overwrite(target, &b.id, &confirmation.token)
            .unwrap();
        assert_eq!(result.state, "applied");
        assert_eq!(result.active_provider_id.as_deref(), Some(b.id.as_str()));
        assert!(result.configuration_revision > old_revision);
        for other in [Target::Codex, Target::ClaudeDesktop, Target::ClaudeCli]
            .into_iter()
            .filter(|other| *other != target)
        {
            assert_eq!(store.status(other).unwrap().state, "unmanaged");
        }
        let backup = std::fs::read_dir(store.data_directory.join("backups"))
            .unwrap()
            .map(|entry| std::fs::read_to_string(entry.unwrap().path()).unwrap())
            .map(|text| decode::<Pending>(&text).unwrap())
            .find(|pending| pending.active.as_deref() == Some(b.id.as_str()))
            .unwrap();
        assert_eq!(
            backup
                .changes
                .iter()
                .find(|change| change.path == path)
                .unwrap()
                .before
                .as_deref(),
            Some(changed.as_str())
        );
        if target == Target::Codex {
            let doc = std::fs::read_to_string(&path)
                .unwrap()
                .parse::<toml_edit::DocumentMut>()
                .unwrap();
            assert_eq!(doc["user_setting"].as_str(), Some("outside-kept"));
            assert_eq!(
                doc["model_providers"]["uni_switch"]["base_url"].as_str(),
                Some("https://new-supplier.example.test/v1")
            );
            assert_eq!(
                doc["model_providers"]["uni_switch"]["experimental_bearer_token"].as_str(),
                Some("test-overwrite-new-key")
            );
        } else {
            assert_eq!(read_json(&path)["userSetting"], "outside-kept");
        }
        assert_eq!(
            store
                .apply_overwrite(target, &b.id, &confirmation.token)
                .unwrap_err()
                .code,
            "overwrite_confirmation_changed"
        );
        store.restore(target).unwrap();
        if target == Target::Codex {
            let doc = std::fs::read_to_string(&path)
                .unwrap()
                .parse::<toml_edit::DocumentMut>()
                .unwrap();
            assert_eq!(doc["model"].as_str(), Some("original-model"));
            assert_eq!(doc["user_setting"].as_str(), Some("outside-kept"));
            assert!(doc.get("model_providers").is_none());
        } else {
            assert_eq!(read_json(&path)["userSetting"], "outside-kept");
            assert!(read_json(&path).get("inferenceGatewayBaseUrl").is_none());
        }
    }
}

#[test]
fn confirmed_overwrite_rejects_modified_files_provider_target_or_expired_approval() {
    for change in [
        "file",
        "provider",
        "target",
        "wrong_target",
        "wrong_provider",
        "invalid_token",
        "expired",
    ] {
        let (temp, mut store) = fixture();
        let provider = store.save(input(Family::Codex, "A")).unwrap();
        store.apply(Target::Codex, &provider.id).unwrap();
        let path = temp.path().join("codex/config.toml");
        let outside = std::fs::read_to_string(&path)
            .unwrap()
            .replace("gateway.example.test", "outside.example.test");
        put(&path, &outside);
        let confirmation = store
            .prepare_apply_overwrite(Target::Codex, &provider.id)
            .unwrap();
        let other = store.save(input(Family::Codex, "B")).unwrap();
        match change {
            "file" => put(&path, &format!("# changed during confirmation\n{outside}")),
            "provider" => {
                let mut edit = input(Family::Codex, "Edited");
                edit.id = Some(provider.id.clone());
                edit.api_key = Some("test-key-during-confirmation".into());
                store.save(edit).unwrap();
            }
            "target" => {
                store
                    .conn
                    .execute(
                        "UPDATE targets SET applied_at_ms=applied_at_ms+1 WHERE id='codex'",
                        [],
                    )
                    .unwrap();
            }
            "expired" => {
                store.overwrite_approval.as_mut().unwrap().created_at =
                    std::time::Instant::now() - std::time::Duration::from_secs(301)
            }
            _ => {}
        }
        let before = adapters::read(&path).unwrap();
        let result = store.apply_overwrite(
            if change == "wrong_target" {
                Target::ClaudeCli
            } else {
                Target::Codex
            },
            if change == "wrong_provider" {
                &other.id
            } else {
                &provider.id
            },
            if change == "invalid_token" {
                "invalid-approval"
            } else {
                &confirmation.token
            },
        );
        assert_eq!(
            result.unwrap_err().code,
            "overwrite_confirmation_changed",
            "{change}"
        );
        assert_eq!(adapters::read(&path).unwrap(), before);
        assert_eq!(
            store
                .status(Target::Codex)
                .unwrap()
                .active_provider_id
                .as_deref(),
            Some(provider.id.as_str())
        );
        assert!(store.overwrite_approval.is_none());
    }
}

#[test]
fn confirmed_overwrite_of_same_provider_catalog_repairs_without_disabling_other_conflicts() {
    let (temp, mut store) = fixture();
    let provider = store.save(input(Family::Codex, "A")).unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
    let catalog = temp.path().join("codex/uni-switch-models.json");
    let mut doc = read_json(&catalog);
    doc["models"][0]["description"] = json!("externally modified catalog");
    put(&catalog, &doc.to_string());
    assert_eq!(
        store.apply(Target::Codex, &provider.id).unwrap_err().code,
        "external_change"
    );
    let approval = store
        .prepare_apply_overwrite(Target::Codex, &provider.id)
        .unwrap();
    assert_eq!(
        store
            .set_provider_fast_mode(&provider.id, true)
            .unwrap_err()
            .code,
        "external_change"
    );
    assert_eq!(
        store.restore(Target::Codex).unwrap_err().code,
        "external_change"
    );
    store
        .apply_overwrite(Target::Codex, &provider.id, &approval.token)
        .unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    assert_ne!(
        read_json(&catalog)["models"][0]["description"],
        doc["models"][0]["description"]
    );
}

#[test]
fn confirmed_overwrite_requires_valid_configuration_and_one_time_preparation() {
    let (temp, mut store) = fixture();
    let provider = store.save(input(Family::Claude, "A")).unwrap();
    assert_eq!(
        store
            .apply_overwrite(Target::ClaudeCli, &provider.id, "anything")
            .unwrap_err()
            .code,
        "overwrite_confirmation_changed"
    );
    assert_eq!(
        store
            .prepare_apply_overwrite(Target::ClaudeCli, &provider.id)
            .unwrap_err()
            .code,
        "overwrite_confirmation_changed"
    );
    store.apply(Target::ClaudeCli, &provider.id).unwrap();
    let path = temp.path().join("claude_cli/settings.json");
    put(&path, "{ invalid json");
    assert_eq!(
        store
            .prepare_apply_overwrite(Target::ClaudeCli, &provider.id)
            .unwrap_err()
            .code,
        "invalid_json"
    );
    assert!(store.overwrite_approval.is_none());
    assert_eq!(
        adapters::read(&path).unwrap().as_deref(),
        Some("{ invalid json")
    );
}

#[test]
fn codex_reapply_without_reasoning_override_keeps_client_effort() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Model routing regression");
    value.model = "gpt-6.1-sol".into();
    value.codex_options.models = ["gpt-6.1-sol", "gpt-5.6-terra"]
        .into_iter()
        .map(|id| ProviderModel {
            id: id.into(),
            enabled: true,
            ..Default::default()
        })
        .collect();
    let provider = store.commit_provider(value, Target::Codex, true).unwrap();
    let config_path = temp.path().join("codex/config.toml");
    let mut config = adapters::read(&config_path)
        .unwrap()
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    config["model"] = toml_edit::value("gpt-5.6-terra");
    config["model_reasoning_effort"] = toml_edit::value("xhigh");
    put(&config_path, &config.to_string());
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
    store.apply(Target::Codex, &provider.id).unwrap();
    let applied = adapters::read(&config_path)
        .unwrap()
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    assert_eq!(applied["model"].as_str(), Some("gpt-6.1-sol"));
    assert_eq!(
        applied
            .get("model_reasoning_effort")
            .and_then(toml_edit::Item::as_str),
        Some("xhigh"),
        "Applying a supplier without an effort override must not reset the Codex user's effort"
    );
}

#[test]
fn codex_status_reports_file_selection_separately_from_applied_default_without_adopting_it() {
    let (temp, mut store) = fixture();
    let mut value = input(Family::Codex, "Independent file selection");
    value.model = "gpt-6.1-sol".into();
    value.codex_options.models = ["gpt-6.1-sol", "gpt-5.6-terra"]
        .into_iter()
        .map(|id| ProviderModel {
            id: id.into(),
            enabled: true,
            ..Default::default()
        })
        .collect();
    let provider = store.commit_provider(value, Target::Codex, true).unwrap();
    let path = temp.path().join("codex/config.toml");
    let mut config = adapters::read(&path)
        .unwrap()
        .unwrap()
        .parse::<toml_edit::DocumentMut>()
        .unwrap();
    config["model"] = toml_edit::value("gpt-5.6-terra");
    config["model_reasoning_effort"] = toml_edit::value("medium");
    put(&path, &config.to_string());
    let before = std::fs::read(&path).unwrap();
    let status = store.status(Target::Codex).unwrap();
    assert_eq!(status.state, "applied");
    assert_eq!(status.applied_model.as_deref(), Some("gpt-6.1-sol"));
    assert_eq!(status.configured_model.as_deref(), Some("gpt-5.6-terra"));
    assert_eq!(
        status.configured_reasoning_effort.as_deref(),
        Some("medium")
    );
    assert_eq!(
        store.provider(&provider.id).unwrap().summary.model,
        "gpt-6.1-sol"
    );
    assert_eq!(std::fs::read(&path).unwrap(), before);
    for target in [Target::ClaudeDesktop, Target::ClaudeCli] {
        let status = store.status(target).unwrap();
        assert!(status.configured_model.is_none());
        assert!(status.configured_reasoning_effort.is_none());
    }
    config["model_providers"]["uni_switch"]["base_url"] =
        toml_edit::value("https://external.example.test/v1");
    put(&path, &config.to_string());
    let conflict = store.status(Target::Codex).unwrap();
    assert_eq!(conflict.state, "external_change");
    assert!(conflict.configured_model.is_none());
    assert!(conflict.configured_reasoning_effort.is_none());
}

#[test]
fn codex_effort_is_kept_on_edit_switch_and_confirmed_overwrite_but_explicit_override_wins() {
    for method in ["edit", "switch", "overwrite", "explicit"] {
        let (temp, mut store) = fixture();
        let config_path = temp.path().join("codex/config.toml");
        put(
            &config_path,
            "model='original'\nmodel_reasoning_effort='medium'\n",
        );
        let mut value = input(Family::Codex, "Effort ownership");
        value.model = "gpt-6.1-sol".into();
        value.codex_options.models = ["gpt-6.1-sol", "gpt-5.6-terra"]
            .into_iter()
            .map(|id| ProviderModel {
                id: id.into(),
                enabled: true,
                ..Default::default()
            })
            .collect();
        let provider = store
            .commit_provider(value.clone(), Target::Codex, true)
            .unwrap();
        let mut config = adapters::read(&config_path)
            .unwrap()
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap();
        config["model_reasoning_effort"] = toml_edit::value("xhigh");
        put(&config_path, &config.to_string());
        match method {
            "edit" => {
                value.id = Some(provider.id.clone());
                value.name = "Renamed supplier".into();
                store.commit_provider(value, Target::Codex, true).unwrap();
            }
            "switch" => {
                value.name = "New supplier".into();
                value.api_key = Some("another-synthetic-key".into());
                let other = store.save(value).unwrap();
                store.apply(Target::Codex, &other.id).unwrap();
            }
            "overwrite" => {
                config["model_providers"]["uni_switch"]["base_url"] =
                    toml_edit::value("https://external.example.test/v1");
                put(&config_path, &config.to_string());
                let confirmation = store
                    .prepare_apply_overwrite(Target::Codex, &provider.id)
                    .unwrap();
                store
                    .apply_overwrite(Target::Codex, &provider.id, &confirmation.token)
                    .unwrap();
            }
            "explicit" => {
                value.id = Some(provider.id.clone());
                value.reasoning_effort = Some("high".into());
                store.commit_provider(value, Target::Codex, true).unwrap();
            }
            _ => unreachable!(),
        }
        let applied = adapters::read(&config_path)
            .unwrap()
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap();
        assert_eq!(
            applied["model_reasoning_effort"].as_str(),
            Some(if method == "explicit" {
                "high"
            } else {
                "xhigh"
            }),
            "{method}"
        );
        store.restore(Target::Codex).unwrap();
        let restored = adapters::read(&config_path)
            .unwrap()
            .unwrap()
            .parse::<toml_edit::DocumentMut>()
            .unwrap();
        assert_eq!(
            restored["model_reasoning_effort"].as_str(),
            Some("medium"),
            "{method} restores the pre-ownership preference"
        );
    }
}
