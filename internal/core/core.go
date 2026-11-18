// Package core 提供词库的格式层实现：book.txt、Gboard v2 zip、微软拼音 DAT。
// 全部逻辑与 Python 版（ms_manager.py）对齐，已逐字节验证。
package core

import (
	"archive/zip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry 一条词库记录：编码在前、短语在后，与 book.txt 存储顺序一致。
type Entry struct {
	Code   string
	Phrase string
	Pos    int
}

const (
	// GboardHeader 是 Gboard v2 词典固定两行头，与真实导出逐字节相同。
	GboardHeader = "# Gboard Dictionary version:2\n" +
		"# Gboard Dictionary format:shortcut\tword\tlanguage_tag\tpos_tag\n"

	MaxZipFiles = 50
	MaxZipBytes = 20 << 20
)

var cjkRe = regexp.MustCompile(`[\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}\x{F900}-\x{FAFF}\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]`)

// LangTag 判语言：含中文/全角 → zh-CN，其余 → zz。
func LangTag(word string) string {
	if cjkRe.MatchString(word) {
		return "zh-CN"
	}
	return "zz"
}

// ---------------- book.txt ----------------

// LoadBook 读 book.txt（UTF-8，容忍 BOM 与 CRLF）。
func LoadBook(path string) ([]Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "\ufeff")
	var out []Entry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		code := strings.TrimSpace(parts[0])
		phrase := strings.TrimSpace(parts[1])
		if code == "" || phrase == "" {
			continue
		}
		pos := 1
		if len(parts) >= 3 {
			if v, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil && v >= 1 && v <= 9 {
				pos = v
			}
		}
		out = append(out, Entry{Code: code, Phrase: phrase, Pos: pos})
	}
	return out, nil
}

// SaveBook 写 book.txt，与 Python 版同构：CRLF、UTF-8 无 BOM、同样的注释头。
func SaveBook(path string, entries []Entry) error {
	var b strings.Builder
	b.WriteString("# 微软拼音词库 - 统一格式\r\n")
	b.WriteString("# 更新时间: " + time.Now().Format("2006-01-02 15:04:05") + "\r\n")
	b.WriteString("# 总计: " + strconv.Itoa(len(entries)) + " 条\r\n")
	b.WriteString("# 格式: 编码\t短语\t候选位置\r\n")
	b.WriteString("#" + strings.Repeat("-", 50) + "\r\n")
	for _, e := range entries {
		b.WriteString(e.Code + "\t" + e.Phrase + "\t" + strconv.Itoa(e.Pos) + "\r\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---------------- Gboard zip ----------------

// ExportZip 生成 Gboard v2 词典包，返回各语言条数。
func ExportZip(entries []Entry, out string) (map[string]int, error) {
	groups := map[string][][2]string{}
	for _, e := range entries {
		tag := LangTag(e.Phrase)
		groups[tag] = append(groups[tag], [2]string{e.Code, e.Phrase})
	}
	perLang := map[string]int{}
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, tag := range []string{"zh-CN", "zz"} {
		rows := groups[tag]
		if len(rows) == 0 {
			continue
		}
		var body strings.Builder
		for _, r := range rows {
			body.WriteString(r[0] + "\t" + r[1] + "\t" + tag + "\t\n")
		}
		hdr := &zip.FileHeader{Name: "dictionary_" + tag + ".txt", Method: zip.Deflate}
		hdr.Modified = time.Now()
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, GboardHeader+body.String()); err != nil {
			return nil, err
		}
		perLang[tag] = len(rows)
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return perLang, nil
}

// GboardImport 导入结果。
type GboardImport struct {
	Entries   []Entry
	PerLang   map[string]int
	Files     int
	Legacy    int
	HeaderWarn []string
	Warnings  []string
	Skipped   int
}

// ImportZip 读 Gboard zip；若文件本身不是 zip，按旧版单文件 txt 兼容解析。
func ImportZip(path string) (*GboardImport, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		// 不是有效 zip：尝试当旧版纯文本词库解析（单文件 dictionary.txt / 2 列旧格式）
		if res, terr := importTextFile(path); terr == nil && res != nil {
			return res, nil
		}
		return nil, err
	}
	defer zr.Close()

	res := &GboardImport{PerLang: map[string]int{}}
	files := zr.File
	if len(files) > MaxZipFiles {
		return nil, ErrZipTooBig
	}
	var total uint64
	for _, f := range files {
		total += f.UncompressedSize64
	}
	if total > MaxZipBytes {
		return nil, ErrZipTooBig
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, zf := range files {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := zf.Name
		base := filepath.Base(name)
		if strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, ".") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			res.Warnings = append(res.Warnings, name+"：无法读取，已跳过")
			continue
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			res.Warnings = append(res.Warnings, name+"：读取失败，已跳过")
			continue
		}
		res.Files++
		text := strings.TrimPrefix(string(raw), "\ufeff")
		if !strings.HasPrefix(text, "# Gboard Dictionary version:2") {
			res.HeaderWarn = append(res.HeaderWarn, name)
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 2 {
				res.Skipped++
				continue
			}
			shortcut := strings.TrimSpace(parts[0])
			word := strings.TrimSpace(parts[1])
			if shortcut == "" || word == "" {
				res.Skipped++
				continue
			}
			res.Entries = append(res.Entries, Entry{Code: shortcut, Phrase: word})
			res.PerLang[LangTag(word)]++
		}
	}
	return res, nil
}

// importTextFile 兼容旧版纯文本词库：2 列（编码\t短语）或 4 列（短码\t短语\t语言\t候选）。
// 命中至少一行才认，避免把任意二进制文件当词库读进来。
func importTextFile(path string) (*GboardImport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	res := &GboardImport{PerLang: map[string]int{}, Files: 1}
	text := strings.TrimPrefix(string(raw), "\ufeff")
	if !strings.HasPrefix(text, "# Gboard Dictionary version:2") {
		res.HeaderWarn = append(res.HeaderWarn, filepath.Base(path))
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			res.Skipped++
			continue
		}
		shortcut := strings.TrimSpace(parts[0])
		word := strings.TrimSpace(parts[1])
		if shortcut == "" || word == "" {
			res.Skipped++
			continue
		}
		res.Entries = append(res.Entries, Entry{Code: shortcut, Phrase: word})
		res.PerLang[LangTag(word)]++
		res.Legacy++
	}
	if len(res.Entries) == 0 {
		return nil, nil
	}
	return res, nil
}


const datHeaderSize = 48

// ExportDat 生成 DAT。ts 为 0 时用当前时间（固定值便于与 Python 版比对）。
func ExportDat(entries []Entry, out string, ts int64) (int, error) {
	if ts == 0 {
		ts = time.Now().Unix()
	}
	now := uint32(ts)

	entriesData := make([][]byte, 0, len(entries))
	offsets := make([]uint32, 0, len(entries))
	var cur uint32

	for _, e := range entries {
		head, tail := splitPhrase(e.Phrase, 2)
		codeField := append(utf16le(e.Code), 0, 0)
		codeField = append(codeField, utf16le(head)...)
		phraseField := append(utf16le(tail), 0, 0)
		codeTotalLen := uint16(len(codeField) + 12)

		buf := make([]byte, 0, 12+len(codeField)+len(phraseField))
		buf = append(buf, 0x10, 0x00, 0x10, 0x00) // marker 0x00100010
		buf = append(buf, byte(codeTotalLen), byte(codeTotalLen>>8))
		buf = append(buf, byte(e.Pos))
		buf = append(buf, 0x06)
		buf = append(buf, 0, 0, 0, 0)
		buf = binary.LittleEndian.AppendUint32(buf, now)
		buf = append(buf, codeField...)
		buf = append(buf, phraseField...)

		entriesData = append(entriesData, buf)
		offsets = append(offsets, cur)
		cur += uint32(len(buf))
	}

	offsetTableStart := uint32(datHeaderSize)
	entryAreaStart := offsetTableStart + uint32(len(entries))*4
	totalLen := entryAreaStart + cur

	var file []byte
	file = append(file, []byte("mschxudp")...)
	file = append(file, 0x02, 0x00, 0x60, 0x00, 0x01, 0x00, 0x00, 0x00)
	file = binary.LittleEndian.AppendUint32(file, offsetTableStart)
	file = binary.LittleEndian.AppendUint32(file, entryAreaStart)
	file = binary.LittleEndian.AppendUint32(file, totalLen)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(entries)))
	file = binary.LittleEndian.AppendUint32(file, now)
	file = append(file, make([]byte, 12)...)
	for _, off := range offsets {
		file = binary.LittleEndian.AppendUint32(file, off)
	}
	for _, d := range entriesData {
		file = append(file, d...)
	}
	return len(entries), os.WriteFile(out, file, 0o644)
}

// ParseDat 解析 DAT，返回 (短语, 编码, 候选位置)。逻辑对齐 Python 版 parse_dat_file。
func ParseDat(path string) ([]Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < datHeaderSize {
		return nil, ErrBadDat
	}
	magic := string(raw[0:8])
	if magic != "mschxudp" && magic != "machxudp" {
		return nil, ErrBadDat
	}
	offsetStart := int64(binary.LittleEndian.Uint32(raw[16:20]))
	// 偏移表里的偏移量是相对「词条区起点」而不是文件头，必须从 20:24 读，不能直接用 48。
	entryAreaStart := int64(binary.LittleEndian.Uint32(raw[20:24]))
	entryCount := int(binary.LittleEndian.Uint32(raw[28:32]))
	if entryCount < 0 || offsetStart < 0 || entryAreaStart < int64(datHeaderSize) {
		return nil, ErrBadDat
	}
	var out []Entry
	for i := 0; i < entryCount; i++ {
		pos := offsetStart + int64(i)*4
		if pos+4 > int64(len(raw)) {
			break
		}
		off := int64(binary.LittleEndian.Uint32(raw[pos : pos+4]))
		p := entryAreaStart + off
		// 词条头是 16 字节：marker(4) + codeTotalLen(2) + pos(1) + flag(1) + reserved(4) + timestamp(4)
		const entryHeaderSize = 16
		if p+entryHeaderSize > int64(len(raw)) {
			continue
		}
		codeTotalLen := int(binary.LittleEndian.Uint16(raw[p+4 : p+6]))
		position := int(raw[p+6])
		if position < 1 || position > 9 {
			position = 1
		}
		codeBytes := int64(codeTotalLen) - 12
		if codeBytes <= 0 || p+entryHeaderSize+codeBytes > int64(len(raw)) {
			continue
		}
		seg := raw[p+entryHeaderSize : p+entryHeaderSize+codeBytes]
		idx := bytesIndex(seg, []byte{0, 0})
		if idx < 0 {
			continue
		}
		code := fromUTF16LE(seg[:idx])
		headText := fromUTF16LE(seg[idx+2:])
		rest := raw[p+entryHeaderSize+codeBytes:]
		idx2 := bytesIndex(rest, []byte{0, 0})
		if idx2 < 0 {
			continue
		}
		tailText := fromUTF16LE(rest[:idx2])
		phrase := headText + tailText
		if code == "" || phrase == "" {
			continue
		}
		out = append(out, Entry{Code: code, Phrase: phrase, Pos: position})
	}
	return out, nil
}

// ---------------- 内部工具 ----------------

func splitPhrase(s string, n int) (string, string) {
	rs := []rune(s)
	if len(rs) >= n {
		return string(rs[:n]), string(rs[n:])
	}
	return s, ""
}

func utf16le(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		if r < 0x10000 {
			out = append(out, byte(r), byte(r>>8))
			continue
		}
		v := r - 0x10000
		hi := 0xD800 + (v >> 10)
		lo := 0xDC00 + (v & 0x3FF)
		out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
	}
	return out
}

func fromUTF16LE(b []byte) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		u := uint32(b[i]) | uint32(b[i+1])<<8
		if u >= 0xD800 && u <= 0xDBFF && i+3 < len(b) {
			lo := uint32(b[i+2]) | uint32(b[i+3])<<8
			if lo >= 0xDC00 && lo <= 0xDFFF {
				sb.WriteRune(rune((u-0xD800)<<10 + (lo - 0xDC00) + 0x10000))
				i += 2
				continue
			}
		}
		sb.WriteRune(rune(u))
	}
	return sb.String()
}

// bytesIndex 在字节流里找 2 字节的分隔符，但只按 UTF-16LE 的 2 字节边界对齐查找。
// 若按字节逐位找，ASCII 字符自身的 0x00 高位字节会造出「错位」的 00 00，把编码切坏（实测 code "ad" 只解出 "a"）。
func bytesIndex(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i += 2 {
		if b[i] == sub[0] && b[i+1] == sub[1] {
			return i
		}
	}
	return -1
}
