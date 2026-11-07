package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
	"github.com/dieqiyun/uni-switch/backend/internal/supplier"
)

func TestRPCBoundariesAndPersistentFrames(t *testing.T) {
	input := `{"version":1,"id":7,"method":"health","params":{}}` + "\n" + `{"version":9,"id":8,"method":"health","params":{}}` + "\n" + `{"version":1,"id":9,"method":"shell.exec","params":{}}` + "\n"
	var output bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("lost protocol frame")
	}
	for i, line := range lines {
		var r contract.Response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.Version != 1 || r.ID != uint64(i+7) {
			t.Fatalf("uncorrelated response: %+v", r)
		}
		if i == 0 && r.Error != nil || i == 1 && (r.Error == nil || r.Error.Code != "core_version") || i == 2 && (r.Error == nil || r.Error.Code != "core_method") {
			t.Fatalf("bad result: %+v", r)
		}
	}
}
func TestInvalidRPCNeverEchoesCredentials(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"id":1,"method":"supplier.discover","params":{"baseUrl":"http://127.0.0.1","apiKey":"synthetic-secret","unexpected":true}}`,
		`{"version":1,"id":1,"method":"supplier.discover","params":{"baseUrl":"http://127.0.0.1","apiKey":"synthetic-secret\nheader"}}`,
		`{"version":1,"id":1,"method":"health"} {"synthetic-secret":true}`,
	} {
		r := Dispatch(context.Background(), supplier.New(), []byte(input))
		data, _ := json.Marshal(r)
		if r.Error == nil || strings.Contains(string(data), "synthetic-secret") {
			t.Fatalf("unsafe error: %s", data)
		}
	}
	oversize := strings.Repeat("x", contract.MaxRequestBytes+1) + "\n"
	var output bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(oversize), &output); err == nil {
		t.Fatal("oversize input accepted")
	}
}

func TestCredentialsKeepExistingDesktopLimits(t *testing.T) {
	request := contract.Request{Version: 1, ID: 1, Method: "supplier.discover"}
	params, _ := json.Marshal(contract.Connection{BaseURL: "invalid-url", APIKey: strings.Repeat("k", 5000), AccountToken: func() *string { s := strings.Repeat("t", 8192); return &s }()})
	request.Params = params
	data, _ := json.Marshal(request)
	response := Dispatch(context.Background(), supplier.New(), data)
	if response.Error == nil || response.Error.Code != "invalid_url" {
		t.Fatalf("existing credential length rejected: %+v", response.Error)
	}
}
