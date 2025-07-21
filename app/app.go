package app

import (
	"log"

	"plus/internal/api"
	"plus/internal/config"
	"plus/internal/service"

	"plus/pkg/repo"
	"plus/pkg/storage/local"

	"github.com/urfave/cli"
	"github.com/valyala/fasthttp"
)

const Name = "plus"

func Run(c *cli.Context) error {
	cfg := &config.Config{
		Listen:      c.String("listen"),
		StoragePath: c.String("storage-path"),
		Debug:       c.Bool("debug"),
	}

	// 初始化存储
	storage := local.NewLocalStorage(cfg.StoragePath)

	repos := repo.NewRepoFactory(storage)

	// 初始化 RPM 仓库管理器
	rpmRepo, err := repos.CreateRepo(repo.RPM)
	if err != nil {
		return err
	}

	// 初始化服务
	repoService := service.NewRepoService(rpmRepo)

	// 初始化处理器
	r := api.NewAPI(repoService, cfg)

	// 设置路由
	router := api.SetupRouter(r)

	log.Printf("Server starting on %s", cfg.Listen)
	return fasthttp.ListenAndServe(cfg.Listen, router)
}
