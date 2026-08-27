package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ReviewTask struct {
	ID                string    `json:"id"`
	PolicyID          string    `json:"policy_id"`
	PolicyVersion     int       `json:"policy_version"`
	Text              string    `json:"text"`
	RiskScore         int       `json:"risk_score"`
	RecommendedAction string    `json:"recommended_action"`
	Status            string    `json:"status"`
	ClaimedBy         string    `json:"claimed_by,omitempty"`
	Conclusion        string    `json:"conclusion,omitempty"`
	Note              string    `json:"note,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type FeedbackCandidate struct {
	ID        string    `json:"id"`
	ReviewID  string    `json:"review_id"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	Category  string    `json:"category,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type reviewStore struct {
	path          string
	candidatePath string
	mu            sync.RWMutex
	reviews       map[string]ReviewTask
	candidates    map[string]FeedbackCandidate
}

func newReviewStore(dataPath string) (*reviewStore, error) {
	s := &reviewStore{path: filepath.Join(dataPath, "reviews.json"), candidatePath: filepath.Join(dataPath, "feedback-candidates.json"), reviews: map[string]ReviewTask{}, candidates: map[string]FeedbackCandidate{}}
	if err := loadJSONMap(s.path, &s.reviews); err != nil {
		return nil, err
	}
	if err := loadJSONMap(s.candidatePath, &s.candidates); err != nil {
		return nil, err
	}
	return s, nil
}
func loadJSONMap(path string, target any) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
func (s *reviewStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.reviews, "", "  ")
	if err != nil {
		return err
	}
	if err = atomicWriteFile(s.path, raw); err != nil {
		return err
	}
	raw, err = json.MarshalIndent(s.candidates, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.candidatePath, raw)
}
func (s *reviewStore) create(text string, response DetectResponse) (ReviewTask, error) {
	if strings.TrimSpace(text) == "" {
		return ReviewTask{}, fmt.Errorf("text required")
	}
	now := time.Now().UTC()
	task := ReviewTask{ID: taskID(), PolicyID: response.PolicyID, PolicyVersion: response.PolicyVersion, Text: text, RiskScore: response.RiskScore, RecommendedAction: response.RecommendedAction, Status: "pending", CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reviews[task.ID] = task
	return task, s.saveLocked()
}
func (s *reviewStore) list(status string) []ReviewTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ReviewTask{}
	for _, item := range s.reviews {
		if status == "" || item.Status == status {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (s *reviewStore) claim(id, reviewer string) (ReviewTask, error) {
	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" {
		return ReviewTask{}, fmt.Errorf("reviewer required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.reviews[id]
	if !ok {
		return ReviewTask{}, os.ErrNotExist
	}
	if task.Status != "pending" {
		return ReviewTask{}, fmt.Errorf("task already claimed or resolved")
	}
	task.Status = "claimed"
	task.ClaimedBy = reviewer
	task.UpdatedAt = time.Now().UTC()
	s.reviews[id] = task
	return task, s.saveLocked()
}
func (s *reviewStore) release(id, reviewer string) (ReviewTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.reviews[id]
	if !ok {
		return ReviewTask{}, os.ErrNotExist
	}
	if task.Status != "claimed" || task.ClaimedBy != strings.TrimSpace(reviewer) {
		return ReviewTask{}, fmt.Errorf("task is not owned by reviewer")
	}
	task.Status = "pending"
	task.ClaimedBy = ""
	task.UpdatedAt = time.Now().UTC()
	s.reviews[id] = task
	return task, s.saveLocked()
}
func (s *reviewStore) resolve(id, reviewer, conclusion, note, candidateType, value, category string) (ReviewTask, error) {
	allowed := map[string]bool{"confirmed_violation": true, "confirmed_safe": true, "false_positive": true, "false_negative": true, "uncertain": true}
	if !allowed[conclusion] {
		return ReviewTask{}, fmt.Errorf("invalid conclusion")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.reviews[id]
	if !ok {
		return ReviewTask{}, os.ErrNotExist
	}
	if task.Status != "claimed" || task.ClaimedBy != strings.TrimSpace(reviewer) {
		return ReviewTask{}, fmt.Errorf("task is not owned by reviewer")
	}
	task.Status = "resolved"
	task.Conclusion = conclusion
	task.Note = strings.TrimSpace(note)
	task.UpdatedAt = time.Now().UTC()
	s.reviews[id] = task
	if candidateType != "" && strings.TrimSpace(value) != "" {
		candidate := FeedbackCandidate{ID: taskID(), ReviewID: id, Type: candidateType, Value: strings.TrimSpace(value), Category: category, Status: "pending", CreatedAt: time.Now().UTC()}
		s.candidates[candidate.ID] = candidate
	}
	return task, s.saveLocked()
}
func (s *reviewStore) listCandidates() []FeedbackCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []FeedbackCandidate{}
	for _, item := range s.candidates {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *reviewStore) getCandidate(id string) (FeedbackCandidate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.candidates[id]
	return c, ok
}

// setCandidateStatus 更新候选状态（applied / dismissed），仅允许变更一次。
func (s *reviewStore) setCandidateStatus(id, status string) (FeedbackCandidate, error) {
	if status != "applied" && status != "dismissed" {
		return FeedbackCandidate{}, fmt.Errorf("invalid candidate status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[id]
	if !ok {
		return FeedbackCandidate{}, os.ErrNotExist
	}
	if c.Status != "pending" {
		return FeedbackCandidate{}, fmt.Errorf("candidate already %s", c.Status)
	}
	c.Status = status
	s.candidates[id] = c
	return c, s.saveLocked()
}

func registerReviewRoutes(admin *gin.RouterGroup, service *detectorService, policies *policyStore, dataPath string) error {
	store, err := newReviewStore(dataPath)
	if err != nil {
		return err
	}
	admin.POST("/reviews", func(c *gin.Context) {
		var req struct {
			PolicyID string `json:"policy_id"`
			Text     string `json:"text"`
		}
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "复核请求无效", nil)
			return
		}
		policy, ok := policies.get(req.PolicyID)
		if !ok {
			writeAPIError(c, 404, "POLICY_NOT_FOUND", "策略不存在", nil)
			return
		}
		response := detectWithPolicy(c.Request.Context(), service.detector(), req.Text, policy)
		task, err := store.create(req.Text, response)
		if err != nil {
			writeAPIError(c, 422, "REVIEW_CREATE_FAILED", err.Error(), nil)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"task": task, "detection": response})
	})
	admin.GET("/reviews", func(c *gin.Context) { c.JSON(200, gin.H{"items": store.list(c.Query("status"))}) })
	admin.POST("/reviews/:id/claim", func(c *gin.Context) {
		var req struct {
			Reviewer string `json:"reviewer"`
		}
		_ = c.ShouldBindJSON(&req)
		task, err := store.claim(c.Param("id"), req.Reviewer)
		reviewMutationResponse(c, task, err)
	})
	admin.POST("/reviews/:id/release", func(c *gin.Context) {
		var req struct {
			Reviewer string `json:"reviewer"`
		}
		_ = c.ShouldBindJSON(&req)
		task, err := store.release(c.Param("id"), req.Reviewer)
		reviewMutationResponse(c, task, err)
	})
	admin.POST("/reviews/:id/resolve", func(c *gin.Context) {
		var req struct {
			Reviewer      string `json:"reviewer"`
			Conclusion    string `json:"conclusion"`
			Note          string `json:"note"`
			CandidateType string `json:"candidate_type"`
			Value         string `json:"value"`
			Category      string `json:"category"`
		}
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "结案请求无效", nil)
			return
		}
		task, err := store.resolve(c.Param("id"), req.Reviewer, req.Conclusion, req.Note, req.CandidateType, req.Value, req.Category)
		reviewMutationResponse(c, task, err)
	})
	admin.GET("/feedback-candidates", func(c *gin.Context) { c.JSON(200, gin.H{"items": store.listCandidates()}) })

	// 把复核结案产生的候选真正落地：whitelist 类型写入 白名单.txt 并热重载。
	admin.POST("/feedback-candidates/:id/apply", func(c *gin.Context) {
		candidate, ok := store.getCandidate(c.Param("id"))
		if !ok {
			writeAPIError(c, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "反馈候选不存在", nil)
			return
		}
		if candidate.Status != "pending" {
			writeAPIError(c, http.StatusConflict, "CANDIDATE_CONFLICT", "候选已被处理", nil)
			return
		}
		switch candidate.Type {
		case "whitelist":
			var cats []string
			if candidate.Category != "" {
				for _, cat := range strings.Split(candidate.Category, ",") {
					if cat = strings.TrimSpace(cat); cat != "" {
						if _, valid := CategoryDisplay[cat]; !valid {
							writeAPIError(c, 422, "INVALID_CATEGORY", "候选类别无效", nil)
							return
						}
						cats = append(cats, cat)
					}
				}
			}
			basePath := service.detector().basePath
			if err := mutateWhitelistFile(basePath, candidate.Value, cats, true); err != nil {
				writeAPIError(c, 422, "WHITELIST_APPLY_FAILED", "写入白名单失败", nil)
				return
			}
			if _, err := store.setCandidateStatus(candidate.ID, "applied"); err != nil {
				writeAPIError(c, http.StatusConflict, "CANDIDATE_CONFLICT", err.Error(), nil)
				return
			}
			c.JSON(http.StatusOK, gin.H{"applied": true, "candidate": candidate, "wordlist": service.reload()})
		default:
			writeAPIError(c, 422, "UNSUPPORTED_CANDIDATE_TYPE", "该候选类型暂不支持自动应用", nil)
		}
	})

	admin.POST("/feedback-candidates/:id/dismiss", func(c *gin.Context) {
		candidate, err := store.setCandidateStatus(c.Param("id"), "dismissed")
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "反馈候选不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, http.StatusConflict, "CANDIDATE_CONFLICT", err.Error(), nil)
			return
		}
		c.JSON(http.StatusOK, candidate)
	})
	return nil
}
func reviewMutationResponse(c *gin.Context, task ReviewTask, err error) {
	if errors.Is(err, os.ErrNotExist) {
		writeAPIError(c, 404, "REVIEW_NOT_FOUND", "复核任务不存在", nil)
		return
	}
	if err != nil {
		writeAPIError(c, 409, "REVIEW_CONFLICT", err.Error(), nil)
		return
	}
	c.JSON(200, task)
}
