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
		{"/", "text/html", "Demo — all tasks, agents, events and metrics are fixtures"},
		{"/styles.css", "text/css", "prefers-reduced-motion:reduce"},
		{"/app.js", "javascript", "ArrowRight"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
			if r.Code != http.StatusOK || !strings.Contains(r.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(r.Body.String(), tc.content) {
				t.Fatalf("asset %s: status=%d type=%s missing expected content=%v", tc.path, r.Code, r.Header().Get("Content-Type"), !strings.Contains(r.Body.String(), tc.content))
			}
			if !strings.Contains(r.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
				t.Fatal("dashboard must not connect to live services")
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
