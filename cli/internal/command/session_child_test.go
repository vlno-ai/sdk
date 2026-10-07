package command

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestDurableSessionChild(t *testing.T) {
	if os.Getenv("D01_HELPER") != "1" {
		return
	}
	if os.Getenv("VLNO_API_KEY") != "" || os.Getenv("D01_PRIVATE") != "" {
		os.Exit(21)
	}
	var input map[string]any
	if json.NewDecoder(os.Stdin).Decode(&input) != nil {
		os.Exit(22)
	}
	conn := input["connection"].(map[string]any)
	auth := conn["mcp"].(map[string]any)["headers"].(map[string]any)["Authorization"].(string)
	f, e := os.OpenFile(os.Getenv("D01_MARKER"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		os.Exit(23)
	}
	f.WriteString("one\n")
	f.Close()
	fmt.Print(auth[:len(auth)/2])
	fmt.Println(auth[len(auth)/2:])
	fmt.Fprintln(os.Stderr, `{"type":"assistant","text":"ordinary native output"}`)
	if os.Getenv("D01_BLOCK") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}
func sessionStartArgs(t *testing.T, fixture *sessionAPI, dir, marker string) []string {
	t.Helper()
	t.Setenv("D01_HELPER", "1")
	t.Setenv("D01_MARKER", marker)
	t.Setenv("D01_PRIVATE", productKey)
	source := fixture.prep["source"].(map[string]any)
	return []string{

		"sessions",

		"start",

		dir,

		"--assessment",

		source["assessmentId"].(string),

		"--revision",

		source["planRevisionId"].(string),

		"--env",

		"D01_HELPER",

		"--env",

		"D01_MARKER",

		"--env",

		"D01_PRIVATE",

		"--agent-timeout",

		"15",

		"--",

		os.Args[0],

		"-test.run=^TestDurableSessionChild$",
	}
}
