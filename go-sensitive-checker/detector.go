package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// 分类键
const (
	PoliticalHigh   = "political_high"
	PoliticalLow    = "political_low"
	PoliticalPerson = "political_person"
	PoliticalBanned = "political_banned_books"
	PoliticalProhib = "political_prohibited"
	ViolentHigh     = "violent_high"
	ViolentLow      = "violent_low"
	ViolentChemical = "violent_chemical"
	PornHigh        = "pornographic_high"
	PornLow         = "pornographic_low"
	AbusiveHigh     = "abusive_high"
	AbusiveLow      = "abusive_low"
	AdvertisingHigh = "advertising_high"
	AdvertisingLow  = "advertising_low"
)

var CategoryDisplay = map[string]string{
	PoliticalHigh:   "政治高敏感",
	PoliticalLow:    "政治低敏感",
	PoliticalPerson: "政治敏感人物",
	PoliticalBanned: "禁书",
	PoliticalProhib: "政治违禁词",
	ViolentHigh:     "暴恐高敏感",
	ViolentLow:      "暴恐低敏感",
	ViolentChemical: "化学药剂",
	PornHigh:        "涉黄高敏感",
	PornLow:         "涉黄低敏感",
	AbusiveHigh:     "辱骂高敏感",
	AbusiveLow:      "辱骂低敏感",
	AdvertisingHigh: "广告高敏感",
	AdvertisingLow:  "广告低敏感",
}

type Detector struct {
	basePath string
	config   DetectorConfig

	defaultOptions DetectOptions

	sensitiveWords map[string]map[string]struct{} // cat -> set(word)
	automata       map[string]*ACAutomaton        // cat -> AC
	fuzzyIndex     map[string]*AliasIndex         // cat -> fuzzy alias index
	pinyinIndex    map[string]*AliasIndex         // cat -> pinyin alias index

	normalizer    *Normalizer
	fuzzyMatcher  *FuzzyMatcher
	pinyinMatcher *PinyinMatcher
	llmAssist     *llmAssistClient
	loadStatus    WordListStatus
}

type WordListStatus struct {
	Ready          bool           `json:"ready"`
	BasePath       string         `json:"base_path"`
	TotalWords     int            `json:"total_words"`
	LoadedFiles    int            `json:"loaded_files"`
	MissingFiles   int            `json:"missing_files"`
	CategoryCounts map[string]int `json:"category_counts"`
	Errors         []string       `json:"errors,omitempty"`
}

func NewDetector(basePath string) *Detector {
	return NewDetectorWithConfig(basePath, DefaultDetectorConfig(basePath))
}

func NewDetectorWithConfig(basePath string, cfg DetectorConfig) *Detector {
	normalizer := NewNormalizer()
	var llmClient *llmAssistClient
	if cfg.EnableLLMAssist {
		llmClient = newLLMAssistClient(cfg)
	}
	d := &Detector{
		basePath:       basePath,
		config:         cfg,
		defaultOptions: cfg.DefaultOptions,
		sensitiveWords: make(map[string]map[string]struct{}),
		automata:       make(map[string]*ACAutomaton),
		fuzzyIndex:     make(map[string]*AliasIndex),
		pinyinIndex:    make(map[string]*AliasIndex),
		normalizer:     normalizer,
		fuzzyMatcher:   NewFuzzyMatcher(normalizer),
		pinyinMatcher:  NewPinyinMatcher(normalizer, cfg.PinyinAliasPath, cfg.EnableAutoPinyin, cfg.EnablePinyinInitials),
		llmAssist:      llmClient,
	}
	for cat := range CategoryDisplay {
		d.sensitiveWords[cat] = make(map[string]struct{})
	}
	d.loadSensitiveWords()
	d.finalizeLoadStatus()
	d.buildAutomata()
	d.buildEnhancedIndexes()
	return d
}

// ================= 词库加载 =================

func (d *Detector) loadSensitiveWords() {
	// 目录与历史版本保持一致，并兼容当前 temp 文件命名。
	d.loadFiles([]string{
		"政治敏感词/政治高敏感词(不含数字不含人名).txt",
		"政治敏感词/政治高敏感词(含数字).txt",
	}, PoliticalHigh)

	d.loadFiles([]string{
		"政治敏感词/政治低敏感词(不含数字不含人名).txt",
		"政治敏感词/政治低敏感词(含数字).txt",
	}, PoliticalLow)

	d.loadFiles([]string{
		"政治敏感词/政治高敏感词(不含数字含人名).txt",
		"政治敏感词/政治低敏感词(不含数字含人名).txt",
	}, PoliticalPerson)

	d.loadFiles([]string{
		"政治敏感词/禁书.txt",
	}, PoliticalBanned)

	d.loadFiles([]string{
		"政治敏感词/违禁词/违禁词（总）(不含数字不含人名).txt",
		"政治敏感词/违禁词/违禁词（含数字）.txt",
	}, PoliticalProhib)

	d.loadFiles([]string{
		"暴恐类敏感词/暴恐高敏感词(不含数字).txt",
		"暴恐类敏感词/暴恐高敏感词(含数字).txt", // 可选，文件不存在时自动跳过
	}, ViolentHigh)

	d.loadFiles([]string{
		"暴恐类敏感词/暴恐低敏感词(不含数字).txt",
		"暴恐类敏感词/暴恐低敏感词(含数字).txt",
	}, ViolentLow)

	d.loadFiles([]string{
		"暴恐类敏感词/化学药剂.txt",
	}, ViolentChemical)

	d.loadFiles([]string{
		"涉黄类敏感词/涉黄高敏感词（添加版）.txt",
		"涉黄类敏感词/涉黄高敏感词（同音替换）.txt",
	}, PornHigh)

	d.loadFiles([]string{
		"涉黄类敏感词/涉黄低敏感词（添加版）.txt",
	}, PornLow)

	d.loadFiles([]string{
		"辱骂类敏感词/辱骂高敏感词（添加版）.txt",
		"辱骂类敏感词/辱骂高敏感词（添加版）(同音替换).txt",
	}, AbusiveHigh)

	d.loadFiles([]string{
		"辱骂类敏感词/辱骂低敏感词（添加版）.txt",
	}, AbusiveLow)

	d.loadFiles([]string{
		"拉人广告敏感词/高敏感词.txt",
		"拉人广告敏感词/高敏感词(同音替换).txt",
	}, AdvertisingHigh)

	d.loadFiles([]string{
		"拉人广告敏感词/低敏感词.txt",
	}, AdvertisingLow)
}

func (d *Detector) loadFiles(relPaths []string, category string) {
	for _, p := range relPaths {
		full := filepath.Join(d.basePath, p)
		f, err := os.Open(full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				d.loadStatus.MissingFiles++
			} else {
				d.loadStatus.Errors = append(d.loadStatus.Errors, fmt.Sprintf("open %s: %v", p, err))
			}
			continue
		}
		d.loadStatus.LoadedFiles++
		sc := bufio.NewScanner(f)
		// 允许较长词条行，同时设置明确上限，避免异常文件无限占用内存。
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			w := strings.TrimSpace(sc.Text())
			if w == "" || strings.HasPrefix(w, "#") {
				continue
			}
			d.sensitiveWords[category][w] = struct{}{}
		}
		if err := sc.Err(); err != nil {
			d.loadStatus.Errors = append(d.loadStatus.Errors, fmt.Sprintf("scan %s: %v", p, err))
		}
		_ = f.Close()
	}
}

func (d *Detector) finalizeLoadStatus() {
	status := WordListStatus{
		BasePath:       d.basePath,
		LoadedFiles:    d.loadStatus.LoadedFiles,
		MissingFiles:   d.loadStatus.MissingFiles,
		CategoryCounts: make(map[string]int, len(d.sensitiveWords)),
		Errors:         append([]string(nil), d.loadStatus.Errors...),
	}
	for category, words := range d.sensitiveWords {
		status.CategoryCounts[category] = len(words)
		status.TotalWords += len(words)
	}
	status.Ready = status.TotalWords > 0 && status.LoadedFiles > 0 && len(status.Errors) == 0
	d.loadStatus = status
}

func (d *Detector) WordListStatus() WordListStatus {
	status := d.loadStatus
	status.CategoryCounts = make(map[string]int, len(d.loadStatus.CategoryCounts))
	for category, count := range d.loadStatus.CategoryCounts {
		status.CategoryCounts[category] = count
	}
	status.Errors = append([]string(nil), d.loadStatus.Errors...)
	return status
}

func (d *Detector) buildAutomata() {
	for cat, set := range d.sensitiveWords {
		words := make([]string, 0, len(set))
		for w := range set {
			words = append(words, w)
		}
		sort.Strings(words)
		ac := NewAC()
		ac.Build(words)
		d.automata[cat] = ac
	}
}

func (d *Detector) buildEnhancedIndexes() {
	for cat, set := range d.sensitiveWords {
		d.fuzzyIndex[cat] = d.fuzzyMatcher.BuildAliasIndex(set)
		d.pinyinIndex[cat] = d.pinyinMatcher.BuildAliasIndex(set)
	}
}

// ================= 检测与统计 =================

type DetectRequest struct {
	Text       string         `json:"text"`
	Categories []string       `json:"categories"` // 可选；为空时使用全部分类
	Options    *DetectOptions `json:"options,omitempty"`
}

type WordHit struct {
	Word                string   `json:"word"`
	Category            string   `json:"category"`
	CountRaw            int      `json:"count_raw"`
	CountNoSymbol       int      `json:"count_no_symbol"`
	CountNormalized     int      `json:"count_normalized"`
	CountFuzzy          int      `json:"count_fuzzy"`
	CountPinyin         int      `json:"count_pinyin"`
	TotalCount          int      `json:"total_count"`
	OccurrenceCount     int      `json:"occurrence_count"`
	Level               string   `json:"level"`          // high/low（单字降级低）
	OriginalLevel       string   `json:"original_level"` // 类别原生级别
	MatchMethods        []string `json:"match_methods"`
	Positions           []int    `json:"positions"`
	PositionsRaw        []int    `json:"positions_raw"` // rune 开始下标
	PositionsNoSymbol   []int    `json:"positions_no_symbol"`
	PositionsNormalized []int    `json:"positions_normalized"`
	PositionsFuzzy      []int    `json:"positions_fuzzy"`
	PositionsPinyin     []int    `json:"positions_pinyin"`
}

type CategoryResult struct {
	Count int            `json:"count"`
	Words []WordHit      `json:"words"`
	Stats map[string]int `json:"stats"` // {"high":x,"low":y}
}

type MaskSuggestion struct {
	SensitiveWord string   `json:"sensitive_word"`
	Category      string   `json:"category"`
	RiskLevel     string   `json:"risk_level"`
	MatchedTexts  []string `json:"matched_texts"`
}

type DetectResponse struct {
	PolicyID             string                    `json:"policy_id,omitempty"`
	PolicyVersion        int                       `json:"policy_version,omitempty"`
	PolicyRuleHits       []CompositeRuleHit        `json:"policy_rule_hits,omitempty"`
	HasSensitive         bool                      `json:"has_sensitive"`
	TotalCount           int                       `json:"total_count"`
	TotalOccurrenceCount int                       `json:"total_occurrence_count"`
	TextWordCount        int                       `json:"text_word_count"`
	TextTotal            int                       `json:"text_total"`
	RiskOccurrence       map[string]int            `json:"risk_occurrence"`
	Categories           map[string]CategoryResult `json:"categories"`
	DetectedWords        []WordHit                 `json:"detected_words"`
	RiskLevel            string                    `json:"risk_level"` // safe/low/high
	CategorySummary      map[string]map[string]int `json:"category_summary"`
	SimilarWords         []any                     `json:"similar_words"`     // 保留字段
	SimilarSensitive     bool                      `json:"similar_sensitive"` // 保持兼容
	NormalizedText       string                    `json:"normalized_text"`
	NormalizedAggressive string                    `json:"normalized_aggressive_text"`
	AppliedOptions       DetectOptions             `json:"applied_options"`
	HitEvidences         []HitEvidence             `json:"hit_evidences"`
	MaskSuggestions      []MaskSuggestion          `json:"mask_suggestions"`
	LLMAssist            *LLMAssistResult          `json:"llm_assist,omitempty"`
}

func (d *Detector) levelOf(category, word string) (level, original string) {
	original = "low"
	if strings.Contains(category, "high") ||
		strings.Contains(category, "banned_books") ||
		strings.Contains(category, "prohibited") ||
		strings.Contains(category, "person") {
		original = "high"
	}
	// 单字降级为 low
	if runeCount(word) == 1 {
		return "low", original
	}
	if original == "high" {
		return "high", original
	}
	return "low", original
}

func runeCount(s string) int {
	return len([]rune(s))
}

func (d *Detector) Detect(text string, categories []string) DetectResponse {
	return d.DetectWithContext(context.Background(), text, categories, nil)
}

func (d *Detector) DetectWithOptions(text string, categories []string, options *DetectOptions) DetectResponse {
	return d.DetectWithContext(context.Background(), text, categories, options)
}

func (d *Detector) DetectWithContext(ctx context.Context, text string, categories []string, options *DetectOptions) DetectResponse {
	cats := d.resolveCategories(categories)
	applied := d.resolveOptions(options)

	normalized := d.normalizer.NormalizeTextWithOptions(text, applied)
	aggressive := d.normalizer.NormalizeTextAggressiveWithOptions(text, applied)
	textNoSymbol, noSymbolMap := stripSymbolsWithMap(text)

	type occurrenceInfo struct {
		Word        string
		Category    string
		RiskLevel   string
		MatchedText string
	}
	type maskGroupAgg struct {
		SensitiveWord string
		Category      string
		RiskLevel     string
		MatchedSet    map[string]struct{}
	}

	resp := DetectResponse{
		Categories:           map[string]CategoryResult{},
		CategorySummary:      map[string]map[string]int{},
		RiskLevel:            "safe",
		RiskOccurrence:       map[string]int{"high": 0, "medium": 0, "low": 0},
		SimilarWords:         []any{},
		SimilarSensitive:     true,
		NormalizedText:       normalized.NormalizedText,
		NormalizedAggressive: aggressive.NormalizedText,
		AppliedOptions:       applied,
		HitEvidences:         make([]HitEvidence, 0, 32),
		MaskSuggestions:      make([]MaskSuggestion, 0, 16),
	}
	resp.TextWordCount = countTextWords(text)
	resp.TextTotal = resp.TextWordCount

	const maxEvidences = 6000
	occurrenceMap := make(map[string]occurrenceInfo, 64)
	maskGroupMap := make(map[string]*maskGroupAgg, 32)

	for _, cat := range cats {
		A := d.automata[cat]
		if A == nil {
			continue
		}

		aggm := map[string]*wordAgg{}
		wordOccurrenceSet := map[string]map[string]struct{}{}

		addHit := func(word, matchType string, start, end int, normalizedUsed, pinyinUsed bool) {
			if word == "" || start < 0 || end <= start {
				return
			}
			matchedText := safeSliceRunes(text, start, end)
			if strings.TrimSpace(matchedText) == "" {
				return
			}

			a := aggm[word]
			if a == nil {
				a = newWordAgg(word, cat)
				aggm[word] = a
			}
			a.add(matchType, start)

			rangeKey := strconv.Itoa(start) + ":" + strconv.Itoa(end)
			if wordOccurrenceSet[word] == nil {
				wordOccurrenceSet[word] = map[string]struct{}{}
			}
			wordOccurrenceSet[word][rangeKey] = struct{}{}

			occKey := cat + "@@" + word + "@@" + rangeKey
			if _, ok := occurrenceMap[occKey]; !ok {
				lvl, _ := d.levelOf(cat, word)
				occurrenceMap[occKey] = occurrenceInfo{
					Word:        word,
					Category:    cat,
					RiskLevel:   lvl,
					MatchedText: matchedText,
				}
			}

			if len(resp.HitEvidences) < maxEvidences {
				resp.HitEvidences = append(resp.HitEvidences, HitEvidence{
					Word:           word,
					Category:       cat,
					MatchType:      matchType,
					MatchedText:    matchedText,
					Start:          start,
					End:            end,
					NormalizedUsed: normalizedUsed,
					PinyinUsed:     pinyinUsed,
				})
			}
		}

		if applied.ExactMatch {
			for _, m := range A.Search(text) {
				addHit(m.Word, matchTypeExactRaw, m.Start, m.End, false, false)
			}
			for _, m := range A.Search(textNoSymbol) {
				start := mapToOriginalIndex(noSymbolMap, m.Start)
				end := mapToOriginalIndex(noSymbolMap, m.End-1) + 1
				addHit(m.Word, matchTypeExactNoSymbol, start, end, true, false)
			}
		}

		if applied.NormalizeMatch {
			for _, m := range A.Search(normalized.NormalizedText) {
				start := mapToOriginalIndex(normalized.IndexMap, m.Start)
				end := mapToOriginalIndex(normalized.IndexMap, m.End-1) + 1
				addHit(m.Word, matchTypeExactNormalized, start, end, true, false)
			}
		}

		if applied.FuzzyMatch {
			idx := d.fuzzyIndex[cat]
			if idx != nil && idx.Automaton != nil {
				for _, m := range idx.Automaton.Search(aggressive.NormalizedText) {
					candidates := idx.AliasToKW[m.Word]
					start := mapToOriginalIndex(aggressive.IndexMap, m.Start)
					end := mapToOriginalIndex(aggressive.IndexMap, m.End-1) + 1
					for _, kw := range candidates {
						addHit(kw, matchTypeFuzzy, start, end, true, false)
					}
				}
			}
		}

		if applied.PinyinMatch {
			idx := d.pinyinIndex[cat]
			if idx != nil && idx.Automaton != nil {
				for _, m := range idx.Automaton.Search(aggressive.NormalizedText) {
					candidates := idx.AliasToKW[m.Word]
					start := mapToOriginalIndex(aggressive.IndexMap, m.Start)
					end := mapToOriginalIndex(aggressive.IndexMap, m.End-1) + 1
					for _, kw := range candidates {
						addHit(kw, matchTypePinyin, start, end, true, true)
					}
				}
			}
		}

		if len(aggm) == 0 {
			continue
		}

		cr := CategoryResult{Stats: map[string]int{"high": 0, "low": 0}}
		wordKeys := sortedWordAggKeys(aggm)
		for _, w := range wordKeys {
			a := aggm[w]
			total := sumCountBy(a.countBy)
			if total == 0 {
				continue
			}
			occurrenceCount := len(wordOccurrenceSet[w])
			lvl, orig := d.levelOf(cat, w)
			a.level = lvl
			a.origLevel = orig
			cr.Stats[lvl]++

			h := WordHit{
				Word:                w,
				Category:            cat,
				CountRaw:            a.countBy[matchTypeExactRaw],
				CountNoSymbol:       a.countBy[matchTypeExactNoSymbol],
				CountNormalized:     a.countBy[matchTypeExactNormalized],
				CountFuzzy:          a.countBy[matchTypeFuzzy],
				CountPinyin:         a.countBy[matchTypePinyin],
				TotalCount:          total,
				OccurrenceCount:     occurrenceCount,
				Level:               lvl,
				OriginalLevel:       orig,
				MatchMethods:        a.matchMethods(),
				Positions:           mergeAndSortPositions(a.posByType),
				PositionsRaw:        append([]int(nil), a.posByType[matchTypeExactRaw]...),
				PositionsNoSymbol:   append([]int(nil), a.posByType[matchTypeExactNoSymbol]...),
				PositionsNormalized: append([]int(nil), a.posByType[matchTypeExactNormalized]...),
				PositionsFuzzy:      append([]int(nil), a.posByType[matchTypeFuzzy]...),
				PositionsPinyin:     append([]int(nil), a.posByType[matchTypePinyin]...),
			}
			cr.Words = append(cr.Words, h)
			resp.DetectedWords = append(resp.DetectedWords, h)
		}
		cr.Count = len(cr.Words)
		resp.Categories[cat] = cr
		resp.TotalCount += cr.Count
	}

	for i := range resp.HitEvidences {
		e := &resp.HitEvidences[i]
		lvl, _ := d.levelOf(e.Category, e.Word)
		e.RiskLevel = lvl
	}

	for _, occ := range occurrenceMap {
		resp.TotalOccurrenceCount++
		resp.RiskOccurrence[occ.RiskLevel]++

		groupKey := occ.Category + "@@" + occ.Word + "@@" + occ.RiskLevel
		group := maskGroupMap[groupKey]
		if group == nil {
			group = &maskGroupAgg{
				SensitiveWord: occ.Word,
				Category:      occ.Category,
				RiskLevel:     occ.RiskLevel,
				MatchedSet:    map[string]struct{}{},
			}
			maskGroupMap[groupKey] = group
		}
		group.MatchedSet[occ.MatchedText] = struct{}{}
	}

	// 风险等级
	if resp.TotalOccurrenceCount > 0 || resp.TotalCount > 0 {
		resp.HasSensitive = true
		switch {
		case resp.RiskOccurrence["high"] > 0:
			resp.RiskLevel = "high"
		case resp.RiskOccurrence["medium"] > 0:
			resp.RiskLevel = "medium"
		default:
			resp.RiskLevel = "low"
		}
	}

	// 分类统计摘要
	for cat, data := range resp.Categories {
		resp.CategorySummary[cat] = map[string]int{
			"total":  data.Count,
			"high":   data.Stats["high"],
			"medium": 0,
			"low":    data.Stats["low"],
		}
	}

	maskKeys := make([]string, 0, len(maskGroupMap))
	for key := range maskGroupMap {
		maskKeys = append(maskKeys, key)
	}
	sort.Strings(maskKeys)

	for _, key := range maskKeys {
		group := maskGroupMap[key]
		matchedTexts := make([]string, 0, len(group.MatchedSet))
		for text := range group.MatchedSet {
			matchedTexts = append(matchedTexts, text)
		}
		sort.Slice(matchedTexts, func(i, j int) bool {
			ri := []rune(matchedTexts[i])
			rj := []rune(matchedTexts[j])
			if len(ri) != len(rj) {
				return len(ri) > len(rj)
			}
			return matchedTexts[i] < matchedTexts[j]
		})
		resp.MaskSuggestions = append(resp.MaskSuggestions, MaskSuggestion{
			SensitiveWord: group.SensitiveWord,
			Category:      group.Category,
			RiskLevel:     group.RiskLevel,
			MatchedTexts:  matchedTexts,
		})
	}

	if applied.EnableLLMAssist {
		llmReport := &LLMAssistResult{
			Enabled: true,
			Used:    false,
			Model:   strings.TrimSpace(d.config.LLMModel),
		}
		switch {
		case !d.config.EnableLLMAssist:
			llmReport.Error = "server disabled llm assist (SENSITIVE_ENABLE_LLM_ASSIST=false)"
		case d.llmAssist == nil:
			llmReport.Error = "llm assist client not initialized"
		default:
			llmResult, err := d.llmAssist.Analyze(ctx, text, cats, resp)
			if err != nil {
				llmReport.Error = err.Error()
			} else {
				llmReport = &llmResult
			}
		}
		resp.LLMAssist = llmReport
	}

	return resp
}

func (d *Detector) resolveCategories(categories []string) []string {
	if len(categories) == 0 {
		categories = make([]string, 0, len(d.automata))
		for cat := range d.automata {
			categories = append(categories, cat)
		}
	}
	uniq := make(map[string]struct{}, len(categories))
	for _, cat := range categories {
		if _, ok := d.automata[cat]; !ok {
			continue
		}
		uniq[cat] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for cat := range uniq {
		out = append(out, cat)
	}
	sort.Strings(out)
	return out
}

func (d *Detector) resolveOptions(options *DetectOptions) DetectOptions {
	applied := d.defaultOptions
	if options != nil {
		applied = *options
		if !applied.ExactMatch && !applied.NormalizeMatch && !applied.FuzzyMatch && !applied.PinyinMatch {
			applied.ExactMatch = true
		}
	}

	if applied.MappingMode == "" {
		applied.MappingMode = d.defaultOptions.MappingMode
	}
	if applied.MappingMode != MappingModeIncremental && applied.MappingMode != MappingModeOverride {
		applied.MappingMode = MappingModeIncremental
	}
	applied.CustomMappings = sanitizeTermMappings(applied.CustomMappings)
	if !applied.EnableTermMapping {
		applied.CustomMappings = nil
	}

	if !d.config.EnableNormalize {
		applied.NormalizeMatch = false
	}
	if !d.config.EnableFuzzy {
		applied.FuzzyMatch = false
	}
	if !d.config.EnablePinyin {
		applied.PinyinMatch = false
	}
	return applied
}

func sanitizeTermMappings(mappings []TermMapping) []TermMapping {
	if len(mappings) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(mappings))
	out := make([]TermMapping, 0, len(mappings))
	for _, m := range mappings {
		from := strings.TrimSpace(m.From)
		to := strings.TrimSpace(m.To)
		if from == "" || to == "" {
			continue
		}
		key := from + "=>" + to
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, TermMapping{From: from, To: to})
	}
	return out
}

func stripSymbolsWithMap(text string) (string, []int) {
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	idx := make([]int, 0, len(runes))
	for i, r := range runes {
		if keepNoSymbolRune(r) {
			out = append(out, r)
			idx = append(idx, i)
		}
	}
	return string(out), idx
}

func keepNoSymbolRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return true
	}
	return r == '_' || r == '，' || r == '。'
}

func countTextWords(text string) int {
	count := 0
	inAlnumWord := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			inAlnumWord = false
			continue
		}
		if unicode.In(r, unicode.Han) {
			count++
			inAlnumWord = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if !inAlnumWord {
				count++
				inAlnumWord = true
			}
			continue
		}
		inAlnumWord = false
	}
	return count
}

func safeSliceRunes(s string, start, end int) string {
	rs := []rune(s)
	if start < 0 {
		start = 0
	}
	if end > len(rs) {
		end = len(rs)
	}
	if start >= end || start >= len(rs) {
		return ""
	}
	return string(rs[start:end])
}

func sumCountBy(m map[string]int) int {
	total := 0
	for _, c := range m {
		total += c
	}
	return total
}

func sortedWordAggKeys(m map[string]*wordAgg) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mergeAndSortPositions(byType map[string][]int) []int {
	uniq := map[int]struct{}{}
	for _, posList := range byType {
		for _, p := range posList {
			uniq[p] = struct{}{}
		}
	}
	res := make([]int, 0, len(uniq))
	for p := range uniq {
		res = append(res, p)
	}
	sort.Ints(res)
	return res
}

func (d *Detector) Statistics() map[string]int {
	stats := map[string]int{}
	total := 0

	// 大类统计
	stats["political"] = len(d.sensitiveWords[PoliticalHigh]) +
		len(d.sensitiveWords[PoliticalLow]) +
		len(d.sensitiveWords[PoliticalPerson]) +
		len(d.sensitiveWords[PoliticalBanned]) +
		len(d.sensitiveWords[PoliticalProhib])

	stats["violent"] = len(d.sensitiveWords[ViolentHigh]) +
		len(d.sensitiveWords[ViolentLow]) +
		len(d.sensitiveWords[ViolentChemical])

	stats["pornographic"] = len(d.sensitiveWords[PornHigh]) +
		len(d.sensitiveWords[PornLow])

	stats["abusive"] = len(d.sensitiveWords[AbusiveHigh]) +
		len(d.sensitiveWords[AbusiveLow])

	stats["advertising"] = len(d.sensitiveWords[AdvertisingHigh]) +
		len(d.sensitiveWords[AdvertisingLow])

	// 子类统计
	for cat, set := range d.sensitiveWords {
		c := len(set)
		stats[cat] = c
		total += c
	}
	stats["total"] = total
	return stats
}
