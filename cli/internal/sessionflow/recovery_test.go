package sessionflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vlno-ai/sdk/cli/internal/session"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestPreparedResumeDoesNotReserveOrCreateAnyIntent(t *testing.T) {
	p := protocol(t)
	calls := 0
	p.Client = fakeClient{func(string, string, any, string) (map[string]any, error) {
		calls++
		return nil, errors.New("unexpected network")
	}}
	if e := p.Resume(context.Background()); e != nil || calls != 0 {
		t.Fatal(e, calls)
	}
	j, _ := p.Store.Read()
	if object(j, "admission")["state"] != "prepared" {
		t.Fatal("prepared state changed")
	}
}
func TestExplicitExternalFinishRemainsDeclarationWithIncompleteCapture(t *testing.T) {
	prepared := fixture(t, "prepared")
	x := object(prepared, "execution")
	x["mode"] = "external"
	x["command"] = nil
	s, e := session.Create(filepath.Join(t.TempDir(), "external"), encoded(t, prepared), encoded(t, fixture(t, "claim")))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := &Protocol{

		Store: s,

		Endpoint: "https://api.example.test",

		Bindings: Bindings{ValidRun: func(v map[string]any, id string) bool { return v["id"] == id }},
	}
	claimed(t, p)
	posts := 0
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		posts++
		body := b.(map[string]any)
		if events, ok := body["events"].([]any); ok {
			return map[string]any{"accepted": []any{events[0].(map[string]any)["id"]}}, nil
		}
		if body["agent_status"] != "completed" {
			t.Error("declaration changed")
		}
		return map[string]any{"id": receipt(t)["runId"]}, nil
	}}
	if e = p.Finish(context.Background(), "completed", true); e != nil {
		t.Fatal(e)
	}
	j, _ := p.Store.Read()
	if object(j, "capture")["state"] != "incomplete" || object(j, "finish")["state"] != "accepted" {
		t.Fatal("false complete capture")
	}
	before := posts
	if p.Finish(context.Background(), "error", true) == nil || posts != before {
		t.Fatal("changed finish declaration accepted")
	}
}
func TestLostClaimRetainsExactActorAndSecret(t *testing.T) {
	p := protocol(t)
	confirmed(t, p)
	lost := true
	claims := []any{}
	p.Bindings.Connection = func(a, r map[string]any) (map[string]any, error) {
		return map[string]any{"task": "ordinary fixture"}, nil
	}
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		claims = append(claims, b)
		if lost {
			return nil, errors.New("lost response")
		}
		return map[string]any{
			"run": map[string]any{"id": receipt(t)["runId"], "state": "running"},
			"case": map[string]any{

				"state": "ready",

				"index": json.Number("0"),

				"scenario": "archive-note@1",

				"worldId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",

				"expiresAt": json.Number(strconv.FormatInt(time.Now().Unix()+120, 10)),

				"connection": map[string]any{},
			},
		}, nil
	}}
	if _, e := p.Claim(context.Background(), true); e == nil {
		t.Fatal("lost reply accepted")
	}
	j, _ := p.Store.Read()
	if object(j, "claim")["state"] != "unknown" {
		t.Fatal("intent lost")
	}
	lost = false
	if _, e := p.Claim(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	if len(claims) != 2 || !reflect.DeepEqual(claims[0], claims[1]) {
		t.Fatal("claim replay changed")
	}
}
func TestRecordCannotReopenGap(t *testing.T) {
	p := protocol(t)
	claimed(t, p)
	if e := p.Gap("capture_incomplete"); e != nil {
		t.Fatal(e)
	}
	before, _ := p.Store.Read()
	if p.Record("stdout", map[string]any{"text": "late"}, nil, false) == nil {
		t.Fatal("capture reopened")
	}
	after, _ := p.Store.Read()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("closed capture mutated")
	}
}
func TestFinishLostReplyPreservesStatusAndDoesNotAcceptConflict(t *testing.T) {
	p := protocol(t)
	claimed(t, p)
	requests := []any{}
	lost := true
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		requests = append(requests, b)
		if lost {
			return nil, errors.New("lost")
		}
		return map[string]any{"id": receipt(t)["runId"]}, nil
	}}
	if p.Finish(context.Background(), "error", true) == nil {
		t.Fatal("lost finish accepted")
	}
	if p.Finish(context.Background(), "completed", true) == nil || len(requests) != 1 {
		t.Fatal("changed status dispatched")
	}
	lost = false
	if e := p.Finish(context.Background(), "", false); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatal("finish body changed")
	}
}
