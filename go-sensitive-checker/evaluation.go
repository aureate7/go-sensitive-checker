package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

// EvalSample 是一条带标注的评测样本。
type EvalSample struct {
	ID   string    `json:"id"`
	Text string    `json:"text"`
	Hits []EvalHit `json:"hits"`
	Note string    `json:"note,omitempty"`
}

// EvalHit 描述样本中一处预期命中：词库词与所属类别。
type EvalHit struct {
	Word     string `json:"word"`
	Category string `json:"category"`
}

// LoadEvalSamples 从 JSONL 文件加载评测样本。
func LoadEvalSamples(path string) ([]EvalSample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open eval set: %w", err)
	}
	defer f.Close()

	var samples []EvalSample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		var s EvalSample
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, fmt.Errorf("parse %s line %d: %w", path, line, err)
		}
		if s.ID == "" {
			return nil, fmt.Errorf("%s line %d: missing id", path, line)
		}
		samples = append(samples, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return samples, nil
}

// catCounts 累计每个类别的 tp / fp / fn。
type catCounts struct {
	tp, fp, fn int
}

// EvalReportEntry 是单个类别的指标。
type EvalReportEntry struct {
	Category  string  `json:"category"`
	TruePos   int     `json:"true_positives"`
	FalsePos  int     `json:"false_positives"`
	FalseNeg  int     `json:"false_negatives"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

// ComputeMetrics 将检测结果与标注对比，输出总体及分类别 PR/F1。
//
// 匹配规则：检测证据命中当且仅当存在同类别、同词库词的预期命中（每条预期只消费一次）；
// 类别正确但词库词不在标注中的算 FP，未被任何证据覆盖的预期算 FN。
func ComputeMetrics(samples []EvalSample, responses []DetectResponse) ([]EvalReportEntry, error) {
	if len(samples) != len(responses) {
		return nil, fmt.Errorf("sample/response length mismatch: %d vs %d", len(samples), len(responses))
	}
	counts := map[string]*catCounts{}

	for i, s := range samples {
		resp := responses[i]
		expected := make(map[string]int, len(s.Hits)) // key: category + "\x00" + word
		for _, h := range s.Hits {
			key := h.Category + "\x00" + h.Word
			expected[key]++
			cc := counts[h.Category]
			if cc == nil {
				cc = &catCounts{}
				counts[h.Category] = cc
			}
			cc.fn++
		}

		for _, ev := range resp.HitEvidences {
			key := ev.Category + "\x00" + ev.Word
			if expected[key] > 0 {
				expected[key]--
				cc := counts[ev.Category]
				cc.fn--
				cc.tp++
			} else {
				cc := counts[ev.Category]
				if cc == nil {
					cc = &catCounts{}
					counts[ev.Category] = cc
				}
				cc.fp++
			}
		}
	}

	cats := make([]string, 0, len(counts))
	for c := range counts {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	report := make([]EvalReportEntry, 0, len(cats))
	total := catCounts{}
	for _, c := range cats {
		cc := counts[c]
		total.tp += cc.tp
		total.fp += cc.fp
		total.fn += cc.fn
		report = append(report, newEvalReportEntry(c, *cc))
	}
	// 只要有任何一个类别出现过，就追加一行汇总。
	if len(report) > 0 || total.tp+total.fp+total.fn > 0 {
		report = append([]EvalReportEntry{newEvalReportEntry("__overall__", total)}, report...)
	}
	return report, nil
}

func newEvalReportEntry(category string, cc catCounts) EvalReportEntry {
	e := EvalReportEntry{Category: category, TruePos: cc.tp, FalsePos: cc.fp, FalseNeg: cc.fn}
	if cc.tp+cc.fp > 0 {
		e.Precision = float64(cc.tp) / float64(cc.tp+cc.fp)
	}
	if cc.tp+cc.fn > 0 {
		e.Recall = float64(cc.tp) / float64(cc.tp+cc.fn)
	}
	if e.Precision+e.Recall > 0 {
		e.F1 = 2 * e.Precision * e.Recall / (e.Precision + e.Recall)
	}
	return e
}

// FormatEvalReport 输出人类可读的对齐表格。
func FormatEvalReport(entries []EvalReportEntry) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-28s %6s %6s %6s %9s %9s %9s\n",
		"CATEGORY", "TP", "FP", "FN", "PREC", "RECALL", "F1"))
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("%-28s %6d %6d %6d %8.1f%% %8.1f%% %8.1f%%\n",
			e.Category, e.TruePos, e.FalsePos, e.FalseNeg,
			e.Precision*100, e.Recall*100, e.F1*100))
	}
	return b.String()
}

// runEval 是 -eval 离线评测入口：加载词库与标注集，输出 PR/F1 报告。
func runEval(args []string) {
	setPath := "evalset/samples.jsonl"
	if len(args) > 0 {
		setPath = args[0]
	}
	cfg := normalizedServerConfig(loadServerConfig())
	detector := NewDetector(cfg.WordListPath)
	status := detector.WordListStatus()
	log.Printf("word list status: ready=%t words=%d loaded_files=%d missing_files=%d",
		status.Ready, status.TotalWords, status.LoadedFiles, status.MissingFiles)

	samples, err := LoadEvalSamples(setPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	responses := make([]DetectResponse, 0, len(samples))
	for _, s := range samples {
		responses = append(responses, detector.Detect(s.Text, nil))
	}
	report, err := ComputeMetrics(samples, responses)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(FormatEvalReport(report))
	if report[0].Category == "__overall__" && report[0].F1 < 0.8 {
		fmt.Println("NOTE: overall F1 below 0.8 threshold")
		os.Exit(2)
	}
}
