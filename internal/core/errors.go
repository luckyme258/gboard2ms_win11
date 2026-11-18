package core

import "errors"

var (
	ErrZipTooBig = errors.New("zip 内容超出限制（上限 50 个文件 / 20 MB），已中止")
	ErrBadDat    = errors.New("不是有效的微软拼音 DAT 文件")
)
