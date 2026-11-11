package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A deleted gateway session's text waits in uni-switch's trash no longer than
// the recording keeps text: the hourly prune erases what has expired there,
// and leaves the note, so Restore still lists the session again.
func TestGatewayDeleteTrashExpires(t *testing.T) {
	gatewayHome(t)
	if err := SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	recent := now.Add(-time.Hour)
	if err := SaveGatewayTurn(GatewayTurn{Agent: "claude", Session: "s1", Time: recent, Output: []Part{{Role: "assistant", Kind: "text", Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	// a bucket from before the retention, as one recorded then would be
	old := now.Add(-9 * 24 * time.Hour).UTC().Format(time.DateOnly)
	oldDir := filepath.Join(gatewayDir(), old, gatewayIdentity("claude", "s1"))
	os.MkdirAll(oldDir, 0o700)
	os.WriteFile(filepath.Join(oldDir, "turns.jsonl"), []byte("{}\n"), 0o600)

	tr, err := DeleteGateway("claude", "s1", "s1", recent)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Items) != 2 {
		t.Fatalf("items %+v", tr.Items)
	}
	if !GatewayHidden()("claude", "s1", recent) || GatewayHidden()("claude", "s1", now) {
		t.Fatal("hidden up to its last request, and not after")
	}
	if err := PruneGatewayConversations(now); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(TrashDir(), "gateway", filepath.Base(tr.Key))
	if _, err := os.Stat(filepath.Join(dir, "files", old)); !os.IsNotExist(err) {
		t.Fatalf("expired text kept in the trash: %v", err)
	}
	keep := filepath.Join(dir, "files", recent.UTC().Format(time.DateOnly))
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unexpired text pruned from the trash: %v", err)
	}
	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	if GatewayHidden()("claude", "s1", recent) {
		t.Fatal("restored, still hidden")
	}
	if got, _ := GatewayTranscript("claude", "s1"); len(got.Parts) == 0 {
		t.Fatal("restore didn't put the unexpired text back")
	}
}

// Delete forever erases the trashed text and keeps the session off the list
// up to its last request.
func TestGatewayPurgeKeepsItHidden(t *testing.T) {
	gatewayHome(t)
	if err := SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	last := time.Now().Add(-time.Hour)
	if err := SaveGatewayTurn(GatewayTurn{Agent: "claude", Session: "s2", Time: last, Output: []Part{{Role: "assistant", Kind: "text", Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	tr, err := DeleteGateway("claude", "s2", "s2", last)
	if err != nil {
		t.Fatal(err)
	}
	if err := Purge(tr.Key); err != nil {
		t.Fatal(err)
	}
	if files, _ := gatewayTrashFiles(); len(files) != 0 || HasGatewayConversations() {
		t.Fatal("purged text still kept")
	}
	if !GatewayHidden()("claude", "s2", last) {
		t.Fatal("purged, listed again")
	}
}

// A session uni-switch recorded nothing of is trashed with an empty list of
// items, not null: the Trash page reads the list (found in a real try, where
// null broke the page).
func TestGatewayDeleteNothingRecorded(t *testing.T) {
	gatewayHome(t)
	last := time.Now().Add(-time.Hour)
	if _, err := DeleteGateway("claude", "s3", "s3", last); err != nil {
		t.Fatal(err)
	}
	list := Trash()
	if len(list) != 1 || list[0].Items == nil || !list[0].Gateway {
		t.Fatalf("trash %+v", list)
	}
	b, _ := json.Marshal(list[0])
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("items not a list: %s", b)
	}
}

// A trash note can't send Restore's moves outside the gateway store.
func TestGatewayRestoreRefusesForeignItems(t *testing.T) {
	gatewayHome(t)
	elsewhere := filepath.Join(t.TempDir(), "x")
	dir := filepath.Join(TrashDir(), "gateway", "n")
	os.MkdirAll(filepath.Join(dir, "files", "2026-10-10"), 0o700)
	writeManifest(dir, Trashed{Agent: "claude", ID: "s", Gateway: true, Items: []moved{{From: elsewhere, Name: "2026-10-10"}}})
	if _, err := Restore("gateway/n"); err == nil {
		t.Fatal("restored to a path outside the gateway store")
	}
	if _, err := os.Stat(elsewhere); !os.IsNotExist(err) {
		t.Fatal("something was moved out")
	}
}
