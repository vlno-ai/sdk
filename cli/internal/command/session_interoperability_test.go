package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func sessionPython(t *testing.T, script string, args ...string) {
	t.Helper()
	root, e := filepath.Abs("../../../python/src")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("python3", append([]string{"-c", script}, args...)...)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("Python session failed: %s %v", out, e)
	}
}

func TestPythonExternalSessionResumesInGoAndReopensInPython(t *testing.T) {
	f := sessionServer(t)
	f.loseReply("finish")
	dir := filepath.Join(t.TempDir(), "external")
	source := f.prep["source"].(map[string]any)
	sessionPython(t, `import os,sys
from vlno import Client,SessionError
client=Client(os.environ['VLNO_ENDPOINT'],os.environ['VLNO_API_KEY'])
with client.assessments.session(sys.argv[1],assessment_id=sys.argv[2],plan_revision_id=sys.argv[3]) as session:
 assert session.connect() is not None
 try:
  session.finish(agent_status='completed')
 except SessionError:
  assert session.store.document['finish']['state']=='unknown'
 else:
  raise AssertionError('response loss was not observed')
`, dir, source["assessmentId"].(string), source["planRevisionId"].(string))
	before := f.snapshot().posts
	status, out, stderr := invoke(t, []string{

		"--json",

		"sessions",

		"status",

		dir,
	}, "")
	if status != 0 || f.snapshot().posts != before || decode(t, out)["process"] != "external_active" {
		t.Fatal(status, out, stderr)
	}
	status, out, stderr = invoke(t, []string{

		"--json",

		"sessions",

		"resume",

		dir,
	}, "")
	report := decode(t, out)
	if status != 0 || report["finish"] != "accepted" || report["capture"] != "incomplete" || f.snapshot().nexts != 1 ||
		f.snapshot().finishes != 2 {
		t.Fatal(status, out, stderr, f.snapshot().nexts, f.snapshot().finishes)
	}
	sessionPython(t, `import sys
from vlno.sessions.store import SessionStore
with SessionStore.open(sys.argv[1]) as store:
 d=store.document
 assert d['execution']['mode']=='external'
 assert d['finish']['command']['agent_status']=='completed' and d['finish']['state']=='accepted'
 assert d['capture']['gapRecorded'] and d['capture']['acknowledgedThrough']==len(d['capture']['events'])
`, dir)
}
