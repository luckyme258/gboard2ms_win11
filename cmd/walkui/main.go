// Gboard / 微软拼音 词库管理器的 Go 原型（walk + Win32 原生控件）。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"github.com/lxn/win"

	"gboard2ms/internal/core"
)

// ---------------- 文案 ----------------

type pair struct{ zh, en string }

var texts = map[string]pair{
	"title":       {"微软拼音词库管理器 (Go)", "MS Pinyin Dictionary Manager (Go)"},
	"search":      {"搜索", "Search"},
	"add":         {"添加", "Add"},
	"del":         {"删除", "Delete"},
	"imp_gboard":  {"导入 Gboard", "Import Gboard"},
	"imp_dat":     {"导入 DAT", "Import DAT"},
	"imp_book":    {"导入 book.txt", "Import book.txt"},
	"exp_gboard":  {"导出 Gboard", "Export Gboard"},
	"exp_dat":     {"导出 DAT", "Export DAT"},
	"save":        {"保存", "Save"},
	"open_folder": {"打开文件夹", "Open Folder"},
	"language":    {"语言", "Language"},
	"stats":       {"总计: %d 条 | 唯一编码: %d 个 | 重复编码: %d 个", "Total: %d | Unique shortcuts: %d | Duplicated: %d"},
	"status":      {"显示 %d / %d 条", "Showing %d / %d"},
	"ready":       {"就绪", "Ready"},
	"col_phrase":  {"短语", "Phrase"},
	"col_code":    {"编码", "Shortcut"},
	"col_pos":     {"候选位置", "Position"},

	"dlg_add":     {"添加词条", "Add entry"},
	"dlg_edit":    {"编辑词条", "Edit entry"},
	"lbl_phrase":  {"短语", "Phrase"},
	"lbl_code":    {"编码", "Shortcut"},
	"lbl_pos":     {"候选位置", "Position"},
	"ok":          {"确定", "OK"},
	"cancel":      {"取消", "Cancel"},
	"err_empty":   {"短语和编码不能为空", "Phrase and shortcut cannot be empty"},
	"err_dup":     {"编码「%s」下已存在短语「%s」", "Shortcut %q already has phrase %q"},
	"confirm_del": {"确定删除选中的词条？", "Delete the selected entry?"},
	"confirm_del_n": {"确定删除选中的 %d 条词条？", "Delete the %d selected entries?"},
	"hint":        {"提示", "Notice"},
	"error":       {"错误", "Error"},
	"warn_sel":    {"请先选中一行", "Select a row first"},

	"imp_gboard_done": {"从 %d 个文件导入 %d 条（%s），跳过重复 %d 条", "Imported %d rows from %d file(s) (%s), skipped %d duplicates"},
	"imp_none":        {"未从文件中解析到有效条目", "No valid rows found in the file"},
	"imp_all_dup":     {"没有新增条目：%d 条已存在", "Nothing new: %d row(s) already exist"},
	"imp_dat_done":    {"从 DAT 导入 %d 条", "Imported %d rows from DAT"},
	"imp_book_done":   {"从 book.txt 导入 %d 条，跳过重复 %d 条", "Imported %d rows from book.txt, skipped %d duplicates"},
	"imp_header_warn": {"以下文件缺少 version:2 头部，已宽松解析：%s", "Missing version:2 header (parsed leniently): %s"},
	"exp_gboard_done": {"导出 %d 条 -> %s（%s）", "Exported %d rows -> %s (%s)"},
	"exp_dat_done":    {"导出 %d 条 -> %s", "Exported %d rows -> %s"},
	"save_done":       {"已保存 %d 条", "Saved %d rows"},
	"save_fail":       {"保存失败: %v", "Save failed: %v"},
	"load_fail":       {"读取 book.txt 失败: %v", "Failed to read book.txt: %v"},
	"ft_zip":          {"Gboard 词典包 (*.zip)", "Gboard dictionary (*.zip)"},
	"ft_dat":          {"微软拼音 (*.dat)", "Microsoft Pinyin (*.dat)"},
	"ft_book":         {"词库 (*.txt)", "Dictionary (*.txt)"},
	"ft_all":          {"所有文件 (*.*)", "All files (*.*)"},
	"dialog_open":     {"选择文件", "Open"},
	"dialog_save":     {"保存到", "Save as"},
}

// ---------------- 表格模型 ----------------

type tableModel struct {
	walk.TableModelBase
	rows []core.Entry
}

func (m *tableModel) RowCount() int { return len(m.rows) }

func (m *tableModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	e := m.rows[row]
	switch col {
	case 0:
		return e.Phrase // 表格列序：短语在前
	case 1:
		return e.Code
	case 2:
		return e.Pos
	}
	return ""
}

// ---------------- 应用 ----------------

type app struct {
	mw     *walk.MainWindow
	table  *walk.TableView
	model  *tableModel
	search *walk.LineEdit
	stats  *walk.Label
	status *walk.Label
	lang   *walk.ComboBox

	lblSearch, lblLang           *walk.Label
	btnAdd, btnDel               *walk.PushButton
	btnImpGboard, btnImpDat      *walk.PushButton
	btnImpBook                   *walk.PushButton
	btnExpGboard, btnExpDat      *walk.PushButton
	btnSave, btnOpen             *walk.PushButton
	pathLbl                      *walk.Label
	font                         *walk.Font
	tableFont                    *walk.Font
	inputFont                    *walk.Font
	tableHwnd                    win.HWND // 内部 SysListView32 句柄，用于行高与双缓冲

	entries  []core.Entry
	filtered []core.Entry
	langIdx  int
	bookPath string
}

// 关于「悬停高亮」：不再自己画。
// 实测（把 CellStyler 整段 return 掉后复查像素）comctl32 的报表列表控件本身就会把鼠标
// 所在行画成浅蓝热区，我们再用 CellStyler 刷一层浅黄只会被盖在下面，叠出来的颜色既不是
// 浅黄也不是系统的蓝（老截图里那种发绿的底色就是两层叠加的结果）。
// 所以这里删掉自绘逻辑和 120ms 轮询：悬停交给系统，避免「自绘 vs 原生」两层互相打架。

func (a *app) t(key string) string {
	p := texts[key]
	if a.langIdx == 1 {
		return p.en
	}
	return p.zh
}

func (a *app) tf(key string, args ...interface{}) string {
	return fmt.Sprintf(a.t(key), args...)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	a := &app{
		langIdx:  detectLang(),
		bookPath: filepath.Join(filepath.Dir(exe), "book.txt"),
	}
	// 调试用：GBOARD2MS_LANG=en|zh 可强制界面语言（不影响自动检测）。
	if v := os.Getenv("GBOARD2MS_LANG"); v == "en" {
		a.langIdx = 1
	} else if v == "zh" {
		a.langIdx = 0
	}
	a.loadBook()
	if err := a.build(); err != nil {
		fmt.Fprintln(os.Stderr, "创建窗口失败:", err)
		os.Exit(1)
	}
	a.refresh()
	// 调试用：GBOARD2MS_DEMO_DIALOG=1 启动后自动弹出「添加」对话框，便于截图核对排版。
	if os.Getenv("GBOARD2MS_DEMO_DIALOG") == "1" {
		go func() {
			time.Sleep(900 * time.Millisecond)
			a.mw.Synchronize(func() { a.onAdd() })
		}()
	}
	a.mw.Run()
}

// detectLang 按系统 UI 语言选择：中文 → zh，其余 → en。
func detectLang() int {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	p := k32.NewProc("GetUserDefaultUILanguage")
	if err := p.Find(); err != nil {
		return 1
	}
	id, _, _ := p.Call()
	if uint32(id)&0x3FF == 0x04 {
		return 0
	}
	return 1
}

func (a *app) build() error {
	err := d.MainWindow{
		AssignTo:      &a.mw,
		Title:         a.t("title"),
		Size:          d.Size{Width: 1060, Height: 680},
		MinSize:       d.Size{Width: 940, Height: 580},
		// 字体必须在创建前给窗口：walk 只在 Create 阶段把窗口字体下发给子控件，
		// 创建完再 SetFont 只会改窗口自己，按钮/标签仍是系统 8pt（且高度不会重算，字会被裁）。
		Font:          d.Font{Family: a.uiFontFamily(), PointSize: 12},
		Layout:        d.VBox{Margins: d.Margins{Left: 14, Top: 12, Right: 14, Bottom: 10}, Spacing: 10},
		OnSizeChanged: a.onResize,
		Children: []d.Widget{
			// 第一行：搜索 + 编辑/文件操作 + 语言选择（13pt 下按钮变宽，工具栏拆两行才不溢出）
			d.Composite{
				Layout: d.HBox{MarginsZero: true, Spacing: 8},
				Children: []d.Widget{
					d.Label{AssignTo: &a.lblSearch, Text: a.t("search")},
					d.Composite{
						Layout:   d.HBox{MarginsZero: true},
						MinSize:  d.Size{Width: 220, Height: 32},
						MaxSize:  d.Size{Width: 220, Height: 32},
						Children: []d.Widget{d.LineEdit{AssignTo: &a.search, OnTextChanged: a.onSearch}},
					},
					d.PushButton{AssignTo: &a.btnAdd, Text: a.t("add"), OnClicked: a.onAdd},
					d.PushButton{AssignTo: &a.btnDel, Text: a.t("del"), OnClicked: a.onDelete},
					d.PushButton{AssignTo: &a.btnSave, Text: a.t("save"), OnClicked: a.onSave},
					d.PushButton{AssignTo: &a.btnOpen, Text: a.t("open_folder"), OnClicked: a.onOpenFolder},
					d.HSpacer{},
					d.Label{AssignTo: &a.lblLang, Text: a.t("language")},
					d.ComboBox{AssignTo: &a.lang, Model: []string{"中文", "English"}, CurrentIndex: a.langIdx,
						MinSize: d.Size{Width: 100}, MaxSize: d.Size{Width: 100},
						OnCurrentIndexChanged: a.onLangChanged},
				},
			},
			// 第二行：导入导出
			d.Composite{
				Layout: d.HBox{MarginsZero: true, Spacing: 8},
				Children: []d.Widget{
					d.PushButton{AssignTo: &a.btnImpGboard, Text: a.t("imp_gboard"), OnClicked: a.onImportGboard},
					d.PushButton{AssignTo: &a.btnImpDat, Text: a.t("imp_dat"), OnClicked: a.onImportDat},
					d.PushButton{AssignTo: &a.btnImpBook, Text: a.t("imp_book"), OnClicked: a.onImportBook},
					d.PushButton{AssignTo: &a.btnExpGboard, Text: a.t("exp_gboard"), OnClicked: a.onExportGboard},
					d.PushButton{AssignTo: &a.btnExpDat, Text: a.t("exp_dat"), OnClicked: a.onExportDat},
					d.HSpacer{},
				},
			},
			d.Label{AssignTo: &a.stats},
			d.TableView{
				AssignTo:              &a.table,
				Columns:               a.columnSpec(),
				MinSize:               d.Size{Height: 260},
				// 关掉隔行灰底：全白底 + 系统原生浅蓝悬停，颜色只有一个，不会和当前行混淆。
				AlternatingRowBG:         false,
				MultiSelection:        true,
				NotSortableByHeaderClick: true,
				OnItemActivated:       a.onEdit,
				OnSizeChanged:         a.onResize,
				OnKeyDown:             a.onTableKeyDown,
			},
			d.Composite{
				Layout:  d.HBox{MarginsZero: true},
				MinSize: d.Size{Height: 28},
				Children: []d.Widget{
					d.Label{AssignTo: &a.status},
					d.HSpacer{},
					d.Label{AssignTo: &a.pathLbl},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}
	a.stats.SetTextColor(walk.RGB(0x55, 0x55, 0x55))
	a.pathLbl.SetTextColor(walk.RGB(0x88, 0x88, 0x88))
	a.pathLbl.SetText(filepath.Dir(a.bookPath))
	a.applyFont()
	// 行高：报表列表的行高由 LVSIL_SMALL 图像列表高度决定，列表控件初始化完成后再设，
	// 否则会崩。悬停高亮交给系统原生，不再自绘。
	go func() {
		time.Sleep(700 * time.Millisecond)
		_ = a.mw
		a.mw.Synchronize(func() {
			a.tableHwnd = findListView(a.table)
			if a.tableHwnd == 0 {
				return
			}
			setRowHeight(a.tableHwnd, 40)
			// 双缓冲：comctl32 先画到内存 DC 再一次性贴屏，鼠标滑动时不再闪白。
			win.SendMessage(a.tableHwnd, win.LVM_SETEXTENDEDLISTVIEWSTYLE,
				win.LVS_EX_DOUBLEBUFFER, win.LVS_EX_DOUBLEBUFFER)
		})
	}()
	a.onResize()
	return nil
}

// findListView 取「真正显示内容的那个」SysListView32。
//
// walk 的 TableView 是个复合控件，内部固定创建两个 SysListView32：
// 冻结列用 hwndFrozenLV（本项目没设冻结列，宽度为 0）和滚动列用 hwndNormalLV。
// EnumChildWindows 按 z 序枚举，第一个拿到的是宽度 0 的冻结列，对它做命中测试、
// 设置行高、局部重绘全部无效（客户区宽度 0 → 永远 -1）。所以按窗口宽度挑最宽的那个。
func findListView(tv *walk.TableView) win.HWND {
	if tv == nil {
		return 0
	}
	var best win.HWND
	bestW := int32(0)
	user32 := syscall.NewLazyDLL("user32.dll")
	enum := user32.NewProc("EnumChildWindows")
	getCls := user32.NewProc("GetClassNameW")
	getRect := user32.NewProc("GetWindowRect")
	cb := syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		var buf [64]uint16
		_, _, _ = getCls.Call(h, uintptr(unsafe.Pointer(&buf[0])), 64)
		if syscall.UTF16ToString(buf[:]) != "SysListView32" {
			return 1 // 继续找
		}
		var rc win.RECT
		_, _, _ = getRect.Call(h, uintptr(unsafe.Pointer(&rc)))
		if w := rc.Right - rc.Left; w > bestW {
			bestW = w
			best = win.HWND(h)
		}
		return 1
	})
	_, _, _ = enum.Call(uintptr(tv.Handle()), cb, 0)
	return best
}

// setRowHeight 用「小图标列表」撑行高：SysListView32 的行高由字体/图像列表高度决定，
// 直接发 LVM_SETITEMHEIGHT 会让 comctl32 崩掉，所以挂一个 1×px 的空图像列表。
// setRowHeight 用「小图标列表」撑行高。
//
// 报表模式（report view）下每行高度由 LVSIL_SMALL 那个小图标列表的图像高度决定，
// 所以这里必须是 LVSIL_SMALL=1，不是 LVSIL_NORMAL=0（写成 0 时高度完全不生效，
// 行高一直停在字体撑出来的 27px，这就是「行距太紧」的真正原因）。
// 直接发 LVM_SETITEMHEIGHT 会让 comctl32 崩掉，所以走图像列表这条路。
func setRowHeight(h win.HWND, px int) {
	if h == 0 {
		return
	}
	comctl := syscall.NewLazyDLL("comctl32.dll")
	imgCreate := comctl.NewProc("ImageList_Create")
	himl, _, _ := imgCreate.Call(1, uintptr(px), 0x20 /* ILC_COLOR32 */, 1, 1)
	if himl == 0 {
		return
	}
	win.SendMessage(h, win.LVM_SETIMAGELIST, 1 /* LVSIL_SMALL */, himl)
}

// invalidateRow 只让某一行的矩形重绘（不擦背景）。目前没有自绘逻辑用到它，保留给后续需要。
func (a *app) invalidateRow(row int) {
	if a.tableHwnd == 0 || row < 0 {
		return
	}
	var rc win.RECT
	rc.Left = 0 // LVIR_BOUNDS
	if win.SendMessage(a.tableHwnd, win.LVM_GETITEMRECT,
		uintptr(row), uintptr(unsafe.Pointer(&rc))) == 0 {
		return
	}
	win.InvalidateRect(a.tableHwnd, &rc, false)
}

// columnSpec 列宽：短语列由 onResize 按窗口宽度动态分配，其余固定。
func (a *app) columnSpec() []d.TableViewColumn {
	return []d.TableViewColumn{
		{Title: a.t("col_phrase"), Width: 520},
		{Title: a.t("col_code"), Width: 190},
		{Title: a.t("col_pos"), Width: 110, Alignment: d.AlignCenter},
	}
}

// ---------------- 字体与布局 ----------------

// uiFontFamily 界面字体族：中文雅黑，英文 Segoe UI。
func (a *app) uiFontFamily() string {
	if a.langIdx == 1 {
		return "Segoe UI"
	}
	return "Microsoft YaHei UI"
}

// applyFont 处理需要在创建之后再放大的控件（输入框、表格）。
// 常规控件（标签 / 按钮 / 状态栏）由 MainWindow 创建时的 Font 下发，不能在这里改。
func (a *app) applyFont() {
	family, size := a.uiFontFamily(), 12
	if a.langIdx == 1 {
		family, size = "Segoe UI", 12
	}
	f, err := walk.NewFont(family, size, 0)
	if err != nil {
		return
	}
	a.font = f

	// 输入类控件（搜索、短语、编码、候选位置）用更大字号 + 固定高度，
	// 否则原生 EDIT 会保持旧高度，12pt 的字直接被裁一截。
	if inf, err := walk.NewFont(family, size+2, 0); err == nil {
		a.inputFont = inf
		if a.search != nil {
			a.search.SetFont(inf)
		}
	}

	// 表格字号单独放大：walk 的原生列表行高由字体高度决定，
	// 用更大的字号换更高的行距（CustomRowHeight 那个字段不生效）。
	if a.table != nil {
		if tf, err := walk.NewFont(family, size+1, 0); err == nil {
			a.tableFont = tf
			a.table.SetFont(tf)
		}
	}
}

// onResize 让短语列吃掉剩余宽度，避免右侧留白或把窄列拉得离谱。
func (a *app) onResize() {
	if a.table == nil {
		return
	}
	cols := a.table.Columns()
	if cols == nil || cols.Len() < 3 {
		return
	}
	w := a.table.Width()
	if w <= 0 {
		return
	}
	first := w - 190 - 110 - 35 // 减去编码列、候选位置列、竖向滚动条
	if first < 260 {
		first = 260
	}
	_ = cols.At(0).SetWidth(first)
}

func (a *app) onTableKeyDown(key walk.Key) {
	if key == walk.KeyDelete {
		a.onDelete()
	}
}

// ---------------- 数据 ----------------

func (a *app) loadBook() {
	entries, err := core.LoadBook(a.bookPath)
	if err != nil {
		if !os.IsNotExist(err) {
			walk.MsgBox(nil, a.t("error"), a.tf("load_fail", err), walk.MsgBoxIconError)
		}
	}
	a.entries = entries
}

func (a *app) refresh() {
	raw := a.search.Text()
	kw := strings.ToLower(strings.TrimSpace(raw))
	a.filtered = a.filtered[:0]
	for _, e := range a.entries {
		if kw == "" || strings.Contains(strings.ToLower(e.Phrase), kw) || strings.Contains(strings.ToLower(e.Code), kw) {
			a.filtered = append(a.filtered, e)
		}
	}
	if a.model == nil {
		a.model = &tableModel{}
	}
	a.model.rows = a.filtered
	a.table.SetModel(a.model)
	a.model.PublishRowsReset()

	codes := map[string]int{}
	for _, e := range a.entries {
		codes[e.Code]++
	}
	dup := 0
	for _, n := range codes {
		if n > 1 {
			dup++
		}
	}
	a.stats.SetText(a.tf("stats", len(a.entries), len(codes), dup))
	a.status.SetText(a.tf("status", len(a.filtered), len(a.entries)))
}

func (a *app) onSearch() { a.refresh() }

func (a *app) currentIndex() int {
	if a.table == nil {
		return -1
	}
	return a.table.CurrentIndex()
}

// ---------------- 增删改 ----------------

// labelBox 把标签包进固定宽容器：Label 自身不认 MaxSize，放 HBox 里会被拉伸，导致各行左边缘错位。
func labelBox(s string) d.Widget {
	return d.Composite{
		Layout:  d.HBox{MarginsZero: true},
		MinSize: d.Size{Width: 80},
		MaxSize: d.Size{Width: 80},
		Children: []d.Widget{
			d.Label{Text: s, Alignment: d.AlignHNearVCenter},
		},
	}
}

func (a *app) entryDialog(title string, phrase, code string, pos int) (core.Entry, bool) {
	var dlg *walk.Dialog
	var phraseEdit, codeEdit *walk.LineEdit
	var posEdit *walk.NumberEdit
	var okBtn, cancelBtn *walk.PushButton
	ok := false

	err := d.Dialog{
		AssignTo: &dlg,
		Title:    title,
		Size:     d.Size{Width: 520, Height: 330},
		MinSize:  d.Size{Width: 520, Height: 330},
		// 对话框是独立窗口，同样要在创建前给字体（创建后再设只会改窗口自身）。
		Font:     d.Font{Family: a.uiFontFamily(), PointSize: 12},
		Layout:   d.VBox{Margins: d.Margins{Left: 20, Top: 18, Right: 20, Bottom: 16}, Spacing: 12},
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		Children: []d.Widget{
			// 表单用 HBox 一行一组：Grid 会无视 MaxSize 把控件拉满，HBox 才收得住。
			d.Composite{
				Layout:  d.VBox{Spacing: 10},
				MinSize: d.Size{Width: 460},
				Children: []d.Widget{
					d.Composite{Layout: d.HBox{Spacing: 12}, Children: []d.Widget{
						labelBox(a.t("lbl_phrase")),
						d.LineEdit{AssignTo: &phraseEdit, Text: phrase, MinSize: d.Size{Width: 300, Height: 32}},
					}},
					d.Composite{Layout: d.HBox{Spacing: 12}, Children: []d.Widget{
						labelBox(a.t("lbl_code")),
						d.LineEdit{AssignTo: &codeEdit, Text: code, MinSize: d.Size{Width: 300, Height: 32}},
					}},
					d.Composite{Layout: d.HBox{Spacing: 12}, Children: []d.Widget{
						labelBox(a.t("lbl_pos")),
						d.Composite{
							Layout:  d.HBox{MarginsZero: true},
							MinSize: d.Size{Width: 88, Height: 32},
							MaxSize: d.Size{Width: 88},
							Children: []d.Widget{
								d.NumberEdit{AssignTo: &posEdit, Value: float64(pos), MinValue: 1, MaxValue: 9, Decimals: 0},
							},
						},
						d.HSpacer{},
					}},
				},
			},
			d.Composite{Layout: d.HBox{Spacing: 10}, Children: []d.Widget{
				d.HSpacer{},
				d.PushButton{AssignTo: &okBtn, Text: a.t("ok"), MinSize: d.Size{Width: 100, Height: 34},
					OnClicked: func() { ok = true; dlg.Accept() }},
				d.PushButton{AssignTo: &cancelBtn, Text: a.t("cancel"), MinSize: d.Size{Width: 100, Height: 34},
					OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(a.mw)
	if err != nil {
		return core.Entry{}, false
	}
	// 输入框单独放大（14pt + 固定 32px 高），常规控件已由对话框创建时的 Font 下发。
	if a.inputFont != nil {
		phraseEdit.SetFont(a.inputFont)
		codeEdit.SetFont(a.inputFont)
		posEdit.SetFont(a.inputFont)
	}
	dlg.Run()
	if !ok {
		return core.Entry{}, false
	}
	p := int(posEdit.Value())
	if p < 1 {
		p = 1
	}
	c := codeEdit.Text()
	ph := phraseEdit.Text()
	return core.Entry{Code: strings.TrimSpace(c), Phrase: strings.TrimSpace(ph), Pos: p}, true
}

func (a *app) onAdd() {
	e, ok := a.entryDialog(a.t("dlg_add"), "", "", 1)
	if !ok {
		return
	}
	if e.Code == "" || e.Phrase == "" {
		walk.MsgBox(a.mw, a.t("error"), a.t("err_empty"), walk.MsgBoxIconError)
		return
	}
	for _, x := range a.entries {
		if x.Code == e.Code && x.Phrase == e.Phrase {
			walk.MsgBox(a.mw, a.t("error"), a.tf("err_dup", e.Code, e.Phrase), walk.MsgBoxIconError)
			return
		}
	}
	a.entries = append(a.entries, e)
	a.refresh()
}

func (a *app) onEdit() {
	idx := a.currentIndex()
	if idx < 0 || idx >= len(a.filtered) {
		return
	}
	old := a.filtered[idx]
	e, ok := a.entryDialog(a.t("dlg_edit"), old.Phrase, old.Code, old.Pos)
	if !ok {
		return
	}
	if e.Code == "" || e.Phrase == "" {
		walk.MsgBox(a.mw, a.t("error"), a.t("err_empty"), walk.MsgBoxIconError)
		return
	}
	for i, x := range a.entries {
		if x.Code == old.Code && x.Phrase == old.Phrase && x.Pos == old.Pos {
			a.entries[i] = e
			break
		}
	}
	a.refresh()
}

func (a *app) onDelete() {
	idxs := a.table.SelectedIndexes()
	if len(idxs) == 0 {
		if i := a.currentIndex(); i >= 0 {
			idxs = []int{i}
		}
	}
	if len(idxs) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("warn_sel"), walk.MsgBoxIconWarning)
		return
	}
	msg := a.t("confirm_del")
	if len(idxs) > 1 {
		msg = a.tf("confirm_del_n", len(idxs))
	}
	if walk.DlgCmdNo == walk.MsgBox(a.mw, a.t("del"), msg, walk.MsgBoxYesNo) {
		return
	}
	// 用「编码+短语+位置」做键，避免按下标删时因顺序变动误删。
	type key struct {
		code   string
		phrase string
		pos    int
	}
	victims := map[key]bool{}
	for _, i := range idxs {
		if i >= 0 && i < len(a.filtered) {
			e := a.filtered[i]
			victims[key{e.Code, e.Phrase, e.Pos}] = true
		}
	}
	kept := a.entries[:0]
	for _, x := range a.entries {
		if victims[key{x.Code, x.Phrase, x.Pos}] {
			continue
		}
		kept = append(kept, x)
	}
	a.entries = kept
	a.refresh()
}

// ---------------- 导入导出 ----------------

func (a *app) pickOpen(title, filter string) string {
	fd := walk.FileDialog{Title: title, Filter: filter}
	if ok, err := fd.ShowOpen(a.mw); err != nil || !ok {
		return ""
	}
	return fd.FilePath
}

func (a *app) pickSave(title, filter, defName string) string {
	fd := walk.FileDialog{Title: title, Filter: filter, FilePath: defName}
	if ok, err := fd.ShowSave(a.mw); err != nil || !ok {
		return ""
	}
	return fd.FilePath
}

func (a *app) onImportGboard() {
	path := a.pickOpen(a.t("dialog_open"), a.t("ft_zip")+"|*.zip|"+a.t("ft_all")+"|*.*")
	if path == "" {
		return
	}
	res, err := core.ImportZip(path)
	if err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
		return
	}
	if len(res.Entries) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("imp_none"), walk.MsgBoxIconWarning)
		return
	}
	existing := map[string]bool{}
	for _, e := range a.entries {
		existing[e.Code+"\x00"+e.Phrase] = true
	}
	added, dup := 0, 0
	for _, e := range res.Entries {
		key := e.Code + "\x00" + e.Phrase
		if existing[key] {
			dup++
			continue
		}
		existing[key] = true
		pos := e.Pos
		if pos < 1 || pos > 9 {
			pos = 1
		}
		a.entries = append(a.entries, core.Entry{Code: e.Code, Phrase: e.Phrase, Pos: pos})
		added++
	}
	a.refresh()

	detail := fmt.Sprintf("zh-CN %d / zz %d", res.PerLang["zh-CN"], res.PerLang["zz"])
	msg := a.tf("imp_gboard_done", res.Files, added, detail, dup)
	if added == 0 {
		msg = a.tf("imp_all_dup", dup)
	}
	if len(res.HeaderWarn) > 0 {
		msg += "\n\n" + a.tf("imp_header_warn", strings.Join(res.HeaderWarn, ", "))
	}
	if len(res.Warnings) > 0 {
		msg += "\n\n" + strings.Join(res.Warnings, "\n")
	}
	walk.MsgBox(a.mw, a.t("hint"), msg, walk.MsgBoxIconInformation)
}

func (a *app) onImportDat() {
	path := a.pickOpen(a.t("dialog_open"), a.t("ft_dat")+"|*.dat|"+a.t("ft_all")+"|*.*")
	if path == "" {
		return
	}
	rows, err := core.ParseDat(path)
	if err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
		return
	}
	if len(rows) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("imp_none"), walk.MsgBoxIconWarning)
		return
	}
	existing := map[string]bool{}
	for _, e := range a.entries {
		existing[e.Code+"\x00"+e.Phrase] = true
	}
	added := 0
	for _, e := range rows {
		key := e.Code + "\x00" + e.Phrase
		if existing[key] {
			continue
		}
		existing[key] = true
		a.entries = append(a.entries, e)
		added++
	}
	a.refresh()
	walk.MsgBox(a.mw, a.t("hint"), a.tf("imp_dat_done", added), walk.MsgBoxIconInformation)
}

// onImportBook 导入另一个 book.txt（或任意「编码\t短语\t候选位置」三列 txt），按
// 「编码 + 短语」去重合并进当前词库；不做整库替换，避免误覆盖现有数据。
func (a *app) onImportBook() {
	path := a.pickOpen(a.t("dialog_open"), a.t("ft_book")+"|*.txt|"+a.t("ft_all")+"|*.*")
	if path == "" {
		return
	}
	rows, err := core.LoadBook(path)
	if err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
		return
	}
	if len(rows) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("imp_none"), walk.MsgBoxIconWarning)
		return
	}
	existing := map[string]bool{}
	for _, e := range a.entries {
		existing[e.Code+"\x00"+e.Phrase] = true
	}
	added, dup := 0, 0
	for _, e := range rows {
		key := e.Code + "\x00" + e.Phrase
		if existing[key] {
			dup++
			continue
		}
		existing[key] = true
		pos := e.Pos
		if pos < 1 || pos > 9 {
			pos = 1
		}
		a.entries = append(a.entries, core.Entry{Code: e.Code, Phrase: e.Phrase, Pos: pos})
		added++
	}
	a.refresh()

	msg := a.tf("imp_book_done", added, dup)
	if added == 0 {
		msg = a.tf("imp_all_dup", dup)
	}
	walk.MsgBox(a.mw, a.t("hint"), msg, walk.MsgBoxIconInformation)
}

func (a *app) onExportGboard() {
	if len(a.entries) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("imp_none"), walk.MsgBoxIconWarning)
		return
	}
	def := fmt.Sprintf("Gboard_Dictionary_%s.zip", time.Now().Format("20060102_150405"))
	path := a.pickSave(a.t("dialog_save"), a.t("ft_zip")+"|*.zip", def)
	if path == "" {
		return
	}
	per, err := core.ExportZip(a.entries, path)
	if err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
		return
	}
	detail := fmt.Sprintf("zh-CN %d / zz %d", per["zh-CN"], per["zz"])
	walk.MsgBox(a.mw, a.t("hint"), a.tf("exp_gboard_done", len(a.entries), path, detail), walk.MsgBoxIconInformation)
}

func (a *app) onExportDat() {
	if len(a.entries) == 0 {
		walk.MsgBox(a.mw, a.t("hint"), a.t("imp_none"), walk.MsgBoxIconWarning)
		return
	}
	def := fmt.Sprintf("ms_export_%s.dat", time.Now().Format("20060102_150405"))
	path := a.pickSave(a.t("dialog_save"), a.t("ft_dat")+"|*.dat", def)
	if path == "" {
		return
	}
	n, err := core.ExportDat(a.entries, path, 0)
	if err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(a.mw, a.t("hint"), a.tf("exp_dat_done", n, path), walk.MsgBoxIconInformation)
}

func (a *app) onSave() {
	if b, err := os.ReadFile(a.bookPath); err == nil {
		_ = os.WriteFile(a.bookPath+".bak", b, 0o644)
	}
	if err := core.SaveBook(a.bookPath, a.entries); err != nil {
		walk.MsgBox(a.mw, a.t("error"), a.tf("save_fail", err), walk.MsgBoxIconError)
		return
	}
	a.status.SetText(a.tf("save_done", len(a.entries)))
}

func (a *app) onOpenFolder() {
	dir := filepath.Dir(a.bookPath)
	if err := exec.Command("explorer", dir).Start(); err != nil {
		walk.MsgBox(a.mw, a.t("error"), err.Error(), walk.MsgBoxIconError)
	}
}

// ---------------- 语言切换 ----------------

func (a *app) onLangChanged() {
	if a.lang == nil {
		return
	}
	idx := a.lang.CurrentIndex()
	if idx == a.langIdx {
		return
	}
	a.langIdx = idx
	a.applyLanguage()
}

func (a *app) applyLanguage() {
	a.mw.SetTitle(a.t("title"))
	a.lblSearch.SetText(a.t("search"))
	a.lblLang.SetText(a.t("language"))
	a.btnAdd.SetText(a.t("add"))
	a.btnDel.SetText(a.t("del"))
	a.btnImpGboard.SetText(a.t("imp_gboard"))
	a.btnImpDat.SetText(a.t("imp_dat"))
	a.btnExpGboard.SetText(a.t("exp_gboard"))
	a.btnExpDat.SetText(a.t("exp_dat"))
	a.btnImpBook.SetText(a.t("imp_book"))
	a.btnSave.SetText(a.t("save"))
	a.btnOpen.SetText(a.t("open_folder"))

	a.applyFont()

	cols := a.table.Columns()
	if cols != nil && cols.Len() >= 3 {
		_ = cols.At(0).SetTitle(a.t("col_phrase"))
		_ = cols.At(1).SetTitle(a.t("col_code"))
		_ = cols.At(2).SetTitle(a.t("col_pos"))
	}
	a.pathLbl.SetText(filepath.Dir(a.bookPath))
	a.refresh()
}
