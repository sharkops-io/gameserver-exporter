package palworld

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusParsesBothEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != "testpw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/api/info":
			w.Write([]byte(`{"version":"v1.0.2.101103","servername":"Wild Nest"}`))
		case "/v1/api/metrics":
			w.Write([]byte(`{"serverfps":51,"currentplayernum":3,"maxplayernum":10,"uptime":24309,"serverframetime":19.5}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := New(Config{URL: srv.URL, Password: "testpw", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Version != "v1.0.2.101103" || got.PlayersOnline != 3 || got.PlayersMax != 10 ||
		got.UptimeSeconds != 24309 || got.FPS != 51 || got.FrameTimeMS != 19.5 {
		t.Errorf("Status = %+v", got)
	}
	if got.LatencySeconds <= 0 {
		t.Error("LatencySeconds should be measured")
	}
}
