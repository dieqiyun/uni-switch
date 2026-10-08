"""Collect only the platform's freshly built, versioned release artifacts."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
VERSION = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))["version"]
PLATFORMS = {
    "windows-x64": [f"uni-switch_{VERSION}_x64-setup.exe", f"uni-switch_{VERSION}_x64.exe", f"uni-switch_{VERSION}_x64-portable.zip"],
    "linux-x64": [f"uni-switch_{VERSION}_amd64.deb", f"uni-switch_{VERSION}_x86_64.AppImage"],
    "macos-universal": [f"uni-switch_{VERSION}_universal.dmg", f"uni-switch_{VERSION}_universal.app.tar.gz"],
}

def tutorial():
    text = (ROOT / "docs/tutorial.md").read_text(encoding="utf-8")
    text = text.replace("(../README.md)", "(https://github.com/dieqiyun/uni-switch)")
    text = text.replace("(sponsors.md)", "(https://github.com/dieqiyun/uni-switch/blob/main/docs/sponsors.md)")
    text = text.replace("(screenshots/", f"(https://raw.githubusercontent.com/dieqiyun/uni-switch/v{VERSION}/docs/screenshots/")
    return text

def only(directory, pattern):
    values = list(directory.glob(pattern))
    if len(values) != 1:
        raise RuntimeError(f"Expected one {pattern} in {directory}, got {len(values)}")
    return values[0]

def collect(platform):
    names = PLATFORMS[platform]
    output = ROOT / "release/ci" / platform
    output.mkdir(parents=True, exist_ok=True)
    base = ROOT / "src-tauri/target"
    build = base / ("universal-apple-darwin/release" if platform == "macos-universal" else "release")
    bundle = build / "bundle"
    if platform == "windows-x64":
        shutil.copy2(only(bundle / "nsis", "*.exe"), output / names[0])
        shutil.copy2(build / "uni-switch.exe", output / names[1])
        notices = {name: ROOT / name for name in ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"]}
        notices.update({"licenses/codex-LICENSE": ROOT / "third-party/codex/LICENSE", "licenses/dependency-licenses.txt": ROOT / "third-party/dependency-licenses.txt"})
        with zipfile.ZipFile(output / names[2], "w", zipfile.ZIP_DEFLATED) as archive:
            archive.write(build / "uni-switch.exe", "uni-switch.exe")
            archive.writestr("使用说明.md", tutorial())
            for name, path in notices.items():
                archive.write(path, name)
    elif platform == "linux-x64":
        shutil.copy2(only(bundle / "deb", "*.deb"), output / names[0])
        shutil.copy2(only(bundle / "appimage", "*.AppImage"), output / names[1])
    else:
        shutil.copy2(only(bundle / "dmg", "*.dmg"), output / names[0])
        app = only(bundle / "macos", "*.app")
        with tarfile.open(output / names[1], "w:gz") as archive:
            archive.add(app, arcname=app.name)
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip():
        raise RuntimeError("Build changed tracked source or lockfiles")
    manifest = {"platform": platform, "version": VERSION, "sourceRevision": revision,
                "assets": [{"name": name, "size": (output / name).stat().st_size,
                            "sha256": hashlib.sha256((output / name).read_bytes()).hexdigest()} for name in names]}
    (output / "build-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(manifest, indent=2))

if __name__ == "__main__":
    collect(sys.argv[1])
