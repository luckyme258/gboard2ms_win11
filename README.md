# gboard2ms_win11 — 微软拼音词库管理器（Go 版）

用 Go + `lxn/walk` 重写的「微软拼音词库管理器」：单个 exe（约 6.7 MB），不依赖 Python 环境，Windows 10/11 直接运行。

> Python 旧版（v1.0 及以前）见 [luckyme258/gboard2ms_win11_python](https://github.com/luckyme258/gboard2ms_win11_python)。
> 本仓库为主线；格式与 Python 版逐字节兼容（zip 双字典 / DAT 可互相导入导出）。

- 选型与结论：`技术选型分析.md`
- 状态：格式层与 Python 版**逐字节一致**；GUI 原型（`lxn/walk`）已可运行
- 数据：`book.txt` 为测试副本（32 条，取自旧仓库 `缓存\试跑\book.txt`），只在本目录使用

## 目录结构

```
gboard2ms_go\
├─ go.mod
├─ internal\core\        # 格式层（唯一实现，GUI 与 CLI 共用）
│   ├─ core.go           # Entry / LoadBook / SaveBook / ExportZip / ImportZip / ExportDat / ParseDat / LangTag
│   └─ errors.go
├─ cmd\walkui\           # GUI 原型
│   ├─ main.go           # 界面 + 事件 + 中英文案表
│   ├─ app.manifest      # comctl32 v6 + PerMonitorV2（视觉样式 / 高 DPI）
│   └─ rsrc.syso         # 由 app.manifest 生成，构建时自动嵌入
├─ spike\main.go         # 格式层 CLI（复用 internal/core）
├─ walkui.exe            # GUI 产物（6.68 MB，单个 exe，无 cgo、无外部依赖）
├─ spike.exe             # CLI 产物
├─ book.txt              # 测试词库（32 条）
└─ out\                  # 试跑输出
```

## 环境

Go 未装到系统 PATH，用便携版：

```powershell
$env:GOROOT  = 'E:\codes\_toolchains\go'
$env:GOPATH  = 'E:\codes\_toolchains\gopath'
$env:GOCACHE = 'E:\codes\_toolchains\gocache'
$env:GOPROXY = 'https://goproxy.cn,direct'      # go.dev 直连不通，走国内镜像
& "$env:GOROOT\bin\go.exe" version               # go1.27.4 windows/amd64
```

## 构建

```powershell
Set-Location 'E:\codes\gboard2ms_go'

# GUI：先生成 manifest 资源，再带 windowsgui 标志构建
Set-Location cmd\walkui
& "$env:GOPATH\bin\rsrc.exe" -manifest app.manifest -o rsrc.syso
Set-Location ..\..
& "$env:GOROOT\bin\go.exe" build -ldflags "-s -w -H=windowsgui" -o walkui.exe ./cmd\walkui

# CLI
& "$env:GOROOT\bin\go.exe" build -o spike.exe ./spike
```

改了 `app.manifest` 才需要重跑 `rsrc`；平时改代码直接 `go build` 即可。

**发布构建统一走 `build.cmd`（已固化）**：脚本内置工具链路径与发布参数，双击或 `build.cmd` 即出 6.7 MB 版本，`build.cmd debug` 出带调试信息的 10.4 MB 版本。

**发布构建必须带 `-s -w`**：默认构建会嵌入 DWARF 调试信息与符号表（约 3.3 MB，占三分之一），发布版不需要。

| 构建方式 | 体积 |
| --- | --- |
| `go build -ldflags "-H=windowsgui"`（带调试信息） | 10.43 MB |
| `go build -ldflags "-s -w -H=windowsgui"`（发布） | **6.68 MB** |

`-s` 去符号表、`-w` 去 DWARF，机器码与 `pclntab` 都保留，所以 panic 仍能打印函数名与行号，只是不能挂调试器。

## 运行

```powershell
.\walkui.exe        # 读同目录 book.txt，不存在则空表
```

界面语言：启动时读系统 UI 语言（中文系统→中文，其余→英文），右上角下拉框可随时切换。
调试时可用 `GBOARD2MS_LANG=en|zh` 强制指定。

已实现：表格（短语/编码/候选位置）、搜索过滤、统计（总数/唯一编码/重复编码）、添加、编辑（双击行）、多选删除（`Ctrl`/`Shift` 选多行 + 按钮或 `Delete` 键）、导入 Gboard zip、导入 DAT、导出 Gboard zip、导出 DAT、保存（自动 `.bak` 备份）、打开文件夹、中英切换。

界面细节：字号几何完全对齐 Python 版 `ms_manager.py`（Tk ttk 的 `font=("微软雅黑", 10/11)` + `rowheight=30`）：中文「Microsoft YaHei UI」、英文「Segoe UI」，控件 12pt、输入框 14pt、表格 13pt、行高固定 40px，窗口 1060×680，对话框 520×330。隔行底色、选中行蓝色高亮；短语列宽度随窗口自适应，编码列 190、候选位置列 110 居中；工具栏两行（第一行搜索 + 增删 + 保存/打开文件夹 + 语言，第二行导入导出），语言选择固定在右上角；底部左侧「显示 x / y 条」、右侧灰色显示 `book.txt` 目录。

窗口默认 1060×680（1366×768 下不会被任务栏挡住），最小 940×580。

## 界面规范（字体 / 间距）

| 层级 | 规格 |
|---|---|
| 窗口标题栏 | 系统绘制，不参与自定义（`NonClient`，无法改字号） |
| 工具栏、标签、状态栏 | Microsoft YaHei UI **12pt**（英文 Segoe UI 12pt） |
| 表格行与表头 | 同族字体 **13pt**；行高强制 **40px**（空图像列表撑高）；鼠标所在行刷浅黄 `#FFF9C4` |
| 次级信息（统计行、路径） | 与正文同字号，靠**灰色**（`#555` / `#888`）降权 |
| 输入框（搜索 / 短语 / 编码 / 候选位置） | 同族字体 **14pt**，高度 32px（原生 EDIT 不会随字体长高，必须显式给高度，否则字被裁） |\n| 对话框 | 520×330，标签列固定 80，输入框 300×32，数字框 88×32，按钮 100×34 |
| 窗口内距 | 14/12/14/10，区块间距 10；工具栏按钮间距 8 |

字号依据：Python 版实测为控件 10pt / 表格 11pt / `rowheight=30`；Go 版按用户要求整体上一档到 12pt / 13pt / 行高 34（`ms_manager` 只作为基线，不做逐像素复刻）。

已知限制：`walk.MsgBox` 是系统 MessageBox，字体由系统决定、改不了；表头暂不支持单独加粗（walk 的 header 与行共用同一字体）。行高不能用 walk 的 `CustomRowHeight`（设了会让 `applyFont` 提前 return、字体失效），也不能发 `LVM_SETITEMHEIGHT`（会让 comctl32 直接崩），只能用「1×30 空图像列表挂到 LVSIL_SMALL」这个办法。

未实现：表头点击排序（已关闭，避免与词库顺序冲突）、拖拽排序、全局 `Ctrl+A` 全选。

## 格式层自检

```powershell
.\spike.exe export-zip  book.txt out\go2.zip
.\spike.exe export-dat  book.txt out\go2.dat 1790000000   # 固定时间戳便于比对
.\spike.exe roundtrip   book.txt out\rt
```

最近一次结果：

```
导出 zip: zh-CN 17 / zz 15
导出 dat: 32 条 / 1600 B
roundtrip 内容集合一致: True
zip 内层逐字节一致（vs Python）: True
DAT 1600 B 逐字节一致（vs Python）: True
```

## 注意

- `zip` 不能比原始字节：Go 每次写 zip 的 `Modified` 秒级不同，只比**解压后内容**。
- `book.txt` 统一 CRLF；短语按 `[]rune` 切前 2 字符（否则中文/emoji 会被切坏）。
- `Tableview` 的 `CustomRowHeight` / `CustomHeaderHeight` 在本版本 walk 里**不起作用**：一旦设了，`applyFont` 会直接 return，连字体都不生效，行高仍是默认。要行距就调表格字号。
- `TableView` 列必须用 `walk.NewTableViewColumn()` + `SetTitle/SetWidth` 构造，不能结构字面量。
- `SetTextColor`、`SetFont` 都不返回值，写成 `_ =` 会编译失败。
- 表格列序：**短语、编码、候选位置**，与 Python 版一致（内部 `Entry{Code, Phrase, Pos}`）。
