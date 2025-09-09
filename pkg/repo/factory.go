package repo

import (
	"fmt"
	"plus/internal/config"
	"plus/pkg/storage"
)

type RepoType string

const (
	RPM RepoType = "rpm"
	DEB RepoType = "deb"
	Files RepoType = "files"
)

type RepoFactory struct {
	storage storage.Storage
	path string
}

var factory = make(map[RepoType]func(storage.Storage) Repo)

func Register(rt RepoType, repo func(storage.Storage) Repo) {
	if _, ok := factory[rt]; ok {
		return
	}
	factory[rt] = repo
}

func NewRepoFactory(cfg *config.Config) *RepoFactory {
	return &RepoFactory{
		path: cfg.StoragePath,
	}
}

func (f *RepoFactory) CreateRepo(repoType RepoType) (Repo, error) {
	s, err := storage.CreateByLable(f.path, string(repoType))
	if err != nil {
		return nil, err
	}
	f.storage = s
	if repo, ok := factory[repoType]; ok {
		return repo(f.storage), nil
	}
	return nil, fmt.Errorf("unsupported repository type: %s", repoType)
}
