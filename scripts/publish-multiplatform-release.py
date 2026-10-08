"""Publish only verified build assets and matching public source; verify readback."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent

def publish(gh, resume=False):
    def run(*args):
        return subprocess.check_output([gh, *args], cwd=ROOT, text=True, encoding="utf-8").strip()
    def api(endpoint, *args):
        return json.loads(run("api", endpoint, *args) or "null")
    version = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))["version"]
    stage = ROOT / f"release/github/v{version}"
    manifest = json.loads((stage / "manifest.json").read_text(encoding="utf-8"))
    repository = json.loads((ROOT / "release-config.json").read_text(encoding="utf-8"))["githubRepository"]
    revision = manifest["sourceRevision"]
    expected = {f"uni-switch_{version}_{suffix}" for suffix in ["x64-setup.exe", "x64.exe", "x64-portable.zip", "amd64.deb", "x86_64.AppImage", "universal.dmg", "universal.app.tar.gz", "source.zip"]} | {"README-zh-CN.md", "SHA256SUMS.txt"}
    if manifest["repository"] != repository or manifest["version"] != version or not manifest["includeSource"] or {a["name"] for a in manifest["assets"]} != expected or len(manifest["assets"]) != len(expected):
        raise RuntimeError("Unexpected release manifest")
    for item in manifest["assets"]:
        data = (stage / item["name"]).read_bytes()
        if len(data) != item["size"] or hashlib.sha256(data).hexdigest() != item["sha256"]:
            raise RuntimeError("Local release asset checksum mismatch")
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT).strip() or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip() != revision:
        raise RuntimeError("Current source does not match prepared release")
    repo = api(f"repos/{repository}")
    if repo["private"] or repo["full_name"] != repository:
        raise RuntimeError("Unexpected release repository")
    if api(f"repos/{repository}/git/ref/heads/{repo['default_branch']}")["object"]["sha"] != revision:
        raise RuntimeError("Corresponding source must already be public on main")
    tag = f"v{version}"
    tags = api(f"repos/{repository}/tags?per_page=100")
    matching_tags = [t for t in tags if t["name"] == tag]
    if matching_tags and (len(matching_tags) != 1 or matching_tags[0]["commit"]["sha"] != revision):
        raise RuntimeError("Release tag already points to other source")
    releases = api(f"repos/{repository}/releases?per_page=100")
    matching = [r for r in releases if r["tag_name"] == tag]
    if matching and (not resume or len(matching) != 1 or not matching[0]["draft"]):
        raise RuntimeError("Will not overwrite a published release")
    if resume and not matching:
        raise RuntimeError("No draft to resume")
    if matching and any(a["name"] not in expected for a in matching[0]["assets"]):
        raise RuntimeError("Unlisted assets in existing draft")
    if not matching_tags:
        api(f"repos/{repository}/git/refs", "--method", "POST", "-f", f"ref=refs/tags/{tag}", "-f", f"sha={revision}")
    paths = [str(stage / item["name"]) for item in manifest["assets"]]
    if matching:
        run("release", "edit", tag, "--repo", repository, "--title", f"uni-switch {version}", "--notes-file", str(stage / "release-notes.md"), "--target", revision, "--draft=true")
        run("release", "upload", tag, "--repo", repository, "--clobber", *paths)
    else:
        run("release", "create", tag, "--repo", repository, "--target", revision, "--title", f"uni-switch {version}", "--notes-file", str(stage / "release-notes.md"), "--draft", *paths)
    with tempfile.TemporaryDirectory(prefix="verify-upload-", dir=stage) as directory:
        run("release", "download", tag, "--repo", repository, "--dir", directory)
        for item in manifest["assets"]:
            if hashlib.sha256((Path(directory) / item["name"]).read_bytes()).hexdigest() != item["sha256"]:
                raise RuntimeError("Downloaded checksum mismatch; release remains draft")
    created_releases = api(f"repos/{repository}/releases?per_page=100")
    drafts = [release for release in created_releases if release["tag_name"] == tag]
    if len(drafts) != 1 or not isinstance(drafts[0].get("id"), int) or not drafts[0]["draft"]:
        raise RuntimeError("Expected one created draft release")
    release = api(f"repos/{repository}/releases/{drafts[0]['id']}")
    if release["tag_name"] != tag or not release["draft"] or len(release["assets"]) != len(expected) or {a["name"] for a in release["assets"]} != expected:
        raise RuntimeError("Draft/asset count mismatch")
    if api(f"repos/{repository}/git/ref/tags/{tag}")["object"]["sha"] != revision:
        raise RuntimeError("Final source tag mismatch")
    run("release", "edit", tag, "--repo", repository, "--draft=false", "--latest")
    latest = api(f"repos/{repository}/releases/latest")
    if latest["tag_name"] != tag or latest["draft"] or latest["prerelease"]:
        raise RuntimeError("Public Latest not yet confirmed")
    for item in manifest["assets"]:
        remote = [a for a in latest["assets"] if a["name"] == item["name"]]
        if len(remote) != 1 or remote[0]["size"] != item["size"] or remote[0]["digest"] != "sha256:" + item["sha256"]:
            raise RuntimeError("Public digest/size mismatch")
    result = {"version": version, "url": latest["html_url"], "sourceRevision": revision, "assets": [{"name":a["name"],"size":a["size"],"digest":a["digest"]} for a in latest["assets"]]}
    (ROOT / ".qa").mkdir(exist_ok=True)
    (ROOT / f".qa/published-{version}.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--gh", default="gh")
    parser.add_argument("--resume-draft", action="store_true")
    args = parser.parse_args()
    publish(args.gh, args.resume_draft)
