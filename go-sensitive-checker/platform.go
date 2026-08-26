package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

type DetectionPolicy struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Categories   []string      `json:"categories"`
	Options      DetectOptions `json:"options"`
	MaxTextRunes int           `json:"max_text_runes"`
	Enabled      bool          `json:"enabled"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type policyStore struct {
	path     string
	mu       sync.RWMutex
	policies map[string]DetectionPolicy
}

func newPolicyStore(dataPath string) (*policyStore, error) {
	store := &policyStore{path: filepath.Join(dataPath, "policies.json"), policies: map[string]DetectionPolicy{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	if len(store.policies) == 0 {
		now := time.Now().UTC()
		store.policies["default"] = DetectionPolicy{ID: "default", Name: "默认全类别策略", Categories: sortedCategories(), Options: DefaultDetectOptions(), MaxTextRunes: 20000, Enabled: true, CreatedAt: now, UpdatedAt: now}
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func sortedCategories() []string {
	out := make([]string, 0, len(CategoryDisplay))
	for category := range CategoryDisplay {
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func (s *policyStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var policies []DetectionPolicy
	if err := json.Unmarshal(raw, &policies); err != nil {
		return err
	}
	for _, policy := range policies {
		s.policies[policy.ID] = policy
	}
	return nil
}

func (s *policyStore) list(enabledOnly bool) []DetectionPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DetectionPolicy, 0, len(s.policies))
	for _, policy := range s.policies {
		if !enabledOnly || policy.Enabled {
			out = append(out, policy)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *policyStore) get(id string) (DetectionPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[id]
	return policy, ok
}

func (s *policyStore) upsert(policy DetectionPolicy) (DetectionPolicy, error) {
	if err := validatePolicy(policy); err != nil {
		return DetectionPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.policies[policy.ID]; ok {
		policy.CreatedAt = existing.CreatedAt
	} else {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now
	s.policies[policy.ID] = policy
	if err := s.saveLocked(); err != nil {
		return DetectionPolicy{}, err
	}
	return policy, nil
}

func (s *policyStore) delete(id string) error {
	if id == "default" {
		return fmt.Errorf("default policy cannot be deleted")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.policies[id]; !ok {
		return os.ErrNotExist
	}
	delete(s.policies, id)
	return s.saveLocked()
}

func (s *policyStore) saveLocked() error {
	policies := make([]DetectionPolicy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
	raw, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.path, append(raw, '\n'))
}

func validatePolicy(policy DetectionPolicy) error {
	policy.ID = strings.TrimSpace(policy.ID)
	if policy.ID == "" || len(policy.ID) > 64 {
		return fmt.Errorf("invalid policy id")
	}
	for _, r := range policy.ID {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return fmt.Errorf("policy id must use lowercase letters, digits, hyphen or underscore")
		}
	}
	if strings.TrimSpace(policy.Name) == "" || len([]rune(policy.Name)) > 100 {
		return fmt.Errorf("invalid policy name")
	}
	if policy.MaxTextRunes < 1 || policy.MaxTextRunes > 100000 {
		return fmt.Errorf("invalid max_text_runes")
	}
	if len(policy.Categories) == 0 {
		return fmt.Errorf("categories required")
	}
	seen := map[string]struct{}{}
	for _, category := range policy.Categories {
		if _, ok := CategoryDisplay[category]; !ok {
			return fmt.Errorf("unknown category %s", category)
		}
		if _, ok := seen[category]; ok {
			return fmt.Errorf("duplicate category %s", category)
		}
		seen[category] = struct{}{}
	}
	return nil
}

type BatchTask struct {
	ID           string     `json:"id"`
	PolicyID     string     `json:"policy_id"`
	Status       string     `json:"status"`
	Total        int        `json:"total"`
	Processed    int        `json:"processed"`
	Sensitive    int        `json:"sensitive"`
	Failed       int        `json:"failed"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Error        string     `json:"error,omitempty"`
	ParentTaskID string     `json:"parent_task_id,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at"`
	ResultBytes  int64      `json:"result_bytes"`
}

type batchCreateRequest struct {
	PolicyID string   `json:"policy_id"`
	Lines    []string `json:"lines"`
}
type batchResult struct {
	Line     int             `json:"line"`
	Text     string          `json:"text"`
	Response *DetectResponse `json:"response,omitempty"`
	Error    string          `json:"error,omitempty"`
}
type taskRuntime struct {
	task   BatchTask
	cancel context.CancelFunc
}

type taskManager struct {
	service         *detectorService
	policies        *policyStore
	path            string
	maxLines        int
	workers         int
	taskSlots       chan struct{}
	retention       time.Duration
	maxStorageBytes int64
	mu              sync.RWMutex
	tasks           map[string]*taskRuntime
}

func newTaskManager(service *detectorService, policies *policyStore, dataPath string, maxLines, workers, maxConcurrentTasks int, retention time.Duration, maxStorageBytes int64) (*taskManager, error) {
	if maxLines <= 0 {
		maxLines = 10000
	}
	if workers <= 0 {
		workers = 4
	}
	if workers > 32 {
		workers = 32
	}
	if maxConcurrentTasks <= 0 {
		maxConcurrentTasks = 2
	}
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	if maxStorageBytes <= 0 {
		maxStorageBytes = 10 << 30
	}
	m := &taskManager{service: service, policies: policies, path: filepath.Join(dataPath, "tasks"), maxLines: maxLines, workers: workers, taskSlots: make(chan struct{}, maxConcurrentTasks), retention: retention, maxStorageBytes: maxStorageBytes, tasks: map[string]*taskRuntime{}}
	if err := os.MkdirAll(m.path, 0o700); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(m.path)
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(m.path, entry.Name(), "task.json"))
		if err != nil {
			continue
		}
		var task BatchTask
		if json.Unmarshal(raw, &task) != nil {
			continue
		}
		if task.Status == "queued" || task.Status == "running" {
			task.Status = "interrupted"
			task.Error = "service restarted before completion"
			now := time.Now().UTC()
			task.CompletedAt = &now
		}
		m.tasks[task.ID] = &taskRuntime{task: task}
		_ = m.persist(&task)
	}
	return m, nil
}

func taskID() string {
	var raw [10]byte
	_, _ = rand.Read(raw[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(raw[:])
}

func (m *taskManager) create(req batchCreateRequest) (BatchTask, error) {
	return m.createWithParent(req, "")
}

func (m *taskManager) createWithParent(req batchCreateRequest, parentID string) (BatchTask, error) {
	policy, ok := m.policies.get(strings.TrimSpace(req.PolicyID))
	if !ok || !policy.Enabled {
		return BatchTask{}, fmt.Errorf("policy not found or disabled")
	}
	if len(req.Lines) == 0 || len(req.Lines) > m.maxLines {
		return BatchTask{}, fmt.Errorf("line count must be between 1 and %d", m.maxLines)
	}
	for _, line := range req.Lines {
		if utf8.RuneCountInString(line) > policy.MaxTextRunes {
			return BatchTask{}, fmt.Errorf("line exceeds policy limit")
		}
	}
	used, _ := directorySize(m.path)
	estimated := int64(0)
	for _, line := range req.Lines {
		estimated += int64(len(line) * 3)
	}
	if used+estimated > m.maxStorageBytes {
		return BatchTask{}, fmt.Errorf("task storage limit exceeded")
	}
	now := time.Now().UTC()
	task := BatchTask{ID: taskID(), PolicyID: policy.ID, Status: "queued", Total: len(req.Lines), CreatedAt: now, ParentTaskID: parentID, ExpiresAt: now.Add(m.retention)}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &taskRuntime{task: task, cancel: cancel}
	m.mu.Lock()
	m.tasks[task.ID] = runtime
	m.mu.Unlock()
	if err := m.persist(&runtime.task); err != nil {
		cancel()
		return BatchTask{}, err
	}
	inputRaw, err := json.Marshal(req.Lines)
	if err != nil {
		cancel()
		return BatchTask{}, err
	}
	if err := atomicWriteFile(filepath.Join(m.path, task.ID, "input.json"), inputRaw); err != nil {
		cancel()
		return BatchTask{}, err
	}
	go func(lines []string) {
		select {
		case m.taskSlots <- struct{}{}:
			defer func() { <-m.taskSlots }()
			m.run(ctx, runtime, policy, lines)
		case <-ctx.Done():
			m.finish(runtime, "cancelled", "")
		}
	}(append([]string(nil), req.Lines...))
	return task, nil
}

func (m *taskManager) run(ctx context.Context, runtime *taskRuntime, policy DetectionPolicy, lines []string) {
	now := time.Now().UTC()
	m.update(runtime, func(task *BatchTask) { task.Status = "running"; task.StartedAt = &now })
	resultPath := filepath.Join(m.path, runtime.task.ID, "results.jsonl")
	file, err := os.OpenFile(resultPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		m.finish(runtime, "failed", err.Error())
		return
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	type batchJob struct {
		index int
		text  string
	}
	jobs := make(chan batchJob)
	results := make(chan batchResult)
	var workers sync.WaitGroup
	for worker := 0; worker < m.workers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				response := m.service.detector().DetectWithContext(ctx, job.text, policy.Categories, &policy.Options)
				select {
				case results <- batchResult{Line: job.index + 1, Text: job.text, Response: &response}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index, line := range lines {
			select {
			case jobs <- batchJob{index: index, text: line}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	pending := make(map[int]batchResult)
	nextLine := 1
	for result := range results {
		pending[result.Line] = result
		for {
			ordered, ok := pending[nextLine]
			if !ok {
				break
			}
			delete(pending, nextLine)
			writeErr := encoder.Encode(ordered)
			m.mu.Lock()
			if writeErr != nil {
				runtime.task.Failed++
			} else {
				runtime.task.Processed++
				if ordered.Response != nil && ordered.Response.HasSensitive {
					runtime.task.Sensitive++
				}
			}
			snapshot := runtime.task
			m.mu.Unlock()
			if snapshot.Processed%100 == 0 {
				_ = m.persist(&snapshot)
			}
			nextLine++
		}
	}
	if ctx.Err() != nil {
		m.finish(runtime, "cancelled", "")
		return
	}
	_ = file.Sync()
	if info, statErr := file.Stat(); statErr == nil {
		m.mu.Lock()
		runtime.task.ResultBytes = info.Size()
		m.mu.Unlock()
	}
	m.finish(runtime, "completed", "")
}

func (m *taskManager) update(runtime *taskRuntime, fn func(*BatchTask)) {
	m.mu.Lock()
	fn(&runtime.task)
	snapshot := runtime.task
	m.mu.Unlock()
	_ = m.persist(&snapshot)
}
func (m *taskManager) finish(runtime *taskRuntime, status, message string) {
	now := time.Now().UTC()
	m.update(runtime, func(task *BatchTask) { task.Status = status; task.Error = message; task.CompletedAt = &now })
}
func (m *taskManager) persist(task *BatchTask) error {
	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(m.path, task.ID, "task.json"), append(raw, '\n'))
}
func atomicWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(name, path)
}

func (m *taskManager) list() []BatchTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]BatchTask, 0, len(m.tasks))
	for _, runtime := range m.tasks {
		out = append(out, runtime.task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (m *taskManager) get(id string) (BatchTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	runtime, ok := m.tasks[id]
	if !ok {
		return BatchTask{}, false
	}
	return runtime.task, true
}
func (m *taskManager) cancel(id string) bool {
	m.mu.RLock()
	runtime, ok := m.tasks[id]
	if !ok {
		m.mu.RUnlock()
		return false
	}
	cancel := runtime.cancel
	status := runtime.task.Status
	m.mu.RUnlock()
	if cancel == nil || status == "completed" || status == "cancelled" || status == "failed" {
		return false
	}
	cancel()
	return true
}

func (m *taskManager) retry(id string) (BatchTask, error) {
	task, ok := m.get(id)
	if !ok {
		return BatchTask{}, os.ErrNotExist
	}
	if task.Status == "queued" || task.Status == "running" {
		return BatchTask{}, fmt.Errorf("task is still active")
	}
	raw, err := os.ReadFile(filepath.Join(m.path, id, "input.json"))
	if err != nil {
		return BatchTask{}, err
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err != nil {
		return BatchTask{}, err
	}
	return m.createWithParent(batchCreateRequest{PolicyID: task.PolicyID, Lines: lines}, id)
}

func (m *taskManager) delete(id string) error {
	m.mu.Lock()
	runtime, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return os.ErrNotExist
	}
	if runtime.task.Status == "queued" || runtime.task.Status == "running" {
		m.mu.Unlock()
		return fmt.Errorf("active task cannot be deleted")
	}
	delete(m.tasks, id)
	m.mu.Unlock()
	return os.RemoveAll(filepath.Join(m.path, id))
}

func (m *taskManager) cleanup(now time.Time) (int, error) {
	tasks := m.list()
	removed := 0
	for _, task := range tasks {
		if task.ExpiresAt.IsZero() || task.ExpiresAt.After(now) || task.Status == "queued" || task.Status == "running" {
			continue
		}
		if err := m.delete(task.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func (m *taskManager) storageStatus() gin.H {
	used, err := directorySize(m.path)
	return gin.H{"used_bytes": used, "max_bytes": m.maxStorageBytes, "available_bytes": m.maxStorageBytes - used, "retention_hours": int(m.retention.Hours()), "error": errorText(err)}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func registerPlatformRoutes(r *gin.Engine, service *detectorService, token, dataPath string, maxLines, workers, maxConcurrentTasks int, retention time.Duration, maxStorageBytes int64) error {
	policies, err := newPolicyStore(dataPath)
	if err != nil {
		return err
	}
	tasks, err := newTaskManager(service, policies, dataPath, maxLines, workers, maxConcurrentTasks, retention, maxStorageBytes)
	if err != nil {
		return err
	}
	r.GET("/api/policies", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"items": policies.list(true)}) })
	if token == "" {
		return nil
	}
	admin := r.Group("/api/platform")
	admin.Use(adminTokenMiddleware(token))
	admin.GET("/policies", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"items": policies.list(false)}) })
	admin.PUT("/policies/:id", func(c *gin.Context) {
		var policy DetectionPolicy
		if c.ShouldBindJSON(&policy) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "策略格式无效", nil)
			return
		}
		policy.ID = c.Param("id")
		saved, err := policies.upsert(policy)
		if err != nil {
			writeAPIError(c, 422, "INVALID_POLICY", err.Error(), nil)
			return
		}
		c.JSON(200, saved)
	})
	admin.DELETE("/policies/:id", func(c *gin.Context) {
		err := policies.delete(c.Param("id"))
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, 404, "POLICY_NOT_FOUND", "策略不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, 422, "POLICY_DELETE_FAILED", err.Error(), nil)
			return
		}
		c.Status(204)
	})
	admin.POST("/tasks", func(c *gin.Context) {
		var req batchCreateRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "任务格式无效", nil)
			return
		}
		task, err := tasks.create(req)
		if err != nil {
			writeAPIError(c, 422, "TASK_INVALID", err.Error(), gin.H{"max_lines": tasks.maxLines})
			return
		}
		c.JSON(202, task)
	})
	admin.GET("/tasks", func(c *gin.Context) { c.JSON(200, gin.H{"items": tasks.list()}) })
	admin.GET("/tasks/:id", func(c *gin.Context) {
		task, ok := tasks.get(c.Param("id"))
		if !ok {
			writeAPIError(c, 404, "TASK_NOT_FOUND", "任务不存在", nil)
			return
		}
		c.JSON(200, task)
	})
	admin.POST("/tasks/:id/cancel", func(c *gin.Context) {
		if !tasks.cancel(c.Param("id")) {
			writeAPIError(c, 409, "TASK_NOT_CANCELLABLE", "任务不存在或无法取消", nil)
			return
		}
		c.JSON(202, gin.H{"cancel_requested": true})
	})
	admin.POST("/tasks/:id/retry", func(c *gin.Context) {
		task, err := tasks.retry(c.Param("id"))
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, 404, "TASK_NOT_FOUND", "任务不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, 409, "TASK_NOT_RETRYABLE", err.Error(), nil)
			return
		}
		c.JSON(http.StatusAccepted, task)
	})
	admin.DELETE("/tasks/:id", func(c *gin.Context) {
		err := tasks.delete(c.Param("id"))
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, 404, "TASK_NOT_FOUND", "任务不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, 409, "TASK_NOT_DELETABLE", err.Error(), nil)
			return
		}
		c.Status(http.StatusNoContent)
	})
	admin.GET("/storage", func(c *gin.Context) { c.JSON(http.StatusOK, tasks.storageStatus()) })
	admin.POST("/tasks/cleanup", func(c *gin.Context) {
		removed, err := tasks.cleanup(time.Now().UTC())
		if err != nil {
			writeAPIError(c, 500, "CLEANUP_FAILED", "任务清理失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"removed": removed})
	})
	admin.GET("/tasks/:id/results", func(c *gin.Context) {
		task, ok := tasks.get(c.Param("id"))
		if !ok {
			writeAPIError(c, 404, "TASK_NOT_FOUND", "任务不存在", nil)
			return
		}
		path := filepath.Join(tasks.path, task.ID, "results.jsonl")
		if c.Query("format") == "csv" {
			exportTaskCSV(c, path)
			return
		}
		c.FileAttachment(path, task.ID+".jsonl")
	})
	return nil
}

func adminTokenMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if subtleTokenMismatch(c.GetHeader("Authorization"), token) {
			writeAPIError(c, 401, "UNAUTHORIZED", "管理令牌无效", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

func exportTaskCSV(c *gin.Context, path string) {
	file, err := os.Open(path)
	if err != nil {
		writeAPIError(c, 404, "RESULT_NOT_FOUND", "任务结果尚不可用", nil)
		return
	}
	defer file.Close()
	c.Header("Content-Disposition", `attachment; filename="results.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"line", "text", "has_sensitive", "risk_level", "total_count", "error"})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var result batchResult
		if json.Unmarshal(scanner.Bytes(), &result) != nil {
			continue
		}
		row := []string{strconv.Itoa(result.Line), result.Text, "false", "", "0", result.Error}
		if result.Response != nil {
			row[2] = strconv.FormatBool(result.Response.HasSensitive)
			row[3] = result.Response.RiskLevel
			row[4] = strconv.Itoa(result.Response.TotalCount)
		}
		_ = writer.Write(row)
	}
	writer.Flush()
}
