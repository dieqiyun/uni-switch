package gateway

import (
	"testing"
)

// Levels go by the probabilities too: a clear new level is taken over the
// turn before's though its confidence field is low, and a torn one still
// keeps the turn before's.
func TestJevLevelSureByProbabilities(t *testing.T) {
	in := []string{"简单任务", "复杂任务"}
	clear := []byte(`{"model":"clef-flash","answers":{"level":{"type":"choice","choice":"简单任务","probabilities":{"简单任务":0.86,"复杂任务":0.14},"confidence":0.31}},"usage":{"input_tokens":180,"output_tokens":0}}`)
	if v, err := readJev(clear, in, true, before{Intent: "复杂任务"}); err != nil || v.Intent != "简单任务" || v.Sure < 0.71 || v.Sure > 0.73 {
		t.Fatalf("clear: %+v %v", v, err)
	}
	torn := []byte(`{"model":"clef-flash","answers":{"level":{"type":"choice","choice":"简单任务","probabilities":{"简单任务":0.54,"复杂任务":0.46},"confidence":0.02}},"usage":{"input_tokens":180,"output_tokens":0}}`)
	if v, _ := readJev(torn, in, true, before{Intent: "复杂任务"}); v.Intent != "复杂任务" {
		t.Fatalf("torn: %+v", v)
	}
	// an answer without probabilities is as sure as its confidence says,
	// as Jev's are
	if v, _ := readJev([]byte(`{"answers":{"intent":{"type":"choice","choice":"复杂任务","confidence":0.43,"probabilities":{}}}}`), in, false, before{}); v.Intent != "复杂任务" || v.Sure != 0.43 {
		t.Fatalf("no probabilities: %+v", v)
	}
}
