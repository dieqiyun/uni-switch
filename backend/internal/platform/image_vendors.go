package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

type imageQueue struct {
	gate  chan struct{}
	users int
}

var imageQueues = struct {
	sync.Mutex
	values map[string]*imageQueue
}{values: map[string]*imageQueue{}}

func acquireImage(ctx context.Context, key string) (func(), error) {
	imageQueues.Lock()
	q := imageQueues.values[key]
	if q == nil {
		q = &imageQueue{gate: make(chan struct{}, 1)}
		imageQueues.values[key] = q
	}
	q.users++
	imageQueues.Unlock()
	drop := func() {
		imageQueues.Lock()
		q.users--
		if q.users == 0 {
			delete(imageQueues.values, key)
		}
		imageQueues.Unlock()
	}
	select {
	case q.gate <- struct{}{}:
		return func() { <-q.gate; drop() }, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

func imageCount(body gateway.Object) (int, error) {
	n := 1
	if v := body["n"]; v != nil {
		switch x := v.(type) {
		case float64:
			n = int(x)
			if x != float64(n) {
				return 0, errors.New("n 必须为整数")
			}
		case string:
			if json.Unmarshal([]byte(x), &n) != nil {
				return 0, errors.New("n 必须为整数")
			}
		case int:
			n = x
		default:
			return 0, errors.New("n 必须为整数")
		}
	}
	if n < 1 || n > 10 {
		return 0, errors.New("n 必须在 1 到 10 之间")
	}
	return n, nil
}

func modelScopeImage(w http.ResponseWriter, r *http.Request, core *gateway.Service, p Profile, body gateway.Object) {
	n, err := imageCount(body)
	if err != nil {
		bad(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	release, err := acquireImage(ctx, credential(p))
	if err != nil {
		bad(w, 504, "图片队列等待超时")
		return
	}
	defer release()
	req := gateway.Object{"model": body["model"], "prompt": body["prompt"]}
	if size := fmtText(body["size"]); size != "" && size != "auto" {
		req["size"] = size
	}
	if refs := anyList(body["images"]); len(refs) > 0 {
		req["image_url"] = refs
	}
	raw, _ := json.Marshal(req)
	data := []any{}
	fail := func(message string) {
		if len(data) > 0 {
			jsonReply(w, 200, gateway.Object{"created": time.Now().Unix(), "data": data})
		} else {
			bad(w, 502, message)
		}
	}
	for range n {
		var result gateway.Object
		for attempt := 0; ; attempt++ {
			address, e := requestURL(p, "images/generations", false)
			if e != nil {
				fail(e.Error())
				return
			}
			post, _ := http.NewRequestWithContext(ctx, "POST", address, strings.NewReader(string(raw)))
			post.Header.Set("Authorization", "Bearer "+p.APIKey)
			post.Header.Set("Content-Type", "application/json")
			post.Header.Set("X-ModelScope-Async-Mode", "true")
			res, e := mediaCore(core).Client.Do(post)
			if e != nil {
				fail("ModelScope 图片请求失败")
				return
			}
			status := res.StatusCode
			result, e = readObject(res)
			if status == 429 && attempt < 3 {
				select {
				case <-ctx.Done():
					fail("ModelScope 图片等待超时")
					return
				case <-time.After(time.Duration(attempt+1) * 3 * time.Second):
					continue
				}
			}
			if e != nil {
				fail(e.Error())
				return
			}
			break
		}
		task := fmtText(result["task_id"])
		for task != "" {
			select {
			case <-ctx.Done():
				fail("ModelScope 图片任务超时")
				return
			case <-time.After(2 * time.Second):
			}
			if strings.ContainsAny(task, "/\\?#\x00") {
				fail("ModelScope 任务标识无效")
				return
			}
			address, e := requestURL(p, "tasks/"+task, false)
			if e != nil {
				fail(e.Error())
				return
			}
			poll, _ := http.NewRequestWithContext(ctx, "GET", address, nil)
			poll.Header.Set("Authorization", "Bearer "+p.APIKey)
			poll.Header.Set("X-ModelScope-Task-Type", "image_generation")
			res, e := core.Client.Do(poll)
			if e != nil {
				fail("ModelScope 任务读取失败")
				return
			}
			result, e = readObject(res)
			if e != nil {
				fail(e.Error())
				return
			}
			switch strings.ToUpper(fmtText(result["task_status"])) {
			case "SUCCEED", "SUCCEEDED", "SUCCESS":
				task = ""
			case "FAILED", "FAILURE", "CANCELED", "CANCELLED":
				fail("ModelScope 图片任务失败")
				return
			}
		}
		before := len(data)
		for _, u := range anyList(result["output_images"]) {
			if fmtText(u) != "" {
				data = append(data, gateway.Object{"url": u})
			}
		}
		for _, im := range anyList(result["images"]) {
			if u := fmtText(object(im)["url"]); u != "" {
				data = append(data, gateway.Object{"url": u})
			}
		}
		if len(data) == before {
			fail("ModelScope 没有返回图片")
			return
		}
	}
	jsonReply(w, 200, gateway.Object{"created": time.Now().Unix(), "data": data})
}

func openRouterImage(w http.ResponseWriter, r *http.Request, core *gateway.Service, p Profile, body gateway.Object) {
	req := mapCopy(body)
	delete(req, "images")
	delete(req, "image")
	delete(req, "size")
	model := strings.ToLower(fmtText(body["model"]))
	chat := strings.Contains(model, "gemini") || strings.Contains(model, "gpt-5-image")
	path := "images"
	if chat {
		content := []any{gateway.Object{"type": "text", "text": body["prompt"]}}
		for _, u := range anyList(body["images"]) {
			content = append(content, gateway.Object{"type": "image_url", "image_url": gateway.Object{"url": u}})
		}
		delete(req, "prompt")
		req["messages"] = []any{gateway.Object{"role": "user", "content": content}}
		req["modalities"] = []string{"image", "text"}
		req["stream"] = false
		path = "chat/completions"
	} else {
		refs := []any{}
		for _, u := range anyList(body["images"]) {
			refs = append(refs, gateway.Object{"type": "image_url", "image_url": gateway.Object{"url": u}})
		}
		if len(refs) > 0 {
			req["input_references"] = refs
		}
	}
	if size := fmtText(body["size"]); size != "" {
		ratio, _ := videoSize(size)
		if strings.Contains(size, ":") {
			ratio = size
		}
		if ratio != "" {
			if chat {
				req["image_config"] = gateway.Object{"aspect_ratio": ratio}
			} else {
				req["aspect_ratio"] = ratio
			}
		} else {
			req["size"] = size
		}
	}
	raw, _ := json.Marshal(req)
	res, err := upstream(r.Context(), mediaCore(core), p, "POST", path, false, raw, "application/json")
	if err != nil {
		bad(w, 502, "OpenRouter 图片请求失败")
		return
	}
	out, err := readObject(res)
	if err != nil {
		bad(w, 502, err.Error())
		return
	}
	data := anyList(out["data"])
	if chat {
		for _, choice := range anyList(out["choices"]) {
			for _, image := range anyList(object(object(choice)["message"])["images"]) {
				if u := fmtText(object(object(image)["image_url"])["url"]); u != "" {
					if _, b, ok := strings.Cut(u, ";base64,"); strings.HasPrefix(u, "data:") && ok {
						data = append(data, gateway.Object{"b64_json": b})
					} else {
						data = append(data, gateway.Object{"url": u})
					}
				}
			}
		}
	}
	if len(data) == 0 {
		bad(w, 502, "OpenRouter 没有返回图片，请检查模型是否支持图片生成")
		return
	}
	jsonReply(w, 200, gateway.Object{"created": time.Now().Unix(), "data": data, "usage": out["usage"]})
}
