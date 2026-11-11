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
    subprocess.run(["node", "scripts/qa-source-core.mjs", "--desktop", str(binary)], cwd=ROOT, check=True)
    print(json.dumps({"platform": platform, "architectures": arches, "embeddedCoreStarted": True}))

if __name__ == "__main__":
    smoke(sys.argv[1])
