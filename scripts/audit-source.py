"""Scan the exact Git index for publishable paths and common credential formats.

Does not print matched values. This is a focused release check, not a complete
secret-detection guarantee. Only synthetic URL fixtures under .test are exempt.
"""
from pathlib import Path
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
    "credential_url": rb"https?://[^\s/@:]+:[^\s/@]+@[^\s\"'`]+",
}
PRIVATE_PATH = r"(^|/)(\.git|\.qa|\.tools|node_modules|target|release|output|\.codex|\.claude)(/|$)|(^|/)\.env|\.(exe|db|sqlite3?|pem|pfx|p12)$"


def audit():
    files = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")[:-1]
    findings, fixtures, size = [], [], 0
    for name in files:
        if re.search(PRIVATE_PATH, name):
            findings.append({"file": name, "type": "private_or_generated_path"})
        data = subprocess.check_output(["git", "show", ":" + name], cwd=ROOT)
        size += len(data)
        for kind, pattern in PATTERNS.items():
            for match in re.finditer(pattern, data):
                item = {"file": name, "line": data[:match.start()].count(b"\n") + 1, "type": kind}
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
