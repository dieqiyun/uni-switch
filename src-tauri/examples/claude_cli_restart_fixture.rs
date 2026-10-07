//! Isolated console client. No real Claude config or conversation is opened.
fn main() {
    let marker = std::env::var_os("UNI_SWITCH_CLI_FIXTURE_MARKER")
        .map(std::path::PathBuf::from)
        .expect("isolated fixture marker");
    let pid = std::process::id();
    let inference_environment_present = [
        "ANTHROPIC_BASE_URL",
        "ANTHROPIC_AUTH_TOKEN",
        "ANTHROPIC_API_KEY",
        "ANTHROPIC_MODEL",
    ]
    .iter()
    .any(|name| std::env::var_os(name).is_some());
    let value = serde_json::json!({
        "pid": pid,
        "configDirectory": std::env::var("CLAUDE_CONFIG_DIR").ok(),
        "currentDirectory": std::env::current_dir().unwrap(),
        "args": std::env::args().skip(1).collect::<Vec<_>>(),
        "inferenceEnvironmentPresent": inference_environment_present,
    });
    std::fs::create_dir_all(marker.parent().unwrap()).unwrap();
    std::fs::write(&marker, value.to_string()).unwrap();
    let exit = marker.with_extension(format!("exit-{pid}"));
    while !exit.exists() {
        std::thread::sleep(std::time::Duration::from_millis(100));
    }
}
