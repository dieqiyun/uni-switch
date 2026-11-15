"""Scan the exact Git index for publishable paths and common credential formats.

Does not print matched values. This is a focused release check, not a complete
secret-detection guarantee. Only synthetic URL fixtures under .test are exempt.
"""
from pathlib import Path
import hashlib
from urllib.parse import urlsplit
import json
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
PATTERNS = {
    "api_key": rb"\bsk-(?:[a-fA-F0-9]{32,}|[A-Za-z0-9_-]{40,})\b",
    "github_token": rb"\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{30,})\b",
    "private_key": rb"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----",
    "google_oauth_secret": rb"\bGOCSPX-[A-Za-z0-9_-]{20,}\b",
    "google_oauth_client": rb"\b[0-9]{8,}-[a-z0-9]{20,}\.apps\.googleusercontent\.com\b",
    "credential_url": rb"https?://[^\s/@:]+:[^\s/@]+@[^\s\"'`]+",
}
PRIVATE_PATH = r"(^|/)(\.git|\.qa|\.tools|node_modules|target|release|output|__pycache__|\.codex|\.claude)(/|$)|(^|/)\.env|\.(exe|db|sqlite3?|pem|pfx|p12|pyc)$"


def audit():
    # Read the exact index, including each staged blob, in one batch. Thousands
    # of imported source files must not require thousands of git processes.
    entries = subprocess.check_output(["git", "ls-files", "--stage", "-z"], cwd=ROOT).decode("utf-8").split("\0")[:-1]
    files = []
    for entry in entries:
        header, name = entry.split("\t", 1)
        mode, oid, stage = header.split()
        if stage != "0":
            raise RuntimeError("Resolve the index conflict before auditing: " + name)
        files.append((name, oid))
    data_stream = subprocess.check_output(["git", "cat-file", "--batch"], cwd=ROOT,
                                         input="".join(oid + "\n" for _, oid in files).encode())
    exceptions_path = ROOT / "docs/upstream-source-audit.json"
    exceptions = {x["file"]: x for x in json.loads(exceptions_path.read_text(encoding="utf-8"))} if exceptions_path.exists() else {}
    findings, fixtures, size, cursor = [], [], 0, 0
    for name, oid in files:
        end = data_stream.index(b"\n", cursor)
        returned, kind, length = data_stream[cursor:end].decode().split()
        if returned != oid or kind != "blob":
            raise RuntimeError("Unexpected staged object: " + name)
        cursor = end + 1
        data = data_stream[cursor:cursor + int(length)]
        cursor += int(length) + 1
        exception = exceptions.get(name, {})
        verified = exception.get("sha256") == hashlib.sha256(data).hexdigest() and bool(exception.get("reason"))
        if re.search(PRIVATE_PATH, name) and not (verified and "private_or_generated_path" in exception.get("types", [])):
            findings.append({"file": name, "type": "private_or_generated_path"})
        size += len(data)
        for kind, pattern in PATTERNS.items():
            for match in re.finditer(pattern, data):
                item = {"file": name, "line": data[:match.start()].count(b"\n") + 1, "type": kind}
                fixture_path = name.endswith("_test.go") or "/testdata/" in name or "/tests/" in name
                if verified and kind in exception.get("types", []) and (not kind.startswith("google_oauth_") or fixture_path):
                    fixtures.append(item)
                    continue
                if kind == "credential_url" and name.endswith((".test.ts", ".test.tsx")):
                    host = urlsplit(match.group().decode()).hostname or ""
                    if host.endswith(".test"):
                        fixtures.append(item)
                        continue
                findings.append(item)
    report = {"files": len(files), "bytes": size, "findings": findings, "syntheticFixtures": fixtures}
    destination = ROOT / ".qa/source-audit.json"
    destination.parent.mkdir(exist_ok=True)
    destination.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2))
    if not files or findings:
        sys.exit(1)


if __name__ == "__main__":
    audit()
