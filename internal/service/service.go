package service

import (
	"context"
	"fmt"
	"io"
	"sync"

	"plus/internal/types"
	"plus/pkg/repo"
)

type RepoService struct {
	repo repo.Repo
	mu   sync.RWMutex
}

func NewRepoService(repo repo.Repo) *RepoService {
	return &RepoService{
		repo: repo,
	}
}

func (s *RepoService) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.repo.UploadPackage(ctx, repoName, filename, reader)
}

func (s *RepoService) DownloadPackage(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.repo.DownloadPackage(ctx, repoName, filename)
}

func (s *RepoService) RefreshMetadata(ctx context.Context, repoName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.repo.RefreshMetadata(ctx, repoName)
}

func (s *RepoService) GetMetadata(ctx context.Context, repoName string, filename string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.repo.GetMetadata(ctx, repoName, filename)
}

func (s *RepoService) ListPackages(ctx context.Context, repoName string) ([]types.PackageInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.repo.ListPackages(ctx, repoName)
}

func (s *RepoService) CreateRepo(ctx context.Context, repoName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.repo.CreateRepo(ctx, repoName)
}

func (s *RepoService) DeleteRepo(ctx context.Context, repoName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.repo.DeleteRepo(ctx, repoName)
}

func (s *RepoService) ListRepos(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.repo.ListRepos(ctx)
}

type MultiRepoService struct {
	repositories map[string]repo.Repo
	factory      *repo.RepoFactory
	mu           sync.RWMutex
}

func NewMultiRepoService(factory *repo.RepoFactory) *MultiRepoService {
	return &MultiRepoService{
		repositories: make(map[string]repo.Repo),
		factory:      factory,
	}
}

func (s *MultiRepoService) RegisterRepo(name string, repoType repo.RepoType) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo, err := s.factory.CreateRepo(repoType)
	if err != nil {
		return err
	}

	s.repositories[name] = repo
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

func (s *MultiRepoService) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
	repo, err := s.GetRepo(repoName)
	if err != nil {
		return err
	}

	return repo.UploadPackage(ctx, repoName, filename, reader)
}

func (s *RepoService) GetPackageChecksum(ctx context.Context, repoName string, filename string) (string, error) {
	return s.repo.GetPackageChecksum(ctx, repoName, filename)
}
