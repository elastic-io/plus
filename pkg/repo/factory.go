package repo

import (
	"fmt"
	"plus/pkg/storage"
)

type RepoType string

const (
	RPM RepoType = "rpm"
	DEB RepoType = "deb"
)

type RepoFactory struct {
	storage storage.Storage
}

var factory = make(map[RepoType]func(storage.Storage) Repo)

func Register(rt RepoType, repo func(storage.Storage) Repo) {
	if _, ok := factory[rt]; ok {
		return
	}
	factory[rt] = repo
}

func NewRepoFactory(storage storage.Storage) *RepoFactory {
	return &RepoFactory{
		storage: storage,
	}
}

func (f *RepoFactory) CreateRepo(repoType RepoType) (Repo, error) {
	if repo, ok := factory[repoType]; ok {
		return repo(f.storage), nil
	}
	return nil, fmt.Errorf("unsupported repository type: %s", repoType)
}
