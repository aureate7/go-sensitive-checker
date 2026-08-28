package main

import "sort"

// HitEvidence records why/how a hit was produced for explainability.
type HitEvidence struct {
	Word           string `json:"word"`
	Category       string `json:"category"`
	MatchType      string `json:"match_type"`
	MatchedText    string `json:"matched_text"`
	Start          int    `json:"start"`
	End            int    `json:"end"`
	NormalizedUsed bool   `json:"normalized_used"`
	PinyinUsed     bool   `json:"pinyin_used"`
	RiskLevel      string `json:"risk_level"`
	LLMVerdict     string `json:"llm_verdict,omitempty"` // confirm/demote/review（逐命中复核后填充）
	LLMReason      string `json:"llm_reason,omitempty"`
}

type wordAgg struct {
	word      string
	category  string
	countBy   map[string]int
	posByType map[string][]int
	level     string
	origLevel string
}

func newWordAgg(word, category string) *wordAgg {
	return &wordAgg{
		word:      word,
		category:  category,
		countBy:   map[string]int{},
		posByType: map[string][]int{},
	}
}

func (a *wordAgg) add(matchType string, start int) {
	a.countBy[matchType]++
	a.posByType[matchType] = append(a.posByType[matchType], start)
}

func (a *wordAgg) matchMethods() []string {
	methods := make([]string, 0, len(a.countBy))
	for k, v := range a.countBy {
		if v > 0 {
			methods = append(methods, k)
		}
	}
	sort.Strings(methods)
	return methods
}
