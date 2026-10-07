//go:build darwin || linux

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func python(t *testing.T, code, path string) *exec.Cmd {
	t.Helper()
	binary, e := exec.LookPath("python3")
	if e != nil {
		t.Fatal("Python is required for cross-language lock qualification")
	}
	root, e := filepath.Abs("../../../python/src")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, "-c", code, path)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	return cmd
}
func TestGoOwnerExcludesPythonAndPythonReadsGoMutation(t *testing.T) {
	s, path := createStore(t)
	script := `import sys
from vlno.sessions.files import PrivateDirectory, SessionFileError
try:
 with PrivateDirectory(sys.argv[1]):
  raise SystemExit('unexpected acquired lock')
except SessionFileError as e:
 assert str(e)=='session_in_use', str(e)
`
	if out, e := python(t, script, path).CombinedOutput(); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	j, e := s.Read()
	if e != nil {
		t.Fatal(e)
	}
	nextRevision(j)
	part(j, "admission")["state"] = "unknown"
	if e = s.Save(encoded(t, j)); e != nil {
		t.Fatal(e)
	}
	s.Close()
	script = `import sys,json
from vlno.sessions.files import PrivateDirectory
with PrivateDirectory(sys.argv[1]) as files:
 j=json.loads(files.read('journal.json',2097152))
 assert j['revision']==2 and j['admission']['state']=='unknown'
 j['revision']=3
 files.replace('journal.json',json.dumps(j,separators=(',',':')).encode())
`
	if out, e := python(t, script, path).CombinedOutput(); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	reopened, e := Open(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := reopened.Read()
	if e != nil || number(got["revision"]) != 3 {
		t.Fatal("Python update unreadable", e)
	}
}
func TestPythonOwnerExcludesGoAndCrashReleasesLock(t *testing.T) {
	s, path := createStore(t)
	s.Close()
	script := `import sys
from vlno.sessions.files import PrivateDirectory
with PrivateDirectory(sys.argv[1]):
 print('locked',flush=True)
 sys.stdin.read()
`
	cmd := python(t, script, path)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	input, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { input.Close(); cmd.Process.Kill(); cmd.Wait() })
	ready := make([]byte, 7)
	if n, e := out.Read(ready); e != nil || n != 7 || string(ready) != "locked\n" {
		t.Fatal("Python lock was not established", e)
	}
	if other, e := Open(path, false); e == nil {
		other.Close()
		t.Fatal("Go acquired Python-held lock")
	}
	cmd.Process.Kill()
	cmd.Wait()
	reopened, e := Open(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if _, e = reopened.Read(); e != nil {
		t.Fatal(e)
	}
}
