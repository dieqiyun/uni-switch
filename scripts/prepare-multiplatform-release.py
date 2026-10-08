"""Verify native CI output and create an exact-source multi-platform release."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("collector", ROOT / "scripts/collect-platform-assets.py")
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)
VERSION = collector.VERSION

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()

def verify_file(path):
    data = path.read_bytes()
    if len(data) < 1024:
        raise RuntimeError(f"Unexpectedly small package: {path.name}")
    if path.suffix == ".exe" and not data.startswith(b"MZ"):
        raise RuntimeError("Invalid Windows executable")
    if path.suffix == ".deb" and not data.startswith(b"!<arch>\n"):
        raise RuntimeError("Invalid Debian archive")
    if path.suffix == ".AppImage" and not data.startswith(b"\x7fELF"):
        raise RuntimeError("Invalid Linux AppImage")
    if path.suffix == ".dmg" and data[-512:-508] != b"koly":
        raise RuntimeError("Invalid UDIF disk image")
    if path.name.endswith("-portable.zip"):
        allowed = {"uni-switch.exe", "使用说明.md", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "licenses/codex-LICENSE", "licenses/dependency-licenses.txt"}
        with zipfile.ZipFile(path) as archive:
            if set(archive.namelist()) != allowed or len(archive.namelist()) != len(allowed) or archive.testzip() is not None:
                raise RuntimeError("Unexpected portable archive content")
    if path.name.endswith(".app.tar.gz"):
        with tarfile.open(path) as archive:
            names = archive.getnames()
            if "uni-switch.app/Contents/MacOS/uni-switch" not in names:
                raise RuntimeError("Missing macOS application executable")
            if any(not n.startswith("uni-switch.app") or ".." in Path(n).parts or n.startswith("/") for n in names):
                raise RuntimeError("Invalid macOS archive paths")

def prepare(artifacts):
    if git("status", "--porcelain"):
        raise RuntimeError("Commit reviewed changes before preparing the release")
    revision = git("rev-parse", "HEAD")
    stage = ROOT / f"release/github/v{VERSION}"
    stage.mkdir(parents=True, exist_ok=True)
    assets = []
    for platform, names in collector.PLATFORMS.items():
        manifest_path = artifacts / f"uni-switch-{platform}/build-manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        if manifest.get("platform") != platform or manifest.get("version") != VERSION or manifest.get("sourceRevision") != revision:
            raise RuntimeError(f"Native build revision/version mismatch for {platform}")
        if [a["name"] for a in manifest["assets"]] != names:
            raise RuntimeError("Native build asset allowlist mismatch")
        actual = {p.name for p in manifest_path.parent.iterdir()}
        if actual != set(names) | {"build-manifest.json"}:
            raise RuntimeError("Unlisted native build artifacts")
        for item in manifest["assets"]:
            source = manifest_path.parent / item["name"]
            if sha(source) != item["sha256"] or source.stat().st_size != item["size"]:
                raise RuntimeError(f"Native artifact checksum mismatch: {source.name}")
            verify_file(source)
            shutil.copy2(source, stage / source.name)
            assets.append(source.name)
    source_name = f"uni-switch_{VERSION}_source.zip"
    subprocess.run(["git", "archive", "--format=zip", f"--prefix=uni-switch-{VERSION}/", "-o", str(stage / source_name), revision], cwd=ROOT, check=True)
    with zipfile.ZipFile(stage / source_name) as archive:
        prefix = f"uni-switch-{VERSION}/"
        names = archive.namelist()
        forbidden = r"(^|/)(\.git|\.qa|\.tools|node_modules|target|release|output|\.codex|\.claude)(/|$)|(^|/)\.env|\.(exe|db|pem|pfx|p12)$"
        if any(not n.startswith(prefix) or re.search(forbidden, n) for n in names):
            raise RuntimeError("Private or generated files in source archive")
        for name in ["LICENSE", "NOTICE", "package.json", "pnpm-lock.yaml", "src/App.tsx", "src-tauri/Cargo.toml", "src-tauri/Cargo.lock", ".github/workflows/build-desktop.yml", "scripts/collect-platform-assets.py"]:
            if prefix + name not in names:
                raise RuntimeError(f"Incomplete source archive: {name}")
    assets += [source_name, "README-zh-CN.md"]
    (stage / "README-zh-CN.md").write_text(collector.tutorial(), encoding="utf-8")
    (stage / "SHA256SUMS.txt").write_text("".join(f"{sha(stage / n)}  {n}\n" for n in assets), encoding="utf-8")
    assets += ["SHA256SUMS.txt"]
    repository = json.loads((ROOT / "release-config.json").read_text(encoding="utf-8"))["githubRepository"]
    manifest = {"version": VERSION, "repository": repository, "sourceRevision": revision, "license": "AGPL-3.0-only", "includeSource": True,
                "assets": [{"name": n, "size": (stage / n).stat().st_size, "sha256": sha(stage / n)} for n in assets]}
    (stage / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    shutil.copy2(ROOT / f"release/notes/{VERSION}.md", stage / "release-notes.md")
    print(json.dumps(manifest, indent=2))

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--artifacts", required=True, type=Path)
    prepare(parser.parse_args().artifacts.resolve())
