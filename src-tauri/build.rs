fn main() {
    {
        println!("cargo:rerun-if-changed=../backend");
        println!("cargo:rerun-if-changed=../scripts/build-go.mjs");
        println!("cargo:rerun-if-env-changed=UNI_SWITCH_GO");
        let target = std::env::var("TARGET").expect("TARGET");
        let mut command = std::process::Command::new("node");
        command
            .arg("../scripts/build-go.mjs")
            .args(["--target", &target]);
        if std::env::var_os("CARGO_FEATURE_QA_WEBVIEW").is_some() {
            command.arg("--qa");
        }
        let status = command
            .status()
            .expect("Node.js and Go >= 1.26 are required to build the Go core");
        assert!(status.success(), "Go core build failed");
        let extension = if target.contains("windows") {
            ".exe"
        } else {
            ""
        };
        let core =
            std::path::PathBuf::from(std::env::var_os("CARGO_MANIFEST_DIR").expect("manifest"))
                .join("binaries")
                .join(format!("uni-switch-core-{target}{extension}"));
        println!("cargo:rustc-env=UNI_SWITCH_GO_CORE_PATH={}", core.display());
        println!("cargo:rerun-if-changed=icons/icon.ico");
        println!("cargo:rerun-if-changed=icons/icon.png");
        #[cfg(feature = "desktop")]
        tauri_build::build();
    }
}
