package repo

import (
	"context"
	"io"
	"plus/internal/types"
)

type Repo interface {
	// 上传包
	UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error

	// 下载包
	DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error)

	// 刷新仓库元数据
	RefreshMetadata(ctx context.Context, repoName string) error

	// 获取元数据文件
	GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error)

	// 列出包
	ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error)

	// 创建仓库
	CreateRepo(ctx context.Context, repoName string) error

	// 删除仓库
	DeleteRepo(ctx context.Context, repoName string) error

	// 列出仓库
	ListRepos(ctx context.Context) ([]string, error)

	// 获取包校验和
	GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error)
}
