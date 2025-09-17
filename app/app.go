package app

import (
	"path/filepath"
	"time"

	"plus/internal/api"
	"plus/internal/config"
	"plus/internal/log"
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
		Log: c.String("log"),
		LogLevel: c.String("log-level"),
	}

	log.Init(cfg.Log, cfg.LogLevel)	

	repos := repo.NewRepoFactory(cfg)

	// 初始化 RPM 仓库管理器
	rpmRepo, err := repos.CreateRepo(repo.RPM)
	if err != nil {
		return err
	}

	log.Logger.Debugf("RPM repo init success: %s", rpmRepo.Type())

	filesRepo, err := repos.CreateRepo(repo.Files)
	if err != nil {
		return err
	}

	log.Logger.Debugf("Files repo init success: %s", filesRepo.Type())

	// 初始化服务
	repoService := service.NewRepoService(rpmRepo, filesRepo)

	log.Logger.Debug("service load success")

	// 初始化处理器
	r := api.NewAPI(repoService, cfg)

	// 设置路由
	router := api.SetupRouter(r)

	log.Logger.Debug("router setup success")

	server := &fasthttp.Server{
		Handler:            router,
		MaxRequestBodySize: MaxRequestBodySize,
		// 其他可选配置
		ReadTimeout:  time.Second * 60,
		WriteTimeout: time.Second * 60,
	}

	log.Logger.Debugf("Server starting on %s", cfg.Listen)
	return server.ListenAndServe(cfg.Listen)
}
