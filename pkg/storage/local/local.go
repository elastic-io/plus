package local

import (
	"context"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"plus/pkg/storage"
)

func init() {
	storage.Register(storage.Local, NewLocalStorage, "rpm", "deb")
}

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) (storage.Storage, error) {
	return &LocalStorage{
		basePath: basePath,
	}, nil
}

func (l *LocalStorage) Store(ctx context.Context, path string, reader io.Reader) error {
	fullPath := filepath.Join(l.basePath, path)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	return err
}

func (l *LocalStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := filepath.Join(l.basePath, path)
	return os.Open(fullPath)
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	fullPath := filepath.Join(l.basePath, path)
	return os.RemoveAll(fullPath)
}

func (l *LocalStorage) ListWithOptions(ctx context.Context, prefix string, opts storage.ListOptions) ([]storage.FileInfo, error) {
	fullPath := filepath.Join(l.basePath, prefix)

	// 如果路径不存在，返回空列表而不是错误
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return []storage.FileInfo{}, nil
	}

	var files []storage.FileInfo
	err := filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("Warning: failed to access %s: %v", path, err)
			return nil
		}

		// 跳过根目录本身
		if path == fullPath {
			return nil
		}

		// 检查深度限制
		if opts.MaxDepth >= 0 {
			relPath, _ := filepath.Rel(fullPath, path)
			depth := strings.Count(relPath, string(filepath.Separator))
			if depth > opts.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		relPath, err := filepath.Rel(fullPath, path)
		if err != nil {
			log.Printf("Warning: failed to get relative path for %s: %v", path, err)
			return nil
		}

		// 获取文件信息
		info, err := d.Info()
		if err != nil {
			log.Printf("Warning: failed to get info for %s: %v", path, err)
			return nil
		}

		// 处理目录
		if d.IsDir() {
			if opts.IncludeDirs {
				files = append(files, storage.FileInfo{
					Name:    relPath,
					Size:    info.Size(),
					IsDir:   true,
					IsRepo:  l.isRepoDirectory(path),
					ModTime: info.ModTime(), // 添加修改时间
				})
			}
		} else {
			// 处理文件
			if len(opts.Extensions) > 0 {
				ext := strings.ToLower(filepath.Ext(d.Name()))
				found := false
				for _, allowedExt := range opts.Extensions {
					if ext == strings.ToLower(allowedExt) {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			files = append(files, storage.FileInfo{
				Name:    relPath,
				Size:    info.Size(),
				IsDir:   false,
				IsRepo:  false,
				ModTime: info.ModTime(), // 添加修改时间
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// 添加 Exists 方法
func (l *LocalStorage) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := filepath.Join(l.basePath, path)
	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// 检查目录是否为仓库目录
func (l *LocalStorage) isRepoDirectory(dirPath string) bool {
	// 检查是否包含 Packages 目录
	packagesPath := filepath.Join(dirPath, "Packages")
	if entries, err := os.ReadDir(packagesPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".rpm") {
				return true
			}
		}
	}

	// 检查是否包含 repodata 目录
	repodataPath := filepath.Join(dirPath, "repodata")
	if _, err := os.Stat(repodataPath); err == nil {
		return true
	}

	return false
}

func (l *LocalStorage) CreateDir(ctx context.Context, path string) error {
	fullPath := filepath.Join(l.basePath, path)
	return os.MkdirAll(fullPath, 0755)
}

func (l *LocalStorage) GetPath(path string) string {
	return filepath.Join(l.basePath, path)
}
