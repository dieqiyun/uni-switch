package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// slowEye is eyeUp with the describing model's answer held: until gate is
// closed for a request that says "hold", and by think for a description.
type slowEye struct {
	eyeUp
	held  chan struct{} // a request that says "hold" has arrived
	gate  chan struct{}
	think time.Duration
	open  func() // closes gate, once
}

func (u *slowEye) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if strings.Contains(string(body), "hold") {
		u.held <- struct{}{}
		<-u.gate
	} else if strings.HasSuffix(modelOf(body), "eye") {
		time.Sleep(u.think)
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	u.eyeUp.ServeHTTP(w, r)
}

// slowEyed is eyed with one request at a time to the provider, so a
// request waits in its lane while another is out.
func slowEyed(t *testing.T, think time.Duration) (*Server, *slowEye) {
	t.Helper()
	fresh(t)
	u := &slowEye{held: make(chan struct{}, 1), gate: make(chan struct{}), think: think}
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(u.gate) }) })
	u.open = func() { once.Do(func() { close(u.gate) }) }
	one := 1
	if err := provider.Save(provider.Provider{ID: "probe", Name: "Probe", Chat: up.URL + "/v1", Key: "key", Models: []string{"text", "eye"}, MaxConcurrency: &one, QueueWait: 60}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("probe", up.URL+"/v1", []catalog.Model{
		{ID: "text", ImageInput: imageInputBool(false)},
		{ID: "eye", Images: true, ImageInput: imageInputBool(true)},
	}); err != nil {
		t.Fatal(err)
	}
	return New(), u
}

func visionLimits(t *testing.T, try, most time.Duration) {
	t.Helper()
	d, m := visionTimeout, visionWaitMost
	visionTimeout, visionWaitMost = try, most
	t.Cleanup(func() { visionTimeout, visionWaitMost = d, m })
}

const pastedImage = `{"model":"probe/text","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`

// #2318 (davisjiahao): the describer's time limit held the wait for its
// provider too, so a description queued behind another request had only
// what the queue left of it, and the image's turn failed. Queued for twice
// the limit, it is still described.
func TestVisionWaitIsntTheDescribersTime(t *testing.T) {
	// 300ms left the describer no room once the lane opened under a loaded
	// run (#2444); queued for twice a 2s limit it still proves the point.
	visionLimits(t, 2*time.Second, 10*time.Second)
	s, u := slowEyed(t, 0)
	held := make(chan int, 1)
	go func() {
		code, _ := postAs(t, s, "", `{"model":"probe/eye","messages":[{"role":"user","content":"hold"}]}`)
		held <- code
	}()
	// the held request has the provider's one slot
	select {
	case <-u.held:
	case <-time.After(5 * time.Second):
		t.Fatal("the held request never reached the provider")
	}
	time.AfterFunc(2*visionTimeout, u.open)
	code, body := postAs(t, s, "", pastedImage)
	if code != 200 {
		t.Fatalf("a description that waited in its lane: %d %s", code, body)
	}
	if got := u.sent("text"); len(got) != 1 || !strings.Contains(got[0], "HELLO") {
		t.Fatalf("text model was sent %v", got)
	}
	if c := <-held; c != 200 {
		t.Fatalf("held request: %d", c)
	}
}

// The limit still ends a describer that takes too long once asked, and the
// usage log says uni-switch stopped waiting, not that the agent canceled.
func TestVisionLimitEndsASlowDescriber(t *testing.T) {
	visionLimits(t, 200*time.Millisecond, 10*time.Second)
	s, _ := slowEyed(t, time.Second)
	code, body := postAs(t, s, "", pastedImage)
	if code != 502 || !strings.Contains(body, "no answer in 200ms") {
		t.Fatalf("slow describer: %d %s", code, body)
	}
	var vision *usage.Record
	for _, r := range usage.Load(time.Time{}) {
		if r.Kind == "vision" {
			vision = &r
		}
	}
	if vision == nil {
		t.Fatal("no vision request in the usage log")
	}
	if vision.Status != 499 || !strings.Contains(vision.Error, "uni-switch stopped waiting") || strings.Contains(vision.Error, "agent canceled") {
		t.Fatalf("vision usage: %d %q", vision.Status, vision.Error)
	}
}
