package core

import (
	"path/filepath"
	"reflect"
	"testing"
)

// 覆盖 DAT 导出的三条关键约束：16 字节词条头、偏移量相对词条区起点、UTF-16LE 边界对齐的 00 00 分隔符。
// 这三处曾经各自踩过坑（偏移按文件头算、分隔符按字节找），回归测试锁死。
func TestDatRoundtrip(t *testing.T) {
	entries := []Entry{
		{Code: "ad", Phrase: "adb devices", Pos: 1},
		{Code: "a", Phrase: "a", Pos: 3},                  // 长度 1：全部进编码字段
		{Code: "x", Phrase: "一", Pos: 2},                  // 长度 1 的中文
		{Code: "m", Phrase: "M哥", Pos: 1},                 // 长度 2：短语字段为空
		{Code: "em", Phrase: "😀测试", Pos: 5},             // 代理对（emoji）
		{Code: "cy", Phrase: "春风得意马蹄疾", Pos: 9},
		{Code: "gp", Phrase: "git push", Pos: 4},
	}

	path := filepath.Join(t.TempDir(), "t.dat")
	n, err := ExportDat(entries, path, 1790000000)
	if err != nil {
		t.Fatalf("ExportDat: %v", err)
	}
	if n != len(entries) {
		t.Fatalf("导出台数 %d，期望 %d", n, len(entries))
	}

	back, err := ParseDat(path)
	if err != nil {
		t.Fatalf("ParseDat: %v", err)
	}
	if !reflect.DeepEqual(entries, back) {
		t.Fatalf("DAT 回读不一致\n源: %#v\n回: %#v", entries, back)
	}
}

func TestZipRoundtrip(t *testing.T) {
	entries := []Entry{
		{Code: "xm", Phrase: "小米9", Pos: 1},
		{Code: "ai", Phrase: "AI", Pos: 1},
		{Code: "ff", Phrase: "fastboot flash", Pos: 2},
	}

	path := filepath.Join(t.TempDir(), "t.zip")
	if _, err := ExportZip(entries, path); err != nil {
		t.Fatalf("ExportZip: %v", err)
	}

	res, err := ImportZip(path)
	if err != nil {
		t.Fatalf("ImportZip: %v", err)
	}
	if len(res.Entries) != len(entries) {
		t.Fatalf("回读 %d 条，期望 %d", len(res.Entries), len(entries))
	}
	// zip 不写候选位置，回读时 Pos 统一为 0，比较时只看编码与短语。
	for i, e := range entries {
		got := res.Entries[i]
		if got.Code != e.Code || got.Phrase != e.Phrase {
			t.Fatalf("第 %d 条不一致: 源 %q/%q, 回 %q/%q", i, e.Code, e.Phrase, got.Code, got.Phrase)
		}
	}
	if res.PerLang["zh-CN"] != 1 || res.PerLang["zz"] != 2 {
		t.Fatalf("语言分组异常: %v", res.PerLang)
	}
}

func TestLangTag(t *testing.T) {
	cases := map[string]string{
		"小米9":        "zh-CN",
		"AI":         "zz",
		"fastboot":   "zz",
		"docker start": "zz",
		"一":          "zh-CN",
		"（全角）":       "zh-CN",
	}
	for in, want := range cases {
		if got := LangTag(in); got != want {
			t.Fatalf("LangTag(%q)=%q，期望 %q", in, got, want)
		}
	}
}
