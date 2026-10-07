//! Isolated Windows window fixture for the restart integration checks.
//! This binary never reads or writes real Codex configuration.
#![cfg_attr(windows, windows_subsystem = "windows")]
#[cfg(windows)]
fn main() {
    use windows_sys::Win32::{
        Foundation::{HWND, LPARAM, LRESULT, WPARAM},
        UI::WindowsAndMessaging::*,
    };
    unsafe extern "system" fn window_proc(
        window: HWND,
        message: u32,
        wp: WPARAM,
        lp: LPARAM,
    ) -> LRESULT {
        match message {
            // Like Codex Windows, closing the window alone retains the app.
            WM_CLOSE => 0,
            WM_QUERYENDSESSION => {
                i32::from(std::env::var_os("UNI_SWITCH_RESTART_IGNORE_CLOSE").is_none()) as LRESULT
            }
            WM_ENDSESSION
                if wp != 0 && std::env::var_os("UNI_SWITCH_RESTART_IGNORE_CLOSE").is_none() =>
            {
                unsafe {
                    DestroyWindow(window);
                }
                0
            }
            WM_DESTROY => {
                unsafe {
                    PostQuitMessage(0);
                }
                0
            }
            _ => unsafe { DefWindowProcW(window, message, wp, lp) },
        }
    }
    let class: Vec<u16> = "Chrome_WidgetWin_1".encode_utf16().chain(Some(0)).collect();
    let title: Vec<u16> = "Isolated Codex restart fixture"
        .encode_utf16()
        .chain(Some(0))
        .collect();
    unsafe {
        let window_class = WNDCLASSW {
            lpfnWndProc: Some(window_proc),
            lpszClassName: class.as_ptr(),
            ..std::mem::zeroed()
        };
        if RegisterClassW(&window_class) == 0 {
            std::process::exit(2);
        }
        let window = CreateWindowExW(
            0,
            class.as_ptr(),
            title.as_ptr(),
            WS_OVERLAPPEDWINDOW,
            0,
            0,
            300,
            200,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            std::ptr::null(),
        );
        if window.is_null() {
            std::process::exit(3);
        }
        let marker = std::env::var_os("UNI_SWITCH_RESTART_MARKER")
            .map(std::path::PathBuf::from)
            .or_else(|| {
                std::env::args().find_map(|arg| {
                    arg.strip_prefix("--user-data-dir=")
                        .map(|p| std::path::PathBuf::from(p).join("restart-fixture.json"))
                })
            });
        if let Some(marker) = marker {
            std::fs::create_dir_all(marker.parent().unwrap()).unwrap();
            let value = serde_json::json!({ "pid":std::process::id(), "codexHome":std::env::var("CODEX_HOME").ok(), "localAppData":std::env::var("LOCALAPPDATA").ok(), "args":std::env::args().skip(1).collect::<Vec<_>>() });
            std::fs::write(marker, value.to_string()).unwrap();
        }
        let mut message: MSG = std::mem::zeroed();
        while GetMessageW(&mut message, std::ptr::null_mut(), 0, 0) > 0 {
            TranslateMessage(&message);
            DispatchMessageW(&message);
        }
    }
}
#[cfg(not(windows))]
fn main() {}
