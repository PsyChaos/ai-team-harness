package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedDashboard(t *testing.T) {
	handler := Handler()
	for _, tc := range []struct{ path, contentType, content string }{
		{"/", "text/html", "Live observations — loading"},
		{"/styles.css", "text/css", "prefers-reduced-motion:reduce"},
		{"/app.js", "javascript", "ArrowRight"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
			if r.Code != http.StatusOK || !strings.Contains(r.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(r.Body.String(), tc.content) {
				t.Fatalf("asset %s: status=%d type=%s missing expected content=%v", tc.path, r.Code, r.Header().Get("Content-Type"), !strings.Contains(r.Body.String(), tc.content))
			}
			if !strings.Contains(r.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
				t.Fatal("dashboard must restrict connections to its own origin")
			}
			for _, external := range []string{"https://", "http://", "{{", "<sc-", "<x-dc", "support.js"} {
				if strings.Contains(r.Body.String(), external) {
					t.Errorf("unexpected external dependency or template artifact %q", external)
				}
			}
		})
	}
}

func TestReadOnlyAssetBoundary(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/", 405}, {"GET", "/assets/", 404}, {"GET", "/../go.mod", 404},
		{"GET", "/theme/support.js", 404}, {"GET", "/api/tasks", 404}, {"HEAD", "/", 200},
	} {
		r := httptest.NewRecorder()
		Handler().ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.method, tc.path, r.Code, tc.want)
		}
		if tc.method == "HEAD" && r.Body.Len() != 0 {
			t.Error("HEAD returned a body")
		}
	}
}

func TestJournalBoundary(t *testing.T) {
	calls := 0
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"version":1,"cursor":0,"events":[]}`))
	})
	handler := WithJournal(api, "owner/repo", "owner/7")
	for _, path := range []string{"/snapshot", "/events?cursor=0"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
	for _, path := range []string{"/snapshot", "/events", "/identity", "/command", "/file?path=/etc/passwd"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("POST", path, nil))
		if r.Code != 405 {
			t.Fatalf("POST %s: %d", path, r.Code)
		}
	}
	if calls != 2 {
		t.Fatalf("unexpected journal dispatches: %d", calls)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/identity", nil))
	if !strings.Contains(r.Body.String(), `"project":"owner/7"`) || !strings.Contains(r.Body.String(), `"repo":"owner/repo"`) {
		t.Fatal(r.Body.String())
	}
	r = httptest.NewRecorder()
	Handler().ServeHTTP(r, httptest.NewRequest("GET", "/snapshot", nil))
	if r.Code != 503 {
		t.Fatalf("missing journal must fail visibly: %d", r.Code)
	}
}
