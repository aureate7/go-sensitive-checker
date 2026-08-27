package main

import (
	"math"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// clientBucket 是单个客户端 IP 的令牌桶。
type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	rps     float64 // 每秒补充的令牌数
	burst   float64 // 桶容量
	buckets map[string]*clientBucket
	now     func() time.Time
}

func newRateLimiter(requestsPerMinute, burst int) *rateLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 120
	}
	if burst <= 0 {
		burst = requestsPerMinute
	}
	return &rateLimiter{
		rps:     float64(requestsPerMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*clientBucket),
		now:     time.Now,
	}
}

// allow 消耗一个令牌；桶空闲时按经过时间补充。返回是否放行及建议重试等待时长。
func (rl *rateLimiter) allow(key string) (bool, time.Duration) {
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if b, ok := rl.buckets[key]; ok {
		elapsed := now.Sub(b.lastRefill).Seconds()
		if elapsed > 0 {
			b.tokens = math.Min(rl.burst, b.tokens+elapsed*rl.rps)
			b.lastRefill = now
		}
		if b.tokens >= 1 {
			b.tokens--
			return true, 0
		}
		retry := time.Duration((1 - b.tokens) / rl.rps * float64(time.Second))
		return false, retry
	}

	// 新客户端从满桶开始，但立即消耗一个令牌。
	rl.buckets[key] = &clientBucket{tokens: rl.burst - 1, lastRefill: now}
	return true, 0
}

// GC 周期性清理长期不活跃的客户端，防止 map 无界增长。
func (rl *rateLimiter) gc(idle time.Duration) int {
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for k, b := range rl.buckets {
		if now.Sub(b.lastRefill) > idle {
			delete(rl.buckets, k)
		}
	}
	return len(rl.buckets)
}

func rateLimitMiddleware(rl *rateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, retry := rl.allow(c.ClientIP())
		if !ok {
			c.Header("Retry-After", retry.Round(time.Second).String())
			writeAPIError(c, 429, "RATE_LIMITED", "请求过于频繁，请稍后重试", gin.H{
				"retry_after_ms": retry.Milliseconds(),
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
