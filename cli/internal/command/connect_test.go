package command

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectPrivateManifestAndNoOverwrite(t *testing.T) {
	token := strings.Repeat("s", 40)
	s := worker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey || r.URL.Path != "/v1/worlds/"+testID+"/connection" {
			t.Error("incorrect request")
		}
		json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "world_id": testID, "expires_at": 123,
			"mcp": map[string]any{"path": "/v1/worlds/" + testID + "/mcp", "headers": map[string]string{"Authorization": "Bearer " + token}},
			"app": map[string]any{"path": "/v1/worlds/" + testID + "/call", "headers": map[string]string{"Authorization": "Bearer " + token}}})
	})
	path := filepath.Join(t.TempDir(), "connection.json")
	args := []string{"worlds", "connect", testID, "--out", path, "--json"}
	code, out, err := invoke(t, args, "")
	if code != 0 || strings.Contains(out+err, token) || strings.Contains(out+err, testKey) {
		t.Fatal(code, out, err)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	value := decode(t, string(raw))
	mcp := value["mcp"].(map[string]any)
	if mcp["url"] != s.URL+"/v1/worlds/"+testID+"/mcp" {
		t.Fatal(value)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	code, _, _ = invoke(t, args, "")
	if code == 0 {
		t.Fatal("overwrote credential file")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("changed credential file")
	}
}

func TestConnectRejectsCredentialRedirectionAndRemovesPartialFile(t *testing.T) {
	worker(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "world_id": testID,
			"mcp": map[string]any{"path": "https://foreign.example/mcp"}})
	})
	path := filepath.Join(t.TempDir(), "connection.json")
	code, _, _ := invoke(t, []string{"worlds", "connect", testID, "--out", path, "--json"}, "")
	if code == 0 {
		t.Fatal("accepted foreign origin")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("left partial file")
	}
}
