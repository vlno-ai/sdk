package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamResumesAfterLastCompleteConsumedFrame(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 40) {
			t.Error("unsafe stream request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			if r.URL.Query().Get("after") != "0" {
				t.Error("bad first cursor")
			}
			fmt.Fprint(w, "id: 7\nevent: trajectory\ndata: {\"nextCursor\":7}\n\nid: 8\nevent: trajectory\ndata: {\"nextCursor\":8}")
		} else {
			if r.URL.Query().Get("after") != "7" {
				t.Error("resumed incomplete event")
			}
			fmt.Fprint(w, "id: 8\nevent: trajectory\ndata: {\"nextCursor\":8}\n\nevent: done\ndata: {}\n\n")
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("a", 40), "", time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	seen := []string{}
	err := c.WatchWorld(ctx, "cw_"+strings.Repeat("b", 32), 0, func(e StreamEvent) (bool, error) {
		if e.Event == "trajectory" {
			seen = append(seen, e.ID)
		}
		return e.Event == "done", nil
	})
	if err != nil || calls != 2 || strings.Join(seen, ",") != "7,8" {
		t.Fatal(err, calls, seen)
	}
}
func TestStreamDoesNotRetryAuthorizationOrFollowRedirects(t *testing.T) {
	for _, status := range []int{301, 401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/secret")
				w.WriteHeader(status)
			}))
			defer server.Close()
			c, _ := New(server.URL, strings.Repeat("a", 40), "", time.Second)
			err := c.WatchWorld(context.Background(), "cw_"+strings.Repeat("b", 32), 0, func(StreamEvent) (bool, error) { t.Fatal("unexpected event"); return false, nil })
			var failure *Error
			if !errors.As(err, &failure) || failure.Status != status || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}
func TestStreamParserBoundsAndFrameIntegrity(t *testing.T) {
	for _, body := range []string{"data: {} {}\n\n", "data: null\n\n", "data: " + strings.Repeat("x", MaxResponseBytes) + "\n\n"} {
		err := readSSE(strings.NewReader(body), func(StreamEvent) (bool, error) { t.Fatal("invalid event delivered"); return false, nil })
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "invalid_stream" {
			t.Fatal(err)
		}
	}
	count := 0
	err := readSSE(strings.NewReader(": heartbeat\r\n\r\nevent: trajectory\r\nid: 1\r\ndata: {\r\ndata: \"a\":1}\r\n\r\n"), func(e StreamEvent) (bool, error) { count++; return false, nil })
	if err != io.EOF || count != 1 {
		t.Fatal(err, count)
	}
}
