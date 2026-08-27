package main

import (
	"os"
	"path/filepath"
	"testing"
)

func evalHit(word, cat string) EvalHit { return EvalHit{Word: word, Category: cat} }

func TestComputeMetricsPerfectDetection(t *testing.T) {
	samples := []EvalSample{
		{ID: "s1", Text: "含敏感词A", Hits: []EvalHit{evalHit("敏感词A", "abusive_high")}},
	}
	resps := []DetectResponse{
		{HitEvidences: []HitEvidence{{Word: "敏感词A", Category: "abusive_high"}}},
	}
	report, err := ComputeMetrics(samples, resps)
	if err != nil {
		t.Fatal(err)
	}
	overall := report[0]
	if overall.Category != "__overall__" || overall.TruePos != 1 || overall.F1 != 1 {
		t.Fatalf("unexpected overall: %+v", overall)
	}
}

func TestComputeMetricsFalsePositivesAndNegatives(t *testing.T) {
	samples := []EvalSample{
		{
			ID:   "s1",
			Text: "文本",
			Hits: []EvalHit{evalHit("词库词", "advertising_high"), evalHit("漏检词", "advertising_low")},
		},
	}
	resps := []DetectResponse{
		{HitEvidences: []HitEvidence{
			{Word: "词库词", Category: "advertising_high"},   // TP
			{Word: "误报词", Category: "violent_high"},       // FP（新类别）
			{Word: "类别错词", Category: "pornographic_high"}, // 标注为 advertising 才算命中 → FP
		}},
	}
	report, err := ComputeMetrics(samples, resps)
	if err != nil {
		t.Fatal(err)
	}
	overall := report[0]
	if overall.Category != "__overall__" {
		t.Fatalf("first row should be overall, got %s", overall.Category)
	}
	if overall.TruePos != 1 || overall.FalsePos != 2 || overall.FalseNeg != 1 {
		t.Fatalf("counts wrong: %+v", overall)
	}
	wantP := 1.0 / 3.0
	wantR := 0.5
	if diff := overall.Precision - wantP; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("precision = %f, want %f", overall.Precision, wantP)
	}
	if overall.Recall != wantR {
		t.Fatalf("recall = %f, want %f", overall.Recall, wantR)
	}
}

func TestComputeMetricsDuplicateHitsConsumedOnce(t *testing.T) {
	samples := []EvalSample{
		{ID: "s1", Text: "重复出现", Hits: []EvalHit{evalHit("重复词", "abusive_low")}},
	}
	resps := []DetectResponse{
		{HitEvidences: []HitEvidence{
			{Word: "重复词", Category: "abusive_low"},
			{Word: "重复词", Category: "abusive_low"}, // 多策略产生的重复证据应被去重，不另计 FP
		}},
	}
	report, _ := ComputeMetrics(samples, resps)
	e := report[0]
	if e.TruePos != 1 || e.FalsePos != 0 || e.FalseNeg != 0 {
		t.Fatalf("expected tp=1 fp=0 fn=0 after dedupe, got %+v", e)
	}
}

func TestComputeMetricsLengthMismatch(t *testing.T) {
	if _, err := ComputeMetrics([]EvalSample{{ID: "a"}}, nil); err == nil {
		t.Fatal("length mismatch should error")
	}
}

func TestLoadEvalSamples(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "samples.jsonl")
	content := `# 注释行
{"id":"s1","text":"hello","hits":[{"word":"w","category":"c"}]}
{"id":"s2","text":"world","hits":[],"note":"clean"}

{"id":"","broken":true}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	samples, err := LoadEvalSamples(path)
	if err == nil {
		t.Fatal("expected parse error on invalid line")
	}
	if samples != nil && len(samples) > 0 && samples[0].ID != "s1" {
		t.Fatalf("parsed prefix sample wrong: %+v", samples[0])
	}

	good := `# only comment

{"id":"x","text":"t","hits":null}
`
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	samples, err = LoadEvalSamples(path)
	if err != nil || len(samples) != 1 || samples[0].ID != "x" {
		t.Fatalf("load failed: %v %+v", err, samples)
	}
}

func TestFormatEvalReportAlignsRows(t *testing.T) {
	out := FormatEvalReport([]EvalReportEntry{
		{Category: "__overall__", TruePos: 3, FalsePos: 1, FalseNeg: 1, Precision: .75, Recall: .75, F1: .75},
	})
	if out == "" || len(out) < 10 {
		t.Fatal("empty report")
	}
}

// End-to-end：仅当本机存在可用词库时运行（CI 环境无词库会自动跳过）。
func TestEvaluationAgainstDetector(t *testing.T) {
	detector := NewDetector("temp")
	if !detector.WordListStatus().Ready {
		t.Skip("wordlist not available; skipping end-to-end evaluation")
	}
	samples, err := LoadEvalSamples(filepath.Join("evalset", "samples.jsonl"))
	if err != nil {
		t.Fatalf("load shipped eval set: %v", err)
	}
	resps := make([]DetectResponse, 0, len(samples))
	for _, s := range samples {
		resps = append(resps, detector.Detect(s.Text, nil))
	}
	report, err := ComputeMetrics(samples, resps)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + FormatEvalReport(report))
}
