package command

import (
	"github.com/vlno-ai/sdk/cli/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurableStartCapturesNativeOutputAndNeverLeaksCredentials(t *testing.T) {
	f := sessionServer(t)
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	status, out, stderr := invoke(t, sessionStartArgs(t, f, dir, marker), "")
	if status != 0 {
		t.Fatal(status, out, stderr)
	}
	if !strings.Contains(out, "recorded output is complete") || !strings.Contains(out, "Assessment: Passed") ||
		strings.Contains(out, "sessions resume") {
		t.Fatal("unclear completion output", out)
	}
	data, e := os.ReadFile(filepath.Join(dir, "journal.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{

		productKey,

		scopedKey,

		f.snapshot().claim,
	} {
		if strings.Contains(string(data)+out+stderr, secret) {
			t.Fatal("credential disclosed")
		}
	}
	if !strings.Contains(string(data), "ordinary native output") || !strings.Contains(string(data), "[redacted]") {
		t.Fatal("native transcript not retained")
	}
	launches, _ := os.ReadFile(marker)
	if string(launches) != "one\n" || f.snapshot().nexts != 1 || f.snapshot().finishes != 1 {
		t.Fatal("unexpected repeated launch", f.snapshot().nexts, f.snapshot().finishes)
	}
}
func TestLostFinishResumesExactRequestWithoutLaunchingAgain(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("finish")
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	args := append([]string{"--json"}, sessionStartArgs(t, f, dir, marker)...)
	status, out, stderr := invoke(t, args, "")
	if status != 1 {
		t.Fatal(status, out, stderr)
	}
	if decode(t, out)["finish"] != "unknown" {
		t.Fatal(out)
	}
	_ = decode(t, stderr)
	status, out, stderr = invoke(t, []string{

		"--json",

		"sessions",

		"resume",

		dir,
	}, "")
	if status != 0 || decode(t, out)["finish"] != "accepted" {
		t.Fatal(status, out, stderr)
	}
	launches, _ := os.ReadFile(marker)
	if string(launches) != "one\n" || f.snapshot().nexts != 1 || f.snapshot().finishes != 2 {
		t.Fatal("resume launched or changed protocol", f.snapshot().nexts, f.snapshot().finishes)
	}
}
func TestLostAdmissionResumeDoesNotBeginNewClaim(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("admission")
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	status, _, _ := invoke(t, sessionStartArgs(t, f, dir, marker), "")
	if status != 1 {
		t.Fatal(status)
	}
	status, out, stderr := invoke(t, []string{

		"sessions",

		"resume",

		dir,
	}, "")
	if status != 0 || f.snapshot().nexts != 0 || !strings.Contains(out, "command has not started") {
		t.Fatal(status, out, stderr, f.snapshot().nexts)
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("resume launched process")
	}
}
func TestSessionStatusAndCancelKeepProcessAndOutcomeSeparate(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("admission")
	dir := filepath.Join(t.TempDir(), "session")
	marker := filepath.Join(t.TempDir(), "launches")
	invoke(t, sessionStartArgs(t, f, dir, marker), "")
	invoke(t, []string{

		"sessions",

		"resume",

		dir,
	}, "")
	before := f.snapshot().posts
	status, out, stderr := invoke(t, []string{

		"sessions",

		"status",

		dir,
	}, "")
	if status != 0 || f.snapshot().posts != before || !strings.Contains(out, "Retained evidence: Not retained yet") {
		t.Fatal(status, out, stderr)
	}
	status, out, stderr = invoke(t, []string{

		"sessions",

		"cancel",

		dir,
	}, "")
	if status != 0 || !strings.Contains(out, "Application access cancelled") || f.snapshot().nexts != 0 {
		t.Fatal(status, out, stderr)
	}
	s, e := session.Open(dir, false)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	j, e := s.Read()
	if e != nil || j["execution"].(map[string]any)["state"] != "not_started" {
		t.Fatal("cancel invented process completion", e)
	}
}
