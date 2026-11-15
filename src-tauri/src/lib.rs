#[cfg(feature = "desktop")]
mod core;

#[cfg(feature = "desktop")]
pub fn run() {
    use std::sync::Arc;
    use tauri::Manager;
    #[tauri::command]
    fn core_endpoint(state: tauri::State<Arc<core::Core>>) -> String {
        state.endpoint.clone()
    }
    let mut context = tauri::generate_context!();
    if std::env::args().any(|a| a == "--background") {
        context.config_mut().app.windows[0].visible = false;
    }
    #[cfg(feature = "qa-webview")]
    {
        let port = std::env::var("UNI_SWITCH_QA_DEBUG_PORT").ok().and_then(|v| v.parse::<u16>().ok()).filter(|p| *p > 0).unwrap_or(9223);
        context.config_mut().app.windows[0].additional_browser_args = Some(format!("--remote-debugging-port={port} --disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection"));
    }
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_autostart::Builder::new().args(["--background"]).build());
    if !cfg!(feature = "qa-webview") {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, args, _| {
            if !args.iter().any(|a| a == "--background") {
                if let Some(w) = app.get_webview_window("main") {
                    let _ = w.show();
                    let _ = w.unminimize();
                    let _ = w.set_focus();
                }
            }
        }));
    }
    let result = builder
        .setup(|app| {
            let directory = match std::env::var_os("UNI_SWITCH_DATA_DIR") {
                Some(d) => std::path::PathBuf::from(d),
                None => app.path().app_local_data_dir()?,
            };
            if !directory.is_absolute() {
                return Err("UNI_SWITCH_DATA_DIR 必须为绝对路径".into());
            }
            std::fs::create_dir_all(&directory)?;
            let core = Arc::new(core::Core::start(&directory, app.handle().clone())?);
            app.manage(core);
            let show =
                tauri::menu::MenuItem::with_id(app, "show", "打开 uni-switch", true, None::<&str>)?;
            let exit =
                tauri::menu::MenuItem::with_id(app, "exit", "退出应用…", true, None::<&str>)?;
            let menu = tauri::menu::Menu::with_items(app, &[&show, &exit])?;
            let mut tray = tauri::tray::TrayIconBuilder::new()
                .tooltip("uni-switch · 后台运行")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| {
                    if event.id.as_ref() == "show" {
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.unminimize();
                            let _ = w.set_focus();
                        }
                    }
                    if event.id.as_ref() == "exit" {
                        // The page's original quit flow names connected clients and
                        // confirms first. No configuration decision lives in Rust.
                        if let Some(w) = app.get_webview_window("main") {
                            let _ = w.show();
                            let _ = w.unminimize();
                            let _ = w.eval("window.uniSwitchQuit?.()");
                        }
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            tray.build(app)?;
            #[cfg(feature = "qa-webview")]
            if let Some(ms) = std::env::var("UNI_SWITCH_QA_EXIT_AFTER_MS")
                .ok()
                .and_then(|v| v.parse::<u64>().ok())
                .filter(|v| (250..=180_000).contains(v))
            {
                let handle = app.handle().clone();
                std::thread::spawn(move || {
                    std::thread::sleep(std::time::Duration::from_millis(ms));
                    handle.exit(0);
                });
            }
            Ok(())
        })
        .on_window_event(|w, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = w.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![core_endpoint])
        .build(context);
    match result {
        Ok(app) => app.run(|handle, event| {
            if matches!(event, tauri::RunEvent::Exit) {
                if let Some(core) = handle.try_state::<Arc<core::Core>>() {
                    core.shutdown();
                }
            }
        }),
        Err(e) => eprintln!("uni-switch 无法启动：{e}"),
    }
}
