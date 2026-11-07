"""Check the fixed Magpie registration-to-client mapping without networking.

Use --source with a downloaded reference tree to additionally verify file hashes
and its onMachine registrations. No upstream code is a build dependency.
"""
import argparse
import hashlib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser()
parser.add_argument("--source", type=Path)
args = parser.parse_args()
reference = json.loads((root / "docs/magpie-clients-reference.json").read_text(encoding="utf-8"))
catalog = json.loads((root / "backend/internal/agent/client-catalog.json").read_text(encoding="utf-8"))
expected = {"codex", "claude_cli", "claude_desktop"} | {c["id"] for c in catalog}
registrations = reference["registrations"]
matched = [r["clientId"] for r in registrations if r["clientId"]]
assert len(matched) == len(set(matched)), "Duplicate client registration"
assert set(matched) == expected, "Reference/client coverage differs"
excluded = {r["function"] for r in registrations if not r["clientId"]}
assert excluded == {"cursor", "devin"}, "Review changed third-party configuration scope"
assert all(r.get("reason") for r in registrations if not r["clientId"])
assert len({c["id"] for c in catalog}) == len(catalog)
assert all(c["protocols"] and c["mode"] in {"file", "launch", "import", "api", "database"} for c in catalog)
if args.source:
    for record in reference["files"]:
        data = (args.source / record["path"]).read_bytes()
        blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        assert blob == record["gitBlobSha1"], record["path"]
        assert hashlib.sha256(data).hexdigest() == record["sha256"], record["path"]
    source = (args.source / "internal/agent/agents.go").read_text(encoding="utf-8")
    section = source[source.index("func onMachine("):source.index("// ---- accessors")]
    functions = re.findall(r"^\s*(\w+)\([^\n]*\),?$", section, re.M)
    assert functions == [r["function"] for r in registrations], "Upstream registration list differs"
print(f"Client reference checked: {len(matched)} configuration entries, {len(catalog)} additional clients, {len(excluded)} native-account-only entries.")
