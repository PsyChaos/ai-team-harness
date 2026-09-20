package journal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func event(id uint64) Event {
	return Event{ID: id, Type: "synthetic", Data: json.RawMessage(`{"status":"running"}`)}
}
func openTest(t *testing.T, retain, pending int) *Journal {
	t.Helper()
	j, err := Open(filepath.Join(t.TempDir(), "journal.json"), Limits{retain, pending, 256})
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func appendTest(t *testing.T, j *Journal, id uint64) {
	t.Helper()
	if err := j.Append(event(id)); err != nil {
		t.Fatal(err)
	}
}

func TestRestartOrderingAndDuplicates(t *testing.T) {
	j := openTest(t, 4, 3)
	for _, id := range []uint64{3, 3, 1} {
		appendTest(t, j, id)
	}
	restored, err := Open(j.path, j.limits)
	if err != nil {
		t.Fatal(err)
	}
	appendTest(t, restored, 2)
	appendTest(t, restored, 1)
	want := []Event{event(1), event(2), event(3)}
	if got := restored.Snapshot(); got.Cursor != 3 || !reflect.DeepEqual(got.Events, want) {
		t.Fatalf("snapshot: %+v", got)
	}
	events, _, err := restored.ReadAfter(1)
	if err != nil || !reflect.DeepEqual(events, want[1:]) {
		t.Fatalf("resume: %v %v", events, err)
	}
	// Public values cannot mutate internal state.
	events[0].Data[0] = 'x'
	if !json.Valid(restored.Snapshot().Events[1].Data) {
		t.Fatal("aliased event data")
	}
}

func TestRetentionAndSustainedLoad(t *testing.T) {
	j := openTest(t, 8, 2)
	appendTest(t, j, 3)
	appendTest(t, j, 4)
	if err := j.Append(event(5)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	appendTest(t, j, 1)
	appendTest(t, j, 2)
	for id := uint64(5); id <= 1000; id++ {
		appendTest(t, j, id)
	}
	s := j.Snapshot()
	if s.Cursor != 1000 || len(s.Events) != 8 || s.Events[0].ID != 993 {
		t.Fatalf("retention: %+v", s)
	}
	if _, _, err := j.ReadAfter(991); !errors.Is(err, ErrCursor) {
		t.Fatalf("stale cursor: %v", err)
	}
	if _, _, err := j.ReadAfter(1001); !errors.Is(err, ErrCursor) {
		t.Fatalf("future cursor: %v", err)
	}
	if events, _, err := j.ReadAfter(992); err != nil || len(events) != 8 {
		t.Fatalf("boundary: %v %v", events, err)
	}
	info, err := os.Stat(j.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 8*256+1024 {
		t.Fatalf("unbounded storage: %d", info.Size())
	}
	restored, err := Open(j.path, j.limits)
	if err != nil || !reflect.DeepEqual(restored.Snapshot(), s) {
		t.Fatalf("retained restart: %v", err)
	}
}

func TestPersistenceFailureDoesNotPublish(t *testing.T) {
	j := openTest(t, 2, 2)
	j.path = filepath.Join(j.path, "missing", "file")
	if err := j.Append(event(1)); err == nil {
		t.Fatal("expected persistence failure")
	}
	if j.Snapshot().Cursor != 0 {
		t.Fatal("published failed write")
	}
}

func TestRejectInvalidStorageAndEvents(t *testing.T) {
	j := openTest(t, 2, 2)
	for _, e := range []Event{{}, {ID: 1, Type: "x", Data: json.RawMessage(`bad`)}, {ID: 1, Type: "x", Data: json.RawMessage(`"` + strings.Repeat("x", 256) + `"`)}} {
		if j.Append(e) == nil {
			t.Fatal("accepted invalid event")
		}
	}
	if err := os.WriteFile(j.path, []byte(`{"version":1,"cursor":2,"events":[],"pending":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(j.path, j.limits); err == nil {
		t.Fatal("accepted corrupt sequence")
	}
}

func readID(t *testing.T, s *bufio.Scanner) uint64 {
	t.Helper()
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "id: ") {
			id, err := strconv.ParseUint(strings.TrimPrefix(s.Text(), "id: "), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
	}
	t.Fatalf("stream ended: %v", s.Err())
	return 0
}

func TestHTTPDisconnectResume(t *testing.T) {
	j := openTest(t, 16, 4)
	for id := uint64(1); id <= 4; id++ {
		appendTest(t, j, id)
	}
	h, err := j.Handler(HTTPOptions{4, time.Second, 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	client, baseURL := pipeHTTP(t, h)
	client.Timeout = 3 * time.Second
	first, err := client.Get(baseURL + "/events?cursor=0")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(first.Body)
	for id := uint64(1); id <= 2; id++ {
		if got := readID(t, scanner); got != id {
			t.Fatalf("got %d want %d", got, id)
		}
	}
	first.Body.Close()
	appendTest(t, j, 6)
	appendTest(t, j, 5)
	appendTest(t, j, 5)
	req, _ := http.NewRequest("GET", baseURL+"/events?cursor=0", nil)
	req.Header.Set("Last-Event-ID", "2")
	second, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	scanner = bufio.NewScanner(second.Body)
	for id := uint64(3); id <= 6; id++ {
		if got := readID(t, scanner); got != id {
			t.Fatalf("resume got %d want %d", got, id)
		}
	}
	appendTest(t, j, 7)
	if got := readID(t, scanner); got != 7 {
		t.Fatalf("live event %d", got)
	}
	response, err := client.Get(baseURL + "/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snap Snapshot
	if err = json.NewDecoder(response.Body).Decode(&snap); err != nil || snap.Cursor != 7 {
		t.Fatalf("snapshot %v %v", snap, err)
	}
	for _, tc := range []struct {
		cursor string
		status int
	}{{"bad", 400}, {"99", 409}} {
		r, err := client.Get(baseURL + "/events?cursor=" + tc.cursor)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != tc.status {
			t.Fatalf("status %d", r.StatusCode)
		}
	}
}

// deadlineWriter models a stalled transport while honoring ResponseController deadlines.
type deadlineWriter struct {
	header   http.Header
	deadline time.Time
	entered  chan struct{}
	once     sync.Once
}

func (w *deadlineWriter) Header() http.Header                { return w.header }
func (w *deadlineWriter) WriteHeader(int)                    {}
func (w *deadlineWriter) Flush()                             {}
func (w *deadlineWriter) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }
func (w *deadlineWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	if w.deadline.IsZero() {
		return 0, fmt.Errorf("missing deadline")
	}
	time.Sleep(time.Until(w.deadline))
	return 0, os.ErrDeadlineExceeded
}

func TestSlowClientAndStreamLimit(t *testing.T) {
	j := openTest(t, 4, 2)
	h, err := j.Handler(HTTPOptions{1, 200 * time.Millisecond, time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := &deadlineWriter{header: make(http.Header), entered: make(chan struct{})}
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, httptest.NewRequest("GET", "/events", nil)); close(done) }()
	<-w.entered
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, httptest.NewRequest("GET", "/events", nil))
	if rejected.Code != 503 {
		t.Fatalf("stream limit %d", rejected.Code)
	}
	for id := uint64(1); id <= 1000; id++ {
		appendTest(t, j, id)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow stream did not terminate")
	}
	if len(j.Snapshot().Events) != 4 {
		t.Fatal("slow reader prevented retention")
	}
	stale := httptest.NewRecorder()
	h.ServeHTTP(stale, httptest.NewRequest("GET", "/events?cursor=1", nil))
	if stale.Code != 409 {
		t.Fatalf("stale cursor %d", stale.Code)
	}
}

// Exercise the HTTP protocol over net.Pipe so integration tests also run in
// sandboxes that prohibit binding TCP sockets.
type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }
func pipeHTTP(t *testing.T, h http.Handler) (*http.Client, string) {
	t.Helper()
	l := &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: h}
	go func() { _ = server.Serve(l) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		select {
		case l.connections <- server:
			return client, nil
		case <-ctx.Done():
			client.Close()
			server.Close()
			return nil, ctx.Err()
		case <-l.done:
			client.Close()
			server.Close()
			return nil, net.ErrClosed
		}
	}}
	t.Cleanup(func() { transport.CloseIdleConnections(); _ = server.Close() })
	return &http.Client{Transport: transport}, "http://journal.test"
}

// Advance the journal during delivery to deterministically force an active
// stream past retention, without relying on scheduler timing or socket buffers.
type advancingWriter struct {
	*httptest.ResponseRecorder
	advance func()
}

func (w *advancingWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *advancingWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "event: observation") && w.advance != nil {
		advance := w.advance
		w.advance = nil
		advance()
	}
	return w.ResponseRecorder.Write(p)
}
func TestActiveStreamRetentionReset(t *testing.T) {
	j := openTest(t, 2, 2)
	appendTest(t, j, 1)
	h, err := j.Handler(HTTPOptions{1, time.Second, time.Second})
	if err != nil {
		t.Fatal(err)
	}
	w := &advancingWriter{ResponseRecorder: httptest.NewRecorder(), advance: func() {
		for id := uint64(2); id <= 10; id++ {
			appendTest(t, j, id)
		}
	}}
	h.ServeHTTP(w, httptest.NewRequest("GET", "/events", nil))
	if !strings.Contains(w.Body.String(), "event: reset\n") {
		t.Fatalf("missing resync signal: %s", w.Body.String())
	}
}
