"""Check core behavior vectors against audited, pinned Magpie source.

Usage: python scripts/check-core-reference.py .qa/magpie-core-reference [--write]
Reference code runs only inside .qa. It never becomes a product dependency.
--write regenerates fixtures after verifying all input files against the
previously audited Git-blob and SHA-256 inventory.
"""
from pathlib import Path
import hashlib
import json
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
COMMIT = "665b9aff126e26eb5fa8931b7395feb6002185f3"
source = Path(sys.argv[1]).resolve()
manifest_path = ROOT / "docs/magpie-core-behavior-reference.json"
writing = "--write" in sys.argv
manifest = json.loads((source / "verified.json" if writing else manifest_path).read_text(encoding="utf-8"))
assert manifest["commit"] == COMMIT
for item in manifest["files"]:
    raw = (source / item["path"]).read_bytes()
    blob = hashlib.sha1(b"blob " + str(len(raw)).encode("ascii") + b"\0" + raw).hexdigest()
    assert blob == item["gitBlobSha1"], item["path"]
    assert hashlib.sha256(raw).hexdigest() == item["sha256"], item["path"]


def function(path, name):
    code = (source / path).read_text(encoding="utf-8")
    match = re.search(r"(?m)^func " + re.escape(name) + r"\(", code)
    if match is None:
        raise ValueError(name)
    start = code.index("{", match.end())
    depth, state, i = 1, "", start + 1
    while depth:
        c = code[i]
        if state in ('"', "'", "`"):
            if c == "\\" and state != "`":
                i += 2
                continue
            if c == state:
                state = ""
        elif state == "//":
            if c == "\n":
                state = ""
        elif state == "/*":
            if code[i:i+2] == "*/":
                state = ""
                i += 2
                continue
        elif code[i:i+2] in ("//", "/*"):
            state = code[i:i+2]
            i += 2
            continue
        elif c in ('"', "'", "`"):
            state = c
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
        i += 1
    return code[match.start():i]


program = '''package main
import("crypto/sha256"; "encoding/hex"; "encoding/json"; "fmt"; "hash/fnv"; "os"; "strconv"; "strings")
var catalog = struct{EffortAliases map[string]string}{map[string]string{"light":"low", "extra_high":"xhigh"}}
'''
for name in ["effortOf", "budgetOf", "effortOfBudget"]:
    program += "\n" + function("internal/gateway/ir.go", name)
program += "\n" + function("internal/gateway/chat_call_ids.go", "placedCallID")
program += "\n" + function("internal/gateway/desktop.go", "aliasNumber")
program += r'''
func main(){
 out:=map[string]any{}
 effort:=[]any{};for _,s:=range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "light", "extra_high", "HIGH", "unrecognized", " unrecognized "} {effort=append(effort,[]any{s,effortOf(s)})};out["effort"]=effort
 budgets:=[]any{};for _,s:=range []string{"", "low", "medium", "high", "xhigh", "max", "unknown"} {budgets=append(budgets,[]any{s,budgetOf(s)})};out["budget"]=budgets
 inverse:=[]any{};for _,n:=range []int{-1,0,1,4096,4097,10000,12000,12001,24000,24001,32000,64000} {inverse=append(inverse,[]any{n,effortOfBudget(n)})};out["budgetEffort"]=inverse
 numbers:=[]any{};for _,id:=range []string{"gpt-5.4", "anthropic/claude-opus-4.6-20260205", "deepseek-v4", "中/模型", "m", "GPT-5.4", "vendor/model"} {numbers=append(numbers,[]any{id,aliasNumber(id)})};out["desktopNumber"]=numbers
 ids:=[]any{};for _,x:=range []struct{message,call int;name,args string}{{0,0,"f","{}"},{1,0,"f","{}"},{1,1,"f","{}"},{2,0,"f","{ \"x\": 1 }"},{2,0,"f","{\"x\":1}"},{2,0,"中文","{\"值\":\"😀\"}"}} {quoted,_:=json.Marshal(x.args);ids=append(ids,[]any{x.message,x.call,x.name,x.args,placedCallID(x.message,x.call,x.name,string(quoted))})};out["historyID"]=ids
 if err:=json.NewEncoder(os.Stdout).Encode(out);err!=nil{panic(err)}
}
'''
directory = ROOT / ".qa/core-oracle"
directory.mkdir(parents=True, exist_ok=True)
path = directory / "main.go"
path.write_text(program, encoding="utf-8", newline="\n")
go = ROOT / ".tools/go/bin/go.exe"
result = subprocess.run([str(go) if go.exists() else "go", "run", str(path)], cwd=ROOT / "backend", capture_output=True)
if result.returncode:
    raise RuntimeError(result.stderr.decode("utf-8", errors="replace"))
data = json.loads(result.stdout)
fixture = ROOT / "backend/internal/gateway/testdata/core-reference-vectors.json"
if writing:
    fixture.parent.mkdir(parents=True, exist_ok=True)
    fixture.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    manifest["scope"] = "Pinned source identity and reviewed five-area behavior contracts; not a claim of complete Magpie branch equivalence. Reference functions execute only under .qa."
    manifest["oracle"] = {"generator": "scripts/check-core-reference.py", "fixture": "backend/internal/gateway/testdata/core-reference-vectors.json", "vectors": {k: len(v) for k, v in data.items()}}
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
else:
    assert data == json.loads(fixture.read_text(encoding="utf-8")), "Core oracle differs"
    assert manifest["oracle"]["vectors"] == {k: len(v) for k, v in data.items()}
print(json.dumps({"commit": COMMIT, "verifiedFiles": len(manifest["files"]), "vectors": sum(len(v) for v in data.values()), "result": "matched"}))
