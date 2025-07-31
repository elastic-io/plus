package storage

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	Store(ctx context.Context, path string, reader io.Reader) error
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	ListWithOptions(ctx context.Context, prefix string, opts ListOptions) ([]FileInfo, error)
	CreateDir(ctx context.Context, path string) error
	GetPath(path string) string
	Exists(ctx context.Context, path string) (bool, error)
}

type FileInfo struct {
	Name    string
	Size    int64
	IsDir   bool
	IsRepo  bool
	ModTime time.Time // 添加修改时间字段
}

type ListOptions struct {
	MaxDepth    int // -1 表示无限深度
	IncludeDirs bool
	Extensions  []string // 文件扩展名过滤
}
