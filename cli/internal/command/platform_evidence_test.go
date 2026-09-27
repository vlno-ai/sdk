package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductEvidenceVerifiesBeforeWritingAndNeverOverwrites(t *testing.T) {
	body := []byte(`{"private":"test trace"}`)
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	corrupt := false
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+productKey {
			t.Error("wrong credentials")
		}
		if strings.HasSuffix(r.URL.Path, "/trace") {
			if corrupt {
				w.Write([]byte(strings.Repeat("x", len(body))))
				return
			}
			w.Write(body)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"schema": "vlno.world-evidence/1", "runId": productID, "status": "complete", "evidenceComplete": false, "artifacts": []any{map[string]any{"caseIndex": 0, "kind": "trace", "evaluationId": testID, "sha256": hash, "size": len(body), "state": "archived"}}})
	})
	t.Setenv("VLNO_API_KEY", productKey)
	path := filepath.Join(t.TempDir(), "trace.json")
	args := []string{"platform", "runs", "artifact", productID, "--case", "0", "--kind", "trace", "--out", path, "--json"}
	code, out, stderr := invoke(t, args, "")
	if code != 0 || strings.Contains(out+stderr, string(body)) || strings.Contains(out+stderr, productKey) {
		t.Fatal(code, out, stderr)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != string(body) {
		t.Fatal("artifact changed", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("artifact permissions")
	}
	code, _, _ = invoke(t, args, "")
	if code == 0 {
		t.Fatal("overwrote file")
	}
	corrupt = true
	args[len(args)-2] = filepath.Join(t.TempDir(), "corrupt.json")
	code, _, stderr = invoke(t, args, "")
	if code == 0 || !strings.Contains(stderr, "artifact_integrity_error") {
		t.Fatal("accepted corruption", stderr)
	}
	if _, err = os.Stat(args[len(args)-2]); !os.IsNotExist(err) {
		t.Fatal("saved invalid bytes")
	}
}
