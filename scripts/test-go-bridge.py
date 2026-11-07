"""Exercise Rust IPC -> our Go core -> synthetic HTTP. Never opens a Store/client."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def test():
    suffix = ".exe" if os.name == "nt" else ""
    fixture = ROOT / f"src-tauri/target/debug/examples/go_core_fixture{suffix}"
    cores = list((ROOT / "src-tauri/binaries").glob(f"uni-switch-core-*{suffix}"))
    core = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else next(
        (p for p in cores if ("windows" if os.name == "nt" else "apple" if sys.platform == "darwin" else "linux") in p.name), None)
    if not fixture.is_file() or core is None or not core.is_file():
        raise RuntimeError("Build Go core and cargo example go_core_fixture first")
    paths = []
    inference = []
    gateway_cancelled = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            assert self.headers.get('Authorization') == 'Bearer synthetic-bridge-key'
            inference.append((self.path, body))
            if self.path == '/cancel/v1/messages':
                self.send_response(200)
                self.send_header('Content-Type', 'text/event-stream')
                self.end_headers()
                try:
                    for _ in range(100):
                        self.wfile.write(b': waiting\n\n')
                        self.wfile.flush()
                        time.sleep(0.03)
                except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                    gateway_cancelled.set()
                return
            if self.path in ('/core/custom/responses', '/core/cut/responses'):
                self.send_response(200)
                self.send_header('Content-Type', 'text/event-stream')
                self.end_headers()
                if self.path == '/core/cut/responses':
                    events = [{'type':'response.output_text.delta','output_index':0,'delta':'unfinished'}]
                else:
                    item={'type':'custom_tool_call','call_id':'custom_1','name':'apply_patch','input':'*** Begin 中文\\x\n'}
                    events=[{'type':'response.output_item.added','output_index':0,'item':{k:v for k,v in item.items() if k!='input'}},
                            {'type':'response.custom_tool_call_input.delta','output_index':0,'delta':'*** Begin '},
                            {'type':'response.custom_tool_call_input.delta','output_index':0,'delta':'中文\\x\n'},
                            {'type':'response.output_item.done','output_index':0,'item':item},
                            {'type':'response.completed','response':{'status':'completed','output':[item]}}]
                data=''.join('data: '+json.dumps(e,ensure_ascii=False)+'\r\n\r\n' for e in events).encode('utf-8')
                for pos in range(0,len(data),7):
                    self.wfile.write(data[pos:pos+7])
                    self.wfile.flush()
                return
            if self.path == '/core/ids/v1/messages':
                previous=body['messages'][0]['content'][0]['id']
                assert previous=='fc_0', 'Salted ID reached upstream'
                assert body['messages'][1]['content'][0]['tool_use_id']=='fc_0'
                value={'content':[{'type':'tool_use','id':'fc_0','name':'f','input':{},'vendor_extension':'retained'}],
                       'stop_reason':'tool_use','vendor_extension':'retained'}
                self.send_response(200)
                self.send_header('Content-Type','application/json')
                self.end_headers()
                self.wfile.write(json.dumps(value).encode('utf-8'))
                return
            if self.path == '/native-anthropic/v1/messages':
                assert body['model'] == 'synthetic-model'
                assert self.headers.get('anthropic-version') == '2023-06-01'
                if body['stream']:
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/event-stream')
                    self.end_headers()
                    events = [
                        {'type':'message_start','message':{'usage':{'input_tokens':12}}},
                        {'type':'content_block_start','index':0,'content_block':{'type':'thinking','thinking':'','signature':''}},
                        {'type':'content_block_delta','index':0,'delta':{'type':'thinking_delta','thinking':'思考'}},
                        {'type':'content_block_delta','index':0,'delta':{'type':'signature_delta','signature':'synthetic-signature'}},
                        {'type':'content_block_stop','index':0},
                        {'type':'content_block_start','index':1,'content_block':{'type':'tool_use','id':'tool_1','name':'lookup','input':{}}},
                        {'type':'content_block_delta','index':1,'delta':{'type':'input_json_delta','partial_json':'{"q":"中文"}'}},
                        {'type':'content_block_stop','index':1},
                        {'type':'message_delta','delta':{'stop_reason':'tool_use'},'usage':{'output_tokens':9}},
                        {'type':'message_stop'},
                    ]
                    data = ''.join('data: '+json.dumps(e,ensure_ascii=False)+'\r\n\r\n' for e in events).encode('utf-8')
                    for pos in range(0,len(data),7):
                        self.wfile.write(data[pos:pos+7])
                        self.wfile.flush()
                    return
                value = {'content':[{'type':'text','text':'gateway-ok'}],'stop_reason':'end_turn'}
                if any('Create a handoff summary' in str(m) for m in body['messages']):
                    value['content'][0]['text']='handoff: continue the synthetic task'
            elif self.path == '/openai/responses':
                if any(m.get('type') == 'function_call_output' for m in body['input']):
                    assert any(m.get('encrypted_content') == 'synthetic-opaque' for m in body['input'])
                    value={'status':'completed','output':[{'type':'message','content':[{'type':'output_text','text':'tool-result-resumed'}]}]}
                else:
                    value={'status':'completed','output':[{'type':'reasoning','id':'rs','summary':[],'encrypted_content':'synthetic-opaque'}, {'type':'function_call','id':'fc','call_id':'call_1','name':'lookup','arguments':'{"q":"x"}'}]}
            elif self.path == '/chat/chat/completions':
                assert body['stop'] == ['STOP']
                value={'choices':[{'message':{'content':'chat-fallback-ok'},'finish_reason':'stop'}]}
            else:
                self.send_response(404)
                self.end_headers()
                return
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.end_headers()
            self.wfile.write(json.dumps(value).encode())

        def do_GET(self):
            paths.append(self.path)
            if self.path == "/public/api/status":
                assert not self.headers.get("Authorization")
                value = {"success": True, "data": {"quota_display_type": "TOKENS"}}
            else:
                assert (self.headers.get("Authorization") == "Bearer synthetic-bridge-key"
                        or self.path == "/denied/models" and self.headers.get("x-api-key") == "synthetic-bridge-key")
                if self.path == "/gateway/v1/models":
                    value = {"object": "list", "data": [{"id": "gpt-4o", "modalities": {"input": ["text"]}}, {"id": "private-test"}]}
                elif self.path == "/sub/v1/usage":
                    value = {"mode": "unrestricted", "balance": 0}
                elif self.path == "/public/api/usage/token/":
                    value = {"data": {"object": "token_usage", "total_available": 100, "total_used": 20, "total_granted": 120, "unlimited_quota": False}}
                elif self.path == "/denied/models":
                    self.send_response(403)
                    self.end_headers()
                    self.wfile.write(b"do-not-echo-provider-secret")
                    return
                elif self.path == "/slow/models":
                    time.sleep(1)
                    value = {"data": ["stale-response"]}
                else:
                    self.send_response(404)
                    self.end_headers()
                    return
            try:
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(value).encode("utf-8"))
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                pass  # The cancellation case intentionally closes this request.

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    (ROOT / ".qa").mkdir(exist_ok=True)
    data = tempfile.TemporaryDirectory(prefix="modelmeta-", dir=ROOT / ".qa")
    directory = Path(data.name)
    metadata = directory / "model-metadata"
    metadata.mkdir()
    (metadata / "models-dev.json").write_text(json.dumps({"openai": {"models": {
        "gpt-4o": {"id": "gpt-4o", "limit": {"context": 128000, "output": 16384},
                   "modalities": {"input": ["text", "image"]}}
    }}}), encoding="utf-8")
    env = {**os.environ, "UNI_SWITCH_DATA_DIR": str(directory), "TEMP": str(directory), "TMP": str(directory)}
    process = subprocess.Popen([str(fixture), str(core), str(directory)], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.DEVNULL, text=True, encoding="utf-8", env=env)
    replies = queue.Queue()
    reader = threading.Thread(target=lambda: [replies.put(line) for line in process.stdout], daemon=True)
    reader.start()

    def call(method, path, **params):
        request = {"method": method, "baseUrl": f"http://127.0.0.1:{server.server_port}{path}", "apiKey": "synthetic-bridge-key"}
        request.update(params)
        process.stdin.write(json.dumps(request) + "\n")
        process.stdin.flush()
        return json.loads(replies.get(timeout=40))

    def gateway(source, path, body, method='gateway', expect_error=False, **options):
        token='a'*64
        payload={'source':source,'operation':'inference','baseUrl':f'http://127.0.0.1:{server.server_port}{path}',
                 'apiKey':'synthetic-bridge-key','authMode':'bearer','token':token,'authorization':'Bearer '+token,
                 'defaultModel':'synthetic-model','models':[{'id':'synthetic-model','enabled':True,
                    'metadata':{'policy':'magpie','reference':'synthetic','vendor':{'id':'synthetic-model'},
                        'profile':{},'capabilities':{},'sources':{},'effortMapping':{}}}],
                 'body':body,'cacheDirectory':str(directory / 'gateway')}
        payload.update(options)
        envelope=call(method,path,call=payload)
        if expect_error:
            assert 'error' in envelope, 'Unfinished native stream succeeded'
            return envelope['error']
        result=envelope['result']
        if method=='gatewayCancel':
            return result
        assert result['status']==200,result
        return result['body']

    def sse_events(raw):
        return [json.loads(line[6:]) for line in raw.splitlines() if line.startswith('data: ') and line[6:]!='[DONE]']

    try:
        models = call("discover", "/gateway/v1")["result"]
        assert models["protocol"] == "openai" and models["authMode"] == "bearer"
        assert [m["id"] for m in models["models"]] == ["gpt-4o", "private-test"]
        resolved = models["models"][0]["metadata"]
        assert resolved["capabilities"]["imageInput"] is False
        assert resolved["catalog"]["inferredImages"] is True
        assert resolved["profile"]["contextWindow"] == 128000
        assert resolved["sources"]["imageInput"] == "vendor"
        assert models["models"][1]["contextWindow"] is None
        assert models["models"][1]["metadata"]["profile"]["contextWindow"] is None
        draft = models["models"][0]
        draft["profileOverrides"] = {"contextWindow": 1000000, "maxOutputTokens": 2000000}
        draft["capabilityOverrides"] = {"imageInput": False}
        result = call("resolve", "/gateway/v1", models=[draft], providerContext=256000)["result"][0]["metadata"]
        assert result["profile"]["contextWindow"] == 1000000 and result["publishedOutput"] == 1000000
        assert result["sources"]["contextWindow"] == "user:model" and result["capabilities"]["imageInput"] is False
        balance = call("balance", "/sub/v1")["result"]
        assert balance["amount"] == 0 and balance["scope"] == "账户余额"
        token = call("balance", "/public/v1")["result"]
        assert token["amount"] == 100 and token["unit"] == "tokens" and token["scope"] == "密钥额度"
        denied = call("discover", "/denied")
        assert denied["error"]["code"] == "supplier_http"
        assert "synthetic-bridge-key" not in json.dumps(denied) and "do-not-echo-provider-secret" not in json.dumps(denied)
        assert paths.count("/denied/models") == 2  # explicit auth-only retry
        assert call("cancel", "/slow")["result"]["cancelled"] is True
        after_cancel = call("discover", "/gateway/v1")["result"]
        assert [m["id"] for m in after_cancel["models"]] == ["gpt-4o", "private-test"]
        request={'model':'synthetic-model','input':'hi'}
        direct=json.loads(gateway('responses','/native-anthropic',request))
        assert direct['output'][0]['content'][0]['text']=='gateway-ok'
        stream=sse_events(gateway('responses','/native-anthropic',{**request,'stream':True,'tools':[{'type':'function','name':'lookup','parameters':{'type':'object'}}]}))
        complete=next(e['response'] for e in stream if e['type']=='response.completed')
        assert complete['output'][0]['encrypted_content'].startswith('uni-claude1:')
        assert complete['output'][1]['call_id']=='tool_1'
        assert json.loads(complete['output'][1]['arguments'])=={'q':'中文'}
        replay=json.loads(gateway('responses','/native-anthropic',{'model':'synthetic-model','input':complete['output']+[{'type':'function_call_output','call_id':'tool_1','output':'done'}]}))
        assert replay['status']=='completed'
        forwarded=inference[-1][1]['messages'][0]['content']
        assert forwarded[0]['signature']=='synthetic-signature'
        initial={'model':'synthetic-model','messages':[{'role':'user','content':'hi'}]}
        first=json.loads(gateway('messages','/openai',initial))
        tool=next(p for p in first['content'] if p['type']=='tool_use')
        assert '__usr32__' in tool['id']
        resumed=json.loads(gateway('messages','/openai',{**initial,'messages':initial['messages']+[{'role':'assistant','content':first['content']},{'role':'user','content':[{'type':'tool_result','tool_use_id':tool['id'],'content':'done'}]}]}))
        assert resumed['content'][0]['text']=='tool-result-resumed'
        chat=json.loads(gateway('messages','/chat',{**initial,'stop_sequences':['STOP']}))
        assert chat['content'][0]['text']=='chat-fallback-ok'
        custom_events=sse_events(gateway('messages','/core/custom',{**initial,'stream':True},upstream='responses'))
        assert any(e['type']=='message_stop' for e in custom_events)
        custom_args=''.join(e['delta']['partial_json'] for e in custom_events if e['type']=='content_block_delta' and e['delta']['type']=='input_json_delta')
        assert json.loads(custom_args)=={'input':'*** Begin 中文\\x\n'}
        repeated={'model':'synthetic-model','messages':[{'role':'assistant','content':[{'type':'tool_use','id':'fc_0','name':'f','input':{}}]},
                  {'role':'user','content':[{'type':'tool_result','tool_use_id':'fc_0','content':'prior'}]}]}
        native=json.loads(gateway('messages','/core/ids',repeated,upstream='messages',nativeRelay=True))
        repeated_id=native['content'][0]['id']
        assert repeated_id.startswith('usr_repeat1_') and native['vendor_extension']=='retained'
        repeated['messages'][0]['content'][0]['id']=repeated_id
        repeated['messages'][1]['content'][0]['tool_use_id']=repeated_id
        gateway('messages','/core/ids',repeated,upstream='messages',nativeRelay=True)
        gateway('responses','/core/cut',{**request,'stream':True},upstream='responses',nativeRelay=True,expect_error=True)
        compact=json.loads(gateway('responses','/native-anthropic',{'model':'synthetic-model','input':[{'role':'user','content':'synthetic history'},{'type':'compaction_trigger'}]}))
        assert compact['output'][0]['type']=='compaction'
        gateway('responses','/native-anthropic',{'model':'synthetic-model','input':compact['output']+[{'role':'user','content':'continue'}]})
        assert 'handoff: continue' in str(inference[-1][1])
        assert gateway('responses','/cancel',{**request,'stream':True},method='gatewayCancel')['cancelled']
        assert gateway_cancelled.wait(5),'Dropping Rust body did not cancel its owned upstream'
        assert process.poll() is None
        process.stdin.close()
        process.wait(timeout=10)
        assert process.returncode == 0
        assert not list(directory.glob("uni-switch-metadata-*")), "metadata payload survives normal exit"
        print(json.dumps({"rustGoHTTP": "passed", "modelsAndRegistry": True, "zeroBalance": True,
                          "tokenUnitsAndAnonymousStatus": True, "sanitizedAuthErrors": True, "persistentIPC": True,
                          "cancellationDiscardsLateResponse": True, "goMetadataMergeAndUserOverrides": True,
                          "goGatewayBothDirections": True, "fragmentedSSEAndSignedToolReplay": True,
                          "opaqueReasoningMultiTurn": True, "chatRoute": True, "compactionAndResume": True,
                          "customInputIncrementalEscaping": True, "repeatedNativeToolIDsAndRestore": True,
                          "nativeTruncationFailsRustBody": True,
                          "droppingRustBodyCancelsUpstream": True,
                          "normalExitCleansMetadataPayload": True}))
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=10)
        server.shutdown()
        server.server_close()
        data.cleanup()


if __name__ == "__main__":
    test()
