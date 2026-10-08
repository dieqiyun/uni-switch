"""Run only a newly built app with isolated home/data, without client credentials."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent

def smoke(platform):
    base = ROOT / "src-tauri/target"
    if platform == "macos-universal":
        app = base / "universal-apple-darwin/release/bundle/macos/uni-switch.app"
        binary = app / "Contents/MacOS/uni-switch"
        arches = subprocess.check_output(["lipo", "-archs", str(binary)], text=True).split()
        assert sorted(arches) == ["arm64", "x86_64"], arches
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
    else:
        binary = base / "release" / ("uni-switch.exe" if platform == "windows-x64" else "uni-switch")
        arches = ["x86_64"]
    with tempfile.TemporaryDirectory(prefix="uni-switch-smoke-", ignore_cleanup_errors=True) as temporary:
        run = Path(temporary)
        home = run / "home"
        for name in ["home", "data", "local", "roaming", "webview", "home/.codex", "home/.claude", "home/.config"]:
            (run / name).mkdir(parents=True, exist_ok=True)
        original = "# isolated platform smoke\n"
        config = home / ".codex/config.toml"
        config.write_text(original, encoding="utf-8")
        env = dict(os.environ, HOME=str(home), USERPROFILE=str(home),
                   XDG_CONFIG_HOME=str(home / ".config"), UNI_SWITCH_DATA_DIR=str(run / "data"),
                   CODEX_HOME=str(home / ".codex"), CLAUDE_CONFIG_DIR=str(home / ".claude"),
                   LOCALAPPDATA=str(run / "local"), APPDATA=str(run / "roaming"),
                   WEBVIEW2_USER_DATA_FOLDER=str(run / "webview"))
        with (run / "log.txt").open("wb") as log:
            proc = subprocess.Popen([str(binary), "--background"], env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 30
                while time.monotonic() < deadline:
                    if proc.poll() is not None:
                        raise RuntimeError((run / "log.txt").read_text(encoding="utf-8", errors="replace"))
                    if (run / "data/uni-switch.db").exists():
                        time.sleep(3)
                        if proc.poll() is not None:
                            raise RuntimeError((run / "log.txt").read_text(encoding="utf-8", errors="replace"))
                        assert config.read_text(encoding="utf-8") == original
                        print(json.dumps({"platform": platform, "architectures": arches, "started": True, "databaseInitialized": True, "clientConfigurationUnchanged": True}))
                        return
                    time.sleep(0.2)
                raise RuntimeError("Isolated app did not initialize; files=" + str([str(p.relative_to(run)) for p in run.rglob("*") if p.is_file()]) + "\n" + (run / "log.txt").read_text(encoding="utf-8", errors="replace"))
            finally:
                if proc.poll() is None:
                    proc.terminate()
                    try:
                        proc.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait(timeout=10)

if __name__ == "__main__":
    smoke(sys.argv[1])
