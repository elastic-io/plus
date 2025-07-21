package rpm

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"plus/internal/types"
	"plus/pkg/repo"
	"plus/pkg/storage"

	"github.com/stianwa/createrepo"
)

func init() {
	repo.Register(repo.RPM, NewRPMRepo)
}

type RPMRepo struct {
	storage storage.Storage
	repo    *createrepo.Repo
}

func NewRPMRepo(storage storage.Storage) repo.Repo {
	return &RPMRepo{
		storage: storage,
	}
}

func (r *RPMRepo) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	// 验证是否为 RPM 文件
	if !strings.HasSuffix(filename, ".rpm") {
		return fmt.Errorf("invalid file type, expected .rpm")
	}

	// 存储文件到 Packages 子目录
	path := filepath.Join(repoName, "Packages", filename)
	if err := r.storage.Store(ctx, path, reader); err != nil {
		return fmt.Errorf("failed to store package: %w", err)
	}

	return nil
}

func (r *RPMRepo) DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	// 从 Packages 子目录获取文件
	path := filepath.Join(repoName, "Packages", filename)
	return r.storage.Get(ctx, path)
}

func (r *RPMRepo) RefreshMetadata(ctx context.Context, repoName string) error {
	repoPath := r.storage.GetPath(repoName)

	// 使用 createrepo 生成元数据
	config := &createrepo.Config{
		CompressAlgo:       "gz",
		ExpungeOldMetadata: 86400,
		WriteConfig:        true,
	}

	var err error
	if r.repo, err = createrepo.NewRepo(repoPath, config); err != nil {
		return fmt.Errorf("failed to new repo: %w", err)
	}

	sum, err := r.repo.Create()
	if err != nil {
		return fmt.Errorf("failed to create repo metadata: %w", err)
	}

	log.Printf("Repository metadata created for %s: %s", repoName, sum)
	return nil
}

func (r *RPMRepo) GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	path := filepath.Join(repoName, "repodata", filename)
	return r.storage.Get(ctx, path)
}

func (r *RPMRepo) ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error) {
	// 列出 Packages 目录下的文件
	packagesPath := filepath.Join(repoName, "Packages")
	files, err := r.storage.ListWithOptions(ctx, packagesPath, storage.ListOptions{
		MaxDepth:    1, // 只列出直接子文件
		IncludeDirs: false,
		Extensions:  []string{".rpm"},
	})
	if err != nil {
		return nil, err
	}

	var packages []types.PackageInfo
	for _, file := range files {
		if strings.HasSuffix(file.Name, ".rpm") {
			info := types.PackageInfo{
				Name: file.Name,
				Size: file.Size,
			}
			packages = append(packages, info)
		}
	}

	return packages, nil
}

func (r *RPMRepo) CreateRepo(ctx context.Context, repoName string) error {
	// 创建仓库目录和 Packages 子目录
	if err := r.storage.CreateDir(ctx, repoName); err != nil {
		return err
	}

	packagesDir := filepath.Join(repoName, "Packages")
	return r.storage.CreateDir(ctx, packagesDir)
}

func (r *RPMRepo) DeleteRepo(ctx context.Context, repoName string) error {
	return r.storage.Delete(ctx, repoName)
}

// pkg/repo/rpm/rpm.go
func (r *RPMRepo) ListRepos(ctx context.Context) ([]string, error) {
	files, err := r.storage.ListWithOptions(ctx, "", storage.ListOptions{
		MaxDepth:    -1,
		IncludeDirs: true,
		Extensions:  nil,
	})
	if err != nil {
		return nil, err
	}

	log.Printf("Found %d files/directories from storage", len(files))

	repoSet := make(map[string]bool)

	for _, file := range files {
		log.Printf("Processing: %s, IsDir: %v", file.Name, file.IsDir)

		// 简单策略：
		// 1. 如果目录直接标记为仓库，添加它
		// 2. 如果发现 Packages 或 repodata 目录，添加其父目录
		if file.IsDir {
			if file.IsRepo {
				repoSet[file.Name] = true
			}

			// 检查特殊目录名
			if strings.HasSuffix(file.Name, "/Packages") || strings.HasSuffix(file.Name, "/repodata") {
				parentDir := filepath.Dir(file.Name)
				if parentDir != "." && parentDir != "/" {
					repoSet[parentDir] = true
				}
			}
		}
	}

	// 转换为切片
	var repos []string
	for repo := range repoSet {
		repos = append(repos, repo)
	}

	log.Printf("Final repos list: %v", repos)
	return repos, nil
}

func (r *RPMRepo) GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error) {
	// 验证文件名
	if !strings.HasSuffix(filename, ".rpm") {
		return "", fmt.Errorf("invalid file type, expected .rpm")
	}

	// 找到最新的 primary.xml.gz 文件
	primaryFile, err := r.findLatestPrimaryXMLFile(ctx, repoName)
	if err != nil {
		return "", fmt.Errorf("failed to find primary.xml file: %w", err)
	}

	// 获取完整路径
	primaryPath := filepath.Join(repoName, "repodata", primaryFile)

	log.Printf("Reading latest primary metadata from: %s", primaryPath)

	reader, err := r.storage.Get(ctx, primaryPath)
	if err != nil {
		return "", fmt.Errorf("failed to get primary.xml.gz: %w", err)
	}
	defer reader.Close()

	// 解压并解析
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	metadata := types.Metadata{}
	decoder := xml.NewDecoder(gzReader)
	if err := decoder.Decode(&metadata); err != nil {
		return "", fmt.Errorf("failed to parse primary.xml: %w", err)
	}

	// 查找指定的包
	targetFile := filepath.Base(filename)
	log.Printf("Searching for package: %s in %d packages", targetFile, len(metadata.Packages))

	for _, pkg := range metadata.Packages {
		locationFile := filepath.Base(pkg.Location.Href)
		log.Printf("Checking package: %s (location: %s)", pkg.Name, pkg.Location.Href)

		if locationFile == targetFile {
			if pkg.Checksum.Type == "sha256" {
				log.Printf("Found SHA256 checksum for %s: %s", filename, pkg.Checksum.Value)
				return pkg.Checksum.Value, nil
			} else {
				log.Printf("Found package but checksum type is %s, not sha256", pkg.Checksum.Type)
			}
		}
	}

	return "", fmt.Errorf("package %s not found in repository metadata", filename)
}

// 查找最新的 primary.xml.gz 文件
func (r *RPMRepo) findLatestPrimaryXMLFile(ctx context.Context, repoName string) (string, error) {
	repodataPath := filepath.Join(repoName, "repodata")

	files, err := r.storage.ListWithOptions(ctx, repodataPath, storage.ListOptions{
		MaxDepth:    1,
		IncludeDirs: false,
		Extensions:  nil, // 获取所有文件
	})
	if err != nil {
		return "", fmt.Errorf("failed to list repodata files: %w", err)
	}

	var primaryFiles []storage.FileInfo

	// 查找所有 primary.xml.gz 文件
	for _, file := range files {
		fileName := filepath.Base(file.Name)
		if strings.HasSuffix(fileName, "-primary.xml.gz") {
			primaryFiles = append(primaryFiles, file)
			log.Printf("Found primary file: %s, modified: %v", fileName, file.ModTime)
		}
	}

	if len(primaryFiles) == 0 {
		return "", fmt.Errorf("no primary.xml.gz files found in repodata")
	}

	// 找到最新的文件（按修改时间排序）
	var latestFile storage.FileInfo
	for i, file := range primaryFiles {
		if i == 0 || file.ModTime.After(latestFile.ModTime) {
			latestFile = file
		}
	}

	latestFileName := filepath.Base(latestFile.Name)
	log.Printf("Using latest primary file: %s (modified: %v)", latestFileName, latestFile.ModTime)

	return latestFileName, nil
}
