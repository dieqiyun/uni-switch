package codexsummary

import (
	"reflect"
	"testing"
)

func TestMessage(t *testing.T) {
	msg, ours := Message(Encrypted("done: a, b\nnext: c"))
	want := UserMessage(Prefix + "\ndone: a, b\nnext: c")
	if !ours || !reflect.DeepEqual(msg, want) {
		t.Fatalf("uni-switch's compaction read as %v, %v; want %v", msg, ours, want)
	}
	if msg, ours := Message("gAAAAABopaque=="); ours || msg != nil {
		t.Fatalf("OpenAI's own compaction read as uni-switch's: %v", msg)
	}
	if msg, ours := Message(Mark + "%%%"); !ours || msg != nil {
		t.Fatalf("an unreadable summary of uni-switch's: %v, %v; want nil, true", msg, ours)
	}
}
