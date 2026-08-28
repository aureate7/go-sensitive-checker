package main

import (
	"os"
	"strconv"
	"strings"
)

const (
	matchTypeExactRaw        = "exact_raw"
	matchTypeExactNoSymbol   = "exact_no_symbol"
	matchTypeExactNormalized = "exact_normalized"
	matchTypeFuzzy           = "fuzzy"
	matchTypePinyin          = "pinyin"

	MappingModeIncremental = "incremental"
	MappingModeOverride    = "override"
)

type TermMapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DetectOptions controls optional strategies in detection pipeline.
type DetectOptions struct {
	ExactMatch        bool          `json:"exact_match"`
	NormalizeMatch    bool          `json:"normalize_match"`
	FuzzyMatch        bool          `json:"fuzzy_match"`
	PinyinMatch       bool          `json:"pinyin_match"`
	EnableTermMapping bool          `json:"enable_term_mapping"`
	EnableLLMAssist   bool          `json:"enable_llm_assist"`
	MappingMode       string        `json:"mapping_mode,omitempty"` // incremental / override
	CustomMappings    []TermMapping `json:"custom_mappings,omitempty"`
}

func DefaultDetectOptions() DetectOptions {
	return DetectOptions{
		ExactMatch:        true,
		NormalizeMatch:    true,
		FuzzyMatch:        true,
		PinyinMatch:       true,
		EnableTermMapping: true,
		MappingMode:       MappingModeIncremental,
	}
}

func (o DetectOptions) FillDefault(def DetectOptions) DetectOptions {
	if !o.ExactMatch && !o.NormalizeMatch && !o.FuzzyMatch && !o.PinyinMatch {
		return def
	}
	return o
}

type DetectorConfig struct {
	DefaultOptions         DetectOptions
	EnableNormalize        bool
	EnableFuzzy            bool
	EnablePinyin           bool
	EnableAutoPinyin       bool
	EnablePinyinInitials   bool
	PinyinAliasPath        string
	EnableLLMAssist        bool
	LLMAPIBaseURL          string
	LLMAPIKey              string
	LLMModel               string
	LLMTimeoutMS           int
	LLMMaxTextRunes        int
	EnableLLMHitReview     bool
	LLMHitReviewDailyLimit int
}

func DefaultDetectorConfig(basePath string) DetectorConfig {
	return DetectorConfig{
		DefaultOptions:         DefaultDetectOptions(),
		EnableNormalize:        envBool("SENSITIVE_ENABLE_NORMALIZE", true),
		EnableFuzzy:            envBool("SENSITIVE_ENABLE_FUZZY", true),
		EnablePinyin:           envBool("SENSITIVE_ENABLE_PINYIN", true),
		EnableAutoPinyin:       envBool("SENSITIVE_ENABLE_AUTO_PINYIN", true),
		EnablePinyinInitials:   envBool("SENSITIVE_ENABLE_PINYIN_INITIALS", false),
		PinyinAliasPath:        envStr("SENSITIVE_PINYIN_ALIAS_FILE", basePath+"/拼音混淆词/拼音映射.txt"),
		EnableLLMAssist:        envBool("SENSITIVE_ENABLE_LLM_ASSIST", false),
		LLMAPIBaseURL:          strings.TrimRight(envStr("SENSITIVE_LLM_API_BASE_URL", "https://api.deepseek.com"), "/"),
		LLMAPIKey:              strings.TrimSpace(envStr("SENSITIVE_LLM_API_KEY", "")),
		LLMModel:               strings.TrimSpace(envStr("SENSITIVE_LLM_MODEL", "deepseek-v4-flash")),
		LLMTimeoutMS:           envInt("SENSITIVE_LLM_TIMEOUT_MS", 10000),
		LLMMaxTextRunes:        envInt("SENSITIVE_LLM_MAX_TEXT_RUNES", 1200),
		EnableLLMHitReview:     envBool("SENSITIVE_ENABLE_LLM_HIT_REVIEW", false),
		LLMHitReviewDailyLimit: envInt("SENSITIVE_LLM_HIT_REVIEW_DAILY_LIMIT", 1000),
	}
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return def
	}
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}
