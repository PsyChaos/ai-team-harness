package journal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type HTTPOptions struct {
	MaxStreams   int
	WriteTimeout time.Duration
	Heartbeat    time.Duration
}

// Handler serves GET /snapshot and GET /events. The caller owns authentication,
// listener configuration and server timeouts; this handler grants no authority.
func (j *Journal) Handler(opts HTTPOptions) (http.Handler, error) {
	if opts.MaxStreams < 1 || opts.WriteTimeout <= 0 || opts.Heartbeat <= 0 {
		return nil, fmt.Errorf("invalid HTTP options")
	}
	slots := make(chan struct{}, opts.MaxStreams)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(j.Snapshot())
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "stream limit", http.StatusServiceUnavailable)
			return
		}
		raw := r.Header.Get("Last-Event-ID")
		if raw == "" {
			raw = r.URL.Query().Get("cursor")
		}
		if raw == "" {
			raw = "0"
		}
		cursor, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor", 400)
			return
		}
		events, changed, err := j.ReadAfter(cursor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		rc := http.NewResponseController(w)
		if err = rc.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)); err != nil {
			http.Error(w, "stream deadlines unsupported", 500)
			return
		}
		defer rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		if _, err = fmt.Fprint(w, ": connected\n\n"); err != nil {
			return
		}
		if rc.Flush() != nil {
			return
		}
		ticker := time.NewTicker(opts.Heartbeat)
		defer ticker.Stop()
		for {
			for _, e := range events {
				if rc.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)) != nil {
					return
				}
				data, _ := json.Marshal(e)
				if _, err = fmt.Fprintf(w, "id: %d\nevent: observation\ndata: %s\n\n", e.ID, data); err != nil {
					return
				}
				if rc.Flush() != nil {
					return
				}
				cursor = e.ID
			}
			events = nil
			select {
			case <-r.Context().Done():
				return
			case <-changed:
			case <-ticker.C:
				if rc.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)) != nil {
					return
				}
				if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if rc.Flush() != nil {
					return
				}
			}
			events, changed, err = j.ReadAfter(cursor)
			if err != nil {
				_ = rc.SetWriteDeadline(time.Now().Add(opts.WriteTimeout))
				_, _ = fmt.Fprint(w, "event: reset\ndata: {\"snapshot\":\"/snapshot\"}\n\n")
				_ = rc.Flush()
				return
			}
		}
	})
	return mux, nil
}
