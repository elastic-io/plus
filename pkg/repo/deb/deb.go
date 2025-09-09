package deb

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"plus/internal/types"
	"plus/pkg/repo"
	"plus/pkg/storage"
)

func init() {
	repo.Register(repo.DEB, NewDEBRepo)
}

type DEBRepo struct {
	storage storage.Storage
}

func NewDEBRepo(storage storage.Storage) repo.Repo {
	return &DEBRepo{
		storage: storage,
	}
}

func (r *DEBRepo) Type() repo.RepoType {
	return repo.DEB
}

func (d *DEBRepo) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	// 验证是否为 DEB 文件
	if !strings.HasSuffix(filename, ".deb") {
		return fmt.Errorf("invalid file type, expected .deb")
	}

	// 存储文件
	path := filepath.Join(repoName, filename)
	if err := d.storage.Store(ctx, path, reader); err != nil {
		return fmt.Errorf("failed to store package: %w", err)
	}

	return nil
}

func (d *DEBRepo) RefreshMetadata(ctx context.Context, repoName string) error {
	repoPath := d.storage.GetPath(repoName)

	// 使用 dpkg-scanpackages 生成 Packages 文件
	cmd := exec.CommandContext(ctx, "dpkg-scanpackages", ".", "/dev/null")
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to generate Packages file: %w", err)
	}

	// 保存 Packages 文件
	packagesPath := filepath.Join(repoPath, "Packages")
	if err := d.storage.Store(ctx, packagesPath, strings.NewReader(string(output))); err != nil {
		return fmt.Errorf("failed to save Packages file: %w", err)
	}

	// 生成压缩版本
	if err := d.compressPackagesFile(ctx, repoPath); err != nil {
		return fmt.Errorf("failed to compress Packages file: %w", err)
	}

	return nil
}

func (d *DEBRepo) compressPackagesFile(ctx context.Context, repoPath string) error {
	// 生成 Packages.gz
	cmd := exec.CommandContext(ctx, "gzip", "-c", "Packages")
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return err
	}

	packagesGzPath := filepath.Join(repoPath, "Packages.gz")
	return d.storage.Store(ctx, packagesGzPath, strings.NewReader(string(output)))
}

// 实现其他接口方法...
func (d *DEBRepo) DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	path := filepath.Join(repoName, filename)
	return d.storage.Get(ctx, path)
}

func (d *DEBRepo) GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	path := filepath.Join(repoName, filename)
	return d.storage.Get(ctx, path)
}

func (d *DEBRepo) ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error) {
	files, err := d.storage.ListWithOptions(ctx, repoName, storage.ListOptions{
		MaxDepth:    -1,
		IncludeDirs: true,
	})
	if err != nil {
		return nil, err
	}

	var packages []types.PackageInfo
	for _, file := range files {
		if strings.HasSuffix(file.Name, ".deb") {
			info := types.PackageInfo{
				Name: file.Name,
				Size: file.Size,
			}
			packages = append(packages, info)
		}
	}

	return packages, nil
}

func (d *DEBRepo) CreateRepo(ctx context.Context, repoName string) error {
	return d.storage.CreateDir(ctx, repoName)
}

func (d *DEBRepo) DeleteRepo(ctx context.Context, repoName string) error {
	return d.storage.Delete(ctx, repoName)
}

func (d *DEBRepo) ListRepos(ctx context.Context) ([]string, error) {
	files, err := d.storage.ListWithOptions(ctx, "", storage.ListOptions{
		MaxDepth: -1,
	})
	if err != nil {
		return nil, err
	}

	var repos []string
	for _, file := range files {
		if file.IsDir {
			repos = append(repos, file.Name)
		}
	}

	return repos, nil
}

func (d *DEBRepo) GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error) {
	return "", nil
}
