package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WhitelistFileName 是词库根目录下的白名单文件名。
// 行格式：
//
//	词条                       —— 全类别生效
//	词条<TAB>category1,category2 —— 仅对列出的类别生效
//	# 开头为注释；空行忽略
const WhitelistFileName = "白名单.txt"

// loadWhitelist 从词库根目录加载白名单。
// 文件缺失不算错误（返回 nil），解析错误逐行跳过并记入 loadStatus.Errors。
func (d *Detector) loadWhitelist() {
	d.whitelistGlobal = make(map[string]struct{})
	d.whitelistByCategory = make(map[string]map[string]struct{})

	path := filepath.Join(d.basePath, WhitelistFileName)
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			d.loadStatus.Errors = append(d.loadStatus.Errors, "open whitelist: "+err.Error())
		}
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		word, cats := parseWhitelistLine(raw)
		if word == "" {
			d.loadStatus.Errors = append(d.loadStatus.Errors,
				WhitelistFileName+": line "+strconv.Itoa(line)+" invalid")
			continue
		}
		if len(cats) == 0 {
			d.whitelistGlobal[word] = struct{}{}
			continue
		}
		for _, c := range cats {
			if d.whitelistByCategory[c] == nil {
				d.whitelistByCategory[c] = make(map[string]struct{})
			}
			d.whitelistByCategory[c][word] = struct{}{}
		}
	}
	if err := sc.Err(); err != nil {
		d.loadStatus.Errors = append(d.loadStatus.Errors, "scan whitelist: "+err.Error())
	}
}

func parseWhitelistLine(raw string) (word string, categories []string) {
	parts := strings.Split(raw, "\t")
	if len(parts) > 2 {
		return "", nil
	}
	word = strings.TrimSpace(parts[0])
	if word == "" || strings.ContainsAny(word, " ") {
		return "", nil
	}
	if len(parts) == 1 {
		return word, nil
	}
	for _, c := range strings.Split(parts[1], ",") {
		if c = strings.TrimSpace(c); c != "" {
			categories = append(categories, c)
		}
	}
	return word, categories
}

// isWhitelisted 判断某词库词是否在给定类别下被豁免。
func (d *Detector) isWhitelisted(category, word string) bool {
	if _, ok := d.whitelistGlobal[word]; ok {
		return true
	}
	if set, ok := d.whitelistByCategory[category]; ok {
		if _, ok := set[word]; ok {
			return true
		}
	}
	return false
}

// WhitelistEntries 返回当前白名单（全量 + 分类别），用于 /api/admin/whitelist 展示。
func (d *Detector) WhitelistEntries() (global []string, byCategory map[string][]string) {
	global = make([]string, 0, len(d.whitelistGlobal))
	for w := range d.whitelistGlobal {
		global = append(global, w)
	}
	byCategory = make(map[string][]string, len(d.whitelistByCategory))
	for c, set := range d.whitelistByCategory {
		list := make([]string, 0, len(set))
		for w := range set {
			list = append(list, w)
		}
		byCategory[c] = list
	}
	return global, byCategory
}
