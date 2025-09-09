package app

import (
	"log"
	"path/filepath"
	"time"

	"plus/internal/api"
	"plus/internal/config"
	"plus/internal/service"

	"plus/pkg/repo"

	"github.com/urfave/cli"
	"github.com/valyala/fasthttp"
)

const Name = "plus"
const MaxRequestBodySize = 8 * 1024 * 1024 * 1024

func Run(c *cli.Context) error {
	cfg := &config.Config{
		Listen:      c.String("listen"),
		StoragePath: filepath.Clean(c.String("storage-path")),
		Debug:       c.Bool("debug"),
	}

	repos := repo.NewRepoFactory(cfg)

	// 初始化 RPM 仓库管理器
	rpmRepo, err := repos.CreateRepo(repo.RPM)
	if err != nil {
		return err
	}

	filesRepo, err := repos.CreateRepo(repo.Files)
	if err != nil {
		return err
	}

	// 初始化服务
	repoService := service.NewRepoService(rpmRepo, filesRepo)

	// 初始化处理器
	r := api.NewAPI(repoService, cfg)

	// 设置路由
	router := api.SetupRouter(r)

	server := &fasthttp.Server{
		Handler:            router,
		MaxRequestBodySize: MaxRequestBodySize,
		// 其他可选配置
		ReadTimeout:  time.Second * 60,
		WriteTimeout: time.Second * 60,
	}

	log.Printf("Server starting on %s", cfg.Listen)
	return server.ListenAndServe(cfg.Listen)
}
