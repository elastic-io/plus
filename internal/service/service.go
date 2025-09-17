package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"plus/internal/log"
	"plus/internal/types"
	"plus/pkg/repo"
)

type RepoService struct {
	repos       map[repo.RepoType]repo.Repo // 按类型存储 repo 实例
	repoTypes   map[string]repo.RepoType    // 存储每个仓库名对应的类型
	repoConfigs map[string]string           // 存储仓库配置信息（如描述等）
	mu          sync.RWMutex
}

func NewRepoService(repos ...repo.Repo) *RepoService {
	rs := &RepoService{
		repos:       make(map[repo.RepoType]repo.Repo),
		repoTypes:   make(map[string]repo.RepoType),
		repoConfigs: make(map[string]string),
	}
	
	// 注册所有类型的 repo
	for _, r := range repos {
		rs.repos[r.Type()] = r
		log.Logger.Debugf("Registered repo type: %s", r.Type())
	}
	
	return rs
}

// 获取指定仓库的 repo 实例
func (s *RepoService) getRepoInstance(repoName string) (repo.Repo, repo.RepoType, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	repoType, exists := s.repoTypes[repoName]
	if !exists {
		// 如果没有记录，尝试推断类型
		inferredType, err := s.inferRepoType(repoName)
		if err != nil {
			return nil, "", fmt.Errorf("repository %s not found and cannot infer type: %w", repoName, err)
		}
		repoType = inferredType
		// 注意：这里需要重新获取写锁来更新 repoTypes
		s.mu.RUnlock()
		s.mu.Lock()
		s.repoTypes[repoName] = repoType
		s.mu.Unlock()
		s.mu.RLock()
	}
	
	repoInstance, exists := s.repos[repoType]
	if !exists {
		return nil, "", fmt.Errorf("no handler for repository type %s", repoType)
	}
	
	return repoInstance, repoType, nil
}

// 推断仓库类型
func (s *RepoService) inferRepoType(repoName string) (repo.RepoType, error) {
	// 尝试从不同类型的 repo 中查找
	for repoType, repoInstance := range s.repos {
		log.Logger.Debugf("Checking repo type and instance: %s\n", repoType)
		repos, err := repoInstance.ListRepos(context.Background())
		if err != nil {
			continue
		}
		
		for _, existingRepo := range repos {
			if existingRepo == repoName {
				log.Logger.Debugf("Inferred repo type for %s: %s", repoName, repoType)
				return repoType, nil
			}
		}
	}
	
	return "", fmt.Errorf("cannot infer type for repository %s", repoName)
}

func (s *RepoService) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	repoInstance, repoType, err := s.getRepoInstance(repoName)
	if err != nil {
		return err
	}
	
	// 验证文件类型
	if err := s.validateFileType(filename, repoType); err != nil {
		return err
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	log.Logger.Debugf("Uploading %s to %s repository: %s", filename, repoType, repoName)
	return repoInstance.UploadPackage(ctx, repoName, filename, reader)
}

func (s *RepoService) DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	repoInstance, _, err := s.getRepoInstance(repoName)
	if err != nil {
		return nil, err
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()

	log.Logger.Debugf("Downloading %s from types %s", filename, repoInstance.Type())
	
	return repoInstance.DownloadPackage(ctx, repoName, filename)
}

func (s *RepoService) DownloadPackageFiles(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	return s.repos[repo.Files].DownloadPackage(ctx, repoName, filename)
}

func (s *RepoService) RefreshMetadata(ctx context.Context, repoName string) error {
	repoInstance, repoType, err := s.getRepoInstance(repoName)
	if err != nil {
		return err
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	log.Logger.Debugf("Refreshing metadata for %s repository: %s", repoType, repoName)
	return repoInstance.RefreshMetadata(ctx, repoName)
}

func (s *RepoService) GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	repoInstance, _, err := s.getRepoInstance(repoName)
	if err != nil {
		return nil, err
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return repoInstance.GetMetadata(ctx, repoName, filename)
}

func (s *RepoService) ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error) {
	repoInstance, _, err := s.getRepoInstance(repoName)
	if err != nil {
		return nil, err
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return repoInstance.ListPackages(ctx, repoName)
}

// 修改：添加类型参数
func (s *RepoService) CreateRepo(ctx context.Context, repoName string, repoTypeStr string) error {
	// 解析仓库类型
	var repoType repo.RepoType
	switch strings.ToLower(repoTypeStr) {
	case "rpm":
		repoType = repo.RPM
	case "deb":
		repoType = repo.DEB
	case "files":
		repoType = repo.Files
	default:
		return fmt.Errorf("unsupported repository type: %s", repoTypeStr)
	}
	
	repoInstance, exists := s.repos[repoType]
	if !exists {
		return fmt.Errorf("no handler for repository type %s", repoType)
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// 创建仓库
	if err := repoInstance.CreateRepo(ctx, repoName); err != nil {
		return err
	}
	
	// 记录仓库类型
	s.repoTypes[repoName] = repoType
	
	log.Logger.Debugf("Created %s repository: %s", repoType, repoName)
	return nil
}

func (s *RepoService) DeleteRepo(ctx context.Context, repoName string) error {
	repoInstance, _, err := s.getRepoInstance(repoName)
	if err != nil {
		return err
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if err := repoInstance.DeleteRepo(ctx, repoName); err != nil {
		return err
	}
	
	// 清理类型记录
	delete(s.repoTypes, repoName)
	delete(s.repoConfigs, repoName)
	
	log.Logger.Debugf("Deleted repository: %s", repoName)
	return nil
}

func (s *RepoService) ListRepos(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	allRepos := make(map[string]bool)
	
	// 从所有类型的 repo 中收集仓库列表
	for repoType, repoInstance := range s.repos {
		log.Logger.Debugf("Listing repos for type: %s\n", repoType)
		repos, err := repoInstance.ListRepos(ctx)
		if err != nil {
			log.Logger.Debugf("Failed to list repos for type %s: %v\n", repoType, err)
			continue
		}
		
		for _, repoName := range repos {
			allRepos[repoName] = true
			// 更新类型映射
			if _, exists := s.repoTypes[repoName]; !exists {
				s.repoTypes[repoName] = repoType
			}
		}
	}
	
	// 转换为切片
	var result []string
	for repoName := range allRepos {
		result = append(result, repoName)
	}
	
	return result, nil
}

// 新增：获取仓库类型
func (s *RepoService) GetRepoType(ctx context.Context, repoName string) (string, error) {
	s.mu.RLock()
	repoType, exists := s.repoTypes[repoName]
	s.mu.RUnlock()
	
	if !exists {
		// 尝试推断类型
		inferredType, err := s.inferRepoType(repoName)
		if err != nil {
			return "unknown", err
		}
		
		s.mu.Lock()
		s.repoTypes[repoName] = inferredType
		s.mu.Unlock()
		
		repoType = inferredType
	}
	
	return string(repoType), nil
}

// 新增：设置仓库类型
func (s *RepoService) SetRepoType(ctx context.Context, repoName string, repoTypeStr string) error {
	var repoType repo.RepoType
	switch strings.ToLower(repoTypeStr) {
	case "rpm":
		repoType = repo.RPM
	case "deb":
		repoType = repo.DEB
	case "files":
		repoType = repo.Files
	default:
		return fmt.Errorf("unsupported repository type: %s", repoTypeStr)
	}
	
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.repoTypes[repoName] = repoType
	return nil
}

// 验证文件类型
func (s *RepoService) validateFileType(filename string, repoType repo.RepoType) error {
	switch repoType {
	case repo.RPM:
		if !strings.HasSuffix(strings.ToLower(filename), ".rpm") {
			return fmt.Errorf("RPM repository only accepts .rpm files")
		}
	case repo.DEB:
		if !strings.HasSuffix(strings.ToLower(filename), ".deb") {
			return fmt.Errorf("DEB repository only accepts .deb files")
		}
	case repo.Files:
		// Files 类型接受任何文件
		return nil
	default:
		return fmt.Errorf("unknown repository type: %s", repoType)
	}
	return nil
}

func (s *RepoService) GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error) {
	repoInstance, _, err := s.getRepoInstance(repoName)
	if err != nil {
		return "", err
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return repoInstance.GetPackageChecksum(ctx, repoName, filename)
}

// 新增：获取仓库详细信息
func (s *RepoService) GetRepoInfo(ctx context.Context, repoName string) (*types.RepoInfo, error) {
	repoInstance, repoType, err := s.getRepoInstance(repoName)
	if err != nil {
		return nil, err
	}
	
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	packages, err := repoInstance.ListPackages(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages: %w", err)
	}
	
	// 统计信息
	var totalSize int64
	rpmCount := 0
	debCount := 0
	
	for _, pkg := range packages {
		totalSize += pkg.Size
		if strings.HasSuffix(strings.ToLower(pkg.Name), ".rpm") {
			rpmCount++
		} else if strings.HasSuffix(strings.ToLower(pkg.Name), ".deb") {
			debCount++
		}
	}
	
	return &types.RepoInfo{
		Status: types.Status{
			Status: "success",
		},
		Name:         repoName,
		Type:         string(repoType),
		PackageCount: len(packages),
		RPMCount:     rpmCount,
		DEBCount:     debCount,
		TotalSize:    totalSize,
		Packages:     packages,
	}, nil
}

// MultiRepoService 保持不变，但可以添加类型支持
type MultiRepoService struct {
	repositories map[string]repo.Repo
	repoTypes    map[string]string // 新增：存储仓库类型映射
	factory      *repo.RepoFactory
	mu           sync.RWMutex
}

func NewMultiRepoService(factory *repo.RepoFactory) *MultiRepoService {
	return &MultiRepoService{
		repositories: make(map[string]repo.Repo),
		repoTypes:    make(map[string]string), // 新增
		factory:      factory,
	}
}

// 修改：添加类型参数
func (s *MultiRepoService) RegisterRepo(name string, repoType repo.RepoType, repoTypeStr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo, err := s.factory.CreateRepo(repoType)
	if err != nil {
		return err
	}

	s.repositories[name] = repo
	s.repoTypes[name] = repoTypeStr // 新增：存储类型信息
	return nil
}

func (s *MultiRepoService) GetRepo(name string) (repo.Repo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	repo, exists := s.repositories[name]
	if !exists {
		return nil, fmt.Errorf("repository %s not found", name)
	}

	return repo, nil
}

// 新增：获取仓库类型
func (s *MultiRepoService) GetRepoType(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	repoType, exists := s.repoTypes[name]
	if !exists {
		return "", fmt.Errorf("repository %s not found", name)
	}

	return repoType, nil
}

func (s *MultiRepoService) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	repo, err := s.GetRepo(repoName)
	if err != nil {
		return err
	}

	return repo.UploadPackage(ctx, repoName, filename, reader)
}

// 新增：带类型验证的上传
func (s *MultiRepoService) UploadPackageWithValidation(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	// 获取仓库类型
	repoType, err := s.GetRepoType(repoName)
	if err != nil {
		return err
	}

	// 验证文件类型
	switch repoType {
	case "rpm":
		if !strings.HasSuffix(strings.ToLower(filename), ".rpm") {
			return fmt.Errorf("repository '%s' only accepts RPM files", repoName)
		}
	case "deb":
		if !strings.HasSuffix(strings.ToLower(filename), ".deb") {
			return fmt.Errorf("repository '%s' only accepts DEB files", repoName)
		}
	case "files":
		// files 类型接受任何文件
	default:
		// 其他类型的验证逻辑
	}

	return s.UploadPackage(ctx, repoName, filename, reader)
}