package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/vlno-ai/sdk/cli/internal/assessment"
	"reflect"
	"time"
)

func NewID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func Now() string                      { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
func Encode(value any) ([]byte, error) { return wireJSON(value) }

// Initial captures verified source and caller before any mutation is dispatched.
func Initial(endpoint string, identity, preparation, command map[string]any) ([]byte, []byte, error) {
	endpoint, e := Origin(endpoint)
	if e != nil {
		return nil, nil, e
	}
	who, ok := object(identity, "schema orgId actor")
	if !ok || who["schema"] != "vlno.assessment-run-access/1" || !bounded(who["orgId"], 200) || !actor(who["actor"]) {
		return nil, nil, ErrCorrupt
	}
	source := part(preparation, "source")
	if !assessment.Preparation(preparation, text(source["assessmentId"]), text(source["planRevisionId"])) ||
		preparation["orgId"] != who["orgId"] ||
		!reflect.DeepEqual(preparation["actor"], who["actor"]) ||
		preparation["state"] != "ready" ||
		part(preparation, "admissionAccess")["canAdmit"] != true ||
		!validCommand(command) {
		return nil, nil, ErrCorrupt
	}
	id, e := NewID()
	if e != nil {
		return nil, nil, e
	}
	key, e := NewID()
	if e != nil {
		return nil, nil, e
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return nil, nil, e
	}
	claim := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(claim))
	now := Now()
	j := map[string]any{

		"schema":    "vlno.client-session/1",
		"id":        id,
		"revision":  json.Number("1"),
		"createdAt": now,
		"updatedAt": now,
		"endpoint":  endpoint,
		"orgId":     who["orgId"],
		"source":    source,

		"admission": map[string]any{

			"state": "prepared",

			"actor": who["actor"],

			"idempotencyKey": key,

			"command": map[string]any{

				"planRevisionId": source["planRevisionId"],

				"approvalReviewId": source["approvalReviewId"],

				"systemRevisionId": source["systemRevisionId"],
			},

			"receipt": nil,
		},

		"claim": map[string]any{

			"file": "claim.json",

			"sha256": hex.EncodeToString(hash[:]),

			"state": "not_started",

			"actor": nil,

			"case": nil,
		},

		"execution": map[string]any{

			"mode": "supervised",

			"state": "not_started",

			"launchId": nil,

			"command": command,

			"pid": nil,

			"exitCode": nil,

			"finishedAt": nil,
		},

		"capture": map[string]any{

			"state": "not_started",

			"nextSequence": json.Number("1"),

			"acknowledgedThrough": json.Number("0"),

			"gapRecorded": false,

			"events": []any{},
		},

		"finish": map[string]any{

			"state": "not_started",

			"actor": nil,

			"command": nil,
		},
		"cancellation": map[string]any{"state": "not_requested", "actor": nil},
		"observation":  nil,
		"recovery":     nil,
	}
	raw, e := wireJSON(j)
	if e != nil {
		return nil, nil, e
	}
	if _, e = ParseJournal(raw); e != nil {
		return nil, nil, e
	}
	private, e := wireJSON(map[string]any{

		"schema": "vlno.client-session-claim/1",

		"sessionId": id,

		"claim": claim,
	})
	return raw, private, e
}
