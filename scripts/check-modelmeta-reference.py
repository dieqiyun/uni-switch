"""Generate/check pure-function oracle vectors from the pinned Magpie source.

The reference runs only in an isolated QA program. It is never linked into the
product. Supply an audited checkout/cache at the commit in the manifest.
Usage: python scripts/check-modelmeta-reference.py SOURCE_DIR [--write]
"""
from pathlib import Path
import hashlib
import json
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
REFERENCE = "665b9aff126e26eb5fa8931b7395feb6002185f3"
source = Path(sys.argv[1])
files = {
    path: (source / path).read_text(encoding="utf-8")
    for path in ["internal/catalog/catalog.go", "internal/catalog/samemodel.go",
                 "internal/provider/models.go", "internal/provider/modelprefs.go",
                 "internal/provider/ultra.go"]
}

def function(path, name):
    code = files[path]
    match = re.search(r"(?m)^func (?:\([^\n]+\) )?" + re.escape(name) + r"\(", code)
    if not match:
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
            if c == "\n": state = ""
        elif state == "/*":
            if code[i:i+2] == "*/": state = ""; i += 2; continue
        elif code[i:i+2] in ("//", "/*"):
            state = code[i:i+2]; i += 2; continue
        elif c in ('"', "'", "`"): state = c
        elif c == "{": depth += 1
        elif c == "}": depth -= 1
        i += 1
    return code[match.start():i]

catalog = "internal/catalog/catalog.go"
program = '''package main
import ("encoding/json"; "os"; "regexp"; "slices"; "strings"; "unicode")
const LeastWindow=2048
type mdModel struct { Limit struct { Context,Input int } }
var EffortRank=[]string{"none","minimal","low","medium","high","xhigh","max","ultra"}
var Levels=EffortRank
var EffortAliases=map[string]string{"light":"low","extra_high":"xhigh"}
'''
same = files["internal/catalog/samemodel.go"]
program += same[same.index("// SameModel"):]
for name in ["Window", "Kinds", "window", "mostListed", "mostNamed", "mostGiven", "EffortLevel", "NearestEffort"]:
    program += "\n" + function(catalog, name)
program += "\n" + function("internal/provider/models.go", "OutputWithin")
program += "\n" + function("internal/provider/modelprefs.go", "effortsKept")
ultra = files["internal/provider/ultra.go"]
program += "\n" + ultra[ultra.index("var ultraModels"):]
program += r'''
func main() {
 out:=map[string]any{}
 names:=[]string{" accounts/fireworks/models/DeepSeek_V4p1_Flash-260910 ","claude-opus-5-5@20260801","gpt-4o-2024-08-06","qwen3-2507","kimi-k2-0905","model-261332","GPT_6 SOL","DeepSeek\u00a0V4p1","a--b","x-20261399","k-221231","k-230101","k-391231","k-400101"}
 ids:=[]any{}; for _,s:=range names { ids=append(ids,[]any{s,SameModel(s)}) }; out["sameModel"]=ids
 windows:=[]any{}; for _,n:=range []int{0,1,2047,2048,4095,4096,128000,1000000} { windows=append(windows,[]int{n,Window(n)}) }; out["window"]=windows
 input:=[]any{}; for _,x:=range [][2]int{{272000,400000},{272000,128000},{100,4095},{4095,100},{100,100},{0,0},{0,2048}} { m:=mdModel{}; m.Limit.Input=x[0];m.Limit.Context=x[1];input=append(input,[]int{x[0],x[1],m.window()}) };out["inputWindow"]=input
 modalities:=[]any{}; for _,xs:=range [][]string{nil,{}, {"text","image/png","image/jpeg","audio/mpeg","application/pdf","video/mp4"},{"Text", "unknown/foo",""," text/plain "}} { modalities=append(modalities,[]any{xs,Kinds(xs)}) };out["kinds"]=modalities
 nearest:=[]any{}; for _,xs:=range [][]string{nil,{}, {"low","high"},{"none","high"},{"light","extra_high"},{"high","max"},{"none"},{"LOW","HIGH"}} { for _,want:=range []string{"none","minimal","low","medium","high","xhigh","max","ultra","light","extra_high","unknown"} {nearest=append(nearest,[]any{want,xs,NearestEffort(want,xs)})} };out["nearest"]=nearest
 kept:=[]any{}; for _,xs:=range [][]string{nil,{}, {"low","high"},{"light","extra_high"}} { for _,ys:=range [][]string{nil,{}, {"high","low"},{"max"},{"bad"}} {kept=append(kept,[]any{xs,ys,effortsKept(xs,ys)})} };out["kept"]=kept
 out["numberVotes"]=[]any{[]any{map[int]int{128000:2,1000000:2},mostGiven(map[int]int{128000:2,1000000:2})}}
 out["levelVotes"]=[]any{[]any{map[string]int{"low,high,max":1,"low,high":1},mostListed(map[string]int{"low,high,max":1,"low,high":1})}}
 out["nameVotes"]=[]any{[]any{map[string]int{"Long Name":1,"Name":1},mostNamed(map[string]int{"Long Name":1,"Name":1})}}
 ultra:=[]any{};for _,id:=range []string{"gpt-6-sol","openai/gpt-6.1-sol-2026-09-14","GPT-6-ASTRA-20260914","gpt-6-luna","gpt-6.1-terra","gpt-6-sol-high"} {for _,levels:=range [][]string{nil,{}, {"low","max"},{"max","ultra"}} {ultra=append(ultra,[]any{id,levels,withUltra(id,levels)})}};out["ultra"]=ultra
 output:=[]any{};for _,x:=range [][2]int{{0,128000},{4095,8192},{128000,128000},{1000000,128000},{1000000,0}} {output=append(output,[]int{x[0],x[1],OutputWithin(x[0],x[1])})};out["outputWithin"]=output
 if err:=json.NewEncoder(os.Stdout).Encode(out);err!=nil {panic(err)}
}
'''
directory = ROOT / ".qa/modelmeta-oracle"
directory.mkdir(parents=True, exist_ok=True)
go = ROOT / ".tools/go/bin/go.exe"
if not go.exists(): go = "go"
path = directory / "main.go"
path.write_text(program, encoding="utf-8", newline="\n")
result = subprocess.run([str(go), "run", str(path)], cwd=ROOT / "backend", capture_output=True, check=True)
data = json.loads(result.stdout)
fixture = ROOT / "backend/internal/modelmeta/testdata/reference-vectors.json"
if "--write" in sys.argv:
    fixture.parent.mkdir(parents=True, exist_ok=True)
    fixture.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
else:
    assert data == json.loads(fixture.read_text(encoding="utf-8")), "reference oracle differs"
manifest = {"repository":"https://github.com/yetone/magpie","commit":REFERENCE,
            "sha256":{p:hashlib.sha256((source/p).read_bytes()).hexdigest() for p in files},
            "reviewedSha256":{p:hashlib.sha256((source/p).read_bytes()).hexdigest() for p in [
                "internal/catalog/live.go", "internal/catalog/family.go", "internal/catalog/labs.go",
                "internal/catalog/fresh.go", "internal/catalog/public.go", "internal/catalog/openrouter.go",
                "internal/catalog/gemini_list.go", "internal/provider/presets.go",
                "internal/provider/modelspec.go", "internal/provider/modellist.go",
                "internal/gateway/anthropic.go", "LICENSE"]},
            "vectors":{key:len(value) for key,value in data.items()}}
manifest_path = ROOT / "docs/magpie-modelmeta-reference.json"
if "--write" in sys.argv:
    manifest_path.write_text(json.dumps(manifest,indent=2)+"\n",encoding="utf-8")
else:
    assert manifest == json.loads(manifest_path.read_text(encoding="utf-8")), "reference source changed"
print(json.dumps({"reference":REFERENCE,"vectors":sum(len(v) for v in data.values()),"result":"matched"}))
