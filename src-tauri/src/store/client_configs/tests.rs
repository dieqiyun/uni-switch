use super::*;
use crate::client_config::{ClientKind, NativeProtocol};
use serde_json::json;

#[test]
fn client_config_unresolved_journals_block_both_old_and_new_configuration_writes() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    let path = temp.path().join("workbuddy/models.json");
    put(&path, "{\"external\":true}");
    let pending = ConfigPending {
        client: ClientKind::Workbuddy,
        changes: vec![Change {
            path: path.clone(),
            before: None,
            after: Some("{}".into()),
        }],
        state: Some(ExtraState::default()),
        revision: 1,
    };
    store
        .conn
        .execute(
            "INSERT INTO config_operations(id,status,data) VALUES('qa-blocking','pending',?1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    assert_eq!(
        store.apply(Target::Codex, &p.id).unwrap_err().code,
        "recovery_conflict"
    );
    assert!(!temp.path().join("codex/config.toml").exists());
    assert_eq!(
        std::fs::read_to_string(path).unwrap(),
        "{\"external\":true}"
    );
    store
        .conn
        .execute("DELETE FROM config_operations WHERE id='qa-blocking'", [])
        .unwrap();
    let path = temp.path().join("codex/config.toml");
    put(&path, "model='external'");
    let pending = Pending {
        target: Target::Codex,
        changes: vec![Change {
            path,
            before: None,
            after: Some("model='after'".into()),
        }],
        baseline: None,
        active: None,
        provider_update: None,
        applied_summary: None,
        accepted_source: None,
    };
    store.conn.execute("INSERT INTO operations(id,status,data,created_at) VALUES('qa-old-blocking','pending',?1,1)",[json(&pending).unwrap()]).unwrap();
    let file = store.config_specs(ClientKind::Zcode).unwrap()[0].0.clone();
    let id = client_config::revision(&file, None);
    let document = store.read_client_config(ClientKind::Zcode, &id).unwrap();
    assert_eq!(
        store.save_client_config(document).unwrap_err().code,
        "recovery_conflict"
    );
    assert!(!file.exists());
}

#[test]
fn client_config_desktop_inventory_exposes_selected_uuid_and_ignores_traversal() {
    let (temp, mut store) = fixture();
    let library = temp.path().join("claude_desktop/Claude-3p/configLibrary");
    let id = "5cb1f4d8-2572-41de-80a1-b2e56570ba7e";
    let profile = library.join(format!("{id}.json"));
    put(&profile, "{\"inferenceProvider\":\"gateway\"}");
    put(
        &library.join("_meta.json"),
        &json!({"appliedId":id,"entries":[]}).to_string(),
    );
    let status = store
        .client_config_status(ClientKind::ClaudeDesktop)
        .unwrap();
    let selected = status
        .files
        .iter()
        .find(|f| f.path.ends_with(&format!("{id}.json")))
        .unwrap();
    let mut document = store
        .read_client_config(ClientKind::ClaudeDesktop, &selected.id)
        .unwrap();
    document.content = "{\"inferenceProvider\":\"gateway\",\"说明\":\"手动\"}\n".into();
    assert!(store.save_client_config(document).unwrap().changed);
    put(
        &library.join("_meta.json"),
        "{\"appliedId\":\"../../private\"}",
    );
    assert!(!store
        .client_config_status(ClientKind::ClaudeDesktop)
        .unwrap()
        .files
        .iter()
        .any(|f| std::path::Path::new(&f.path).ends_with("private.json")));
}
#[cfg(windows)]
#[test]
fn client_config_rejects_reparse_point_and_does_not_modify_link_destination() {
    let (temp, store) = fixture();
    let actual = temp.path().join("actual.json");
    put(&actual, "{\"keep\":true}");
    let link = temp.path().join("workbuddy/models.json");
    std::fs::create_dir_all(link.parent().unwrap()).unwrap();
    if let Err(error) = std::os::windows::fs::symlink_file(&actual, &link) {
        assert_eq!(error.kind(), std::io::ErrorKind::PermissionDenied);
        return;
    }
    assert_eq!(
        client_config::check_path(&link).unwrap_err().code,
        "symlink"
    );
    let file = client_config::revision(
        &store.config_specs(ClientKind::Workbuddy).unwrap()[0].0,
        None,
    );
    assert_eq!(
        store
            .read_client_config(ClientKind::Workbuddy, &file)
            .unwrap_err()
            .code,
        "symlink"
    );
    assert_eq!(std::fs::read_to_string(actual).unwrap(), "{\"keep\":true}");
}

#[test]
fn client_config_active_supplier_cannot_be_deleted_and_edits_become_pending() {
    let (_temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    store
        .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Responses, None)
        .unwrap();
    assert_eq!(store.delete(&p.id).unwrap_err().code, "in_use");
    let mut changed = store.provider(&p.id).unwrap();
    changed.summary.base_url = "https://new.example/v1".into();
    store
        .conn
        .execute(
            "UPDATE providers SET data=?1 WHERE id=?2",
            params![json(&changed).unwrap(), p.id],
        )
        .unwrap();
    assert_eq!(
        store.client_config_status(ClientKind::Zcode).unwrap().state,
        "saved_changes"
    );
    store.restore_client_config(ClientKind::Zcode).unwrap();
    store.delete(&p.id).unwrap();
}

#[test]
fn client_config_recovery_rolls_back_partial_multi_file_write_and_finishes_completed_write() {
    let (temp, store) = fixture();
    let patch = temp.path().join("dsh/cordis.patch.yml");
    let creds = temp.path().join("dsh/.credentials.yaml");
    let changes = vec![
        Change {
            path: patch.clone(),
            before: Some("- id: original\n".into()),
            after: Some("- id: after\n".into()),
        },
        Change {
            path: creds.clone(),
            before: None,
            after: Some("version: 1\nrefs: {}\nrecords: {}\n".into()),
        },
    ];
    let pending = ConfigPending {
        client: ClientKind::Dsh,
        changes: changes.clone(),
        state: Some(ExtraState {
            revision: 42,
            ..Default::default()
        }),
        revision: 42,
    };
    put(&patch, changes[0].after.as_deref().unwrap());
    store
        .conn
        .execute(
            "INSERT INTO config_operations(id,status,data) VALUES('qa-partial','pending',?1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    drop(store);
    let mut store = Store::open(temp.path().join("data")).unwrap();
    assert_eq!(std::fs::read_to_string(&patch).unwrap(), "- id: original\n");
    assert!(!creds.exists());
    put(&patch, changes[0].after.as_deref().unwrap());
    put(&creds, changes[1].after.as_deref().unwrap());
    store
        .conn
        .execute(
            "INSERT INTO config_operations(id,status,data) VALUES('qa-complete','pending',?1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    store.recover_config_operations().unwrap();
    assert_eq!(store.extra_state(ClientKind::Dsh).unwrap().revision, 42);
}
#[test]
fn client_config_recovery_does_not_overwrite_concurrent_external_content() {
    let (temp, mut store) = fixture();
    let file = temp.path().join("workbuddy/models.json");
    put(&file, "{\"external\":true}");
    let pending = ConfigPending {
        client: ClientKind::Workbuddy,
        changes: vec![Change {
            path: file.clone(),
            before: Some("{}".into()),
            after: Some("{\"new\":true}".into()),
        }],
        state: Some(ExtraState::default()),
        revision: 5,
    };
    store
        .conn
        .execute(
            "INSERT INTO config_operations(id,status,data) VALUES('qa-conflict','pending',?1)",
            [json(&pending).unwrap()],
        )
        .unwrap();
    assert_eq!(
        store.recover_config_operations().unwrap_err().code,
        "recovery_conflict"
    );
    assert_eq!(
        std::fs::read_to_string(file).unwrap(),
        "{\"external\":true}"
    );
}
#[test]
fn client_config_auth_and_missing_file_restore_noop_editor_preserves_applied_state() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Claude);
    store
        .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Messages, None)
        .unwrap();
    let status = store.client_config_status(ClientKind::Zcode).unwrap();
    let doc = store
        .read_client_config(ClientKind::Zcode, &status.files[0].id)
        .unwrap();
    let value: serde_json::Value = serde_json::from_str(&doc.content).unwrap();
    assert_eq!(
        value["config"]["providerConfigRules"]["providerRules"][0]["config"]["api"]["headers"]
            ["Authorization"],
        "Bearer synthetic-private-api-key"
    );
    assert!(!store.save_client_config(doc).unwrap().changed);
    assert_eq!(
        store.client_config_status(ClientKind::Zcode).unwrap().state,
        "applied"
    );
    store.restore_client_config(ClientKind::Zcode).unwrap();
    assert!(!temp.path().join("zcode/provider_config.json").exists());
    let p = provider(&mut store, Family::Codex);
    let patch = temp.path().join("dsh/cordis.patch.yml");
    put(&patch, "[]\n");
    store
        .apply_client_config(ClientKind::Dsh, &p.id, NativeProtocol::Responses, None)
        .unwrap();
    assert!(std::fs::read_to_string(&patch)
        .unwrap()
        .contains("openai-responses"));
    store.restore_client_config(ClientKind::Dsh).unwrap();
    assert_eq!(std::fs::read_to_string(patch).unwrap(), "[]\n");
}

fn fixture() -> (tempfile::TempDir, Store) {
    // macOS exposes its temporary directory through /var -> /private/var.
    // Use its actual parent so these fixtures exercise real config paths while
    // keeping the editor's symlink protections enabled on every platform.
    let parent = std::env::temp_dir().canonicalize().unwrap();
    let temp = tempfile::tempdir_in(parent).unwrap();
    let mut store = Store::open(temp.path().join("data")).unwrap();
    for client in [
        ClientKind::Codex,
        ClientKind::ClaudeCli,
        ClientKind::ClaudeDesktop,
        ClientKind::Zcode,
        ClientKind::Dsh,
        ClientKind::Workbuddy,
    ] {
        store
            .set_client_config_directory(
                client,
                temp.path().join(client.id()).to_string_lossy().into_owned(),
            )
            .unwrap();
    }
    (temp, store)
}

#[cfg(unix)]
#[test]
fn client_config_rejects_symlink_and_does_not_modify_link_destination() {
    let (temp, store) = fixture();
    let actual = temp.path().join("actual.json");
    put(&actual, r#"{"keep":true}"#);
    let link = temp.path().join("workbuddy/models.json");
    std::fs::create_dir_all(link.parent().unwrap()).unwrap();
    std::os::unix::fs::symlink(&actual, &link).unwrap();
    let status = store
        .client_config_status(ClientKind::Workbuddy)
        .unwrap_err();
    assert_eq!(status.code, "symlink");
    let file = client_config::revision(&link, None);
    assert_eq!(
        store
            .read_client_config(ClientKind::Workbuddy, &file)
            .unwrap_err()
            .code,
        "symlink"
    );
    assert_eq!(std::fs::read_to_string(actual).unwrap(), r#"{"keep":true}"#);
}
fn provider(store: &mut Store, family: Family) -> Provider {
    store
        .save(ProviderInput {
            id: None,
            family,
            name: "中文供应商".into(),
            base_url: "https://gateway.example/gateway/v1".into(),
            api_key: Some("synthetic-private-api-key".into()),
            balance_access_token: None,
            model: "qa-model".into(),
            auth_mode: "bearer".into(),
            reasoning_effort: None,
            codex_options: CodexOptions::default(),
        })
        .unwrap()
}
fn put(path: &std::path::Path, content: &str) {
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, content).unwrap();
}
#[test]
fn client_config_manual_files_validate_noop_conflict_and_directory_binding() {
    let (temp, mut store) = fixture();
    for client in [
        ClientKind::Codex,
        ClientKind::ClaudeCli,
        ClientKind::Zcode,
        ClientKind::Dsh,
        ClientKind::Workbuddy,
    ] {
        let status = store.client_config_status(client).unwrap();
        let document = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        assert!(!std::path::Path::new(&document.path).exists());
        let mut invalid = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        invalid.content = "[broken \"synthetic-private-api-key\"".into();
        let error = store.save_client_config(invalid).unwrap_err();
        assert!(!error.message.contains("synthetic-private-api-key"));
        assert!(!std::path::Path::new(&document.path).exists());
        let mut document = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        document.content = match document.format.as_str() {
            "toml" => "# 中文注释\nmodel='manual'\n",
            "yaml" => "# 中文注释\n- id: manual\n  config: !!js |\n    (() => ({safe: true}))()\n",
            _ => "{\"说明\":\"中文内容\"}\n",
        }
        .into();
        let result = store.save_client_config(document).unwrap();
        assert!(result.changed);
        assert!(std::path::Path::new(&result.backup_path.unwrap()).is_file());
        let document = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        assert!(!store.save_client_config(document).unwrap().changed);
        let mut stale = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        stale.content = if stale.format == "toml" {
            "model='changed'".into()
        } else if stale.format == "yaml" {
            "[]\n".into()
        } else {
            "{}\n".into()
        };
        put(std::path::Path::new(&stale.path), "external-edit");
        assert_eq!(
            store.save_client_config(stale).unwrap_err().code,
            "config_conflict"
        );
        assert_eq!(
            std::fs::read_to_string(&status.files[0].path).unwrap(),
            "external-edit"
        );
        let old = store
            .read_client_config(client, &status.files[0].id)
            .unwrap();
        store
            .set_client_config_directory(
                client,
                temp.path()
                    .join(format!("{}-new", client.id()))
                    .to_string_lossy()
                    .into_owned(),
            )
            .unwrap();
        assert_eq!(
            store.save_client_config(old).unwrap_err().code,
            "config_file_changed"
        );
    }
    assert!(store
        .read_client_config(ClientKind::Codex, "../../private.json")
        .is_err());
}
#[test]
fn client_config_zcode_preserves_personal_providers_and_restores_exact_source() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Claude);
    let path = temp.path().join("zcode/provider_config.json");
    let original=json!({"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[{"providerId":"keep","enabled":false,"config":{"group":"standard-personal"}}]},"modelConfigRules":{"providerModelRules":[],"manualProviderModelRules":[]},"providerOrder":["keep"]}}).to_string();
    put(&path, &original);
    let write = store
        .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Messages, None)
        .unwrap();
    assert!(write.changed);
    let content: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(
        content["config"]["providerConfigRules"]["providerRules"][0]["providerId"],
        "keep"
    );
    assert_eq!(
        content["config"]["providerConfigRules"]["providerRules"][1]["config"]["api"]["type"],
        "anthropic-messages"
    );
    assert_eq!(
        content["config"]["defaultModelSelection"]["modelId"],
        "qa-model"
    );
    assert!(
        !store
            .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Messages, None)
            .unwrap()
            .changed
    );
    store.restore_client_config(ClientKind::Zcode).unwrap();
    assert_eq!(std::fs::read_to_string(path).unwrap(), original);
}
#[test]
fn client_config_dsh_tags_credentials_and_messages_base_are_preserved() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Claude);
    let patch = temp.path().join("dsh/cordis.patch.yml");
    let creds = temp.path().join("dsh/.credentials.yaml");
    let original = "# 中文配置保持原样\n- id: custom\n  config: !!js |\n    ({ enabled: true })\n";
    put(&patch, original);
    put(&creds, "version: 1\nrefs:\n  KEEP: existing\nrecords: {}\n");
    let original_creds = std::fs::read_to_string(&creds).unwrap();
    store
        .apply_client_config(ClientKind::Dsh, &p.id, NativeProtocol::Messages, None)
        .unwrap();
    let text = std::fs::read_to_string(&patch).unwrap();
    assert!(text.starts_with(original));
    let doc: serde_yaml_ng::Value = serde_yaml_ng::from_str(&text).unwrap();
    assert_eq!(
        doc[1]["insert"][0]["config"]["providers"]["uni-switch"]["baseURL"],
        serde_yaml_ng::Value::String("https://gateway.example/gateway".into())
    );
    assert_eq!(
        doc[2]["config"]["provider"],
        serde_yaml_ng::Value::String("uni-switch".into())
    );
    let credential: serde_yaml_ng::Value =
        serde_yaml_ng::from_str(&std::fs::read_to_string(&creds).unwrap()).unwrap();
    assert_eq!(
        credential["refs"]["KEEP"],
        serde_yaml_ng::Value::String("existing".into())
    );
    assert_eq!(
        credential["refs"]["UNI_SWITCH_DSH_API_KEY"],
        serde_yaml_ng::Value::String("synthetic-private-api-key".into())
    );
    assert!(
        !store
            .apply_client_config(ClientKind::Dsh, &p.id, NativeProtocol::Messages, None)
            .unwrap()
            .changed
    );
    store.restore_client_config(ClientKind::Dsh).unwrap();
    assert_eq!(std::fs::read_to_string(&patch).unwrap(), original);
    assert_eq!(std::fs::read_to_string(&creds).unwrap(), original_creds);
}
#[test]
fn client_config_workbuddy_chat_merges_and_rejects_unsupported_protocol_before_write() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    let path = temp.path().join("workbuddy/models.json");
    put(&path,"{\"models\":[{\"id\":\"keep\",\"custom\":42}],\"availableModels\":[\"keep\"],\"other\":true}");
    let original = std::fs::read_to_string(&path).unwrap();
    assert_eq!(
        store
            .apply_client_config(
                ClientKind::Workbuddy,
                &p.id,
                NativeProtocol::Responses,
                None
            )
            .unwrap_err()
            .code,
        "unsupported_protocol"
    );
    assert_eq!(std::fs::read_to_string(&path).unwrap(), original);
    store
        .apply_client_config(
            ClientKind::Workbuddy,
            &p.id,
            NativeProtocol::ChatCompletions,
            None,
        )
        .unwrap();
    let doc: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(doc["models"][0]["custom"], 42);
    assert_eq!(
        doc["models"][1]["url"],
        "https://gateway.example/gateway/v1/chat/completions"
    );
    assert_eq!(doc["availableModels"], json!(["keep", "qa-model"]));
    assert_eq!(doc["other"], true);
    store.restore_client_config(ClientKind::Workbuddy).unwrap();
    assert_eq!(std::fs::read_to_string(&path).unwrap(), original);
    let claude = provider(&mut store, Family::Claude);
    assert_eq!(
        store
            .apply_client_config(
                ClientKind::Workbuddy,
                &claude.id,
                NativeProtocol::ChatCompletions,
                None
            )
            .unwrap_err()
            .code,
        "unsupported_protocol"
    );
}
#[test]
fn client_config_manual_provider_edits_require_exact_snapshot_confirmation_and_backup() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    store
        .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Responses, None)
        .unwrap();
    let status = store.client_config_status(ClientKind::Zcode).unwrap();
    let mut doc = store
        .read_client_config(ClientKind::Zcode, &status.files[0].id)
        .unwrap();
    let mut value: serde_json::Value = serde_json::from_str(&doc.content).unwrap();
    value["config"]["defaultModelSelection"]["modelId"] = json!("manual-model");
    doc.content = value.to_string();
    store.save_client_config(doc).unwrap();
    assert_eq!(
        store.client_config_status(ClientKind::Zcode).unwrap().state,
        "manual_changes"
    );
    assert_eq!(
        store
            .apply_client_config(ClientKind::Zcode, &p.id, NativeProtocol::Responses, None)
            .unwrap_err()
            .code,
        "config_conflict"
    );
    let confirmed = store
        .client_config_status(ClientKind::Zcode)
        .unwrap()
        .revision;
    put(&temp.path().join("zcode/provider_config.json"), "{}\n");
    assert_eq!(
        store
            .apply_client_config(
                ClientKind::Zcode,
                &p.id,
                NativeProtocol::Responses,
                Some(&confirmed)
            )
            .unwrap_err()
            .code,
        "config_conflict"
    );
}
#[test]
fn client_config_dsh_invalid_credentials_never_partially_writes_patch() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    let patch = temp.path().join("dsh/cordis.patch.yml");
    let creds = temp.path().join("dsh/.credentials.yaml");
    put(&patch, "- id: keep\n");
    put(&creds, "LEGACY_KEY: secret\n");
    assert_eq!(
        store
            .apply_client_config(ClientKind::Dsh, &p.id, NativeProtocol::Responses, None)
            .unwrap_err()
            .code,
        "unsupported_schema"
    );
    assert_eq!(std::fs::read_to_string(patch).unwrap(), "- id: keep\n");
    assert_eq!(
        std::fs::read_to_string(creds).unwrap(),
        "LEGACY_KEY: secret\n"
    );
}
#[test]
fn client_config_existing_managed_target_manual_save_survives_reopen_and_requires_confirmation() {
    let (temp, mut store) = fixture();
    let p = provider(&mut store, Family::Codex);
    store.apply(Target::Codex, &p.id).unwrap();
    let status = store.client_config_status(ClientKind::Codex).unwrap();
    let file = status
        .files
        .iter()
        .find(|f| f.path.ends_with("config.toml"))
        .unwrap();
    let mut doc = store
        .read_client_config(ClientKind::Codex, &file.id)
        .unwrap();
    doc.content = "model='manual-edit'\n".into();
    store.save_client_config(doc).unwrap();
    assert_eq!(
        store.status(Target::Codex).unwrap().state,
        "external_change"
    );
    drop(store);
    let mut store = Store::open(temp.path().join("data")).unwrap();
    assert_eq!(
        std::fs::read_to_string(temp.path().join("codex/config.toml")).unwrap(),
        "model='manual-edit'\n"
    );
    assert_eq!(
        store.apply(Target::Codex, &p.id).unwrap_err().code,
        "external_change"
    );
    let confirmation = store.prepare_apply_overwrite(Target::Codex, &p.id).unwrap();
    store
        .apply_overwrite(Target::Codex, &p.id, &confirmation.token)
        .unwrap();
    assert_eq!(store.status(Target::Codex).unwrap().state, "applied");
}
