package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterBurstThenReject(t *testing.T) {
	now := time.Now()
	rl := newRateLimiter(60, 3) // 每秒 1 个令牌，桶容量 3
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := rl.allow("ip-a"); !ok {
			t.Fatalf("request %d within burst should pass", i+1)
		}
	}
	if ok, retry := rl.allow("ip-a"); ok || retry <= 0 {
		t.Fatalf("request beyond burst should be rejected with positive retry, got ok=%v retry=%v", ok, retry)
	}

	// 另一个 IP 不受影响
	if ok, _ := rl.allow("ip-b"); !ok {
		t.Fatal("independent IP should have its own bucket")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	now := time.Now()
	rl := newRateLimiter(60, 1)
	rl.now = func() time.Time { return now }

	if ok, _ := rl.allow("ip"); !ok {
		t.Fatal("first request should pass")
	}
	if ok, _ := rl.allow("ip"); ok {
		t.Fatal("second immediate request should be rejected")
	}
	now = now.Add(2 * time.Second) // 补充 2 个令牌（封顶 1）
	if ok, _ := rl.allow("ip"); !ok {
		t.Fatal("request after refill window should pass")
	}
}

func TestRateLimiterGCRemovesIdle(t *testing.T) {
	now := time.Now()
	rl := newRateLimiter(60, 10)
	rl.now = func() time.Time { return now }
	_, _ = rl.allow("stale")
	now = now.Add(time.Hour)
	n := rl.gc(30 * time.Minute)
	if n != 0 {
		t.Fatalf("expected idle client removed, %d remain", n)
	}
}

func TestRateLimitMiddlewareResponds429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := newRateLimiter(1, 1)
	r := gin.New()
	r.POST("/api/detect", rateLimitMiddleware(rl), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	passed := 0
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/detect", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		r.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			passed++
		} else if w.Code == http.StatusTooManyRequests {
			if w.Header().Get("Retry-After") == "" && i == 1 {
				t.Fatal("429 response should include Retry-After header")
			}
		} else {
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
	if passed != 1 {
		t.Fatalf("burst=1 limiter allowed %d requests, want 1", passed)
	}
}
