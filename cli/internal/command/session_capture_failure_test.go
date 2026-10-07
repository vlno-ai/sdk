package command

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vlno-ai/sdk/cli/internal/session"
)

func TestLostRunningUploadKeepsErrorIntentAndNeverRelaunches(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("output")
	t.Setenv("D01_BLOCK", "1")
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	args := sessionStartArgs(t, f, dir, marker)
	for i, arg := range args {
		if arg == "--" {
			args = append(append(append([]string{}, args[:i]...), "--env", "D01_BLOCK"), args[i:]...)
			break
		}
	}
	status, out, stderr := invoke(t, append([]string{"--json"}, args...), "")
	if status != 1 || decode(t, out)["capture"] != "incomplete" {
		t.Fatal(status, out, stderr)
	}
	s, e := session.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Read()
	s.Close()
	if e != nil {
		t.Fatal(e)
	}
	finish := j["finish"].(map[string]any)
	if finish["state"] != "unknown" || finish["command"].(map[string]any)["agent_status"] != "error" {
		t.Fatal("upload failure misreported as timeout", finish)
	}
	before := f.snapshot()
	if before.finishes != 0 {
		t.Fatal("failed operation retried a mutation")
	}
	lost := before.events[len(before.events)-1]
	status, out, stderr = invoke(t, []string{
		"--json",
		"sessions",
		"resume",
		dir,
	}, "")
	if status != 0 || decode(t, out)["finish"] != "accepted" {
		t.Fatal(status, out, stderr)
	}
	after := f.snapshot()
	if !reflect.DeepEqual(lost, after.events[len(before.events)]) || after.nexts != 1 || after.finishes != 1 {
		t.Fatal("recovery changed event or repeated process protocol")
	}
	launches, _ := os.ReadFile(marker)
	if string(launches) != "one\n" {
		t.Fatal("process repeated")
	}
}
