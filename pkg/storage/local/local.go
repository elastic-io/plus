package local

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"plus/pkg/storage"
	"plus/internal/log"
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
	
	// 首先尝试直接打开（os.Open 默认跟随软链接）
	file, err := os.Open(fullPath)
	if err != nil {
		// 如果失败，尝试解析软链接后再打开
		if realPath, evalErr := filepath.EvalSymlinks(fullPath); evalErr == nil {
			log.Logger.Debugf("Resolved symlink %s -> %s", fullPath, realPath)
			return os.Open(realPath)
		}
		log.Logger.Debugf("Failed to open file %s: %v", fullPath, err)
	}
	return file, err
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
			log.Logger.Debugf("Warning: failed to access %s: %v", path, err)
			return nil
		}

		// 跳过根目录本身
		if path == fullPath {
			return nil
		}

		// 处理软链接
		originalPath := path
		isSymlink := d.Type()&fs.ModeSymlink != 0
		
		if isSymlink {
			// 解析软链接
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				log.Logger.Debugf("Warning: broken symlink %s: %v", path, err)
				return nil // 跳过断开的软链接
			}
			
			// 获取真实文件的信息
			realInfo, err := os.Stat(realPath)
			if err != nil {
				log.Logger.Debugf("Warning: failed to stat symlink target %s: %v", realPath, err)
				return nil
			}
			
			// 创建新的 DirEntry 使用真实文件的信息
			d = fs.FileInfoToDirEntry(realInfo)
			log.Logger.Debugf("Following symlink %s -> %s", originalPath, realPath)
		}

		// 检查深度限制
		if opts.MaxDepth >= 0 {
			relPath, _ := filepath.Rel(fullPath, originalPath)
			depth := strings.Count(relPath, string(filepath.Separator))
			if depth > opts.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		relPath, err := filepath.Rel(fullPath, originalPath)
		if err != nil {
			log.Logger.Debugf("Warning: failed to get relative path for %s: %v", originalPath, err)
			return nil
		}

		// 获取文件信息
		info, err := d.Info()
		if err != nil {
			log.Logger.Debugf("Warning: failed to get info for %s: %v", originalPath, err)
			return nil
		}

		// 处理目录
		if d.IsDir() {
			if opts.IncludeDirs {
				files = append(files, storage.FileInfo{
					Name:    relPath,
					Size:    info.Size(),
					IsDir:   true,
					IsRepo:  l.isRepoDirectory(originalPath),
					ModTime: info.ModTime(),
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
				ModTime: info.ModTime(),
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// Exists 方法 - 改进软链接处理
func (l *LocalStorage) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := filepath.Join(l.basePath, path)
	
	// 使用 Stat 检查文件是否存在（会跟随软链接）
	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 检查是否是断开的软链接
			if _, lstatErr := os.Lstat(fullPath); lstatErr == nil {
				// 软链接存在但目标不存在
				log.Logger.Debugf("Broken symlink detected: %s", fullPath)
				return false, nil
			}
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// 检查目录是否为仓库目录 - 改进软链接处理
func (l *LocalStorage) isRepoDirectory(dirPath string) bool {
	realDirPath := dirPath
	if resolved, err := filepath.EvalSymlinks(dirPath); err == nil {
		realDirPath = resolved
	}
	
	// 1. 首先检查最可靠的标识
	if _, err := os.Stat(filepath.Join(realDirPath, "repodata/repomd.xml")); err == nil {
		return true
	}
	
	// 2. 检查 repodata 目录
	if _, err := os.Stat(filepath.Join(realDirPath, "repodata")); err == nil {
		return true
	}
	
	// 3. 最后检查 Packages 目录（限制检查数量）
	packagesPath := filepath.Join(realDirPath, "Packages")
	if stat, err := os.Stat(packagesPath); err == nil && stat.IsDir() {
		// 使用 glob 模式快速检查是否有 RPM 文件
		if matches, err := filepath.Glob(filepath.Join(packagesPath, "*.rpm")); err == nil && len(matches) > 0 {
			return true
		}
	}
	
	return false
}
/*
func (l *LocalStorage) isRepoDirectory(dirPath string) bool {
	// 解析可能的软链接
	realDirPath := dirPath
	if resolved, err := filepath.EvalSymlinks(dirPath); err == nil {
		realDirPath = resolved
		log.Logger.Debugf("Resolved repo directory symlink %s -> %s", dirPath, realDirPath)
	}
	
	// 检查是否包含 Packages 目录
	packagesPath := filepath.Join(realDirPath, "Packages")
	if entries, err := os.ReadDir(packagesPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".rpm") {
				return true
			}
		}
	}

	// 检查是否包含 repodata 目录
	repodataPath := filepath.Join(realDirPath, "repodata")
	if _, err := os.Stat(repodataPath); err == nil {
		return true
	}

	return false
}
*/

func (l *LocalStorage) CreateDir(ctx context.Context, path string) error {
	fullPath := filepath.Join(l.basePath, path)
	return os.MkdirAll(fullPath, 0755)
}

func (l *LocalStorage) GetPath(path string) string {
	return filepath.Join(l.basePath, path)
}

// 新增辅助方法：安全的软链接解析
func (l *LocalStorage) resolvePath(path string) (string, error) {
	fullPath := filepath.Join(l.basePath, path)
	
	// 尝试解析软链接
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// 如果解析失败，返回原路径
		return fullPath, nil
	}
	
	// 安全检查：确保解析后的路径仍在 basePath 范围内
	realBasePath, err := filepath.EvalSymlinks(l.basePath)
	if err != nil {
		realBasePath = l.basePath
	}
	
	relPath, err := filepath.Rel(realBasePath, realPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		log.Logger.Warnf("Symlink %s points outside base directory: %s", fullPath, realPath)
		return fullPath, nil // 返回原路径，不跟随危险的软链接
	}
	
	return realPath, nil
}