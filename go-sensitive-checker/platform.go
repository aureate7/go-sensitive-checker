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
	ID              string          `json:"id"`
	Version         int             `json:"version"`
	Name            string          `json:"name"`
	Categories      []string        `json:"categories"`
	Options         DetectOptions   `json:"options"`
	MaxTextRunes    int             `json:"max_text_runes"`
	Enabled         bool            `json:"enabled"`
	Whitelist       []string        `json:"whitelist,omitempty"`
	Rules           []CompositeRule `json:"rules,omitempty"`
	ContextRules    []ContextRule   `json:"context_rules,omitempty"`
	ReviewThreshold int             `json:"review_threshold"`
	BlockThreshold  int             `json:"block_threshold"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type CompositeRule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Terms       []string `json:"terms"`
	MaxDistance int      `json:"max_distance"`
	RiskLevel   string   `json:"risk_level"`
	Action      string   `json:"action"`
}

type CompositeRuleHit struct {
	RuleID    string   `json:"rule_id"`
	RuleName  string   `json:"rule_name"`
	Terms     []string `json:"terms"`
	RiskLevel string   `json:"risk_level"`
	Action    string   `json:"action"`
}

// ContextRule adjusts the policy score when a hit appears near configured
// phrases. Negative deltas model benign/reporting context; positive deltas
// model dangerous intent or co-occurrence. Rules are versioned with policies.
type ContextRule struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Phrases    []string `json:"phrases"`
	Words      []string `json:"words,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Window     int      `json:"window"`
	ScoreDelta int      `json:"score_delta"`
}

type ContextRuleHit struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	Phrase     string `json:"phrase"`
	Word       string `json:"word"`
	Category   string `json:"category"`
	ScoreDelta int    `json:"score_delta"`
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
		store.policies["default"] = DetectionPolicy{ID: "default", Version: 1, Name: "默认全类别策略", Categories: sortedCategories(), Options: DefaultDetectOptions(), MaxTextRunes: 20000, Enabled: true, ReviewThreshold: 25, BlockThreshold: 70, CreatedAt: now, UpdatedAt: now}
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
		if policy.Version < 1 {
			policy.Version = 1
		}
		if policy.ReviewThreshold <= 0 {
			policy.ReviewThreshold = 25
		}
		if policy.BlockThreshold <= 0 {
			policy.BlockThreshold = 70
		}
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
	if policy.ReviewThreshold <= 0 {
		policy.ReviewThreshold = 25
	}
	if policy.BlockThreshold <= 0 {
		policy.BlockThreshold = 70
	}
	if err := validatePolicy(policy); err != nil {
		return DetectionPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := s.policies[policy.ID]; ok {
		policy.CreatedAt = existing.CreatedAt
		policy.Version = existing.Version + 1
	} else {
		policy.CreatedAt = now
		policy.Version = 1
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
	if policy.ReviewThreshold <= 0 {
		policy.ReviewThreshold = 25
	}
	if policy.BlockThreshold <= 0 {
		policy.BlockThreshold = 70
	}
	if policy.ReviewThreshold >= policy.BlockThreshold || policy.BlockThreshold > 100 {
		return fmt.Errorf("invalid action thresholds")
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
	for _, item := range policy.Whitelist {
		if strings.TrimSpace(item) == "" || len([]rune(item)) > 256 {
			return fmt.Errorf("invalid whitelist item")
		}
	}
	ruleIDs := map[string]struct{}{}
	for _, rule := range policy.Rules {
		if strings.TrimSpace(rule.ID) == "" || len(rule.Terms) < 2 {
			return fmt.Errorf("invalid composite rule")
		}
		if _, exists := ruleIDs[rule.ID]; exists {
			return fmt.Errorf("duplicate rule id %s", rule.ID)
		}
		ruleIDs[rule.ID] = struct{}{}
		if rule.RiskLevel != "low" && rule.RiskLevel != "medium" && rule.RiskLevel != "high" {
			return fmt.Errorf("invalid rule risk level")
		}
		for _, term := range rule.Terms {
			if strings.TrimSpace(term) == "" {
				return fmt.Errorf("empty rule term")
			}
		}
	}
	contextIDs := map[string]struct{}{}
	for _, rule := range policy.ContextRules {
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Name) == "" || len(rule.Phrases) == 0 {
			return fmt.Errorf("invalid context rule")
		}
		if _, exists := contextIDs[rule.ID]; exists {
			return fmt.Errorf("duplicate context rule id %s", rule.ID)
		}
		contextIDs[rule.ID] = struct{}{}
		if rule.Window < 0 || rule.Window > 500 {
			return fmt.Errorf("invalid context rule window")
		}
		if rule.ScoreDelta < -100 || rule.ScoreDelta > 100 || rule.ScoreDelta == 0 {
			return fmt.Errorf("invalid context rule score_delta")
		}
		for _, phrase := range rule.Phrases {
			if strings.TrimSpace(phrase) == "" {
				return fmt.Errorf("empty context phrase")
			}
		}
		for _, category := range rule.Categories {
			if _, ok := CategoryDisplay[category]; !ok {
				return fmt.Errorf("unknown context category %s", category)
			}
		}
	}
	return nil
}

func detectWithPolicy(ctx context.Context, detector *Detector, text string, policy DetectionPolicy) DetectResponse {
	masked := maskWhitelist(text, policy.Whitelist)
	response := detector.DetectWithContext(ctx, masked, policy.Categories, &policy.Options)
	response.PolicyID = policy.ID
	response.PolicyVersion = policy.Version
	response.PolicyRuleHits = matchCompositeRules(masked, policy.Rules)
	response.ContextRuleHits = matchContextRules(masked, response.HitEvidences, policy.ContextRules)
	for _, hit := range response.PolicyRuleHits {
		response.HasSensitive = true
		if riskRank(hit.RiskLevel) > riskRank(response.RiskLevel) {
			response.RiskLevel = hit.RiskLevel
		}
	}
	applyRiskScore(&response, policy)
	return response
}

func applyRiskScore(response *DetectResponse, policy DetectionPolicy) {
	breakdown := map[string]int{
		"high_occurrences":   response.RiskOccurrence["high"] * 30,
		"medium_occurrences": response.RiskOccurrence["medium"] * 20,
		"low_occurrences":    response.RiskOccurrence["low"] * 10,
	}
	for _, hit := range response.PolicyRuleHits {
		breakdown["composite_rules"] += map[string]int{"high": 35, "medium": 25, "low": 15}[hit.RiskLevel]
	}
	for _, hit := range response.ContextRuleHits {
		breakdown["context_rules"] += hit.ScoreDelta
	}
	score := 0
	for _, value := range breakdown {
		score += value
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	reviewThreshold, blockThreshold := policy.ReviewThreshold, policy.BlockThreshold
	if reviewThreshold <= 0 {
		reviewThreshold = 25
	}
	if blockThreshold <= 0 {
		blockThreshold = 70
	}
	response.RiskScore, response.ScoreBreakdown, response.RecommendedAction = score, breakdown, "allow"
	if score >= blockThreshold {
		response.RecommendedAction = "block"
	} else if score >= reviewThreshold {
		response.RecommendedAction = "review"
	} else if score > 0 {
		response.RecommendedAction = "mask"
	}
}

func matchContextRules(text string, evidences []HitEvidence, rules []ContextRule) []ContextRuleHit {
	runes := []rune(text)
	hits := make([]ContextRuleHit, 0)
	seen := map[string]struct{}{}
	contains := func(items []string, value string) bool {
		if len(items) == 0 {
			return true
		}
		for _, item := range items {
			if item == value {
				return true
			}
		}
		return false
	}
	for _, evidence := range evidences {
		for _, rule := range rules {
			if !contains(rule.Words, evidence.Word) || !contains(rule.Categories, evidence.Category) {
				continue
			}
			window := rule.Window
			if window <= 0 {
				window = 20
			}
			start, end := evidence.Start-window, evidence.End+window
			if start < 0 {
				start = 0
			}
			if end > len(runes) {
				end = len(runes)
			}
			contextText := string(runes[start:end])
			for _, phrase := range rule.Phrases {
				phrase = strings.TrimSpace(phrase)
				if phrase == "" || !strings.Contains(contextText, phrase) {
					continue
				}
				key := rule.ID + "\x00" + evidence.Category + "\x00" + evidence.Word + "\x00" + phrase
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				hits = append(hits, ContextRuleHit{rule.ID, rule.Name, phrase, evidence.Word, evidence.Category, rule.ScoreDelta})
			}
		}
	}
	return hits
}

func maskWhitelist(text string, whitelist []string) string {
	runes := []rune(text)
	masked := append([]rune(nil), runes...)
	items := append([]string(nil), whitelist...)
	sort.Slice(items, func(i, j int) bool { return len([]rune(items[i])) > len([]rune(items[j])) })
	for _, item := range items {
		needle := []rune(strings.TrimSpace(item))
		if len(needle) == 0 {
			continue
		}
		for start := 0; start+len(needle) <= len(runes); start++ {
			match := true
			for offset := range needle {
				if runes[start+offset] != needle[offset] {
					match = false
					break
				}
			}
			if match {
				for offset := range needle {
					masked[start+offset] = ' '
				}
				start += len(needle) - 1
			}
		}
	}
	return string(masked)
}

func matchCompositeRules(text string, rules []CompositeRule) []CompositeRuleHit {
	runes := []rune(strings.ToLower(text))
	hits := make([]CompositeRuleHit, 0)
	for _, rule := range rules {
		positions := make([]int, 0, len(rule.Terms))
		matched := true
		for _, raw := range rule.Terms {
			position := runeIndex(runes, []rune(strings.ToLower(strings.TrimSpace(raw))))
			if position < 0 {
				matched = false
				break
			}
			positions = append(positions, position)
		}
		if !matched {
			continue
		}
		if rule.MaxDistance > 0 {
			sort.Ints(positions)
			if positions[len(positions)-1]-positions[0] > rule.MaxDistance {
				continue
			}
		}
		hits = append(hits, CompositeRuleHit{RuleID: rule.ID, RuleName: rule.Name, Terms: append([]string(nil), rule.Terms...), RiskLevel: rule.RiskLevel, Action: rule.Action})
	}
	return hits
}

func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return -1
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		match := true
		for offset := range needle {
			if haystack[start+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return start
		}
	}
	return -1
}

func riskRank(level string) int {
	switch level {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

type BatchTask struct {
	ID            string     `json:"id"`
	PolicyID      string     `json:"policy_id"`
	PolicyVersion int        `json:"policy_version"`
	Status        string     `json:"status"`
	Total         int        `json:"total"`
	Processed     int        `json:"processed"`
	Sensitive     int        `json:"sensitive"`
	Failed        int        `json:"failed"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Error         string     `json:"error,omitempty"`
	ParentTaskID  string     `json:"parent_task_id,omitempty"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ResultBytes   int64      `json:"result_bytes"`
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
	return m.createWithPolicy(req.Lines, policy, parentID)
}

func (m *taskManager) createWithPolicy(lines []string, policy DetectionPolicy, parentID string) (BatchTask, error) {
	if len(lines) == 0 || len(lines) > m.maxLines {
		return BatchTask{}, fmt.Errorf("line count must be between 1 and %d", m.maxLines)
	}
	for _, line := range lines {
		if utf8.RuneCountInString(line) > policy.MaxTextRunes {
			return BatchTask{}, fmt.Errorf("line exceeds policy limit")
		}
	}
	used, _ := directorySize(m.path)
	estimated := int64(0)
	for _, line := range lines {
		estimated += int64(len(line) * 3)
	}
	if used+estimated > m.maxStorageBytes {
		return BatchTask{}, fmt.Errorf("task storage limit exceeded")
	}
	now := time.Now().UTC()
	task := BatchTask{ID: taskID(), PolicyID: policy.ID, PolicyVersion: policy.Version, Status: "queued", Total: len(lines), CreatedAt: now, ParentTaskID: parentID, ExpiresAt: now.Add(m.retention)}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &taskRuntime{task: task, cancel: cancel}
	m.mu.Lock()
	m.tasks[task.ID] = runtime
	m.mu.Unlock()
	if err := m.persist(&runtime.task); err != nil {
		cancel()
		return BatchTask{}, err
	}
	inputRaw, err := json.Marshal(lines)
	if err != nil {
		cancel()
		return BatchTask{}, err
	}
	if err := atomicWriteFile(filepath.Join(m.path, task.ID, "input.json"), inputRaw); err != nil {
		cancel()
		return BatchTask{}, err
	}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		cancel()
		return BatchTask{}, err
	}
	if err := atomicWriteFile(filepath.Join(m.path, task.ID, "policy.json"), policyRaw); err != nil {
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
	}(append([]string(nil), lines...))
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
				response := detectWithPolicy(ctx, m.service.detector(), job.text, policy)
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
	policyRaw, err := os.ReadFile(filepath.Join(m.path, id, "policy.json"))
	if err != nil {
		return BatchTask{}, err
	}
	var policy DetectionPolicy
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		return BatchTask{}, err
	}
	return m.createWithPolicy(lines, policy, id)
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

type evaluationSample struct {
	Text               string   `json:"text"`
	ExpectedSensitive  bool     `json:"expected_sensitive"`
	ExpectedCategories []string `json:"expected_categories,omitempty"`
}

type evaluationRequest struct {
	PolicyID string             `json:"policy_id"`
	Samples  []evaluationSample `json:"samples"`
}
type evaluationFailure struct {
	Index    int    `json:"index"`
	Expected bool   `json:"expected"`
	Actual   bool   `json:"actual"`
	Reason   string `json:"reason"`
}
type evaluationReport struct {
	PolicyID      string              `json:"policy_id"`
	PolicyVersion int                 `json:"policy_version"`
	Total         int                 `json:"total"`
	TP            int                 `json:"tp"`
	FP            int                 `json:"fp"`
	TN            int                 `json:"tn"`
	FN            int                 `json:"fn"`
	Precision     float64             `json:"precision"`
	Recall        float64             `json:"recall"`
	F1            float64             `json:"f1"`
	Failures      []evaluationFailure `json:"failures"`
}

func evaluatePolicy(ctx context.Context, detector *Detector, policy DetectionPolicy, samples []evaluationSample) (evaluationReport, error) {
	if len(samples) == 0 || len(samples) > 5000 {
		return evaluationReport{}, fmt.Errorf("sample count must be between 1 and 5000")
	}
	report := evaluationReport{PolicyID: policy.ID, PolicyVersion: policy.Version, Total: len(samples), Failures: []evaluationFailure{}}
	for index, sample := range samples {
		if utf8.RuneCountInString(sample.Text) > policy.MaxTextRunes {
			return report, fmt.Errorf("sample %d exceeds policy text limit", index)
		}
		response := detectWithPolicy(ctx, detector, sample.Text, policy)
		switch {
		case sample.ExpectedSensitive && response.HasSensitive:
			report.TP++
		case sample.ExpectedSensitive && !response.HasSensitive:
			report.FN++
			report.Failures = append(report.Failures, evaluationFailure{Index: index, Expected: true, Actual: false, Reason: "漏报"})
		case !sample.ExpectedSensitive && response.HasSensitive:
			report.FP++
			report.Failures = append(report.Failures, evaluationFailure{Index: index, Expected: false, Actual: true, Reason: "误报"})
		default:
			report.TN++
		}
		for _, expectedCategory := range sample.ExpectedCategories {
			if _, ok := response.Categories[expectedCategory]; !ok {
				report.Failures = append(report.Failures, evaluationFailure{Index: index, Expected: true, Actual: response.HasSensitive, Reason: "类别漏报: " + expectedCategory})
			}
		}
	}
	if report.TP+report.FP > 0 {
		report.Precision = float64(report.TP) / float64(report.TP+report.FP)
	}
	if report.TP+report.FN > 0 {
		report.Recall = float64(report.TP) / float64(report.TP+report.FN)
	}
	if report.Precision+report.Recall > 0 {
		report.F1 = 2 * report.Precision * report.Recall / (report.Precision + report.Recall)
	}
	return report, nil
}

func registerPlatformRoutes(r *gin.Engine, service *detectorService, token, dataPath string, maxLines, workers, maxConcurrentTasks int, retention time.Duration, maxStorageBytes int64, webhook *webhookNotifier) error {
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
	if err := registerReviewRoutes(admin, service, policies, dataPath, webhook); err != nil {
		return err
	}
	admin.GET("/policies", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"items": policies.list(false)}) })
	admin.POST("/detect", func(c *gin.Context) {
		var req struct {
			PolicyID string `json:"policy_id"`
			Text     string `json:"text"`
		}
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "请求格式无效", nil)
			return
		}
		policy, ok := policies.get(req.PolicyID)
		if !ok || !policy.Enabled {
			writeAPIError(c, 404, "POLICY_NOT_FOUND", "策略不存在或未启用", nil)
			return
		}
		if utf8.RuneCountInString(req.Text) > policy.MaxTextRunes {
			writeAPIError(c, 413, "TEXT_TOO_LARGE", "文本超过策略限制", nil)
			return
		}
		c.JSON(200, detectWithPolicy(c.Request.Context(), service.detector(), req.Text, policy))
	})
	admin.POST("/evaluations", func(c *gin.Context) {
		var req evaluationRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, 400, "INVALID_JSON", "评测格式无效", nil)
			return
		}
		policy, ok := policies.get(req.PolicyID)
		if !ok {
			writeAPIError(c, 404, "POLICY_NOT_FOUND", "策略不存在", nil)
			return
		}
		report, err := evaluatePolicy(c.Request.Context(), service.detector(), policy, req.Samples)
		if err != nil {
			writeAPIError(c, 422, "EVALUATION_INVALID", err.Error(), nil)
			return
		}
		c.JSON(200, report)
	})
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
