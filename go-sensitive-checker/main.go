package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	RateLimitRPM    int
	RateLimitBurst  int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	ReloadToken     string
	AdminToken      string
	DataPath        string
	MaxMappings     int
	MaxMappingRunes int
	MaxBatchLines   int
	BatchWorkers    int
	MaxBatchTasks   int
	TaskRetention   time.Duration
	TaskMaxStorage  int64
	Webhook         *webhookNotifier
}

type detectorService struct {
	current      atomic.Pointer[Detector]
	reloadMu     sync.Mutex
	detectTotal  atomic.Uint64
	detectErrors atomic.Uint64
	busyRejected atomic.Uint64
	reviews      *reviewStore // 共享复核存储（registerReviewRoutes 注入）
	reloadTotal  atomic.Uint64
}

func newDetectorService(detector *Detector) *detectorService {
	service := &detectorService{}
	service.current.Store(detector)
	return service
}

func (s *detectorService) detector() *Detector { return s.current.Load() }

func (s *detectorService) reload() WordListStatus {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	current := s.detector()
	next := NewDetectorWithConfig(current.basePath, current.config)
	status := next.WordListStatus()
	if status.Ready {
		s.current.Store(next)
		s.reloadTotal.Add(1)
	}
	return status
}

func loadServerConfig() serverConfig {
	return serverConfig{
		Address:         envStr("SENSITIVE_SERVER_ADDRESS", ":8008"),
		WordListPath:    envStr("SENSITIVE_WORDLIST_PATH", "temp"),
		AllowedOrigins:  splitCSV(envStr("SENSITIVE_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")),
		MaxBodyBytes:    int64(envInt("SENSITIVE_MAX_BODY_BYTES", 1<<20)),
		MaxTextRunes:    envInt("SENSITIVE_MAX_TEXT_RUNES", 20000),
		MaxConcurrent:   envInt("SENSITIVE_MAX_CONCURRENT", 8),
		RateLimitRPM:    envInt("SENSITIVE_RATE_LIMIT_RPM", 120),
		RateLimitBurst:  envInt("SENSITIVE_RATE_LIMIT_BURST", 0),
		ReadTimeout:     time.Duration(envInt("SENSITIVE_READ_TIMEOUT_SECONDS", 10)) * time.Second,
		WriteTimeout:    time.Duration(envInt("SENSITIVE_WRITE_TIMEOUT_SECONDS", 30)) * time.Second,
		IdleTimeout:     time.Duration(envInt("SENSITIVE_IDLE_TIMEOUT_SECONDS", 60)) * time.Second,
		ShutdownTimeout: time.Duration(envInt("SENSITIVE_SHUTDOWN_TIMEOUT_SECONDS", 10)) * time.Second,
		ReloadToken:     strings.TrimSpace(envStr("SENSITIVE_RELOAD_TOKEN", "")),
		AdminToken:      strings.TrimSpace(envStr("SENSITIVE_ADMIN_TOKEN", envStr("SENSITIVE_RELOAD_TOKEN", ""))),
		DataPath:        envStr("SENSITIVE_DATA_PATH", "data"),
		MaxMappings:     envInt("SENSITIVE_MAX_CUSTOM_MAPPINGS", 500),
		MaxMappingRunes: envInt("SENSITIVE_MAX_MAPPING_RUNES", 128),
		MaxBatchLines:   envInt("SENSITIVE_MAX_BATCH_LINES", 10000),
		BatchWorkers:    envInt("SENSITIVE_BATCH_WORKERS", 4),
		MaxBatchTasks:   envInt("SENSITIVE_MAX_CONCURRENT_TASKS", 2),
		TaskRetention:   time.Duration(envInt("SENSITIVE_TASK_RETENTION_HOURS", 168)) * time.Hour,
		TaskMaxStorage:  int64(envInt("SENSITIVE_TASK_MAX_STORAGE_BYTES", 10<<30)),
		Webhook:         newWebhookNotifier(),
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
	if strings.TrimSpace(cfg.DataPath) == "" {
		cfg.DataPath = "data"
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.MaxTextRunes <= 0 {
		cfg.MaxTextRunes = 20000
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.RateLimitRPM <= 0 {
		cfg.RateLimitRPM = 120
	}
	if cfg.RateLimitBurst <= 0 {
		cfg.RateLimitBurst = cfg.RateLimitRPM / 10
		if cfg.RateLimitBurst < 10 {
			cfg.RateLimitBurst = 10
		}
	}
	if cfg.MaxMappings <= 0 {
		cfg.MaxMappings = 500
	}
	if cfg.MaxMappingRunes <= 0 {
		cfg.MaxMappingRunes = 128
	}
	if cfg.MaxBatchLines <= 0 {
		cfg.MaxBatchLines = 10000
	}
	if cfg.BatchWorkers <= 0 {
		cfg.BatchWorkers = 4
	}
	if cfg.MaxBatchTasks <= 0 {
		cfg.MaxBatchTasks = 2
	}
	if cfg.TaskRetention <= 0 {
		cfg.TaskRetention = 7 * 24 * time.Hour
	}
	if cfg.TaskMaxStorage <= 0 {
		cfg.TaskMaxStorage = 10 << 30
	}
	return cfg
}

func newRouter(service *detectorService, cfg serverConfig) *gin.Engine {
	cfg = normalizedServerConfig(cfg)
	r := gin.New()
	r.Use(requestIDMiddleware(), gin.Logger(), gin.Recovery(), securityHeaders())
	_ = r.SetTrustedProxies(nil)
	if len(cfg.AllowedOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins: cfg.AllowedOrigins,
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowHeaders: []string{"Content-Type", "Authorization"},
			MaxAge:       12 * time.Hour,
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
	detectLimiter := newRateLimiter(cfg.RateLimitRPM, cfg.RateLimitBurst)
	r.POST("/api/detect", func(c *gin.Context) {
		if ok, retry := detectLimiter.allow(c.ClientIP()); !ok {
			c.Header("Retry-After", retry.Round(time.Second).String())
			writeAPIError(c, http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后重试", gin.H{
				"retry_after_ms": retry.Milliseconds(),
			})
			return
		}
		service.detectTotal.Add(1)
		detector := service.detector()
		if !detector.WordListStatus().Ready {
			service.detectErrors.Add(1)
			writeAPIError(c, http.StatusServiceUnavailable, "WORDLIST_NOT_READY", "词库尚未就绪", nil)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxBodyBytes)
		var req detectReq
		if err := c.ShouldBindJSON(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				service.detectErrors.Add(1)
				writeAPIError(c, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "请求体超过限制", gin.H{"max_body_bytes": cfg.MaxBodyBytes})
				return
			}
			service.detectErrors.Add(1)
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "请求体不是有效 JSON", nil)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			service.detectErrors.Add(1)
			writeAPIError(c, http.StatusUnprocessableEntity, "TEXT_REQUIRED", "检测文本不能为空", nil)
			return
		}
		if utf8.RuneCountInString(req.Text) > cfg.MaxTextRunes {
			service.detectErrors.Add(1)
			writeAPIError(c, http.StatusRequestEntityTooLarge, "TEXT_TOO_LARGE", "检测文本超过字符限制", gin.H{"max_text_runes": cfg.MaxTextRunes})
			return
		}
		if validationErr := validateDetectRequest(req, cfg); validationErr != nil {
			service.detectErrors.Add(1)
			writeAPIError(c, http.StatusUnprocessableEntity, validationErr.Code, validationErr.Message, validationErr.Details)
			return
		}
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		case <-c.Request.Context().Done():
			return
		default:
			service.busyRejected.Add(1)
			c.Header("Retry-After", "1")
			writeAPIError(c, http.StatusTooManyRequests, "SERVER_BUSY", "服务器繁忙，请稍后重试", nil)
			return
		}
		response := detector.DetectWithContext(c.Request.Context(), req.Text, req.Categories, req.Options)
		if cfg.Webhook != nil {
			cfg.Webhook.Notify("high_risk_hit", "检测到高风险内容", describeRiskSummary(&response), response.RiskLevel)
		}
		// LLM 判为疑似误报的命中自动回流为白名单候选，等待人工确认。
		if service.reviews != nil && response.LLMAssist != nil && len(response.LLMAssist.HitReviews) > 0 {
			if added, err := service.reviews.recordDemoteCandidates(response.LLMAssist.HitReviews); err != nil {
				log.Printf("record demote candidates failed: %v", err)
			} else if added > 0 {
				log.Printf("recorded %d demote whitelist candidate(s)", added)
			}
		}
		c.JSON(http.StatusOK, response)
	})

	r.GET("/api/statistics", func(c *gin.Context) { c.JSON(http.StatusOK, service.detector().Statistics()) })
	r.GET("/api/categories", func(c *gin.Context) { c.JSON(http.StatusOK, CategoryDisplay) })
	r.GET("/api/status", func(c *gin.Context) {
		detector := service.detector()
		c.JSON(http.StatusOK, gin.H{
			"wordlist":     detector.WordListStatus(),
			"capabilities": gin.H{"llm_assist": detector.config.EnableLLMAssist, "hot_reload": cfg.ReloadToken != "", "wordlist_admin": cfg.AdminToken != ""},
			"limits":       gin.H{"max_body_bytes": cfg.MaxBodyBytes, "max_text_runes": cfg.MaxTextRunes, "max_concurrent": cfg.MaxConcurrent, "max_custom_mappings": cfg.MaxMappings},
			"metrics":      gin.H{"detect_total": service.detectTotal.Load(), "detect_errors": service.detectErrors.Load(), "busy_rejected": service.busyRejected.Load(), "reload_total": service.reloadTotal.Load()},
		})
	})
	if cfg.ReloadToken != "" {
		r.POST("/api/admin/wordlist/reload", func(c *gin.Context) {
			if subtleTokenMismatch(c.GetHeader("Authorization"), cfg.ReloadToken) {
				writeAPIError(c, http.StatusUnauthorized, "UNAUTHORIZED", "无权执行词库重载", nil)
				return
			}
			status := service.reload()
			if !status.Ready {
				writeAPIError(c, http.StatusUnprocessableEntity, "WORDLIST_RELOAD_FAILED", "新词库校验失败，当前词库保持不变", status)
				return
			}
			c.JSON(http.StatusOK, gin.H{"reloaded": true, "wordlist": status})
		})
	}
	if cfg.AdminToken != "" {
		registerAdminRoutes(r, newAdminManager(service, cfg.DataPath), cfg.AdminToken)
	}
	if err := registerPlatformRoutes(r, service, cfg.AdminToken, cfg.DataPath, cfg.MaxBatchLines, cfg.BatchWorkers, cfg.MaxBatchTasks, cfg.TaskRetention, cfg.TaskMaxStorage, cfg.Webhook); err != nil {
		log.Printf("platform features disabled: %v", err)
	}
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "alive"}) })
	r.GET("/health/ready", func(c *gin.Context) {
		status := service.detector().WordListStatus()
		code := http.StatusOK
		if !status.Ready {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, status)
	})
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

type apiValidationError struct {
	Code, Message string
	Details       any
}

func validateDetectRequest(req detectReq, cfg serverConfig) *apiValidationError {
	if len(req.Categories) == 0 {
		return &apiValidationError{Code: "CATEGORIES_REQUIRED", Message: "至少选择一个检测类别"}
	}
	seen := make(map[string]struct{}, len(req.Categories))
	for _, category := range req.Categories {
		if _, ok := CategoryDisplay[category]; !ok {
			return &apiValidationError{Code: "INVALID_CATEGORY", Message: "包含未知检测类别", Details: gin.H{"category": category}}
		}
		if _, ok := seen[category]; ok {
			return &apiValidationError{Code: "DUPLICATE_CATEGORY", Message: "检测类别不能重复", Details: gin.H{"category": category}}
		}
		seen[category] = struct{}{}
	}
	if req.Options == nil {
		return nil
	}
	if len(req.Options.CustomMappings) > cfg.MaxMappings {
		return &apiValidationError{Code: "TOO_MANY_MAPPINGS", Message: "自定义映射数量超过限制", Details: gin.H{"max_custom_mappings": cfg.MaxMappings}}
	}
	if req.Options.MappingMode != "" && req.Options.MappingMode != MappingModeIncremental && req.Options.MappingMode != MappingModeOverride {
		return &apiValidationError{Code: "INVALID_MAPPING_MODE", Message: "映射模式无效"}
	}
	for index, mapping := range req.Options.CustomMappings {
		if strings.TrimSpace(mapping.From) == "" || strings.TrimSpace(mapping.To) == "" || utf8.RuneCountInString(mapping.From) > cfg.MaxMappingRunes || utf8.RuneCountInString(mapping.To) > cfg.MaxMappingRunes {
			return &apiValidationError{Code: "INVALID_MAPPING", Message: "自定义映射包含空值或超长内容", Details: gin.H{"index": index, "max_mapping_runes": cfg.MaxMappingRunes}}
		}
	}
	return nil
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			var raw [12]byte
			if _, err := rand.Read(raw[:]); err == nil {
				requestID = hex.EncodeToString(raw[:])
			} else {
				requestID = time.Now().UTC().Format("20060102150405.000000000")
			}
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func writeAPIError(c *gin.Context, status int, code, message string, details any) {
	payload := gin.H{
		"error": gin.H{
			"code":       code,
			"message":    message,
			"request_id": c.GetString("request_id"),
		},
	}
	if details != nil {
		payload["error"].(gin.H)["details"] = details
	}
	c.JSON(status, payload)
}

func subtleTokenMismatch(header, expected string) bool {
	provided := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if len(provided) != len(expected) {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1
}

func main() {
	// 离线评测模式：go run . -eval evalset/samples.jsonl
	if len(os.Args) > 1 && os.Args[1] == "-eval" {
		runEval(os.Args[2:])
		return
	}
	cfg := normalizedServerConfig(loadServerConfig())
	detector := NewDetector(cfg.WordListPath)
	service := newDetectorService(detector)
	status := detector.WordListStatus()
	log.Printf("word list status: ready=%t words=%d loaded_files=%d missing_files=%d path=%s", status.Ready, status.TotalWords, status.LoadedFiles, status.MissingFiles, status.BasePath)
	for _, loadErr := range status.Errors {
		log.Printf("word list error: %s", loadErr)
	}

	server := &http.Server{
		Addr: cfg.Address, Handler: newRouter(service, cfg),
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
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server stopped unexpectedly: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		_ = server.Close()
	}
}
