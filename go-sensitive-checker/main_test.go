package main

import (
	"bytes"
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
	if _, err := store.upsert(policy); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	reloaded, err := newPolicyStore(dataPath)
	if err != nil {
		t.Fatalf("reload policy store: %v", err)
	}
	if got, ok := reloaded.get("comments"); !ok || got.Name != "评论审核" {
		t.Fatalf("policy not persisted: %+v", got)
	}
	policy.ID = "Invalid ID"
	if _, err := store.upsert(policy); err == nil {
		t.Fatal("expected invalid policy id error")
	}
}

func TestBatchTaskCompletesAndPersistsResults(t *testing.T) {
	dataPath := t.TempDir()
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	policies, err := newPolicyStore(dataPath)
	if err != nil {
		t.Fatalf("create policies: %v", err)
	}
	manager, err := newTaskManager(service, policies, dataPath, 10, 2, 1)
	if err != nil {
		t.Fatalf("create task manager: %v", err)
	}
	task, err := manager.create(batchCreateRequest{PolicyID: "default", Lines: []string{"普通文本", "你是傻逼"}})
	if err != nil {
		t.Fatalf("create task: %v", err)
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
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task did not complete: %+v", task)
}

func TestBatchTaskRejectsConfiguredLineLimit(t *testing.T) {
	dataPath := t.TempDir()
	service := newDetectorService(NewDetector(setupTestWordRepo(t)))
	policies, _ := newPolicyStore(dataPath)
	manager, _ := newTaskManager(service, policies, dataPath, 1, 1, 1)
	_, err := manager.create(batchCreateRequest{PolicyID: "default", Lines: []string{"one", "two"}})
	if err == nil {
		t.Fatal("expected line limit error")
	}
}
