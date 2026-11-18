// 可行性验证用 CLI：复用 internal/core 的格式层，链路与 GUI 完全同一份实现。
// 用法：
//   spike export-zip <book.txt> <out.zip>
//   spike export-dat <book.txt> <out.dat> [unix秒]
//   spike import-zip <in.zip> <out.book>
//   spike roundtrip  <book.txt> <workdir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gboard2ms/internal/core"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "export-zip":
		if len(os.Args) != 4 {
			usage()
		}
		entries, err := core.LoadBook(os.Args[2])
		must(err)
	fmt.Printf("读出 %d 条\n", len(entries))
		per, err := core.ExportZip(entries, os.Args[3])
		must(err)
		fmt.Printf("导出 zip: zh-CN %d / zz %d -> %s\n", per["zh-CN"], per["zz"], os.Args[3])

	case "export-dat":
		if len(os.Args) < 4 {
			usage()
		}
		ts := int64(0)
		if len(os.Args) >= 5 {
			fmt.Sscan(os.Args[4], &ts)
		}
		entries, err := core.LoadBook(os.Args[2])
		must(err)
		n, err := core.ExportDat(entries, os.Args[3], ts)
		must(err)
		st, _ := os.Stat(os.Args[3])
		fmt.Printf("导出 dat: %d 条 / %d B -> %s\n", n, st.Size(), os.Args[3])

	case "import-zip":
		if len(os.Args) != 4 {
			usage()
		}
		res, err := core.ImportZip(os.Args[2])
		must(err)
		fmt.Printf("解析 %d 个文件 / %d 条（zh-CN %d / zz %d）\n",
			res.Files, len(res.Entries), res.PerLang["zh-CN"], res.PerLang["zz"])
		if len(res.HeaderWarn) > 0 {
			fmt.Printf("头部缺失: %v\n", res.HeaderWarn)
		}
		entries := make([]core.Entry, 0, len(res.Entries))
		for _, e := range res.Entries {
			pos := e.Pos
			if pos < 1 || pos > 9 {
				pos = 1
			}
			entries = append(entries, core.Entry{Code: e.Code, Phrase: e.Phrase, Pos: pos})
		}
		must(core.SaveBook(os.Args[3], entries))
		fmt.Printf("写回 book.txt: %d 条 -> %s\n", len(entries), os.Args[3])

	case "roundtrip":
		if len(os.Args) != 4 {
			usage()
		}
		entries, err := core.LoadBook(os.Args[2])
		must(err)
		dir := os.Args[3]
		must(os.MkdirAll(dir, 0o755))
		zipPath := filepath.Join(dir, "rt.zip")
		per, err := core.ExportZip(entries, zipPath)
		must(err)
		res, err := core.ImportZip(zipPath)
		must(err)
		fmt.Printf("源 %d 条 -> zip(zh %d / zz %d) -> 回读 %d 条（zh %d / zz %d）\n",
			len(entries), per["zh-CN"], per["zz"], len(res.Entries), res.PerLang["zh-CN"], res.PerLang["zz"])
		sortKeys := func(list []core.Entry) []string {
			ks := make([]string, 0, len(list))
			for _, e := range list {
				ks = append(ks, fmt.Sprintf("%s\x00%s", e.Code, e.Phrase))
			}
			sort.Strings(ks)
			return ks
		}
		a, b := sortKeys(entries), sortKeys(res.Entries)
		same := len(a) == len(b)
		if same {
			for i := range a {
				if a[i] != b[i] {
					same = false
					break
				}
			}
		}
		// 导出按语言分组（zh-CN 在前），回读顺序与源不同属预期，这里只比内容集合。
		if same {
			fmt.Println("内容集合一致: True")
		} else {
			fmt.Println("内容集合一致: False")
			os.Exit(1)
		}

	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: spike export-zip|export-dat|import-zip|roundtrip ...")
	os.Exit(2)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "失败:", err)
		os.Exit(1)
	}
}
