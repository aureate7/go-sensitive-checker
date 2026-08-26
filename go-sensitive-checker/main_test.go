package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func performJSONRequest(router http.Handler, method, path string, payload any, headers map[string]string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestReadinessFailsForEmptyWordList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	detector := NewDetector(t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	newRouter(newDetectorService(detector), serverConfig{DataPath: t.TempDir()}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for empty word list, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDetectRejectsTextOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	detector := NewDetector(setupTestWordRepo(t))
	body, err := json.Marshal(detectReq{Text: "超过限制"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/detect", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	newRouter(newDetectorService(detector), serverConfig{MaxTextRunes: 2, MaxBodyBytes: 1024, MaxConcurrent: 1, DataPath: t.TempDir()}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	newRouter(newDetectorService(NewDetector(t.TempDir())), serverConfig{DataPath: t.TempDir()}).ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected security header: %q", got)
	}
}

func TestDetectRejectsUnknownCategoryWithStableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter(newDetectorService(NewDetector(setupTestWordRepo(t))), serverConfig{DataPath: t.TempDir()})
	recorder := performJSONRequest(router, http.MethodPost, "/api/detect", detectReq{Text: "测试", Categories: []string{"unknown"}}, nil)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "INVALID_CATEGORY" || payload.Error.RequestID == "" {
		t.Fatalf("unexpected error payload: %+v", payload.Error)
	}
}

func TestWordListReloadKeepsCurrentDetectorOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := setupTestWordRepo(t)
	service := newDetectorService(NewDetector(base))
	before := service.detector()
	before.basePath = t.TempDir()
	router := newRouter(service, serverConfig{ReloadToken: "test-secret", DataPath: t.TempDir()})
	recorder := performJSONRequest(router, http.MethodPost, "/api/admin/wordlist/reload", nil, map[string]string{"Authorization": "Bearer test-secret"})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected failed reload status, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.detector() != before {
		t.Fatal("failed reload replaced the active detector")
	}
}

func TestStatusReportsCapabilitiesAndLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter(newDetectorService(NewDetector(setupTestWordRepo(t))), serverConfig{MaxTextRunes: 123, ReloadToken: "enabled", DataPath: t.TempDir()})
	recorder := performJSONRequest(router, http.MethodGet, "/api/status", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"max_text_runes":123`)) || !bytes.Contains(recorder.Body.Bytes(), []byte(`"hot_reload":true`)) {
		t.Fatalf("unexpected status: %s", recorder.Body.String())
	}
}

func TestAdminWordLifecycleAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	router := newRouter(service, serverConfig{AdminToken: "admin-secret", DataPath: t.TempDir()})

	unauthorized := performJSONRequest(router, http.MethodGet, "/api/admin/words", nil, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", unauthorized.Code)
	}

	headers := map[string]string{"Authorization": "Bearer admin-secret"}
	created := performJSONRequest(router, http.MethodPost, "/api/admin/words", wordMutationRequest{
		Category: AbusiveHigh, Word: "新增测试词", Reason: "unit test",
	}, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", created.Code, created.Body.String())
	}
	if _, ok := service.detector().sensitiveWords[AbusiveHigh]["新增测试词"]; !ok {
		t.Fatal("created word not active after reload")
	}

	duplicate := performJSONRequest(router, http.MethodPost, "/api/admin/words", wordMutationRequest{
		Category: AbusiveHigh, Word: "新增测试词",
	}, headers)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expected duplicate conflict, got %d", duplicate.Code)
	}

	preview := performJSONRequest(router, http.MethodPost, "/api/admin/words/import/preview", importRequest{
		Category: AbusiveHigh, Content: "新增测试词\n批量新词\n批量新词\n",
	}, headers)
	if preview.Code != http.StatusOK || !bytes.Contains(preview.Body.Bytes(), []byte("批量新词")) {
		t.Fatalf("unexpected preview: %d %s", preview.Code, preview.Body.String())
	}

	versions := performJSONRequest(router, http.MethodGet, "/api/admin/wordlists/versions", nil, headers)
	if versions.Code != http.StatusOK || !bytes.Contains(versions.Body.Bytes(), []byte("version")) {
		t.Fatalf("expected version snapshot: %d %s", versions.Code, versions.Body.String())
	}

	audit := performJSONRequest(router, http.MethodGet, "/api/admin/audit", nil, headers)
	if audit.Code != http.StatusOK || !bytes.Contains(audit.Body.Bytes(), []byte("word.create")) {
		t.Fatalf("expected audit entry: %d %s", audit.Code, audit.Body.String())
	}

	deleted := performJSONRequest(router, http.MethodDelete, "/api/admin/words", wordMutationRequest{
		Category: AbusiveHigh, Word: "新增测试词", Reason: "unit test cleanup",
	}, headers)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", deleted.Code, deleted.Body.String())
	}
	if _, ok := service.detector().sensitiveWords[AbusiveHigh]["新增测试词"]; ok {
		t.Fatal("deleted word remains active")
	}

	var versionPayload struct {
		Items []struct {
			Version string `json:"version"`
		} `json:"items"`
	}
	latestVersions := performJSONRequest(router, http.MethodGet, "/api/admin/wordlists/versions", nil, headers)
	if err := json.Unmarshal(latestVersions.Body.Bytes(), &versionPayload); err != nil || len(versionPayload.Items) == 0 {
		t.Fatalf("decode versions: %v %s", err, latestVersions.Body.String())
	}
	rolledBack := performJSONRequest(router, http.MethodPost, "/api/admin/wordlists/rollback/"+versionPayload.Items[0].Version, nil, headers)
	if rolledBack.Code != http.StatusOK {
		t.Fatalf("rollback failed: %d %s", rolledBack.Code, rolledBack.Body.String())
	}
	if _, ok := service.detector().sensitiveWords[AbusiveHigh]["新增测试词"]; !ok {
		t.Fatal("rollback did not restore deleted word")
	}
}

func TestParseImportRejectsUnknownCategory(t *testing.T) {
	_, err := parseImport("unknown", "test", nil)
	if err == nil {
		t.Fatal("expected unknown category error")
	}
}

func TestPolicyStorePersistsAndValidatesPolicies(t *testing.T) {
	dataPath := t.TempDir()
	store, err := newPolicyStore(dataPath)
	if err != nil {
		t.Fatalf("create policy store: %v", err)
	}
	if _, ok := store.get("default"); !ok {
		t.Fatal("default policy missing")
	}
	policy := DetectionPolicy{ID: "comments", Name: "评论审核", Categories: []string{AbusiveHigh}, Options: DefaultDetectOptions(), MaxTextRunes: 5000, Enabled: true}
	saved, err := store.upsert(policy)
	if err != nil {
		t.Fatalf("save policy: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("expected policy version 1, got %d", saved.Version)
	}
	saved.Name = "评论审核二版"
	updated, err := store.upsert(saved)
	if err != nil || updated.Version != 2 {
		t.Fatalf("policy version did not increment: %+v %v", updated, err)
	}
	reloaded, err := newPolicyStore(dataPath)
	if err != nil {
		t.Fatalf("reload policy store: %v", err)
	}
	if got, ok := reloaded.get("comments"); !ok || got.Name != "评论审核二版" || got.Version != 2 {
		t.Fatalf("policy not persisted: %+v", got)
	}
	policy.ID = "Invalid ID"
	if _, err := store.upsert(policy); err == nil {
		t.Fatal("expected invalid policy id error")
	}
}

func TestPolicyWhitelistAndCompositeRules(t *testing.T) {
	detector := NewDetector(setupTestWordRepo(t))
	policy := DetectionPolicy{ID: "quality", Version: 3, Name: "质量策略", Categories: []string{AbusiveHigh}, Options: DefaultDetectOptions(), MaxTextRunes: 1000, Enabled: true, Whitelist: []string{"引用傻逼用于举报"}, Rules: []CompositeRule{{ID: "contact", Name: "引流组合", Terms: []string{"加我", "微信"}, MaxDistance: 10, RiskLevel: "high", Action: "review"}}}
	whitelisted := detectWithPolicy(context.Background(), detector, "引用傻逼用于举报", policy)
	if whitelisted.HasSensitive {
		t.Fatalf("whitelist did not suppress hit: %+v", whitelisted)
	}
	combined := detectWithPolicy(context.Background(), detector, "请加我微信", policy)
	if !combined.HasSensitive || combined.RiskLevel != "high" || len(combined.PolicyRuleHits) != 1 {
		t.Fatalf("composite rule not applied: %+v", combined)
	}
	if combined.PolicyID != "quality" || combined.PolicyVersion != 3 {
		t.Fatalf("policy trace missing: %+v", combined)
	}
}

func TestPolicyEvaluationMetrics(t *testing.T) {
	detector := NewDetector(setupTestWordRepo(t))
	policy := DetectionPolicy{ID: "eval", Version: 1, Name: "评测", Categories: []string{AbusiveHigh}, Options: DefaultDetectOptions(), MaxTextRunes: 1000, Enabled: true}
	report, err := evaluatePolicy(context.Background(), detector, policy, []evaluationSample{{Text: "你是傻逼", ExpectedSensitive: true}, {Text: "正常内容", ExpectedSensitive: false}, {Text: "傻逼", ExpectedSensitive: false}, {Text: "未命中的期待", ExpectedSensitive: true}})
	if err != nil {
		t.Fatal(err)
	}
	if report.TP != 1 || report.TN != 1 || report.FP != 1 || report.FN != 1 {
		t.Fatalf("unexpected confusion matrix: %+v", report)
	}
	if report.Precision != 0.5 || report.Recall != 0.5 || report.F1 != 0.5 {
		t.Fatalf("unexpected metrics: %+v", report)
	}
}

func TestRiskScoreAndRecommendedAction(t *testing.T) {
	detector := NewDetector(setupTestWordRepo(t))
	policy := DetectionPolicy{ID: "score", Version: 1, Name: "评分", Categories: []string{AbusiveHigh}, Options: DefaultDetectOptions(), MaxTextRunes: 1000, Enabled: true, ReviewThreshold: 20, BlockThreshold: 50}
	response := detectWithPolicy(context.Background(), detector, "傻逼 傻逼", policy)
	if response.RiskScore < 50 || response.RecommendedAction != "block" || response.ScoreBreakdown["high_occurrences"] == 0 {
		t.Fatalf("unexpected scoring: %+v", response)
	}
}

func TestReviewClaimResolveAndFeedbackCandidate(t *testing.T) {
	store, err := newReviewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.create("测试文本", DetectResponse{PolicyID: "default", PolicyVersion: 1, RiskScore: 30, RecommendedAction: "review"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.claim(task.ID, "alice")
	if err != nil || claimed.Status != "claimed" {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if _, err := store.claim(task.ID, "bob"); err == nil {
		t.Fatal("second reviewer claimed same task")
	}
	resolved, err := store.resolve(task.ID, "alice", "false_positive", "上下文豁免", "whitelist", "测试文本", "")
	if err != nil || resolved.Status != "resolved" {
		t.Fatalf("resolve: %+v %v", resolved, err)
	}
	candidates := store.listCandidates()
	if len(candidates) != 1 || candidates[0].Type != "whitelist" {
		t.Fatalf("candidate not generated: %+v", candidates)
	}
}

func TestBatchTaskCompletesAndPersistsResults(t *testing.T) {
	dataPath := t.TempDir()
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	policies, err := newPolicyStore(dataPath)
	if err != nil {
		t.Fatalf("create policies: %v", err)
	}
	manager, err := newTaskManager(service, policies, dataPath, 10, 2, 1, time.Hour, 1<<20)
	if err != nil {
		t.Fatalf("create task manager: %v", err)
	}
	task, err := manager.create(batchCreateRequest{PolicyID: "default", Lines: []string{"普通文本", "你是傻逼"}})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.PolicyVersion != 1 {
		t.Fatalf("task did not capture policy version: %+v", task)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := manager.get(task.ID)
		if current.Status == "completed" {
			if current.Processed != 2 || current.Sensitive != 1 {
				t.Fatalf("unexpected task counters: %+v", current)
			}
			raw, readErr := os.ReadFile(filepath.Join(dataPath, "tasks", task.ID, "results.jsonl"))
			if readErr != nil || !bytes.Contains(raw, []byte(`"has_sensitive":true`)) {
				t.Fatalf("unexpected results: %v %s", readErr, raw)
			}
			if bytes.Index(raw, []byte(`"line":1`)) > bytes.Index(raw, []byte(`"line":2`)) {
				t.Fatal("parallel results are not ordered by input line")
			}
			retried, retryErr := manager.retry(task.ID)
			if retryErr != nil || retried.ParentTaskID != task.ID {
				t.Fatalf("retry failed: %+v %v", retried, retryErr)
			}
			for time.Now().Before(deadline.Add(3 * time.Second)) {
				retryState, _ := manager.get(retried.ID)
				if retryState.Status == "completed" {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err := manager.delete(task.ID); err != nil {
				t.Fatalf("delete completed task: %v", err)
			}
			if _, ok := manager.get(task.ID); ok {
				t.Fatal("deleted task remains listed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task did not complete: %+v", task)
}

func TestTaskCleanupRemovesExpiredTerminalTasks(t *testing.T) {
	dataPath := t.TempDir()
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	policies, _ := newPolicyStore(dataPath)
	manager, _ := newTaskManager(service, policies, dataPath, 10, 1, 1, time.Millisecond, 1<<20)
	task, err := manager.create(batchCreateRequest{PolicyID: "default", Lines: []string{"test"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	removed, err := manager.cleanup(time.Now().UTC())
	if err != nil || removed != 1 {
		t.Fatalf("cleanup removed=%d err=%v", removed, err)
	}
	if _, ok := manager.get(task.ID); ok {
		t.Fatal("expired task remains")
	}
}

func TestBatchTaskRejectsConfiguredLineLimit(t *testing.T) {
	dataPath := t.TempDir()
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	policies, _ := newPolicyStore(dataPath)
	manager, _ := newTaskManager(service, policies, dataPath, 1, 1, 1, time.Hour, 1<<20)
	_, err := manager.create(batchCreateRequest{PolicyID: "default", Lines: []string{"one", "two"}})
	if err == nil {
		t.Fatal("expected line limit error")
	}
}
