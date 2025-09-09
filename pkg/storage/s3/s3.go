package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"plus/pkg/storage"
	"strings"
	"time"

	"github.com/elastic-io/mindb"
)

func init() {
	storage.Register(storage.S3, NewMinDBStorage, "files")
}

var bucket = "mindb"

type MinDBStorage struct {
	db     *mindb.DB
	bucket string
}

// NewMinDBStorage 创建新的 MinDB 存储实例
func NewMinDBStorage(dbPath string) (storage.Storage, error) {
	db, err := mindb.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("创建 MinDB 实例失败: %w", err)
	}

	storage := &MinDBStorage{
		db:     db,
		bucket: bucket,
	}

	// 确保桶存在
	exists, err := db.BucketExists(bucket)
	if err != nil {
		return nil, fmt.Errorf("检查桶是否存在失败: %w", err)
	}

	if !exists {
		if err := db.CreateBucket(bucket); err != nil {
			return nil, fmt.Errorf("创建桶失败: %w", err)
		}
	}

	return storage, nil
}

// Store 存储文件
func (m *MinDBStorage) Store(ctx context.Context, path string, reader io.Reader) error {
	// 读取所有数据
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("读取数据失败: %w", err)
	}

	// 创建 ObjectData
	objectData := &mindb.ObjectData{
			Key:         m.normalizePath(path),
			Data:        data,
			Size:        int64(len(data)),
			ContentType: m.getContentType(path),
			Metadata: map[string]string{
				"upload-time": time.Now().UTC().Format(time.RFC3339),
			},
			LastModified: time.Now(),
	}

	// 上传对象
	if err := m.db.PutObject(m.bucket, objectData); err != nil {
		return fmt.Errorf("上传对象失败: %w", err)
	}

	return nil
}

// Get 获取文件
func (m *MinDBStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	objectData, err := m.db.GetObject(m.bucket, m.normalizePath(path))
	if err != nil {
		return nil, fmt.Errorf("获取对象失败: %w", err)
	}

	return io.NopCloser(bytes.NewReader(objectData.Data)), nil
}

// Delete 删除文件
func (m *MinDBStorage) Delete(ctx context.Context, path string) error {
	normalizedPath := m.normalizePath(path)
	
	// 如果是目录，需要删除所有子对象
	if strings.HasSuffix(normalizedPath, "/") || m.isDirectory(ctx, normalizedPath) {
		return m.deleteDirectory(ctx, normalizedPath)
	}

	if err := m.db.DeleteObject(m.bucket, normalizedPath); err != nil {
		return fmt.Errorf("删除对象失败: %w", err)
	}
	return nil
}

// ListWithOptions 列出文件和目录
// ListWithOptions 列出文件和目录
func (m *MinDBStorage) ListWithOptions(ctx context.Context, prefix string, opts storage.ListOptions) ([]storage.FileInfo, error) {
	var result []storage.FileInfo
	var marker string
	normalizedPrefix := m.normalizePath(prefix)
	
	// 确保前缀以 / 结尾（如果不为空）
	if normalizedPrefix != "" && !strings.HasSuffix(normalizedPrefix, "/") {
		normalizedPrefix += "/"
	}

	fmt.Printf("ListWithOptions: prefix='%s', normalizedPrefix='%s'\n", prefix, normalizedPrefix)

	seen := make(map[string]bool) // 防止重复

	for {
		// 不使用分隔符，获取所有对象
		objects, _, err := m.db.ListObjects(
			m.bucket,
			normalizedPrefix,
			marker,
			"", // 不使用分隔符
			1000,
		)
		if err != nil {
			return nil, fmt.Errorf("列出对象失败: %w", err)
		}

		fmt.Printf("Found %d objects with prefix '%s'\n", len(objects), normalizedPrefix)

		// 用于收集目录信息
		directories := make(map[string]storage.FileInfo)

		// 处理对象
		for _, obj := range objects {
			objKey := obj.Key
			fmt.Printf("Processing object: %s, Size: %d\n", objKey, obj.Size)
			
			if seen[objKey] {
				continue
			}
			seen[objKey] = true

			// 跳过不符合前缀的对象
			if normalizedPrefix != "" && !strings.HasPrefix(objKey, normalizedPrefix) {
				continue
			}

			// 计算相对路径
			relativePath := strings.TrimPrefix(objKey, normalizedPrefix)
			if relativePath == "" {
				continue
			}

			// 判断是否为目录占位符
			isDirectoryPlaceholder := strings.HasSuffix(objKey, "/") && obj.Size == 0

			if isDirectoryPlaceholder {
				// 这是一个目录占位符
				if opts.IncludeDirs {
					dirName := strings.TrimSuffix(relativePath, "/")
					if dirName != "" && m.shouldIncludeByDepth(objKey, normalizedPrefix, opts.MaxDepth) {
						directories[dirName] = storage.FileInfo{
							Name:    dirName,
							Size:    0,
							IsDir:   true,
							IsRepo:  m.isRepoDirectory(objKey, true),
							ModTime: obj.LastModified,
						}
					}
				}
			} else {
				// 这是一个普通文件
				if m.shouldIncludeFile(objKey, normalizedPrefix, opts) {
					// 检查文件是否在子目录中
					pathParts := strings.Split(relativePath, "/")
					if len(pathParts) > 1 && opts.IncludeDirs {
						// 文件在子目录中，确保父目录被包含
						for i := 1; i <= len(pathParts)-1; i++ {
							dirPath := strings.Join(pathParts[:i], "/")
							if dirPath != "" && !seen[normalizedPrefix+dirPath+"/"] {
								parentDirKey := normalizedPrefix + dirPath + "/"
								if m.shouldIncludeByDepth(parentDirKey, normalizedPrefix, opts.MaxDepth) {
									directories[dirPath] = storage.FileInfo{
										Name:    dirPath,
										Size:    0,
										IsDir:   true,
										IsRepo:  m.isRepoDirectory(parentDirKey, true),
										ModTime: time.Now(), // 使用当前时间作为推断的目录时间
									}
								}
							}
						}
					}

					// 添加文件本身
					result = append(result, storage.FileInfo{
						Name:    relativePath,
						Size:    obj.Size,
						IsDir:   false,
						IsRepo:  false,
						ModTime: obj.LastModified,
					})
				}
			}
		}

		// 添加收集到的目录
		for _, dir := range directories {
			result = append(result, dir)
		}

		// 检查是否还有更多对象
		if len(objects) < 1000 {
			break
		}

		// 设置下一页的 marker
		if len(objects) > 0 {
			marker = objects[len(objects)-1].Key
		}
	}

	fmt.Printf("Final result count: %d\n", len(result))
	for _, item := range result {
		fmt.Printf("Result item: Name='%s', IsDir=%v, IsRepo=%v\n", item.Name, item.IsDir, item.IsRepo)
	}
	
	return result, nil
}

// isRepoDirectory 判断目录是否为仓库
// isRepoDirectory 判断目录是否为仓库
func (m *MinDBStorage) isRepoDirectory(path string, isDir bool) bool {
	if !isDir {
		return false
	}

	// 确保路径以 / 结尾
	dirPath := path
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}

	// 检查是否包含仓库类型标记文件
	markerPath := dirPath + ".repo-type"
	if exists, _ := m.Exists(context.Background(), markerPath); exists {
		fmt.Printf("Found repo marker at: %s\n", markerPath)
		return true
	}

	// 检查是否包含典型的仓库结构
	repoIndicators := []string{
		"Packages/",     // RPM 仓库
		"repodata/",     // RPM 仓库
		"dists/",        // DEB 仓库
	}

	for _, indicator := range repoIndicators {
		checkPath := dirPath + indicator
		if exists, _ := m.Exists(context.Background(), checkPath); exists {
			fmt.Printf("Found repo indicator at: %s\n", checkPath)
			return true
		}
	}

	return false
}

// hasRepoTypeMarker 检查是否有仓库类型标记
func (m *MinDBStorage) hasRepoTypeMarker(dirPath string) bool {
	markerPath := strings.TrimSuffix(dirPath, "/") + "/.repo-type"
	exists, _ := m.Exists(context.Background(), markerPath)
	return exists
}


// CreateDir 创建目录
func (m *MinDBStorage) CreateDir(ctx context.Context, path string) error {
	// 在对象存储中，目录通过以 "/" 结尾的空对象来表示
	dirPath := m.normalizePath(path)
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}

	objectData := &mindb.ObjectData{
		Key:         dirPath,
		Data:        []byte{}, // 空数据表示目录
		Size:        0,
		ContentType: "application/x-directory",
		Metadata: map[string]string{
			"content-type": "application/x-directory",
			"create-time":  time.Now().UTC().Format(time.RFC3339),
			"is-directory": "true", // 明确标记为目录
		},
		LastModified: time.Now(),
	}

	if err := m.db.PutObject(m.bucket, objectData); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	fmt.Printf("Created directory object: %s\n", dirPath) // 调试日志
	return nil
}

// GetPath 获取完整路径
func (m *MinDBStorage) GetPath(path string) string {
	return fmt.Sprintf("mindb://%s/%s", m.bucket, m.normalizePath(path))
}

// Exists 检查文件或目录是否存在
func (m *MinDBStorage) Exists(ctx context.Context, path string) (bool, error) {
	normalizedPath := m.normalizePath(path)

	// 尝试直接获取对象
	_, err := m.db.GetObject(m.bucket, normalizedPath)
	if err == nil {
		return true, nil
	}

	// 如果直接获取失败，尝试作为目录检查
	if !strings.HasSuffix(normalizedPath, "/") {
		_, err = m.db.GetObject(m.bucket, normalizedPath+"/")
		if err == nil {
			return true, nil
		}
	}

	// 最后尝试列出以该路径为前缀的对象
	objects, _, err := m.db.ListObjects(m.bucket, normalizedPath, "", "", 1)
	if err != nil {
		return false, fmt.Errorf("检查对象存在性失败: %w", err)
	}

	return len(objects) > 0, nil
}

// Close 关闭数据库连接
func (m *MinDBStorage) Close() error {
	return m.db.Close()
}

// 辅助方法

// normalizePath 标准化路径
func (m *MinDBStorage) normalizePath(path string) string {
	// 移除开头的斜杠
	path = strings.TrimPrefix(path, "/")
	// 标准化路径分隔符
	path = filepath.ToSlash(path)
	return path
}

// getContentType 根据文件扩展名获取内容类型
func (m *MinDBStorage) getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// isDirectory 检查路径是否为目录
func (m *MinDBStorage) isDirectory(ctx context.Context, path string) bool {
	// 检查是否存在以该路径为前缀的对象
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	
	objects, _, err := m.db.ListObjects(m.bucket, path, "", "", 1)
	if err != nil {
		return false
	}
	
	return len(objects) > 0
}

// deleteDirectory 删除目录及其所有内容
func (m *MinDBStorage) deleteDirectory(ctx context.Context, dirPath string) error {
	if !strings.HasSuffix(dirPath, "/") {
		dirPath += "/"
	}

	var marker string
	for {
		objects, _, err := m.db.ListObjects(m.bucket, dirPath, marker, "", 1000)
		if err != nil {
			return fmt.Errorf("列出目录对象失败: %w", err)
		}

		// 删除所有对象
		for _, obj := range objects {
			if err := m.db.DeleteObject(m.bucket, obj.Key); err != nil {
				return fmt.Errorf("删除对象 %s 失败: %w", obj.Key, err)
			}
		}

		if len(objects) < 1000 {
			break
		}

		marker = objects[len(objects)-1].Key
	}

	// 删除目录占位符
	_ = m.db.DeleteObject(m.bucket, dirPath)

	return nil
}

// shouldIncludeFile 检查文件是否应该包含在结果中
func (m *MinDBStorage) shouldIncludeFile(key, prefix string, opts storage.ListOptions) bool {
	// 检查深度
	if !m.shouldIncludeByDepth(key, prefix, opts.MaxDepth) {
		return false
	}

	// 检查扩展名
	if len(opts.Extensions) > 0 {
		ext := strings.ToLower(filepath.Ext(key))
		found := false
		for _, allowedExt := range opts.Extensions {
			if strings.ToLower(allowedExt) == ext {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// shouldIncludeByDepth 检查路径深度是否符合要求
func (m *MinDBStorage) shouldIncludeByDepth(path, prefix string, maxDepth int) bool {
	if maxDepth == -1 {
		return true // 无限深度
	}

	relativePath := strings.TrimPrefix(path, prefix)
	relativePath = strings.Trim(relativePath, "/")

	if relativePath == "" {
		return false
	}

	depth := strings.Count(relativePath, "/") + 1
	return depth <= maxDepth
}

// getRelativeName 获取相对于前缀的名称
func (m *MinDBStorage) getRelativeName(fullPath, prefix string) string {
	relativePath := strings.TrimPrefix(fullPath, prefix)
	return strings.Trim(relativePath, "/")
}

// isRepoFile 判断是否为仓库文件
func (m *MinDBStorage) isRepoFile(path string) bool {
	// 根据你的业务逻辑来判断
	repoIndicators := []string{".git/", ".repo/", ".hg/", ".svn/", "Dockerfile", "docker-compose"}
	lowerPath := strings.ToLower(path)
	
	for _, indicator := range repoIndicators {
		if strings.Contains(lowerPath, strings.ToLower(indicator)) {
			return true
		}
	}
	
	// 检查常见的仓库配置文件
	fileName := filepath.Base(path)
	repoFiles := []string{"dockerfile", "docker-compose.yml", "docker-compose.yaml", 
		"makefile", "rakefile", "gemfile", "package.json", "pom.xml", "build.gradle"}
	
	for _, repoFile := range repoFiles {
		if strings.EqualFold(fileName, repoFile) {
			return true
		}
	}
	
	return false
}