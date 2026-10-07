package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vlno-ai/sdk/cli/internal/session"
)

func TestLostCancellationResumesExactEmptyJSONWithoutClaiming(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("admission")
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	invoke(t, sessionStartArgs(t, f, dir, marker), "")
	status, out, stderr := invoke(t, []string{"--json", "sessions", "resume", dir}, "")
	if status != 0 {
		t.Fatal(status, out, stderr)
	}
	f.loseReply("cancel")
	status, out, stderr = invoke(t, []string{"--json", "sessions", "cancel", dir}, "")
	if status != 1 || decode(t, out)["cancellation"] != "unknown" || f.snapshot().cancellations != 1 {
		t.Fatal("cancellation did not send exact JSON object or preserve response loss", status, out, stderr)
	}
	store, e := session.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	j, e := store.Read()
	store.Close()
	if e != nil {
		t.Fatal(e)
	}
	actor := j["cancellation"].(map[string]any)["actor"]
	status, out, stderr = invoke(t, []string{"--json", "sessions", "resume", dir}, "")
	if status != 0 || decode(t, out)["cancellation"] != "requested" {
		t.Fatal(status, out, stderr)
	}
	counts := f.snapshot()
	if counts.cancellations != 2 || counts.nexts != 0 || counts.finishes != 0 {
		t.Fatal("cancellation recovery allocated or finished work", counts)
	}
	store, e = session.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	j, e = store.Read()
	if e != nil {
		t.Fatal(e)
	}
	before, _ := session.Encode(actor)
	after, _ := session.Encode(j["cancellation"].(map[string]any)["actor"])
	if string(before) != string(after) {
		t.Fatal("cancellation actor changed")
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("recovery launched a process")
	}
}
