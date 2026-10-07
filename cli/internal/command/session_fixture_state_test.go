package command

import "net/http"

type sessionCounts struct {
	posts, nexts, finishes, cancellations int
	claim                                 string
	events                                []map[string]any
}

func (f *sessionAPI) snapshot() sessionCounts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sessionCounts{
		posts:         f.posts,
		nexts:         f.nexts,
		finishes:      f.finishes,
		cancellations: f.cancellations,
		claim:         f.claim,
		events:        append([]map[string]any{}, f.events...),
	}
}
func dropReply(w http.ResponseWriter) {
	conn, _, e := w.(http.Hijacker).Hijack()
	if e == nil {
		conn.Close()
	}
}

func (f *sessionAPI) loseReply(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch kind {
	case "finish":
		f.loseFinish = true
	case "admission":
		f.loseAdmission = true
	case "cancel":
		f.loseCancel = true
	case "output":
		f.loseOutput = true
	}
}
