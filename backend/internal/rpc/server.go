// Package rpc exposes a small allowlisted NDJSON API over inherited pipes.
// Management stays on inherited pipes. The Go core also owns the authenticated
// loopback inference listener and client configuration transactions.
package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/configuration"
	"github.com/dieqiyun/uni-switch/backend/internal/contract"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/localgateway"
	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
	"github.com/dieqiyun/uni-switch/backend/internal/platform"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
	"github.com/dieqiyun/uni-switch/backend/internal/supplier"
)

func decode(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func Dispatch(ctx context.Context, service *supplier.Service, line []byte) (response contract.Response) {
	response.Version = contract.Version
	defer func() {
		if recover() != nil {
			response.Result = nil
			response.Error = contract.Fail("core_internal", "Go 后端无法完成此操作")
		}
	}()
	var request contract.Request
	if len(line) > contract.MaxRequestBytes || decode(line, &request) != nil {
		response.Error = contract.Fail("core_request", "Go 后端请求格式无效")
		return
	}
	response.ID = request.ID
	if request.Version != contract.Version {
		response.Error = contract.Fail("core_version", "Go 后端协议版本不匹配，请重新安装完整应用")
		return
	}
	if request.Method == "health" {
		response.Result = map[string]any{"protocolVersion": contract.Version, "implementation": "go", "service": "uni-switch-core"}
		return
	}
	if request.Method == "gateway.listen" || request.Method == "gateway.healthy" {
		var p struct {
			Directory string             `json:"directory"`
			Route     localgateway.Route `json:"route"`
		}
		if decode(request.Params, &p) != nil {
			response.Error = contract.Fail("core_request", "网关参数无效")
			return
		}
		if request.Method == "gateway.healthy" {
			response.Result = localgateway.Healthy(p.Route)
			return
		}
		route, err := localgateway.Start(p.Directory)
		if err != nil {
			var detail *edit.Error
			if errors.As(err, &detail) {
				response.Error = contract.Fail(detail.Code, detail.Message)
			} else {
				response.Error = contract.Fail("bridge_server", "无法启动本地 Go 网关")
			}
		} else {
			response.Result = route
		}
		return
	}
	if strings.HasPrefix(request.Method, "agent.client.") || strings.HasPrefix(request.Method, "configuration.") {
		var result any
		var err error
		if strings.HasPrefix(request.Method, "agent.client.") {
			result, err = agent.ClientRPC(request.Method, request.Params)
		} else {
			result, err = configuration.Dispatch(ctx, request.Method, request.Params)
		}
		if err != nil {
			var detail *edit.Error
			if errors.As(err, &detail) {
				response.Error = contract.Fail(detail.Code, detail.Message)
			} else {
				response.Error = contract.Fail("configuration_error", "配置操作失败，请检查文件和应用数据目录")
			}
		} else {
			response.Result = result
			if result == nil {
				response.Result = json.RawMessage("null")
			}
		}
		return
	}
	if strings.HasPrefix(request.Method, "platform.") {
		result, err := platform.Dispatch(ctx, request.Method, request.Params)
		if err != nil {
			response.Error = contract.Fail("platform_error", err.Error())
		} else {
			response.Result = result
		}
		return
	}
	if strings.HasPrefix(request.Method, "state.") {
		var input state.Input
		decoder := json.NewDecoder(bytes.NewReader(request.Params))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			response.Error = contract.Fail("core_request", "状态存储参数无效")
			return
		}
		result, err := state.Execute(ctx, request.Method, input)
		if err != nil {
			response.Error = contract.Fail("database_error", "本地状态存储操作失败，请检查数据目录和磁盘空间")
		} else {
			response.Result = result
		}
		return
	}
	if request.Method == "agent.plan" {
		var input agent.Input
		if decode(request.Params, &input) != nil {
			response.Error = contract.Fail("core_request", "客户端配置参数无效")
			return
		}
		plan, err := agent.Build(input)
		if err != nil {
			var detailed *edit.Error
			if errors.As(err, &detailed) {
				response.Error = contract.Fail(detailed.Code, detailed.Message)
			} else {
				response.Error = contract.Fail("invalid_config", "无法生成客户端配置")
			}
		} else {
			response.Result = plan
		}
		return
	}
	if request.Method == "agent.primary" {
		var input agent.PrimaryInput
		if decode(request.Params, &input) != nil {
			response.Error = contract.Fail("core_request", "客户端配置参数无效")
			return
		}
		plan, err := agent.Primary(input)
		if err != nil {
			var detailed *edit.Error
			if errors.As(err, &detailed) {
				response.Error = contract.Fail(detailed.Code, detailed.Message)
			} else {
				response.Error = contract.Fail("invalid_config", "无法生成客户端配置")
			}
		} else {
			response.Result = plan
		}
		return
	}
	if strings.HasPrefix(request.Method, "files.") {
		var input struct {
			Path    string        `json:"path"`
			Content *string       `json:"content"`
			Changes []edit.Change `json:"changes"`
		}
		if decode(request.Params, &input) != nil {
			response.Error = contract.Fail("core_request", "文件事务参数无效")
			return
		}
		var err error
		switch request.Method {
		case "files.write":
			err = edit.WriteAtomic(input.Path, input.Content)
		case "files.privateDirectory":
			err = edit.PrivateDirectory(input.Path)
		case "files.apply":
			err = edit.Apply(input.Changes)
		case "files.rollback":
			err = edit.Rollback(input.Changes)
		default:
			response.Error = contract.Fail("core_method", "未知文件操作")
			return
		}
		if err != nil {
			var detailed *edit.Error
			if errors.As(err, &detailed) {
				response.Error = contract.Fail(detailed.Code, detailed.Message)
			} else {
				response.Error = contract.Fail("file_error", "配置文件操作失败")
			}
		} else {
			response.Result = true
		}
		return
	}
	if request.Method == "gateway.catalog" {
		var input gateway.CatalogInput
		if json.Unmarshal(request.Params, &input) != nil {
			response.Error = contract.Fail("core_request", "模型路由参数无效")
			return
		}
		v, e := gateway.Catalog(input)
		if e != nil {
			response.Error = contract.Fail("model_route", e.Error())
		} else {
			response.Result = v
		}
		return
	}
	if request.Method == "gateway.test" {
		var p gateway.Probe
		if decode(request.Params, &p) != nil {
			response.Error = contract.Fail("core_request", "测试参数无效")
			return
		}
		v, e := gateway.NewService().TestConnection(ctx, p)
		if e != nil {
			response.Error = contract.Fail("model_test", e.Error())
		} else {
			response.Result = v
		}
		return
	}
	if strings.HasPrefix(request.Method, "metadata.") {
		switch request.Method {
		case "metadata.parseModels":
			var rows map[string]any
			decoder := json.NewDecoder(bytes.NewReader(request.Params))
			decoder.UseNumber()
			if decoder.Decode(&rows) != nil {
				response.Error = contract.Fail("core_request", "模型目录无效")
				return
			}
			response.Result, response.Error = supplier.ParseModelList(rows)
			if response.Error != nil {
				response.Result = nil
			}
		case "metadata.parse":
			var row map[string]any
			decoder := json.NewDecoder(bytes.NewReader(request.Params))
			decoder.UseNumber()
			if decoder.Decode(&row) != nil {
				response.Error = contract.Fail("core_request", "模型条目无效")
				return
			}
			id, _ := row["id"].(string)
			if id == "" {
				id, _ = row["name"].(string)
			}
			facts := modelmeta.ParseVendor(id, row)
			response.Result = modelmeta.Resolve(facts, nil, modelmeta.Preferences{}, "")
		case "metadata.resolve":
			var b modelmeta.Batch
			if decode(request.Params, &b) != nil {
				response.Error = contract.Fail("core_request", "模型资料参数无效")
				return
			}
			catalog, _, _ := (modelmeta.Cache{Directory: b.CacheDirectory, Fallback: b.FallbackCache}).Load()
			result, err := b.Resolve(catalog)
			if err != nil {
				response.Error = contract.Fail("model_metadata", "无法解析模型资料或用户设置")
				return
			}
			response.Result = result
		case "metadata.status", "metadata.update":
			var p struct {
				CacheDirectory string `json:"cacheDirectory"`
				FallbackCache  string `json:"fallbackCache,omitempty"`
				Force          bool   `json:"force,omitempty"`
			}
			if decode(request.Params, &p) != nil {
				response.Error = contract.Fail("core_request", "模型目录参数无效")
				return
			}
			cache := modelmeta.Cache{Directory: p.CacheDirectory, Fallback: p.FallbackCache}
			if request.Method == "metadata.status" {
				response.Result = cache.Status()
			} else {
				result, err := cache.Update(ctx, p.Force)
				if err != nil {
					response.Error = contract.Fail("model_catalog", err.Error())
				} else {
					response.Result = result
				}
			}
		default:
			response.Error = contract.Fail("core_method", "Go 后端不支持此操作")
		}
		return
	}
	if request.Method != "supplier.discover" && request.Method != "supplier.balance" && request.Method != "supplier.balance.auto" {
		response.Error = contract.Fail("core_method", "Go 后端不支持此操作")
		return
	}
	var c contract.Connection
	if decode(request.Params, &c) != nil || c.BaseURL == "" || strings.TrimSpace(c.APIKey) == "" || strings.IndexFunc(c.APIKey, unicode.IsControl) >= 0 || c.AccountToken != nil && (len(*c.AccountToken) > 8192 || strings.IndexFunc(*c.AccountToken, unicode.IsControl) >= 0) {
		response.Error = contract.Fail("core_request", "Go 后端连接参数无效")
		return
	}
	switch request.Method {
	case "supplier.discover":
		response.Result, response.Error = service.Discover(ctx, c)
	case "supplier.balance":
		response.Result, response.Error = service.QueryBalance(ctx, c)
	case "supplier.balance.auto":
		response.Result, response.Error = service.AutoBalance(ctx, c)
	}
	if response.Error != nil {
		response.Result = nil
	}
	return
}

// Each process serves multiple sequential requests, retaining connection pools
// and bounded in-memory strategy caches. The desktop owns process lifetime.
func Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), contract.MaxRequestBytes+2)
	encoder := json.NewEncoder(output)
	service := supplier.New()
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(scanner.Bytes()) > contract.MaxRequestBytes {
			_ = encoder.Encode(contract.Response{Version: contract.Version, Error: contract.Fail("core_request", "Go 后端请求超过大小限制")})
			return io.ErrShortBuffer
		}
		response := Dispatch(ctx, service, scanner.Bytes())
		data, err := json.Marshal(response)
		if err != nil || len(data) > contract.MaxResponseBytes {
			response = contract.Response{Version: contract.Version, ID: response.ID, Error: contract.Fail("core_response_limit", "Go 后端结果超过大小限制")}
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = encoder.Encode(contract.Response{Version: contract.Version, Error: contract.Fail("core_request", "Go 后端请求超过大小限制")})
		return err
	}
	return nil
}
