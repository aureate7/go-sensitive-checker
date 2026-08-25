package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var categoryWriteFiles = map[string]string{
	PoliticalHigh:   "政治敏感词/政治高敏感词(不含数字不含人名).txt",
	PoliticalLow:    "政治敏感词/政治低敏感词(不含数字不含人名).txt",
	PoliticalPerson: "政治敏感词/政治高敏感词(不含数字含人名).txt",
	PoliticalBanned: "政治敏感词/禁书.txt",
	PoliticalProhib: "政治敏感词/违禁词/违禁词（总）(不含数字不含人名).txt",
	ViolentHigh:     "暴恐类敏感词/暴恐高敏感词(不含数字).txt",
	ViolentLow:      "暴恐类敏感词/暴恐低敏感词(不含数字).txt",
	ViolentChemical: "暴恐类敏感词/化学药剂.txt",
	PornHigh:        "涉黄类敏感词/涉黄高敏感词（添加版）.txt",
	PornLow:         "涉黄类敏感词/涉黄低敏感词（添加版）.txt",
	AbusiveHigh:     "辱骂类敏感词/辱骂高敏感词（添加版）.txt",
	AbusiveLow:      "辱骂类敏感词/辱骂低敏感词（添加版）.txt",
	AdvertisingHigh: "拉人广告敏感词/高敏感词.txt",
	AdvertisingLow:  "拉人广告敏感词/低敏感词.txt",
}

type adminManager struct {
	service     *detectorService
	versionPath string
	auditPath   string
	mu          sync.Mutex
}

type wordMutationRequest struct {
	Category string `json:"category"`
	Word     string `json:"word"`
	Reason   string `json:"reason"`
}

type importRequest struct {
	Category string `json:"category"`
	Content  string `json:"content"`
	Reason   string `json:"reason"`
}

type importPreview struct {
	Category   string   `json:"category"`
	Valid      []string `json:"valid"`
	Duplicates []string `json:"duplicates"`
	Invalid    int      `json:"invalid_count"`
}

type auditEntry struct {
	Time      time.Time `json:"time"`
	Action    string    `json:"action"`
	Category  string    `json:"category,omitempty"`
	Word      string    `json:"word,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Version   string    `json:"version,omitempty"`
	RequestID string    `json:"request_id"`
	RemoteIP  string    `json:"remote_ip"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

func newAdminManager(service *detectorService, dataPath string) *adminManager {
	if strings.TrimSpace(dataPath) == "" {
		dataPath = "data"
	}
	return &adminManager{
		service:     service,
		versionPath: filepath.Join(dataPath, "wordlist-versions"),
		auditPath:   filepath.Join(dataPath, "audit.jsonl"),
	}
}

func (m *adminManager) listWords(category, query string, page, pageSize int) (gin.H, error) {
	detector := m.service.detector()
	if category != "" {
		if _, ok := CategoryDisplay[category]; !ok {
			return nil, fmt.Errorf("unknown category")
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	type item struct {
		Word         string `json:"word"`
		Category     string `json:"category"`
		CategoryName string `json:"category_name"`
	}
	items := make([]item, 0)
	for cat, words := range detector.sensitiveWords {
		if category != "" && cat != category {
			continue
		}
		for word := range words {
			if query != "" && !strings.Contains(strings.ToLower(word), query) {
				continue
			}
			items = append(items, item{Word: word, Category: cat, CategoryName: CategoryDisplay[cat]})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Word < items[j].Word
	})
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return gin.H{"items": items[start:end], "total": len(items), "page": page, "page_size": pageSize}, nil
}

func parseImport(category, content string, existing map[string]struct{}) (importPreview, error) {
	if _, ok := CategoryDisplay[category]; !ok {
		return importPreview{}, fmt.Errorf("unknown category")
	}
	preview := importPreview{Category: category, Valid: []string{}, Duplicates: []string{}}
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if word == "" || strings.HasPrefix(word, "#") || strings.HasPrefix(word, "//") {
			continue
		}
		if len([]rune(word)) > 256 {
			preview.Invalid++
			continue
		}
		if _, ok := existing[word]; ok {
			preview.Duplicates = append(preview.Duplicates, word)
			continue
		}
		if _, ok := seen[word]; ok {
			preview.Duplicates = append(preview.Duplicates, word)
			continue
		}
		seen[word] = struct{}{}
		preview.Valid = append(preview.Valid, word)
	}
	if err := scanner.Err(); err != nil {
		return preview, err
	}
	sort.Strings(preview.Valid)
	sort.Strings(preview.Duplicates)
	return preview, nil
}

func (m *adminManager) mutate(category string, add []string, remove string) (WordListStatus, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rel, ok := categoryWriteFiles[category]
	if !ok {
		return WordListStatus{}, "", fmt.Errorf("category is not writable")
	}
	current := m.service.detector()
	set := make(map[string]struct{}, len(current.sensitiveWords[category])+len(add))
	for word := range current.sensitiveWords[category] {
		set[word] = struct{}{}
	}
	for _, word := range add {
		set[word] = struct{}{}
	}
	if remove != "" {
		if _, exists := set[remove]; !exists {
			return WordListStatus{}, "", os.ErrNotExist
		}
		delete(set, remove)
	}
	version, err := m.snapshot(current.basePath)
	if err != nil {
		return WordListStatus{}, "", err
	}
	words := make([]string, 0, len(set))
	for word := range set {
		words = append(words, word)
	}
	sort.Strings(words)
	target := filepath.Join(current.basePath, filepath.FromSlash(rel))
	if err := atomicWriteLines(target, words); err != nil {
		return WordListStatus{}, version, err
	}
	status := m.service.reload()
	if !status.Ready {
		return status, version, fmt.Errorf("reloaded word list is not ready")
	}
	return status, version, nil
}

func atomicWriteLines(path string, words []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".words-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	writer := bufio.NewWriter(tmp)
	for _, word := range words {
		if _, err := writer.WriteString(word + "\n"); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmpName, path)
}

func (m *adminManager) snapshot(basePath string) (string, error) {
	version := time.Now().UTC().Format("20060102T150405.000000000Z")
	target := filepath.Join(m.versionPath, version)
	err := filepath.WalkDir(basePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".txt") {
			return nil
		}
		rel, err := filepath.Rel(basePath, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(target, rel))
	})
	return version, err
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (m *adminManager) versions() ([]gin.H, error) {
	entries, err := os.ReadDir(m.versionPath)
	if errors.Is(err, os.ErrNotExist) {
		return []gin.H{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, gin.H{"version": entry.Name()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["version"].(string) > out[j]["version"].(string) })
	return out, nil
}

func (m *adminManager) rollback(version string) (WordListStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if version == "" || strings.ContainsAny(version, `/\\`) {
		return WordListStatus{}, fmt.Errorf("invalid version")
	}
	source := filepath.Join(m.versionPath, version)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return WordListStatus{}, os.ErrNotExist
	}
	base := m.service.detector().basePath
	if _, err := m.snapshot(base); err != nil {
		return WordListStatus{}, err
	}
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(base, rel))
	})
	if err != nil {
		return WordListStatus{}, err
	}
	status := m.service.reload()
	if !status.Ready {
		return status, fmt.Errorf("rolled back word list is not ready")
	}
	return status, nil
}

func (m *adminManager) audit(entry auditEntry) {
	entry.Time = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(m.auditPath), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(m.auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(entry)
}

func (m *adminManager) readAudit(limit int) ([]auditEntry, error) {
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	file, err := os.Open(m.auditPath)
	if errors.Is(err, os.ErrNotExist) {
		return []auditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries := make([]auditEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry auditEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}
