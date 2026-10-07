package sessionflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLostAdmissionKeepsExactKeyBodyAndSource(t *testing.T) {
	p := protocol(t)
	var bodies []any
	var keys []string
	lost := true
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		bodies = append(bodies, b)
		keys = append(keys, k)
		j, _ := p.Store.Read()
		if object(j, "admission")["state"] != "unknown" {
			t.Fatal("POST preceded durable intent")
		}
		if lost {
			return nil, errors.New("synthetic lost reply")
		}
		return receipt(t), nil
	}}
	if p.Admit(context.Background()) == nil {
		t.Fatal("lost reply reported saved")
	}
	lost = false
	if e := p.Admit(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(bodies) != 2 || !reflect.DeepEqual(bodies[0], bodies[1]) || keys[0] != keys[1] {
		t.Fatal("admission rebased")
	}
}
func TestChangedActorBlocksSavedMutationBeforePOST(t *testing.T) {
	p := protocol(t)
	posts := 0
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "POST" {
			posts++
		}
		v := who(t)
		object(v, "actor")["reference"] = "sa_" + strings.Repeat("b", 32)
		return v, nil
	}}
	if p.Admit(context.Background()) == nil || posts != 0 {
		t.Fatal("changed actor dispatched")
	}
}
func TestLostEventReplyReplaysSameDurableEvent(t *testing.T) {
	p := protocol(t)
	claimed(t, p)
	secret, _ := p.Store.PrivateClaim()
	if e := p.Record("stdout", map[string]any{"text": "visible " + secret}, []string{secret}, false); e != nil {
		t.Fatal(e)
	}
	var sent []any
	lost := true
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if m == "GET" {
			return who(t), nil
		}
		event := b.(map[string]any)["events"].([]any)[0].(map[string]any)
		sent = append(sent, event)
		if lost {
			return nil, errors.New("lost")
		}
		return map[string]any{"accepted": []any{event["id"]}}, nil
	}}
	if p.Flush(context.Background()) == nil {
		t.Fatal("unknown ack accepted")
	}
	lost = false
	if e := p.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(sent[0], sent[1]) || strings.Contains(string(encoded(t, sent[0])), secret) {
		t.Fatal("event changed or secret leaked")
	}
}
func TestResumeMarksLostLaunchAndNeverCreatesClaimOrFinishIntent(t *testing.T) {
	p := protocol(t)
	claimed(t, p)
	if e := p.Store.Update(func(j map[string]any) error {
		x := object(j, "execution")
		x["state"] = "start_unknown"
		x["launchId"] = "99999999-9999-4999-8999-999999999999"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	forbidden := 0
	p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
		if strings.HasSuffix(path, "/access") {
			return who(t), nil
		}
		if strings.HasSuffix(path, "/transcript") {
			event := b.(map[string]any)["events"].([]any)[0].(map[string]any)
			return map[string]any{"accepted": []any{event["id"]}}, nil
		}
		if m == "POST" {
			forbidden++
		}
		return nil, errors.New("status unavailable")
	}}
	_ = p.Resume(context.Background())
	j, _ := p.Store.Read()
	if forbidden != 0 || object(j, "execution")["state"] != "exit_unknown" || object(j, "capture")["state"] != "incomplete" ||
		object(j, "finish")["state"] != "not_started" {
		t.Fatal("resume created execution/finish or hid uncertainty")
	}
}
