package command

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A generic stream uploader. It does not interpret vendor-specific messages or
// run a model. Native JSON is retained verbatim in the captured stream.
func (r *runner) productTranscript(action, id string, args []string) error {
	if !productRunID.MatchString(id) {
		return fail("invalid_run_id")
	}
	fs := flags("platform transcript")
	index := fs.Int("case", -1, "")
	after := fs.Int64("after", 0, "")
	claimPath := fs.String("claim-file", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *index < 0 || *index > 15 || *after < 0 || *after > 9007199254740991 {
		return fail("usage")
	}
	if action == "record" && (*claimPath == "" || *after != 0) {
		return fail("usage")
	}
	if action == "transcript" && *claimPath != "" {
		return fail("usage")
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	client, err := r.client()
	if err != nil {
		return err
	}
	path := "/v1/world-runs/" + id + "/cases/" + strconv.Itoa(*index) + "/transcript"
	if action == "transcript" {
		value, err := client.Request(r.ctx, "GET", path+"?after="+strconv.FormatInt(*after, 10), nil, "")
		if err != nil {
			return err
		}
		if value["schema"] != "vlno.world-transcript/1" || value["runId"] != id || value["caseIndex"] != json.Number(strconv.Itoa(*index)) {
			return fail("invalid_response")
		}
		return r.emit(value)
	}
	claim, err := productClaim(*claimPath, false)
	if err != nil {
		return err
	}
	clean := func(v string) string {
		return strings.ReplaceAll(strings.ReplaceAll(v, settings.APIKey, "[redacted]"), claim, "[redacted]")
	}
	send := func(kind string, data map[string]any) error {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return err
		}
		random[6] = (random[6] & 15) | 64
		random[8] = (random[8] & 63) | 128
		h := hex.EncodeToString(random)
		eventID := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		event := map[string]any{"id": eventID, "kind": kind, "occurredAt": time.Now().UTC().Format(time.RFC3339Nano), "data": data}
		// Remove the known controller secrets before any event can leave this process.
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if json.Unmarshal([]byte(clean(string(encoded))), &event) != nil {
			return fail("invalid_event")
		}
		value, err := client.Request(r.ctx, "POST", path, map[string]any{"claim": claim, "events": []any{event}}, "")
		if err != nil {
			return err
		}
		accepted, ok := value["accepted"].([]any)
		if !ok || len(accepted) != 1 || accepted[0] != eventID {
			return fail("invalid_transcript_acknowledgement")
		}
		return nil
	}
	if err := send("lifecycle", map[string]any{"event": "stream_capture_started", "streams": []string{"stdin"}}); err != nil {
		return err
	}
	reader := bufio.NewReader(r.in)
	pending := ""
	flush := func(final bool) error {
		for len([]rune(pending)) > 4096 || (final && len(pending) > 0) {
			chars := []rune(pending)
			size := 2048
			if len(chars) < size {
				size = len(chars)
			}
			cut := len(string(chars[:size]))
			for _, secret := range []string{settings.APIKey, claim} {
				for start := 0; start < len(pending); {
					pos := strings.Index(pending[start:], secret)
					if pos < 0 {
						break
					}
					pos += start
					if pos < cut && cut < pos+len(secret) {
						cut = pos
					}
					start = pos + len(secret)
				}
			}
			for _, span := range regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{1,512}|vlno_live_[A-Za-z0-9_-]{1,128}`).FindAllStringIndex(pending, -1) {
				if span[0] < cut && cut < span[1] {
					cut = span[0]
				}
			}
			if cut == 0 {
				cut = len(string(chars[:min(1024, len(chars))]))
			}
			if err := send("stdout", map[string]any{"text": pending[:cut], "stream": "stdin"}); err != nil {
				return err
			}
			pending = pending[cut:]
		}
		return nil
	}
	for {
		ch, _, err := reader.ReadRune()
		if err == io.EOF {
			if err = flush(true); err != nil {
				return err
			}
			break
		}
		if err != nil {
			return fail("transcript_input_failed")
		}
		pending += string(ch)
		if ch == '\n' || len([]rune(pending)) > 4096 {
			if err = flush(ch == '\n'); err != nil {
				return err
			}
		}
	}
	if err = send("lifecycle", map[string]any{"event": "stream_capture_finished", "streams": []string{"stdin"}}); err != nil {
		return err
	}
	return r.emit(map[string]any{"run_id": id, "case_index": *index, "recorded": true})
}
