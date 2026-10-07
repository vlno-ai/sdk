package sessionflow

import (
	"context"
	"reflect"
	"testing"
)

func TestResumeIdentityMismatchCannotChangeLostLaunchJournal(t *testing.T) {
	for _, field := range []string{"orgId", "actor"} {
		t.Run(field, func(t *testing.T) {
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
			before, _ := p.Store.Read()
			posts := 0
			p.Client = fakeClient{func(m, path string, b any, k string) (map[string]any, error) {
				if m == "POST" {
					posts++
				}
				value := who(t)
				if field == "orgId" {
					value[field] = "different-org"
				} else {
					value[field] = map[string]any{"kind": "service_key", "reference": "sa_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
				}
				return value, nil
			}}
			if p.Resume(context.Background()) == nil {
				t.Fatal("changed identity accepted")
			}
			after, _ := p.Store.Read()
			if posts != 0 || !reflect.DeepEqual(before, after) {
				t.Fatal("unauthorized recovery mutated journal or API")
			}
		})
	}
}
