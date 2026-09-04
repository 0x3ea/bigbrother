package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0x3ea/bigbrother/internal/api"
	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
	"github.com/0x3ea/bigbrother/internal/store"
	"github.com/gin-gonic/gin"
)

func newEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	targets := []config.Target{
		{Name: "ok-target", Type: "https", Target: "https://a.com/", IntervalMS: 3000, TimeoutMS: 5000},
		{Name: "bad-target", Type: "tcp", Target: "a.com:443", IntervalMS: 1000, TimeoutMS: 2000},
		{Name: "new-target", Type: "http", Target: "http://b.com/", IntervalMS: 6000, TimeoutMS: 3000},
	}

	st := store.NewMemory()
	st.Save(targets[0].Name, prober.Result{
		Success:   true,
		Latency:   87 * time.Second,
		Timestamp: time.Now(),
	})
	st.Save(targets[1].Name, prober.Result{
		Success:   false,
		Latency:   2 * time.Second,
		Timestamp: time.Now(),
		Error:     errors.New("i/o timeout"),
	})
	return api.New(st, targets).Engine()
}

func TestStatus_ThreeStates(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	newEngine(t).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}

	var got []struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		LatencyMS int64  `json:"latency_ms"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, w.Body)
	}
	want := map[string]struct {
		status    string
		latencyMS int64
		errMsg    string
	}{
		"ok-target":  {"up", 87000, ""},
		"bad-target": {"down", 2000, "i/o timeout"},
		"new-target": {"pending", 0, ""},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for _, it := range got {
		exp, ok := want[it.Name]
		if !ok {
			t.Fatalf("unexpected target %q in response", it.Name)
		}
		if it.Status != exp.status || it.LatencyMS != exp.latencyMS || it.Error != exp.errMsg {
			t.Errorf("%s: got (%s,%d,%q), want (%s,%d,%q)",
				it.Name, it.Status, it.LatencyMS, it.Error, exp.status, exp.latencyMS, exp.errMsg)
		}
	}
}

func TestStatus_NoZeroTimeLeak(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	newEngine(t).ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "0001-01-01") {
		t.Fatalf("zero time leaked: %s", w.Body)
	}
}
