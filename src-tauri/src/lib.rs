pub mod adapters;
#[cfg(feature = "desktop")]
pub mod background;
pub mod bridge;
pub mod discovery;
pub mod error;
pub mod project_links;
pub mod restart;
pub mod service_website;
pub mod store;
pub mod supplier;
pub mod types;
pub mod updates;
pub mod writer;

#[cfg(feature = "desktop")]
mod desktop {
    use crate::{
        error::{AppError, Result},
        store::Store,
        types::*,
    };
    use std::sync::{Arc, Mutex};
    use tauri::{Manager, State};
    type AppState = Arc<Mutex<Store>>;
    fn locked<'a>(state: &'a State<'_, AppState>) -> Result<std::sync::MutexGuard<'a, Store>> {
        state
            .lock()
            .map_err(|_| AppError::new("busy", "配置服务异常，请重新打开应用"))
    }

    #[tauri::command]
    fn get_overview(state: State<AppState>) -> Result<Overview> {
        locked(&state)?.overview()
    }
    #[tauri::command]
    fn get_background_settings() -> Result<crate::background::BackgroundSettings> {
        crate::background::settings()
    }
    #[tauri::command]
    fn get_update_source() -> crate::updates::UpdateSource {
        crate::updates::source()
    }
    #[tauri::command]
    async fn check_app_update() -> Result<crate::updates::UpdateCheck> {
        crate::updates::check().await
    }
    #[tauri::command]
    fn open_app_release(url: String) -> Result<()> {
        crate::updates::open_release(&url)
    }
    #[tauri::command]
    fn open_project_page(page: crate::project_links::ProjectPage) -> Result<()> {
        crate::project_links::open(page)
    }
    #[tauri::command]
    fn open_service_website() -> Result<()> {
        crate::service_website::open()
    }
    #[tauri::command]
    fn set_background_start(enabled: bool) -> Result<crate::background::BackgroundSettings> {
        crate::background::set(enabled)
    }
    #[tauri::command]
    async fn get_runtime_status(
        state: State<'_, AppState>,
        target: Target,
    ) -> Result<serde_json::Value> {
        let (directory, applied_at, required, route) = locked(&state)?.runtime_context(target)?;
        let (client_running, restart_required, desktop) =
            tauri::async_runtime::spawn_blocking(move || {
                let desktop = crate::restart::client_runtime(target, &directory, applied_at);
                if target != Target::Codex {
                    return (desktop.running, desktop.restart_required, desktop);
                }
                let (running, restart) =
                    crate::discovery::client_runtime(target, &directory, applied_at);
                (
                    running || desktop.running,
                    restart || desktop.restart_required,
                    desktop,
                )
            })
            .await
            .map_err(|_| AppError::new("runtime", "客户端状态检查未完成"))?;
        let healthy = match route {
            Some(route) => crate::bridge::healthy(&route).await,
            None => false,
        };
        Ok(
            serde_json::json!({"target":target,"clientRunning":client_running,"restartRequired":restart_required,"bridgeRequired":required,"bridgeHealthy":healthy,"configurationRevision":applied_at,"desktopRunning":target != Target::ClaudeCli && desktop.running,"desktopRestartRequired":target != Target::ClaudeCli && desktop.restart_required,"canRestartDesktop":target != Target::ClaudeCli && desktop.can_restart,"canRestartClient":desktop.can_restart,"restartInProgress":desktop.restart_in_progress,"restartReason":desktop.reason}),
        )
    }
    #[tauri::command]
    async fn restart_codex_desktop(
        state: State<'_, AppState>,
        configuration_revision: u64,
    ) -> Result<crate::restart::RestartResult> {
        restart_client(state, Target::Codex, configuration_revision).await
    }
    #[tauri::command]
    async fn restart_client(
        state: State<'_, AppState>,
        target: Target,
        configuration_revision: u64,
    ) -> Result<crate::restart::RestartResult> {
        let (directory, revision, _, _) = locked(&state)?.runtime_context(target)?;
        if revision != configuration_revision {
            return Err(AppError::new(
                "restart_configuration_changed",
                "配置刚刚发生变化，请稍后重试以加载最新配置",
            ));
        }
        tauri::async_runtime::spawn_blocking(move || {
            crate::restart::restart_client(target, &directory)
        })
        .await
        .map_err(|_| {
            AppError::new(
                "restart_failed",
                "重启操作未完成，请手动退出对应客户端后重新打开；新配置已保存",
            )
        })?
    }
    #[tauri::command]
    fn save_provider(state: State<AppState>, input: ProviderInput) -> Result<Provider> {
        locked(&state)?.save(input)
    }
    #[tauri::command]
    async fn commit_provider(
        state: State<'_, AppState>,
        input: ProviderInput,
        target: Target,
        apply: bool,
    ) -> Result<serde_json::Value> {
        let upstream =
            input
                .codex_options
                .upstream_protocol
                .unwrap_or(if input.family == Family::Codex {
                    input.codex_options.protocol
                } else if input.codex_options.claude_protocol == ClaudeProtocol::Openai {
                    CodexProtocol::Openai
                } else {
                    CodexProtocol::Anthropic
                });
        if apply && ((target == Target::Codex) != (upstream == CodexProtocol::Openai)) {
            let route = locked(&state)?
                .runtime_context(target)?
                .3
                .ok_or_else(|| AppError::new("bridge_unavailable", "兼容服务尚未启动"))?;
            if !crate::bridge::healthy(&route).await {
                return Err(AppError::new(
                    "bridge_unavailable",
                    "兼容服务正在自动恢复，请稍后再使用；原供应商与配置未改变",
                ));
            }
        }
        let (provider, reused) = locked(&state)?.commit_unique_provider(input, target, apply)?;
        let mut result = serde_json::to_value(provider)
            .map_err(|_| AppError::new("serialization", "供应商结果无法读取"))?;
        result["reusedExisting"] = serde_json::json!(reused);
        Ok(result)
    }
    #[tauri::command]
    async fn discover_provider_connection(
        state: State<'_, AppState>,
        input: ConnectionInput,
        protocol: Option<CodexProtocol>,
        auth_mode: Option<String>,
    ) -> Result<ModelSyncResult> {
        let (base_url, key) = locked(&state)?.connection(input)?;
        crate::supplier::discover_connection(&base_url, &key, protocol, auth_mode.as_deref()).await
    }
    #[tauri::command]
    fn delete_provider(state: State<AppState>, provider_id: String) -> Result<()> {
        locked(&state)?.delete(&provider_id)
    }
    #[tauri::command]
    async fn apply_provider(
        state: State<'_, AppState>,
        target: Target,
        provider_id: String,
    ) -> Result<TargetStatus> {
        let (required, route) = {
            let store = locked(&state)?;
            (
                store.provider_requires_bridge(target, &provider_id)?,
                store.runtime_context(target)?.3,
            )
        };
        if required
            && !crate::bridge::healthy(
                &route.ok_or_else(|| AppError::new("bridge_unavailable", "兼容服务尚未启动"))?,
            )
            .await
        {
            return Err(AppError::new(
                "bridge_unavailable",
                "兼容服务正在自动恢复，请稍后重试",
            ));
        }
        locked(&state)?.apply(target, &provider_id)
    }
    #[tauri::command]
    async fn sync_provider_targets(
        state: State<'_, AppState>,
        provider_id: String,
    ) -> Result<serde_json::Value> {
        let targets = locked(&state)?.overview()?.targets;
        let mut results = Vec::new();
        for status in targets
            .into_iter()
            .filter(|s| s.active_provider_id.as_deref() == Some(&provider_id))
        {
            if status.state == "applied" {
                continue;
            }
            let result =
                {
                    let (required, route) = {
                        let store = locked(&state)?;
                        (
                            store.provider_requires_bridge(status.target, &provider_id)?,
                            store.runtime_context(status.target)?.3,
                        )
                    };
                    if required
                        && !crate::bridge::healthy(&route.ok_or_else(|| {
                            AppError::new("bridge_unavailable", "兼容服务尚未启动")
                        })?)
                        .await
                    {
                        Err(AppError::new(
                            "bridge_unavailable",
                            "后台兼容服务正在恢复，请稍后重试",
                        ))
                    } else {
                        locked(&state)?.sync_connection(status.target, &provider_id)
                    }
                };
            match result {
                Ok(_) => results.push(serde_json::json!({"target":status.target,"success":true})),
                Err(error) => results.push(
                    serde_json::json!({"target":status.target,"success":false,"error":error}),
                ),
            }
        }
        Ok(serde_json::json!(results))
    }
    #[tauri::command]
    fn set_provider_fast_mode(
        state: State<AppState>,
        provider_id: String,
        enabled: bool,
    ) -> Result<FastModeResult> {
        locked(&state)?.set_provider_fast_mode(&provider_id, enabled)
    }
    #[tauri::command]
    async fn detect_provider_protocol(
        state: State<'_, AppState>,
        expected: Provider,
    ) -> Result<Provider> {
        let (base, key, protocol, auth) = locked(&state)?.protocol_probe_connection(&expected)?;
        let result =
            crate::supplier::discover_connection(&base, &key, protocol, auth.as_deref()).await?;
        locked(&state)?.accept_protocol_detection(expected, result)
    }
    #[tauri::command]
    fn set_protocol_conversion(
        state: State<AppState>,
        expected: Provider,
        target: Target,
        enabled: bool,
    ) -> Result<ProtocolConversionResult> {
        locked(&state)?.set_protocol_conversion(expected, target, enabled)
    }
    #[tauri::command]
    fn repair_reasoning_levels(
        state: State<AppState>,
        provider_id: String,
    ) -> Result<ReasoningRepairResult> {
        locked(&state)?.repair_reasoning_levels(&provider_id)
    }
    #[tauri::command]
    fn rename_provider(
        state: State<AppState>,
        expected: Provider,
        name: String,
    ) -> Result<Provider> {
        locked(&state)?.rename_provider(expected, name)
    }
    #[tauri::command]
    async fn quick_model_settings(
        state: State<'_, AppState>,
        input: QuickModelInput,
    ) -> Result<ModelWriteResult> {
        let route = {
            let store = locked(&state)?;
            if store.status(input.target)?.active_provider_id.as_deref() == Some(&input.expected.id)
                && store.provider_requires_bridge(input.target, &input.expected.id)?
            {
                store.runtime_context(input.target)?.3
            } else {
                None
            }
        };
        if let Some(route) = route {
            if !crate::bridge::healthy(&route).await {
                return Err(AppError::new(
                    "bridge_unavailable",
                    "兼容服务正在恢复，请稍后重试；此次没有写入",
                ));
            }
        }
        locked(&state)?.quick_model_settings(input)
    }
    #[tauri::command]
    fn update_provider_models(
        state: State<AppState>,
        input: ModelWriteInput,
    ) -> Result<ModelWriteResult> {
        locked(&state)?.update_provider_models(input)
    }
    #[tauri::command]
    fn restore_original(state: State<AppState>, target: Target) -> Result<TargetStatus> {
        locked(&state)?.restore(target)
    }
    #[tauri::command]
    fn import_current(state: State<AppState>, target: Target) -> Result<Provider> {
        locked(&state)?.import(target)
    }
    #[tauri::command]
    fn set_directory(state: State<AppState>, target: Target, directory: String) -> Result<()> {
        locked(&state)?.set_directory(target, directory)
    }

    #[tauri::command]
    async fn sync_provider_models(
        state: State<'_, AppState>,
        input: ConnectionInput,
        auth_mode: Option<String>,
        protocol: Option<CodexProtocol>,
    ) -> Result<ModelSyncResult> {
        let (base_url, key) = locked(&state)?.connection(input)?;
        crate::supplier::sync_models_protocol(
            &base_url,
            &key,
            auth_mode.as_deref().unwrap_or("bearer"),
            protocol == Some(CodexProtocol::Anthropic),
        )
        .await
    }

    #[tauri::command]
    async fn query_provider_balance(
        state: State<'_, AppState>,
        input: ConnectionInput,
        query: Option<BalanceQuery>,
    ) -> Result<BalanceResult> {
        let (base_url, key, fallback, token) =
            locked(&state)?.auto_balance_connection(input, query)?;
        crate::supplier::query_auto_balance(&base_url, &key, fallback, token.as_deref()).await
    }

    pub fn run() {
        let mut context = tauri::generate_context!();
        if std::env::args().any(|a| a == "--background") {
            context.config_mut().app.windows[0].visible = false;
        }
        #[cfg(feature = "qa-webview")]
        let context = {
            let mut context = context;
            context.config_mut().app.windows[0].additional_browser_args = Some(
                "--remote-debugging-port=9223 --disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection".into(),
            );
            context
        };
        let mut builder = tauri::Builder::default();
        if !cfg!(feature = "qa-webview") {
            builder = builder.plugin(tauri_plugin_single_instance::init(|app, args, _| {
                if !args.iter().any(|a| a == "--background") {
                    if let Some(window) = app.get_webview_window("main") {
                        let _ = window.show();
                        let _ = window.unminimize();
                        let _ = window.set_focus();
                    }
                }
            }));
        }
        let result = builder
            .plugin(tauri_plugin_dialog::init())
            .setup(|app| {
                let directory = std::env::var_os("UNI_SWITCH_DATA_DIR")
                    .map(std::path::PathBuf::from)
                    .unwrap_or(app.path().app_local_data_dir()?);
                let mut store = Store::open(directory.clone())?;
                let (route, listener) = crate::bridge::Route::start(&directory)?;
                store.set_bridge_route(route.clone());
                let state = Arc::new(Mutex::new(store));
                app.manage(state.clone());
                tauri::async_runtime::spawn(async move {
                    crate::bridge::maintain(listener, route, state).await;
                });
                let show = tauri::menu::MenuItem::with_id(
                    app,
                    "show",
                    "打开 uni-switch",
                    true,
                    None::<&str>,
                )?;
                let exit = tauri::menu::MenuItem::with_id(
                    app,
                    "exit",
                    "退出应用…",
                    true,
                    None::<&str>,
                )?;
                let menu = tauri::menu::Menu::with_items(app, &[&show, &exit])?;
                let mut tray = tauri::tray::TrayIconBuilder::new()
                    .tooltip("uni-switch · 后台运行")
                    .menu(&menu)
                    .show_menu_on_left_click(false)
                    .on_menu_event(|app, event| match event.id.as_ref() {
                        "show" => {
                            if let Some(window) = app.get_webview_window("main") {
                                let _ = window.show();
                                let _ = window.unminimize();
                                let _ = window.set_focus();
                            }
                        }
                        "exit" => {
                            let required = app.state::<AppState>().lock().is_ok_and(|store| store.bridge_required());
                            if required {
                                use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
                                let handle = app.clone();
                                app.dialog().message("退出后，使用兼容转换的 Codex / Claude 将暂时无法连接。重新打开 uni-switch 即可恢复。").title("退出 uni-switch？").buttons(MessageDialogButtons::OkCancel).show(move |confirmed| { if confirmed { handle.exit(0); } });
                            } else { app.exit(0); }
                        },
                        _ => {}
                    })
                    .on_tray_icon_event(|tray, event| {
                        if matches!(event, tauri::tray::TrayIconEvent::DoubleClick { .. }) {
                            if let Some(window) = tray.app_handle().get_webview_window("main") {
                                let _ = window.show();
                                let _ = window.unminimize();
                                let _ = window.set_focus();
                            }
                        }
                    });
                if let Some(icon) = app.default_window_icon() {
                    tray = tray.icon(icon.clone());
                }
                tray.build(app)?;
                Ok(())
            })
            .on_window_event(|window, event| {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = window.hide();
                }
            })
            .invoke_handler(tauri::generate_handler![
                get_overview,
                get_update_source,
                check_app_update,
                open_app_release,
                open_service_website,
                open_project_page,
                get_background_settings,
                set_background_start,
                sync_provider_targets,
                get_runtime_status,
                restart_codex_desktop,
                restart_client,
                save_provider,
                commit_provider,
                discover_provider_connection,
                delete_provider,
                apply_provider,
                set_provider_fast_mode,
                repair_reasoning_levels,
                update_provider_models,
                quick_model_settings,
                detect_provider_protocol,
                set_protocol_conversion,
                rename_provider,
                restore_original,
                import_current,
                set_directory,
                sync_provider_models,
                query_provider_balance
            ])
            .run(context);
        if let Err(error) = result {
            eprintln!("uni-switch 无法启动：{error}");
        }
    }
}

#[cfg(feature = "desktop")]
pub use desktop::run;
