package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupReviewFlowRouter 构建带平台路由的测试路由（含复核与白名单闭环）。
func setupReviewFlowRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	base := setupWhitelistWordRepo(t)
	dataPath := t.TempDir()
	router := newRouter(newDetectorService(NewDetector(base)), serverConfig{
		DataPath:   dataPath,
		AdminToken: "tok",
	})
	return router, base
}

func TestReviewFeedbackToWhitelistLoop(t *testing.T) {
	router, base := setupReviewFlowRouter(t)

	doJSON := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strPtr(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	doJSON(http.MethodGet, "/api/platform/policies", "") // 触发路由初始化（default 策略由 store 种子提供）

	// 1. 提交复核
	rec := doJSON(http.MethodPost, "/api/platform/reviews", `{"policy_id":"default","text":"免费领取奖品"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create review = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Task ReviewTask `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Task.ID == "" {
		t.Fatalf("parse create response: %v %s", err, rec.Body.String())
	}

	// 2. 领取并按误报结案，产生白名单候选
	if rec = doJSON(http.MethodPost, "/api/platform/reviews/"+created.Task.ID+"/claim", `{"reviewer":"alice"}`); rec.Code != http.StatusOK {
		t.Fatalf("claim = %d", rec.Code)
	}
	rec = doJSON(http.MethodPost, "/api/platform/reviews/"+created.Task.ID+"/resolve",
		`{"reviewer":"alice","conclusion":"false_positive","note":"","candidate_type":"whitelist","value":"免费领取","category":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve = %d", rec.Code)
	}

	// 3. 查询候选列表拿到 pending 候选 ID
	rec = doJSON(http.MethodGet, "/api/platform/feedback-candidates", "")
	var list struct {
		Items []FeedbackCandidate `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Value != "免费领取" {
		t.Fatalf("expected one whitelist candidate, got %+v", list.Items)
	}
	candidateID := list.Items[0].ID

	// 应用前：词条仍可命中
	detectorBefore := NewDetector(base)
	if resp := detectorBefore.Detect("免费领取", nil); len(resp.HitEvidences) == 0 {
		t.Fatal("word should hit before applying candidate")
	}

	// 4. 应用为白名单 → 白名单文件写入 + 服务热重载
	rec = doJSON(http.MethodPost, "/api/platform/feedback-candidates/"+candidateID+"/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply candidate = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(base, WhitelistFileName)); err != nil {
		t.Fatalf("whitelist file missing after apply: %v", err)
	}

	// 热重载后的当前检测器应不再命中该词：通过路由再次创建复核任务验证
	rec = doJSON(http.MethodPost, "/api/platform/reviews", `{"policy_id":"default","text":"免费领取奖品"}`)
	var second struct {
		Detection DetectResponse `json:"detection"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	for _, ev := range second.Detection.HitEvidences {
		if ev.Word == "免费领取" {
			t.Fatalf("applied whitelist should suppress future detections: %+v", ev)
		}
	}

	// 5. 重复应用应冲突；dismiss 已应用候选也应失败/不存在
	rec = doJSON(http.MethodPost, "/api/platform/feedback-candidates/"+candidateID+"/apply", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusConflict {
		t.Fatalf("double apply unexpected code %d", rec.Code)
	}
}

func TestReviewStoreCandidateStatusGuards(t *testing.T) {
	dir := t.TempDir()
	store, err := newReviewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.create("text", DetectResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.claim(task.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.resolve(task.ID, "bob", "false_positive", "", "whitelist", "词A", "")
	if err != nil || resolved.Conclusion != "false_positive" {
		t.Fatalf("resolve failed: %v %+v", err, resolved)
	}
	candidates := store.listCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	// 状态守卫：非法状态拒绝、二次变更拒绝、不存在返回 ErrNotExist
	if _, err := store.setCandidateStatus(candidates[0].ID, "bogus"); err == nil {
		t.Fatal("invalid status should error")
	}
	applied, err := store.setCandidateStatus(candidates[0].ID, "applied")
	if err != nil || applied.Status != "applied" {
		t.Fatalf("apply failed: %v %+v", err, applied)
	}
	if _, err := store.setCandidateStatus(candidates[0].ID, "dismissed"); err == nil {
		t.Fatal("second transition should conflict")
	}
	if _, err := store.setCandidateStatus("missing", "applied"); err != os.ErrNotExist {
		t.Fatalf("missing candidate should return ErrNotExist, got %v", err)
	}
}
