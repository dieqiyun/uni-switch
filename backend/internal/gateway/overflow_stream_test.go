package gateway

import (
	"testing"
)

// The error object ASD-max's Codex got (#1944), as the ChatGPT backend
// puts it in a response.failed: a conversation past the model's window.
const overflowError = `{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","type":"invalid_request_error","param":"input","code":"context_length_exceeded"}`

const overflowFailed = `event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":` + overflowError + `}}`

// overflowStream is what the ChatGPT backend answered gpt-5.5 asked with
// about 2 MB of input on 2026-10-13, as it came (its safety_identifier
// left out): a 200, its frames, an error event and response.failed, each
// saying context_length_exceeded.
var overflowStream = []string{
	`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"resp_0aef60959b6fc063016acdd9f0ac6c87d09ebb9c76c496f0c9","object":"response","created_at":1791875568,"status":"in_progress","access_programs":null,"background":false,"completed_at":null,"error":null,"frequency_penalty":0.0,"incomplete_details":null,"instructions":"You are a helpful assistant.","max_output_tokens":null,"max_tool_calls":null,"model":"gpt-5.5","moderation":null,"output":[],"parallel_tool_calls":true,"presence_penalty":0.0,"previous_response_id":null,"prompt_cache_key":"ffbebcaa-3636-42d4-a4ae-ca41c6dcec8e","prompt_cache_retention":"24h","reasoning":{"context":"current_turn","effort":"medium","mode":"standard","summary":null},"safety_identifier":"user-redacted","service_tier":"auto","store":false,"temperature":1.0,"text":{"format":{"type":"text"},"verbosity":"medium"},"tool_choice":"auto","tool_usage":{"image_gen":{"input_tokens":0,"input_tokens_details":{"image_tokens":0,"text_tokens":0},"output_tokens":0,"output_tokens_details":{"image_tokens":0,"text_tokens":0},"total_tokens":0},"web_search":{"num_requests":0}},"tools":[],"top_logprobs":0,"top_p":0.98,"truncation":"disabled","usage":null,"user":null,"metadata":{}},"sequence_number":0}`,
	`event: response.in_progress` + "\n" + `data: {"type":"response.in_progress","response":{"id":"resp_0aef60959b6fc063016acdd9f0ac6c87d09ebb9c76c496f0c9","object":"response","created_at":1791875568,"status":"in_progress","access_programs":null,"background":false,"completed_at":null,"error":null,"frequency_penalty":0.0,"incomplete_details":null,"instructions":"You are a helpful assistant.","max_output_tokens":null,"max_tool_calls":null,"model":"gpt-5.5","moderation":null,"output":[],"parallel_tool_calls":true,"presence_penalty":0.0,"previous_response_id":null,"prompt_cache_key":"ffbebcaa-3636-42d4-a4ae-ca41c6dcec8e","prompt_cache_retention":"24h","reasoning":{"context":"current_turn","effort":"medium","mode":"standard","summary":null},"safety_identifier":"user-redacted","service_tier":"auto","store":false,"temperature":1.0,"text":{"format":{"type":"text"},"verbosity":"medium"},"tool_choice":"auto","tool_usage":{"image_gen":{"input_tokens":0,"input_tokens_details":{"image_tokens":0,"text_tokens":0},"output_tokens":0,"output_tokens_details":{"image_tokens":0,"text_tokens":0},"total_tokens":0},"web_search":{"num_requests":0}},"tools":[],"top_logprobs":0,"top_p":0.98,"truncation":"disabled","usage":null,"user":null,"metadata":{}},"sequence_number":1}`,
	`event: error` + "\n" + `data: {"type":"error","error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again.","param":"input"},"sequence_number":2}`,
	`event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"id":"resp_0aef60959b6fc063016acdd9f0ac6c87d09ebb9c76c496f0c9","object":"response","created_at":1791875568,"status":"failed","access_programs":null,"background":false,"completed_at":null,"error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again."},"frequency_penalty":0.0,"incomplete_details":null,"instructions":"You are a helpful assistant.","max_output_tokens":null,"max_tool_calls":null,"model":"gpt-5.5","moderation":null,"output":[],"parallel_tool_calls":true,"presence_penalty":0.0,"previous_response_id":null,"prompt_cache_key":"ffbebcaa-3636-42d4-a4ae-ca41c6dcec8e","prompt_cache_retention":"24h","reasoning":{"context":"current_turn","effort":"medium","mode":"standard","summary":null},"safety_identifier":"user-redacted","service_tier":"auto","store":false,"temperature":1.0,"text":{"format":{"type":"text"},"verbosity":"medium"},"tool_choice":"auto","tool_usage":{"image_gen":{"input_tokens":0,"input_tokens_details":{"image_tokens":0,"text_tokens":0},"output_tokens":0,"output_tokens_details":{"image_tokens":0,"text_tokens":0},"total_tokens":0},"web_search":{"num_requests":0}},"tools":[],"top_logprobs":0,"top_p":0.98,"truncation":"disabled","usage":null,"user":null,"metadata":{}},"sequence_number":3}`,
}

// An error in a stream that says the conversation is too long for the
// model stands for a 400, not a rate limit: its "context_length_exceeded"
// held quotaWords' "exceeded", and the same request was asked again 7 or 8
// times (#1944). A real rate limit, an overload and a spent quota read as
// before.
func TestStreamOverflowNotRateLimit(t *testing.T) {
	for _, c := range []struct {
		ev   string
		want int
	}{
		{overflowFailed, 400},
		{`event: error` + "\n" + `data: {"type":"error","error":` + overflowError + `}`, 400},
		{`event: response.failed` + "\n" + `data: {"type":"response.failed","response":{"status":"failed","error":{"message":"Rate limit reached for gpt-5.5 in organization org-x on tokens per min (TPM): Limit 30000, Used 25000, Requested 9000. Please try again in 8s.","type":"tokens","param":null,"code":"rate_limit_exceeded"}}}`, 429},
		{overloadedFailed, 529},
		{overflowStream[2], 400},
		{overflowStream[3], 400},
		{`event: error` + "\n" + `data: {"type":"error","error":{"message":"The usage limit has been reached","type":"usage_limit_reached"}}`, 429},
	} {
		kind, status, msg := streamEvent([]byte(c.ev + "\n\n"))
		if kind != eventError || status != c.want {
			t.Errorf("%s: kind %d status %d (want %d) %q", c.ev, kind, status, c.want, msg)
		}
	}
	if quotaSaid(overflowError) || !quotaSaid("You exceeded your current quota, please check your plan and billing details.") {
		t.Error("quotaSaid")
	}
}
