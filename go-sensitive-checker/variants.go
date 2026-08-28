package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type GeneratedVariant struct {
	Word       string `json:"word"`
	Category   string `json:"category"`
	Alias      string `json:"alias"`
	Type       string `json:"type"`
	Confidence int    `json:"confidence"`
}

type VariantCatalog struct {
	Version     int                `json:"version"`
	GeneratedAt time.Time          `json:"generated_at"`
	Variants    []GeneratedVariant `json:"variants"`
}

var visualSubstitutions = map[rune][]rune{
	'枪': {'鎗'}, '台': {'臺'}, '后': {'後'}, '发': {'發'}, '里': {'裏'},
	'国': {'國'}, '党': {'黨'}, '药': {'藥'}, '钱': {'錢'}, '网': {'網'},
}

func generateVariants(words map[string]map[string]struct{}, pinyinMatcher *PinyinMatcher) VariantCatalog {
	seen := map[string]struct{}{}
	out := make([]GeneratedVariant, 0)
	add := func(v GeneratedVariant) {
		v.Alias = strings.TrimSpace(v.Alias)
		if v.Alias == "" || v.Alias == v.Word {
			return
		}
		key := v.Category + "\x00" + v.Word + "\x00" + v.Alias
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	for category, set := range words {
		for word := range set {
			runes := []rune(word)
			if len(runes) > 1 {
				add(GeneratedVariant{word, category, strings.Join(strings.Split(word, ""), " "), "separator", 95})
			}
			for i, r := range runes {
				for _, replacement := range visualSubstitutions[r] {
					copyRunes := append([]rune(nil), runes...)
					copyRunes[i] = replacement
					add(GeneratedVariant{word, category, string(copyRunes), "visual", 85})
				}
			}
			for _, alias := range pinyinMatcher.autoPinyinAliases(word) {
				add(GeneratedVariant{word, category, alias, "pinyin", 80})
			}
			initials := pinyinInitials(pinyinMatcher.autoPinyinAliases(word))
			if initials != "" {
				add(GeneratedVariant{word, category, initials, "pinyin_initials", 65})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].Word != out[j].Word {
			return out[i].Word < out[j].Word
		}
		return out[i].Alias < out[j].Alias
	})
	return VariantCatalog{Version: 1, GeneratedAt: time.Now().UTC(), Variants: out}
}

func pinyinInitials(aliases []string) string {
	for _, alias := range aliases {
		parts := strings.Fields(alias)
		if len(parts) < 2 {
			continue
		}
		var b strings.Builder
		for _, part := range parts {
			for _, r := range part {
				if unicode.IsLetter(r) {
					b.WriteRune(r)
					break
				}
			}
		}
		if b.Len() >= 2 {
			return b.String()
		}
	}
	return ""
}

func (d *Detector) loadGeneratedVariants(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var catalog VariantCatalog
	if json.Unmarshal(raw, &catalog) != nil {
		return
	}
	for _, v := range catalog.Variants {
		if _, ok := d.sensitiveWords[v.Category][v.Word]; !ok {
			continue
		}
		alias := d.normalizer.NormalizeTextAggressive(v.Alias).NormalizedText
		if alias == "" {
			continue
		}
		index := d.fuzzyIndex[v.Category]
		if v.Type == "pinyin" || v.Type == "pinyin_initials" {
			index = d.pinyinIndex[v.Category]
		}
		if index == nil {
			continue
		}
		index.AliasToKW[alias] = appendUniq(index.AliasToKW[alias], v.Word)
	}
	rebuildAliasIndexes(d.fuzzyIndex)
	rebuildAliasIndexes(d.pinyinIndex)
}

func rebuildAliasIndexes(indexes map[string]*AliasIndex) {
	for category, old := range indexes {
		aliases := make([]string, 0, len(old.AliasToKW))
		for alias := range old.AliasToKW {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		ac := NewAC()
		ac.Build(aliases)
		old.Automaton = ac
		indexes[category] = old
	}
}

func runGenerateVariants(args []string) {
	cfg := normalizedServerConfig(loadServerConfig())
	d := NewDetector(cfg.WordListPath)
	output := d.config.VariantAliasPath
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		output = args[0]
	}
	catalog := generateVariants(d.sensitiveWords, d.pinyinMatcher)
	raw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate variants: %v\n", err)
		os.Exit(1)
	}
	if err := atomicWriteFile(output, append(raw, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "write variants: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("generated %d variants: %s\n", len(catalog.Variants), output)
}
