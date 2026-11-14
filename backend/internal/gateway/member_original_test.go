package gateway

import (
	"io"

	"net/http/httptest"

	"testing"
)

// A writer inside the namer that holds a body longer than it, and writes
// it after the namer's finish, still has it reach the agent, named: the
// namer has no finish left to write it from its buffer (#4129).
func TestMemberWriterWrittenAfterFinish(t *testing.T) {
	for _, tc := range []struct {
		name, ct, body, want string
	}{
		{"whole", "application/json", `{"model":"m1","x":1}`, `{"model":"fake/m1","x":1}`},
		{"a stream's last line, cut", "text/event-stream", "data: {\"model\":\"m1\"}\n\ndata: [DO", "data: {\"model\":\"fake/m1\"}\n\ndata: [DO"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m := &memberWriter{w: rec, naming: nameMember, paths: memberPaths}
			m.setMember("fake/m1", "m1")
			m.Header().Set("Content-Type", tc.ct)
			m.WriteHeader(200)
			m.finish()
			io.WriteString(m, tc.body)
			if got := rec.Body.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
