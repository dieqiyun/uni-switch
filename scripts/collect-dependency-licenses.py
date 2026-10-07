"""Collect dependency notices from installed packages; no network or credentials.

Run after pnpm install and cargo fetch. This includes build/dev dependencies
and the current Cargo host target for the platform being distributed.
"""
from pathlib import Path
import json
import os
import shutil
import subprocess

ROOT = Path(__file__).resolve().parent.parent


def license_files(directory, explicit=None):
    files = set()
    if explicit:
        path = directory / explicit
        if path.is_file() and path.resolve().is_relative_to(directory.resolve()):
            files.add(path)
    for pattern in ("LICENSE*", "LICENCE*", "COPYING*", "NOTICE*", "license*", "licence*", "copying*", "notice*", "LICENSES/*"):
        files.update(path for path in directory.glob(pattern) if path.is_file())
    return sorted(files)


def collect():
    cargo = shutil.which("cargo") or str(Path.home() / ".cargo/bin/cargo.exe")
    host = next(line.split(": ", 1)[1] for line in subprocess.check_output(
        [cargo.replace("cargo.exe", "rustc.exe") if cargo.endswith("cargo.exe") else "rustc", "-vV"],
        text=True, encoding="utf-8").splitlines() if line.startswith("host: "))
    metadata = json.loads(subprocess.check_output(
        [cargo, "metadata", "--manifest-path", str(ROOT / "src-tauri/Cargo.toml"),
         "--locked", "--offline", "--filter-platform", host, "--all-features", "--format-version", "1"],
        cwd=ROOT, text=True, encoding="utf-8",
    ))
    entries = []
    resolved = {node["id"] for node in metadata["resolve"]["nodes"]}
    for package in metadata["packages"]:
        if package["id"] not in resolved:
            continue
        if package["name"] == "uni-switch":
            continue
        directory = Path(package["manifest_path"]).parent
        entries.append(("Rust", package["name"], package["version"],
                        package.get("license") or "See upstream license file",
                        package.get("repository") or f'https://crates.io/crates/{package["name"]}/{package["version"]}',
                        directory, package.get("license_file")))
    installed = ROOT / "node_modules/.pnpm"
    if not installed.is_dir():
        raise RuntimeError("Run pnpm install before collecting JavaScript licenses")
    seen = set()
    for modules in sorted(installed.glob("*/node_modules")):
        for directory in sorted(modules.iterdir()):
            packages = directory.iterdir() if directory.name.startswith("@") else [directory]
            for package_dir in packages:
                manifest = package_dir / "package.json"
                if not manifest.is_file():
                    continue
                package = json.loads(manifest.read_text(encoding="utf-8"))
                identity = (package.get("name"), package.get("version"))
                if not all(identity) or identity in seen:
                    continue
                seen.add(identity)
                repository = package.get("repository") or {}
                if isinstance(repository, dict):
                    repository = repository.get("url")
                license_id = package.get("license") or package.get("licenses") or "See upstream license file"
                if not isinstance(license_id, str):
                    license_id = json.dumps(license_id, ensure_ascii=False)
                entries.append(("JavaScript", *identity, license_id,
                                repository or f'https://www.npmjs.com/package/{identity[0]}/v/{identity[1]}',
                                package_dir, None))
    lines = ["uni-switch — Third-party dependency licenses", "",
             "Generated from locked, locally installed packages. Dependencies retain their own licenses.",
             "Both runtime and build/development packages are listed; inclusion does not imply runtime use.",
             "Source versions are also recorded in pnpm-lock.yaml and src-tauri/Cargo.lock.", ""]
    missing = []
    supplements = ROOT / "third-party/dependency-license-supplements"
    provenance = json.loads((supplements / "sources.json").read_text(encoding="utf-8"))
    aliases = {"webview2-com-sys": "webview2-com"}
    for kind, name, version, license_id, source, directory, explicit in sorted(entries, key=lambda e: (e[0], e[1], e[2])):
        lines += ["=" * 72, f"{kind}: {name}@{version}", f"License: {license_id}", f"Source: {source}", ""]
        files = license_files(directory, explicit)
        supplement = supplements / f"{aliases.get(name, name)}.txt"
        if not files and supplement.is_file():
            lines += [f"Supplement source: {provenance.get(aliases.get(name, name), 'See sources.json')}",
                      supplement.read_text(encoding="utf-8").strip(), ""]
            continue
        if not files and kind == "JavaScript" and name.startswith(("@esbuild/", "@rollup/", "@tauri-apps/cli-")):
            parent = "esbuild" if name.startswith("@esbuild/") else "rollup" if name.startswith("@rollup/") else "@tauri-apps/cli"
            parent_entry = next((e for e in entries if e[1] == parent and e[2] == version), None)
            if parent_entry:
                files = license_files(parent_entry[5])
                directory = parent_entry[5]
                lines.append(f"License files from corresponding parent package: {parent}@{version}")
        if not files:
            missing.append(f"{kind}: {name}@{version} ({license_id})")
            lines.append("No standalone license text is shipped in the installed package. See upstream source and license metadata above.")
        for file in files:
            lines += [f"--- {file.relative_to(directory).as_posix()} ---", file.read_text(encoding="utf-8", errors="replace").strip(), ""]
    destination = ROOT / "third-party/dependency-licenses.txt"
    destination.write_text("\n".join(lines) + "\n", encoding="utf-8", newline="\n")
    print(f"Collected {len(entries)} dependency notices ({destination.stat().st_size} bytes).")
    for item in missing:
        print(f"Metadata-only notice: {item}")


if __name__ == "__main__":
    collect()
