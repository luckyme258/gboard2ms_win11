#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
微软拼音词库管理器
内部格式: book.txt (短语\t编码\t候选位置)
支持导入: Gboard TXT、微软拼音 DAT
支持导出: Gboard TXT、微软拼音 DAT
"""

import os
import re
import sys
import struct
import platform
import subprocess
import shutil
import tkinter as tk
from tkinter import ttk, messagebox, filedialog
from collections import defaultdict
from datetime import datetime
from pathlib import Path


def get_base_path():
    """获取可执行文件所在目录（兼容 Python 脚本和打包后的 exe）"""
    if getattr(sys, 'frozen', False):
        return Path(sys.executable).parent
    else:
        return Path(__file__).parent


# 获取脚本所在目录
SCRIPT_DIR = get_base_path()
BOOK_FILE = SCRIPT_DIR / "book.txt"


class MsPhraseManager:
    def __init__(self, root):
        self.root = root
        self.root.title("微软拼音词库管理器")
        self.root.geometry("1000x700")
        
        # 内部数据存储 (phrase, code, position)
        self.phrases = []
        self.filtered = []
        self.search_var = tk.StringVar()
        
        self.setup_ui()
        self.load_book()
    
    def setup_ui(self):
        # 工具栏
        toolbar = ttk.Frame(self.root)
        toolbar.pack(fill=tk.X, padx=5, pady=5)
        
        ttk.Label(toolbar, text="搜索:").pack(side=tk.LEFT, padx=5)
        search_entry = ttk.Entry(toolbar, textvariable=self.search_var, width=30)
        search_entry.pack(side=tk.LEFT, padx=5)
        search_entry.bind('<KeyRelease>', self.on_search)
        
        ttk.Button(toolbar, text="添加", command=self.add_phrase).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="删除", command=self.delete_selected).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="导入 Gboard", command=self.import_gboard).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="导入 DAT", command=self.import_ms_dat).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="导出 Gboard", command=self.export_gboard).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="导出 DAT", command=self.export_dat).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="保存", command=self.save_book).pack(side=tk.LEFT, padx=5)
        ttk.Button(toolbar, text="打开文件夹", command=self.open_folder).pack(side=tk.LEFT, padx=5)
        
        # 显示统计
        self.stats_label = ttk.Label(self.root, text="", relief=tk.SUNKEN, anchor=tk.W)
        self.stats_label.pack(fill=tk.X, side=tk.TOP, padx=5, pady=2)
        
        # 表格
        frame = ttk.Frame(self.root)
        frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        
        columns = ("短语", "编码", "候选位置")
        self.tree = ttk.Treeview(frame, columns=columns, show="headings", selectmode="extended")
        self.tree.heading("短语", text="短语")
        self.tree.heading("编码", text="编码")
        self.tree.heading("候选位置", text="候选位置")
        
        # 增大列宽以适应更大的字体
        self.tree.column("短语", width=500)
        self.tree.column("编码", width=200)
        self.tree.column("候选位置", width=100)
        
        scrollbar = ttk.Scrollbar(frame, orient=tk.VERTICAL, command=self.tree.yview)
        self.tree.configure(yscrollcommand=scrollbar.set)
        self.tree.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        scrollbar.pack(side=tk.RIGHT, fill=tk.Y)
        
        # 设置字体（全局样式）
        style = ttk.Style()
        style.configure("Treeview", font=("微软雅黑", 11), rowheight=30)
        style.configure("Treeview.Heading", font=("微软雅黑", 11, "bold"))
        style.configure("TLabel", font=("微软雅黑", 10))
        style.configure("TButton", font=("微软雅黑", 10))
        style.configure("TEntry", font=("微软雅黑", 10))
        
        self.tree.bind("<Double-1>", self.edit_phrase)
        # 绑定 Delete 键删除
        self.tree.bind("<Delete>", lambda e: self.delete_selected())
        # 绑定 Ctrl+A 全选
        self.tree.bind("<Control-a>", self.select_all)
        self.tree.bind("<Control-A>", self.select_all)
        
        self.context_menu = tk.Menu(self.root, tearoff=0, font=("微软雅黑", 10))
        self.context_menu.add_command(label="编辑", command=self.edit_phrase)
        self.context_menu.add_command(label="删除", command=self.delete_selected)
        self.tree.bind("<Button-3>", self.show_context_menu)
        
        # 底部状态栏
        self.status = ttk.Label(self.root, text="就绪", relief=tk.SUNKEN, anchor=tk.W)
        self.status.pack(fill=tk.X, side=tk.BOTTOM, padx=5, pady=2)
    
    # ========== 全选功能 ==========
    
    def select_all(self, event=None):
        """Ctrl+A 全选"""
        for item in self.tree.get_children():
            self.tree.selection_add(item)
        return "break"
    
    # ========== 批量删除 ==========
    
    def delete_selected(self):
        """删除选中的词条（支持多选）"""
        selected = self.tree.selection()
        if not selected:
            messagebox.showwarning("提示", "请先选中要删除的词条")
            return
        
        if messagebox.askyesno("确认删除", f"确定要删除选中的 {len(selected)} 条词条吗？"):
            # 获取所有选中的值
            to_delete = []
            for item in selected:
                values = self.tree.item(item, "values")
                if values and len(values) >= 3:
                    to_delete.append((values[0], values[1], int(values[2])))
            
            # 从数据中删除
            for phrase, code, pos in to_delete:
                for i, (p, c, position) in enumerate(self.phrases):
                    if p == phrase and c == code and position == pos:
                        del self.phrases[i]
                        break
            
            self.refresh_display()
            self.update_stats()
    
    # ========== 内部格式 (book.txt) 操作 ==========
    
    def load_book(self):
        """加载 book.txt 到内存"""
        self.phrases = []
        if not BOOK_FILE.exists():
            self.status.config(text=f"book.txt 不存在，将创建新文件")
            self.update_stats()
            self.refresh_display()
            return
        
        try:
            with open(BOOK_FILE, 'r', encoding='utf-8') as f:
                for line in f:
                    line = line.rstrip('\n')
                    if not line or line.startswith('#'):
                        continue
                    parts = line.split('\t')
                    if len(parts) >= 2:
                        phrase = parts[0].strip()
                        code = parts[1].strip()
                        position = 1
                        if len(parts) >= 3 and parts[2].strip().isdigit():
                            position = int(parts[2].strip())
                        self.phrases.append((phrase, code, position))
            self.status.config(text=f"已加载 {len(self.phrases)} 条短语")
        except Exception as e:
            messagebox.showerror("加载失败", f"无法读取 book.txt：{e}")
        
        self.update_stats()
        self.refresh_display()
    
    def save_book(self):
        """保存到 book.txt"""
        try:
            if BOOK_FILE.exists():
                backup = BOOK_FILE.with_suffix(".txt.bak")
                shutil.copy(BOOK_FILE, backup)
            
            with open(BOOK_FILE, 'w', encoding='utf-8') as f:
                f.write("# 微软拼音词库 - 统一格式\n")
                f.write(f"# 更新时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n")
                f.write(f"# 总计: {len(self.phrases)} 条\n")
                f.write("# 格式: 短语\t编码\t候选位置\n")
                f.write("#" + "-" * 50 + "\n")
                for phrase, code, position in self.phrases:
                    f.write(f"{phrase}\t{code}\t{position}\n")
            
            self.status.config(text=f"已保存到 book.txt ({len(self.phrases)} 条)")
            messagebox.showinfo("保存成功", f"已保存 {len(self.phrases)} 条短语到 book.txt")
        except Exception as e:
            messagebox.showerror("保存失败", f"无法保存文件：{str(e)}")
    
    def update_stats(self):
        """更新统计信息"""
        code_count = defaultdict(int)
        for _, code, _ in self.phrases:
            code_count[code] += 1
        multi_codes = {k: v for k, v in code_count.items() if v > 1}
        
        stats = f"总计: {len(self.phrases)} 条 | 唯一编码: {len(code_count)} 个 | 重复编码: {len(multi_codes)} 个"
        self.stats_label.config(text=stats)
    
    # ========== 表格显示 ==========
    
    def refresh_display(self):
        search_text = self.search_var.get().strip().lower()
        if search_text:
            self.filtered = [p for p in self.phrases 
                           if search_text in p[0].lower() or search_text in p[1].lower()]
        else:
            self.filtered = self.phrases.copy()
        self.update_tree()
        self.status.config(text=f"显示 {len(self.filtered)} / {len(self.phrases)} 条")
    
    def update_tree(self):
        for row in self.tree.get_children():
            self.tree.delete(row)
        for phrase, code, position in self.filtered:
            self.tree.insert("", tk.END, values=(phrase, code, position))
    
    def on_search(self, event=None):
        self.refresh_display()
    
    # ========== 增删改查 ==========
    
    def add_phrase(self):
        dialog = tk.Toplevel(self.root)
        dialog.title("添加短语")
        dialog.geometry("450x280")
        dialog.transient(self.root)
        dialog.grab_set()
        
        ttk.Label(dialog, text="短语:").grid(row=0, column=0, padx=5, pady=5, sticky=tk.W)
        text_entry = ttk.Entry(dialog, width=45)
        text_entry.grid(row=0, column=1, padx=5, pady=5)
        
        ttk.Label(dialog, text="编码:").grid(row=1, column=0, padx=5, pady=5, sticky=tk.W)
        code_entry = ttk.Entry(dialog, width=45)
        code_entry.grid(row=1, column=1, padx=5, pady=5)
        
        ttk.Label(dialog, text="候选位置(1-9):").grid(row=2, column=0, padx=5, pady=5, sticky=tk.W)
        position_spin = ttk.Spinbox(dialog, from_=1, to=9, width=10)
        position_spin.insert(0, "1")
        position_spin.grid(row=2, column=1, padx=5, pady=5, sticky=tk.W)
        
        def save():
            phrase = text_entry.get().strip()
            code = code_entry.get().strip()
            position = int(position_spin.get())
            if not phrase or not code:
                messagebox.showerror("错误", "短语和编码不能为空")
                return
            for p, c, pos in self.phrases:
                if p == phrase and c == code:
                    messagebox.showerror("错误", f"短语 '{phrase}' 与编码 '{code}' 已存在")
                    return
            self.phrases.append((phrase, code, position))
            self.refresh_display()
            self.update_stats()
            dialog.destroy()
        
        ttk.Button(dialog, text="确定", command=save).grid(row=3, column=0, pady=10)
        ttk.Button(dialog, text="取消", command=dialog.destroy).grid(row=3, column=1, pady=10)
    
    def edit_phrase(self, event=None):
        # 获取选中的项目（支持双击编辑第一个选中的）
        selected = self.tree.selection()
        if not selected:
            # 如果没有选中，尝试获取焦点项目
            item = self.tree.focus()
            if not item:
                return
        else:
            item = selected[0]
        
        values = self.tree.item(item, "values")
        if not values or len(values) < 2:
            return
        old_phrase, old_code, old_position = values[0], values[1], int(values[2])
        
        dialog = tk.Toplevel(self.root)
        dialog.title("编辑短语")
        dialog.geometry("450x280")
        dialog.transient(self.root)
        dialog.grab_set()
        
        ttk.Label(dialog, text="短语:").grid(row=0, column=0, padx=5, pady=5, sticky=tk.W)
        text_entry = ttk.Entry(dialog, width=45)
        text_entry.insert(0, old_phrase)
        text_entry.grid(row=0, column=1, padx=5, pady=5)
        
        ttk.Label(dialog, text="编码:").grid(row=1, column=0, padx=5, pady=5, sticky=tk.W)
        code_entry = ttk.Entry(dialog, width=45)
        code_entry.insert(0, old_code)
        code_entry.grid(row=1, column=1, padx=5, pady=5)
        
        ttk.Label(dialog, text="候选位置(1-9):").grid(row=2, column=0, padx=5, pady=5, sticky=tk.W)
        position_spin = ttk.Spinbox(dialog, from_=1, to=9, width=10)
        position_spin.insert(0, str(old_position))
        position_spin.grid(row=2, column=1, padx=5, pady=5, sticky=tk.W)
        
        def save():
            new_phrase = text_entry.get().strip()
            new_code = code_entry.get().strip()
            new_position = int(position_spin.get())
            if not new_phrase or not new_code:
                messagebox.showerror("错误", "短语和编码不能为空")
                return
            for i, (p, c, pos) in enumerate(self.phrases):
                if p == new_phrase and c == new_code and not (p == old_phrase and c == old_code):
                    messagebox.showerror("错误", f"短语 '{new_phrase}' 与编码 '{new_code}' 已存在")
                    return
                if p == old_phrase and c == old_code and pos == old_position:
                    self.phrases[i] = (new_phrase, new_code, new_position)
                    break
            self.refresh_display()
            self.update_stats()
            dialog.destroy()
        
        ttk.Button(dialog, text="确定", command=save).grid(row=3, column=0, pady=10)
        ttk.Button(dialog, text="取消", command=dialog.destroy).grid(row=3, column=1, pady=10)
    
    # ========== 导入 Gboard TXT（修正版）==========
    
    def import_gboard(self):
        """导入 Gboard 格式文件 (编码\t短语)"""
        filepath = filedialog.askopenfilename(
            title="选择 Gboard 导出的文件",
            filetypes=[("文本文件", "*.txt"), ("所有文件", "*.*")]
        )
        if not filepath:
            return
        
        new_entries = []
        try:
            with open(filepath, 'r', encoding='utf-8') as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith('#'):
                        continue
                    parts = line.split('\t')
                    if len(parts) >= 2:
                        code = parts[0].strip()
                        phrase = parts[1].strip()
                        phrase = re.sub(r'\s*(zh-CN|zh-TW|zh-HK|en-US|ja-JP)\s*$', '', phrase, flags=re.IGNORECASE)
                        if code and phrase:
                            new_entries.append((phrase, code))
        except UnicodeDecodeError:
            messagebox.showerror("错误", "文件编码错误，请确保文件是 UTF-8 编码的文本文件")
            return
        
        if not new_entries:
            messagebox.showwarning("警告", "未从文件中解析到有效条目")
            return
        
        existing_set = {(p, c) for p, c, _ in self.phrases}
        added = 0
        for phrase, code in new_entries:
            if (phrase, code) in existing_set:
                continue
            max_pos = 0
            for p, c, pos in self.phrases:
                if c == code and pos > max_pos:
                    max_pos = pos
            position = max_pos + 1
            if position > 9:
                position = 9
            self.phrases.append((phrase, code, position))
            existing_set.add((phrase, code))
            added += 1
        
        self.refresh_display()
        self.update_stats()
        messagebox.showinfo("导入完成", f"成功导入 {added} 条新短语")
    
    # ========== 导入微软拼音 DAT ==========
    
    def parse_dat_file(self, filepath):
        """解析微软拼音 DAT 文件"""
        phrases = []
        
        with open(filepath, 'rb') as f:
            data = f.read()
            
            if len(data) < 48:
                raise ValueError("文件太小")
            magic = data[0:8]
            if magic not in [b'machxudp', b'mschxudp']:
                raise ValueError(f"无效的文件标识: {magic}")
            
            offset_start = struct.unpack('<I', data[16:20])[0]
            entry_start = struct.unpack('<I', data[20:24])[0]
            entry_count = struct.unpack('<I', data[28:32])[0]
            
            offsets = []
            for i in range(entry_count):
                pos = offset_start + i * 4
                offsets.append(struct.unpack('<I', data[pos:pos+4])[0])
            
            for offset in offsets:
                pos = entry_start + offset
                if pos + 16 > len(data):
                    continue
                
                code_total_len = struct.unpack('<H', data[pos+4:pos+6])[0]
                order = data[pos+6]
                
                code_start = pos + 16
                code_bytes_len = code_total_len - 12
                code_end = code_start + code_bytes_len
                if code_end > len(data):
                    continue
                code_raw = data[code_start:code_end]
                
                phrase_start = code_end
                phrase_end = phrase_start
                while phrase_end + 2 <= len(data):
                    if data[phrase_end:phrase_end+2] == b'\x00\x00':
                        break
                    phrase_end += 2
                phrase_raw = data[phrase_start:phrase_end]
                
                null_pos = code_raw.find(b'\x00\x00')
                if null_pos > 0:
                    real_code = code_raw[:null_pos].decode('utf-16le', errors='replace')
                    phrase_part1 = code_raw[null_pos+2:].decode('utf-16le', errors='replace')
                else:
                    real_code = code_raw.decode('utf-16le', errors='replace')
                    phrase_part1 = ""
                
                phrase_part2 = phrase_raw.decode('utf-16le', errors='replace')
                full_phrase = phrase_part1 + phrase_part2
                
                full_phrase = full_phrase.strip()
                real_code = real_code.strip()
                
                if full_phrase and real_code:
                    phrases.append((full_phrase, real_code, order))
        
        return phrases
    
    def import_ms_dat(self):
        """导入微软拼音 DAT 文件"""
        filepath = filedialog.askopenfilename(
            title="选择微软拼音 DAT 文件",
            filetypes=[("DAT文件", "*.dat"), ("所有文件", "*.*")]
        )
        if not filepath:
            return
        
        try:
            new_entries = self.parse_dat_file(filepath)
        except Exception as e:
            messagebox.showerror("解析失败", f"无法解析 DAT 文件：{e}")
            return
        
        if not new_entries:
            messagebox.showwarning("警告", "未从文件中解析到有效条目")
            return
        
        existing_set = {(p, c) for p, c, _ in self.phrases}
        added = 0
        for phrase, code, position in new_entries:
            if (phrase, code) in existing_set:
                continue
            self.phrases.append((phrase, code, position))
            existing_set.add((phrase, code))
            added += 1
        
        self.refresh_display()
        self.update_stats()
        messagebox.showinfo("导入完成", f"成功导入 {added} 条新短语")
    
    # ========== 导出功能 ==========
    
    def export_gboard(self):
        """导出为 Gboard 格式 (编码\t短语)"""
        if not self.phrases:
            messagebox.showwarning("警告", "词库为空，无法导出")
            return
        
        output_path = filedialog.asksaveasfilename(
            title="导出为 Gboard 格式",
            defaultextension=".txt",
            filetypes=[("文本文件", "*.txt"), ("所有文件", "*.*")],
            initialfile=f"gboard_export_{datetime.now().strftime('%Y%m%d_%H%M%S')}.txt"
        )
        
        if output_path:
            with open(output_path, 'w', encoding='utf-8') as f:
                f.write("# Gboard Dictionary export\n")
                f.write(f"# Export time: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}\n")
                f.write(f"# Total: {len(self.phrases)} entries\n")
                f.write("# Format: shortcut\tword\tlanguage_tag\n")
                for phrase, code, _ in self.phrases:
                    # 判断是否包含中文字符
                    lang_tag = "zh-CN" if re.search(r'[\u4e00-\u9fff]', phrase) else "zz"
                    f.write(f"{code}\t{phrase}\t{lang_tag}\n")
            messagebox.showinfo("导出成功", f"已导出 {len(self.phrases)} 条短语")
    
    def build_dat_file(self, entries, output_path):
        """构建微软拼音 DAT 文件"""
        magic = b'mschxudp'
        version = b'\x02\x00\x60\x00\x01\x00\x00\x00'
        marker = struct.pack('<I', 0x00100010)
        unknown_flag = b'\x06'
        reserved = struct.pack('<I', 0x00000000)
        current_timestamp = int(datetime.now().timestamp())
        
        entries_data = []
        entry_offsets = []
        current_offset = 0
        
        for phrase, code, position in entries:
            if len(phrase) >= 2:
                phrase_part1 = phrase[:2]
                phrase_part2 = phrase[2:]
            else:
                phrase_part1 = phrase
                phrase_part2 = ""
            
            code_field = code.encode('utf-16le') + b'\x00\x00' + phrase_part1.encode('utf-16le')
            phrase_field = phrase_part2.encode('utf-16le') + b'\x00\x00'
            code_total_len = len(code_field) + 12
            
            header = marker
            header += struct.pack('<H', code_total_len)
            header += struct.pack('<B', position)
            header += unknown_flag
            header += reserved
            header += struct.pack('<I', current_timestamp)
            
            entry_data = header + code_field + phrase_field
            entries_data.append(entry_data)
            entry_offsets.append(current_offset)
            current_offset += len(entry_data)
        
        header_size = 48
        offset_table_size = len(entries) * 4
        offset_table_start = header_size
        entry_area_start = offset_table_start + offset_table_size
        
        offset_table = b''
        for offset in entry_offsets:
            offset_table += struct.pack('<I', offset)
        
        entry_area = b''.join(entries_data)
        total_len = entry_area_start + len(entry_area)
        
        file_header = magic
        file_header += version
        file_header += struct.pack('<I', offset_table_start)
        file_header += struct.pack('<I', entry_area_start)
        file_header += struct.pack('<I', total_len)
        file_header += struct.pack('<I', len(entries))
        file_header += struct.pack('<I', current_timestamp)
        file_header += b'\x00' * 12
        
        with open(output_path, 'wb') as f:
            f.write(file_header)
            f.write(offset_table)
            f.write(entry_area)
        
        return len(entries)
    
    def export_dat(self):
        """导出为微软拼音 DAT 格式"""
        if not self.phrases:
            messagebox.showwarning("警告", "词库为空，无法导出")
            return
        
        output_path = filedialog.asksaveasfilename(
            title="导出为微软拼音格式",
            defaultextension=".dat",
            filetypes=[("DAT文件", "*.dat"), ("所有文件", "*.*")],
            initialfile=f"ms_export_{datetime.now().strftime('%Y%m%d_%H%M%S')}.dat"
        )
        
        if output_path:
            count = self.build_dat_file(self.phrases, Path(output_path))
            messagebox.showinfo("导出成功", f"已导出 {count} 条短语到\n{output_path}\n\n可在微软拼音中导入使用")
    
    # ========== 辅助功能 ==========
    
    def open_folder(self):
        """打开脚本所在目录"""
        if platform.system() == "Windows":
            os.startfile(str(SCRIPT_DIR))
        else:
            subprocess.run(["xdg-open", str(SCRIPT_DIR)])
    
    def show_context_menu(self, event):
        item = self.tree.identify_row(event.y)
        if item:
            self.tree.selection_set(item)
            self.context_menu.post(event.x_root, event.y_root)


def main():
    root = tk.Tk()
    app = MsPhraseManager(root)
    root.mainloop()


if __name__ == "__main__":
    main()