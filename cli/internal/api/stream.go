package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StreamEvent is a complete SSE frame. Trajectory IDs are durable receipt cursors.
type StreamEvent struct {
	Event string
	ID    string
	Data  map[string]any
}

var streamRunID = regexp.MustCompile(`^cw_[a-f0-9]{32}$`)
var streamCursor = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})$`)
var stopStream = errors.New("stream consumer finished")

// WatchWorld reconnects read-only GETs. It never retries a case claim or tool call.
// The callback returns true once the caller's condition is satisfied.
func (c *Client) WatchWorld(ctx context.Context, id string, after int64, handle func(StreamEvent) (bool, error)) error {
	if !streamRunID.MatchString(id) || after < 0 || after > 9007199254740991 {
		return &Error{Code: "invalid_stream_request"}
	}
	cursor := strconv.FormatInt(after, 10)
	backoff := 250 * time.Millisecond
	for {
		err := c.worldStream(ctx, id, cursor, func(event StreamEvent) (bool, error) {
			if event.Event == "trajectory" {
				if !streamCursor.MatchString(event.ID) {
					return false, &Error{Code: "invalid_stream"}
				}
				n, parseErr := strconv.ParseInt(event.ID, 10, 64)
				old, _ := strconv.ParseInt(cursor, 10, 64)
				if parseErr != nil || n < old || n > 9007199254740991 {
					return false, &Error{Code: "invalid_stream"}
				}
			}
			done, err := handle(event)
			if err == nil && event.Event == "trajectory" {
				cursor = event.ID
			}
			if err == nil {
				backoff = 250 * time.Millisecond
			}
			return done, err
		})
		if errors.Is(err, stopStream) {
			return nil
		}
		if ctx.Err() != nil {
			return transportError(ctx.Err())
		}
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Code != "transport_error" && !(apiErr.Status == 429 || apiErr.Status >= 500) {
			return err
		}
		// EOF is a lease renewal or broken connection, never successful completion.
		if err != nil && !errors.As(err, &apiErr) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return transportError(ctx.Err())
		case <-timer.C:
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) worldStream(ctx context.Context, id, cursor string, handle func(StreamEvent) (bool, error)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/v1/world-runs/"+id+"/events?after="+cursor, nil)
	if err != nil {
		return &Error{Code: "invalid_stream_request"}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/event-stream")
	req.Close = true
	client := *c.http
	client.Timeout = 60 * time.Second // server renews authentication at most every 45s
	res, err := client.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return &Error{Code: "stream_http_error", Status: res.StatusCode}
	}
	if strings.Split(res.Header.Get("Content-Type"), ";")[0] != "text/event-stream" {
		return &Error{Code: "invalid_stream"}
	}
	return readSSE(res.Body, handle)
}

func readSSE(body io.Reader, handle func(StreamEvent) (bool, error)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), MaxResponseBytes)
	event := StreamEvent{Event: "message"}
	var data strings.Builder
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > MaxResponseBytes {
			return &Error{Code: "invalid_stream"}
		}
		if line == "" {
			if data.Len() > 0 {
				decoder := json.NewDecoder(strings.NewReader(data.String()))
				decoder.UseNumber()
				if decoder.Decode(&event.Data) != nil || event.Data == nil {
					return &Error{Code: "invalid_stream"}
				}
				var extra any
				if decoder.Decode(&extra) != io.EOF {
					return &Error{Code: "invalid_stream"}
				}
				done, err := handle(event)
				if err != nil {
					return err
				}
				if done {
					return stopStream
				}
			}
			event = StreamEvent{Event: "message"}
			data.Reset()
			size = 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event.Event = value
		case "id":
			if strings.ContainsRune(value, 0) {
				return &Error{Code: "invalid_stream"}
			}
			event.ID = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return &Error{Code: "invalid_stream"}
		}
		return transportError(err)
	}
	return io.EOF
}
