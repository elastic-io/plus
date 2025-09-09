package files

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"plus/internal/types"
	"plus/pkg/repo"
	"plus/pkg/storage"
)

func init() {
	repo.Register(repo.Files, NewFilesRepo)
}

type FilesRepo struct {
	storage storage.Storage
}

func NewFilesRepo(storage storage.Storage) repo.Repo {
	return &FilesRepo{
		storage: storage,
	}
}

func (r *FilesRepo) Type() repo.RepoType {
	return repo.Files
}

func (r *FilesRepo) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	// Files 仓库接受任何类型的文件，直接存储到仓库根目录
	path := filepath.Join(repoName, filename)

	log.Printf("Uploading file to Files repo: %s -> %s", filename, path)

	if err := r.storage.Store(ctx, path, reader); err != nil {
		return fmt.Errorf("failed to store file: %w", err)
	}

	log.Printf("Successfully uploaded file: %s", filename)
	return nil
}

func (r *FilesRepo) DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	path := filepath.Join(repoName, filename)

	log.Printf("Downloading file from Files repo: %s", path)

	reader, err := r.storage.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file %s: %w", filename, err)
	}

	return reader, nil
}

func (r *FilesRepo) RefreshMetadata(ctx context.Context, repoName string) error {
	// Files 仓库不需要元数据刷新，直接返回成功
	log.Printf("RefreshMetadata called for Files repo: %s (no action needed)", repoName)
	return nil
}

func (r *FilesRepo) GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	// Files 仓库不维护元数据文件
	log.Printf("GetMetadata called for Files repo: %s/%s (not supported)", repoName, filename)
	return nil, fmt.Errorf("metadata not supported for Files repository")
}

func (r *FilesRepo) ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error) {
	log.Printf("Listing files in Files repo: %s", repoName)

	// 列出仓库目录下的所有文件（递归）
	files, err := r.storage.ListWithOptions(ctx, repoName, storage.ListOptions{
		MaxDepth:    -1,    // 递归列出所有文件
		IncludeDirs: false, // 只包含文件，不包含目录
		Extensions:  nil,   // 接受所有文件类型
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var packages []types.PackageInfo
	for _, file := range files {
		// 计算相对于仓库根目录的路径
		relativePath := strings.TrimPrefix(file.Name, repoName+"/")
		if relativePath == file.Name {
			// 如果没有前缀，说明文件就在根目录
			relativePath = filepath.Base(file.Name)
		}

		info := types.PackageInfo{
			Name: relativePath,
			Size: file.Size,
		}
		packages = append(packages, info)

		log.Printf("Found file: %s (size: %d bytes)", relativePath, file.Size)
	}

	log.Printf("Listed %d files in Files repo: %s", len(packages), repoName)
	return packages, nil
}

func (r *FilesRepo) CreateRepo(ctx context.Context, repoName string) error {
	log.Printf("Creating Files repo: %s", repoName)

	// 创建仓库目录
	if err := r.storage.CreateDir(ctx, repoName); err != nil {
		return fmt.Errorf("failed to create Files repository directory: %w", err)
	}

	// 创建仓库类型标记文件
	markerPath := filepath.Join(repoName, ".repo-type")
	markerContent := strings.NewReader("files")
	if err := r.storage.Store(ctx, markerPath, markerContent); err != nil {
		log.Printf("Warning: failed to create repo type marker: %v", err)
		// 不返回错误，因为这只是一个标记文件
	}

	log.Printf("Successfully created Files repo: %s", repoName)
	return nil
}

func (r *FilesRepo) DeleteRepo(ctx context.Context, repoName string) error {
	log.Printf("Deleting Files repo: %s", repoName)

	if err := r.storage.Delete(ctx, repoName); err != nil {
		return fmt.Errorf("failed to delete Files repository: %w", err)
	}

	log.Printf("Successfully deleted Files repo: %s", repoName)
	return nil
}

func (r *FilesRepo) ListRepos(ctx context.Context) ([]string, error) {
	log.Printf("Listing all Files repositories")

	files, err := r.storage.ListWithOptions(ctx, "", storage.ListOptions{
		MaxDepth:    -1,   // 递归搜索
		IncludeDirs: true, // 包含目录
		Extensions:  nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list directories: %w", err)
	}

	log.Printf("Found %d files/directories from storage", len(files))

	repoSet := make(map[string]bool)

	for _, file := range files {
		//log.Printf("Processing: %s, IsDir: %v, IsRepo: %v", file.Name, file.IsDir, file.IsRepo)

		if file.IsDir {
			// 首先检查是否已经被标记为仓库
			if file.IsRepo {
				repoSet[file.Name] = true
				log.Printf("Directory %s is marked as repo, adding to list", file.Name)
				continue
			}

			// 检查是否有 .repo-type 标记文件
			if r.hasRepoTypeMarker(ctx, file.Name, "files") {
				repoSet[file.Name] = true
				log.Printf("Directory %s has files repo marker, adding to list", file.Name)
				continue
			}
		}
	}

	// 转换为切片
	var repos []string
	for repo := range repoSet {
		repos = append(repos, repo)
	}

	log.Printf("Final Files repos list: %v\n", repos)
	return repos, nil
}

func (r *FilesRepo) GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error) {
	log.Printf("Computing checksum for file: %s/%s", repoName, filename)

	// 获取文件
	path := filepath.Join(repoName, filename)
	reader, err := r.storage.Get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("file %s not found in repository %s: %w", filename, repoName, err)
	}
	defer reader.Close()

	// 计算 SHA256 校验和
	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return "", fmt.Errorf("failed to compute checksum for %s: %w", filename, err)
	}

	checksum := fmt.Sprintf("%x", hasher.Sum(nil))
	log.Printf("Computed SHA256 checksum for %s: %s", filename, checksum)

	return checksum, nil
}

// 新增：检查是否有仓库类型标记文件
func (r *FilesRepo) hasRepoTypeMarker(ctx context.Context, dirPath, expectedType string) bool {
	// 确保路径格式正确
	markerPath := filepath.Join(dirPath, ".repo-type")

	log.Printf("Checking repo type marker at: %s", markerPath)

	reader, err := r.storage.Get(ctx, markerPath)
	if err != nil {
		log.Printf("Failed to get repo type marker %s: %v", markerPath, err)
		return false
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		log.Printf("Failed to read repo type marker %s: %v", markerPath, err)
		return false
	}

	actualType := strings.TrimSpace(string(content))
	log.Printf("Repo type marker content: '%s', expected: '%s'", actualType, expectedType)

	return actualType == expectedType
}

// 新增：检查是否为系统目录
func (r *FilesRepo) isSystemDirectory(dirPath string) bool {
	systemDirs := []string{
		".db.sys",
		"buckets",
		".git",
		".svn",
		"tmp",
		"temp",
	}

	for _, sysDir := range systemDirs {
		if strings.HasPrefix(dirPath, sysDir) {
			return true
		}
	}

	return false
}

// 检查目录是否包含文件
func (r *FilesRepo) hasFilesInDirectory(ctx context.Context, dirPath string) (bool, error) {
	files, err := r.storage.ListWithOptions(ctx, dirPath, storage.ListOptions{
		MaxDepth:    1,     // 只检查直接子项
		IncludeDirs: false, // 只检查文件
		Extensions:  nil,
	})
	if err != nil {
		return false, err
	}

	return len(files) > 0, nil
}

// 新增：支持子目录上传
func (r *FilesRepo) UploadToSubdir(ctx context.Context, repoName string, subdir string, filename string, reader io.Reader) error {
	// 构建完整路径，支持子目录
	var path string
	if subdir != "" {
		path = filepath.Join(repoName, subdir, filename)

		// 确保子目录存在
		subdirPath := filepath.Join(repoName, subdir)
		if err := r.storage.CreateDir(ctx, subdirPath); err != nil {
			log.Printf("Warning: failed to create subdir %s: %v", subdirPath, err)
		}
	} else {
		path = filepath.Join(repoName, filename)
	}

	log.Printf("Uploading file to Files repo with subdir: %s -> %s", filename, path)

	if err := r.storage.Store(ctx, path, reader); err != nil {
		return fmt.Errorf("failed to store file: %w", err)
	}

	log.Printf("Successfully uploaded file to subdir: %s", filename)
	return nil
}

// 新增：获取文件信息
func (r *FilesRepo) GetFileInfo(ctx context.Context, repoName string, filename string) (*types.PackageInfo, error) {
	path := filepath.Join(repoName, filename)

	files, err := r.storage.ListWithOptions(ctx, filepath.Dir(path), storage.ListOptions{
		MaxDepth:    1,
		IncludeDirs: false,
		Extensions:  nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	targetFile := filepath.Base(path)
	for _, file := range files {
		if filepath.Base(file.Name) == targetFile {
			return &types.PackageInfo{
				Name: filename,
				Size: file.Size,
			}, nil
		}
	}

	return nil, fmt.Errorf("file %s not found", filename)
}
