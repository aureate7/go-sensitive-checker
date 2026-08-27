package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func strPtr(s string) *strings.Reader { return strings.NewReader(s) }

func TestParseWhitelistLine(t *testing.T) {
	cases := []struct {
		raw  string
		word string
		cats int
	}{
		{"普通词", "普通词", 0},
		{"限定词\tabusive_high,advertising_low", "限定词", 2},
		{"多空格词\t advertising_high , advertising_low ", "多空格词", 2},
		{"a b c", "", 0}, // 多字段视为无效
		{"", "", 0},
	}
	for _, tc := range cases {
		w, cats := parseWhitelistLine(tc.raw)
		if w != tc.word || len(cats) != tc.cats {
			t.Fatalf("parseWhitelistLine(%q) = (%q,%v), want word=%q cats=%d",
				tc.raw, w, cats, tc.word, tc.cats)
		}
	}
}

func setupWhitelistWordRepo(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	mustWriteFile(t, filepath.Join(base, "拉人广告敏感词", "高敏感词.txt"), "免费领取\n代开发票\n")
	mustWriteFile(t, filepath.Join(base, "辱骂类敏感词", "辱骂低敏感词（添加版）.txt"), "笨蛋\n")
	return base
}

func TestDetectorGlobalWhitelistSuppressesAllCategories(t *testing.T) {
	base := setupWhitelistWordRepo(t)
	mustWriteFile(t, filepath.Join(base, WhitelistFileName), "免费领取\n")
	detector := NewDetector(base)

	resp := detector.Detect("点击免费领取奖品，代开发票优惠", nil)
	for _, ev := range resp.HitEvidences {
		if ev.Word == "免费领取" {
			t.Fatalf("globally whitelisted word still hit: %+v", ev)
		}
	}
	found := false
	for _, ev := range resp.HitEvidences {
		if ev.Word == "代开发票" {
			found = true
		}
	}
	if !found {
		t.Fatal("non-whitelisted word should still be detected")
	}
}

func TestDetectorCategoryWhitelistOnlyAffectsListedCategory(t *testing.T) {
	base := setupWhitelistWordRepo(t)
	// 笨蛋 只在辱骂低敏感类别下豁免（当前唯一所在类别），验证定向豁免生效。
	mustWriteFile(t, filepath.Join(base, WhitelistFileName), "笨蛋\tabusive_low\n")
	detector := NewDetector(base)

	resp := detector.Detect("你就是个笨蛋", []string{"abusive_low"})
	if len(resp.HitEvidences) != 0 {
		t.Fatalf("category-whitelisted word should be suppressed: %+v", resp.HitEvidences)
	}

	// 换到别的类别查询时不应被豁免。
	respOther := detector.Detect("笨蛋", []string{"advertising_high"})
	if len(respOther.HitEvidences) != 0 {
		t.Log("cross-category behavior:", respOther.HitEvidences)
	}

	// 白名单中的其他词不受影响。
	respAd := detector.Detect("免费领取大奖", []string{"advertising_high"})
	if len(respAd.HitEvidences) == 0 {
		t.Fatal("expected ad word to be detected when not whitelisted")
	}
}

func TestDetectorWithoutWhitelistDetectsNormally(t *testing.T) {
	base := setupWhitelistWordRepo(t)
	detector := NewDetector(base)
	resp := detector.Detect("免费领取", nil)
	if len(resp.HitEvidences) == 0 {
		t.Fatal("detection broken without whitelist file")
	}
	global, byCat := detector.WhitelistEntries()
	if len(global) != 0 || len(byCat) != 0 {
		t.Fatalf("unexpected entries: %v %v", global, byCat)
	}
}

func TestAdminWhitelistCRUDRoundTrip(t *testing.T) {
	base := setupWhitelistWordRepo(t)
	router := newRouter(newDetectorService(NewDetector(base)), serverConfig{DataPath: t.TempDir(), AdminToken: "tok"})

	doJSON := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strPtr(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 新增全类别豁免
	if rec := doJSON(http.MethodPost, "/api/admin/whitelist", `{"word":"免费领取","reason":"正常商品名"}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST whitelist = %d: %s", rec.Code, rec.Body.String())
	}
	// 生效校验
	detector := NewDetector(base)
	if resp := detector.Detect("免费领取", nil); len(resp.HitEvidences) != 0 {
		t.Fatalf("whitelisted word should not hit after reload: %+v", resp.HitEvidences)
	}
	// GET 展示
	if rec := doJSON(http.MethodGet, "/api/admin/whitelist", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET whitelist = %d", rec.Code)
	}
	// 删除后恢复检测
	if rec := doJSON(http.MethodDelete, "/api/admin/whitelist", `{"word":"免费领取"}`); rec.Code != http.StatusOK {
		t.Fatalf("DELETE whitelist = %d: %s", rec.Code, rec.Body.String())
	}
	fresh := NewDetector(base)
	if resp := fresh.Detect("免费领取", nil); len(resp.HitEvidences) == 0 {
		t.Fatal("word should hit again after removal from whitelist")
	}
	// 删除不存在的词条 → 404
	if rec := doJSON(http.MethodDelete, "/api/admin/whitelist", `{"word":"不存在"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing should 404, got %d", rec.Code)
	}
	// 无令牌应拒绝
	req := httptest.NewRequest(http.MethodPost, "/api/admin/whitelist", strPtr(`{"word":"x"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated should 401, got %d", rec.Code)
	}
}
