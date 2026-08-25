package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type detectReq struct {
	Text       string         `json:"text"`
	Categories []string       `json:"categories"`
	Options    *DetectOptions `json:"options,omitempty"`
}

type serverConfig struct {
	Address         string
	WordListPath    string
	AllowedOrigins  []string
	MaxBodyBytes    int64
	MaxTextRunes    int
	MaxConcurrent   int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func loadServerConfig() serverConfig {
	return serverConfig{
		Address:         envStr("SENSITIVE_SERVER_ADDRESS", ":8008"),
		WordListPath:    envStr("SENSITIVE_WORDLIST_PATH", "temp"),
		AllowedOrigins:  splitCSV(envStr("SENSITIVE_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		MaxBodyBytes:    int64(envInt("SENSITIVE_MAX_BODY_BYTES", 1<<20)),
		MaxTextRunes:    envInt("SENSITIVE_MAX_TEXT_RUNES", 20000),
		MaxConcurrent:   envInt("SENSITIVE_MAX_CONCURRENT", 8),
		ReadTimeout:     time.Duration(envInt("SENSITIVE_READ_TIMEOUT_SECONDS", 10)) * time.Second,
		WriteTimeout:    time.Duration(envInt("SENSITIVE_WRITE_TIMEOUT_SECONDS", 30)) * time.Second,
		IdleTimeout:     time.Duration(envInt("SENSITIVE_IDLE_TIMEOUT_SECONDS", 60)) * time.Second,
		ShutdownTimeout: time.Duration(envInt("SENSITIVE_SHUTDOWN_TIMEOUT_SECONDS", 10)) * time.Second,
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizedServerConfig(cfg serverConfig) serverConfig {
	if cfg.MaxBodyBytes <= 0 { cfg.MaxBodyBytes = 1 << 20 }
	if cfg.MaxTextRunes <= 0 { cfg.MaxTextRunes = 20000 }
	if cfg.MaxConcurrent <= 0 { cfg.MaxConcurrent = 8 }
	return cfg
}

func newRouter(detector *Detector, cfg serverConfig) *gin.Engine {
	cfg = normalizedServerConfig(cfg)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), securityHeaders())
	_ = r.SetTrustedProxies(nil)
	if len(cfg.AllowedOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins: cfg.AllowedOrigins,
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowHeaders: []string{"Content-Type", "Authorization"},
			MaxAge: 12 * time.Hour,
		}))
	}

	templatePattern := filepath.Join("templates", "*.html")
	if matches, _ := filepath.Glob(templatePattern); len(matches) > 0 {
		r.LoadHTMLGlob(templatePattern)
		r.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index_new.html", gin.H{"title": "敏感词检测系统 (Go + Gin)"})
		})
	}

	semaphore := make(chan struct{}, cfg.MaxConcurrent)
	r.POST("/api/detect", func(c *gin.Context) {
		if !detector.WordListStatus().Ready {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "word list is not ready"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxBodyBytes)
		var req detectReq
		if err := c.ShouldBindJSON(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body exceeds configured limit"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "text is required"})
			return
		}
		if utf8.RuneCountInString(req.Text) > cfg.MaxTextRunes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "text exceeds configured limit", "max_text_runes": cfg.MaxTextRunes})
			return
		}
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		case <-c.Request.Context().Done():
			return
		default:
			c.Header("Retry-After", "1")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "server is busy"})
			return
		}
		c.JSON(http.StatusOK, detector.DetectWithContext(c.Request.Context(), req.Text, req.Categories, req.Options))
	})

	r.GET("/api/statistics", func(c *gin.Context) { c.JSON(http.StatusOK, detector.Statistics()) })
	r.GET("/api/categories", func(c *gin.Context) { c.JSON(http.StatusOK, CategoryDisplay) })
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "alive"}) })
	r.GET("/health/ready", func(c *gin.Context) {
		status := detector.WordListStatus()
		code := http.StatusOK
		if !status.Ready { code = http.StatusServiceUnavailable }
		c.JSON(code, status)
	})
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

func main() {
	cfg := normalizedServerConfig(loadServerConfig())
	detector := NewDetector(cfg.WordListPath)
	status := detector.WordListStatus()
	log.Printf("word list status: ready=%t words=%d loaded_files=%d missing_files=%d path=%s", status.Ready, status.TotalWords, status.LoadedFiles, status.MissingFiles, status.BasePath)
	for _, loadErr := range status.Errors { log.Printf("word list error: %s", loadErr) }

	server := &http.Server{
		Addr: cfg.Address, Handler: newRouter(detector, cfg),
		ReadTimeout: cfg.ReadTimeout, ReadHeaderTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("sensitive checker listening on %s", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-signals:
		log.Printf("received signal %s; shutting down", sig)
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) { log.Fatalf("server stopped unexpectedly: %v", err) }
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		_ = server.Close()
	}
}
