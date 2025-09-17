package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"plus/assets"
	"plus/internal/config"
	"plus/internal/log"
	"plus/internal/metrics"
	"plus/internal/middleware"
	"plus/internal/service"
	"plus/internal/types"
	"plus/internal/utils"

	"github.com/valyala/fasthttp"
)

type API struct {
	repoService *service.RepoService
	config      *config.Config
}

func NewAPI(repoService *service.RepoService, config *config.Config) *API {
	return &API{
		repoService: repoService,
		config:      config,
	}
}

func (h *API) RefreshRepo(ctx *fasthttp.RequestCtx) {
	// 解析路径: /repo/{repoPath}/refresh，支持多层路径
	path := string(ctx.Path())

	// 移除 /repo/ 前缀和 /refresh 后缀
	if !strings.HasPrefix(path, "/repo/") || !strings.HasSuffix(path, "/refresh") {
		ctx.Error("Invalid refresh path", fasthttp.StatusBadRequest)
		return
	}

	// 提取完整的仓库路径
	repoPath := strings.TrimPrefix(path, "/repo/")
	repoPath = strings.TrimSuffix(repoPath, "/refresh")

	if repoPath == "" {
		ctx.Error("Repository path is required", fasthttp.StatusBadRequest)
		return
	}
	
	log.Logger.Debugf("🔄 Refreshing repository: %s", repoPath)

	// 检查仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, repoPath)
	if err != nil {
		log.Logger.Debugf("Failed to get repository type for %s: %v", repoPath, err)
		h.sendJSONError(ctx, "Repository not found", fasthttp.StatusNotFound)
		return
	}

	// Files 类型仓库不需要刷新元数据
	if repoType == "files" {
		log.Logger.Debugf("Repository %s is files type, no metadata refresh needed", repoPath)
		h.sendJSONError(ctx, "Files repositories do not require metadata refresh", fasthttp.StatusBadRequest)
		return
	}

	err = h.repoService.RefreshMetadata(ctx, repoPath)
	if err != nil {
		log.Logger.Debugf("Refresh metadata failed for repo %s: %v", repoPath, err)
		h.sendJSONError(ctx, fmt.Sprintf("Refresh failed: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	response := &types.RepoStatus{
		Status: types.Status{
			Status:  "success",
			Message: "Repository metadata refreshed successfully",
		},
		Repo: repoPath,
	}

	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

// 发送 JSON 成功响应
func (h *API) sendJSONResponse(ctx *fasthttp.RequestCtx, data io.WriterTo, statusCode int) {
	ctx.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
	ctx.SetStatusCode(statusCode)

	if _, err := data.WriteTo(ctx); err != nil {
		log.Logger.Debugf("Failed to encode JSON response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(`{"status":"error","message":"Internal server error"}`)
	}
}

// 发送 JSON 错误响应
func (h *API) sendJSONError(ctx *fasthttp.RequestCtx, message string, statusCode int) {
	response := types.Status{
		Status:  "error",
		Message: message,
		Code:    statusCode,
	}

	ctx.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
	ctx.SetStatusCode(statusCode)

	if _, err := response.WriteTo(ctx); err != nil {
		log.Logger.Debugf("Failed to encode JSON error response: %v", err)
		ctx.SetBodyString(fmt.Sprintf(`{"status":"error","message":"%s"}`, message))
	}
}

// 发送 JSON 成功响应（简化版）
func (h *API) sendSuccess(ctx *fasthttp.RequestCtx, message string) {
	response := &types.Status{
		Status:  "success",
		Message: message,
		Code:    fasthttp.StatusOK,
	}

	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

func (h *API) Health(ctx *fasthttp.RequestCtx) {
	response := &types.Status{
		Status: "healthy",
		Server: "plus",
	}

	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

func (h *API) Metrics(ctx *fasthttp.RequestCtx) {
	m := metrics.GetMetrics()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	response := &types.Metrics{
		Requests: types.Requests{
			Total:     m.RequestCount,
			Uploads:   m.UploadCount,
			Downloads: m.DownloadCount,
			Errors:    m.ErrorCount,
			Active:    m.ActiveRequests,
		},
		Performance: types.Performance{
			ResponseTimeMs: m.ResponseTime,
			Goroutines:     runtime.NumGoroutine(),
		},
		Memory: types.Memory{
			AllocMB:      memStats.Alloc / 1024 / 1024,
			TotalAllocMB: memStats.TotalAlloc / 1024 / 1024,
			SysMB:        memStats.Sys / 1024 / 1024,
			GCCycles:     memStats.NumGC,
		},
	}

	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

func (h *API) Ready(ctx *fasthttp.RequestCtx) {
	// 检查存储是否可用
	_, err := h.repoService.ListRepos(ctx)
	if err != nil {
		ctx.Error("Service not ready", fasthttp.StatusServiceUnavailable)
		return
	}

	response := &types.ReadyCheck{
		Status: types.Status{
			Status: "ready"},
		Checks: types.Checks{
			Storage: "ok",
		},
	}

	ctx.Response.Header.Set("Content-Type", "application/json")
	response.WriteTo(ctx)
}

func SetupRouter(h *API) fasthttp.RequestHandler {
	patterns := map[string]*regexp.Regexp{
		"download_rpm": regexp.MustCompile(`^/repo/(.+)/rpm/([^/]+)$`),
		"download_deb": regexp.MustCompile(`^/repo/(.+)/deb/([^/]+)$`),
		"metadata":     regexp.MustCompile(`^/repo/(.+)/repodata/(.+)$`),
		"deb_metadata": regexp.MustCompile(`^/repo/(.+)/(Packages|Packages\.gz|Release)$`),
		"upload":       regexp.MustCompile(`^/repo/(.+)/upload$`),
		"refresh":      regexp.MustCompile(`^/repo/(.+)/refresh$`),
		"checksum":     regexp.MustCompile(`^/repo/(.+)/checksum/([^/]+)$`),
		"repo_info":    regexp.MustCompile(`^/repo/([^/]+(?:/[^/]+)*)$`),
		"repo_files":   regexp.MustCompile(`^/repo/(.+)/files/?(.*)$`),
		"repo_browse":  regexp.MustCompile(`^/repo/(.+)/browse/?(.*)$`),
		"direct_browse": regexp.MustCompile(`^/([^/]+(?:/[^/]+)+)/?(.*)$`),
	}

	// 根据环境选择静态文件处理器
	var staticHandler fasthttp.RequestHandler
	if h.config != nil && h.config.DevMode {
		// 开发模式：使用外部文件
		staticHandler = createExternalStaticHandler("./static")
		log.Logger.Info("Using external static files (development mode)")
	} else {
		// 生产模式：使用嵌入文件
		staticHandler = createEmbeddedStaticHandler()
		log.Logger.Info("Using embedded static files (production mode)")
	}

	repoHandler := createRepoHandler(h.config.StoragePath)

	return middleware.CORSMiddleware(
		middleware.LoggingMiddleware(
			middleware.MetricsMiddleware(
				func(ctx *fasthttp.RequestCtx) {
					path := string(ctx.Path())
					method := string(ctx.Method())

					log.Logger.Debugf("🔍 Request: %s %s", method, path)

					// 1. Web UI 静态文件服务
					if method == "GET" && strings.HasPrefix(path, "/static/") {
						handleWebStatic(ctx, staticHandler)
						return
					}

					// 2. 根路径处理
					if method == "GET" && path == "/" {
						handleRootPath(ctx)
						return
					}

					// 3. 仓库列表页面
					if method == "GET" && path == "/repo/" {
						handleRepoListPage(ctx, h)
						return
					}

					// 4. API 端点
					if handleAPIEndpoints(ctx, method, path, h) {
						return
					}

					// 5. 仓库相关端点 - 优先匹配特定端点
					if handleRepoEndpoints(ctx, method, h.config.StoragePath, path, patterns, h) {
						return
					}

					// 6. 直接路径浏览 - 只处理 files 类型仓库
					if method == "GET" && h.handleDirectFileSystemAccess(ctx, path) {
						return
					}

					// 7. 仓库文件直接访问 - 最后匹配
					if method == "GET" && strings.HasPrefix(path, "/repo/") {
						if h.handleRepoFileAccess(ctx, repoHandler) {
							return
						}
					}

					ctx.Error("Not Found", fasthttp.StatusNotFound)
				},
			),
		),
	)
}

func (h *API) handleDirectFileSystemAccess(ctx *fasthttp.RequestCtx, path string) bool {
    // 排除特殊路径
    if path == "/" || strings.HasPrefix(path, "/static/") || 
       strings.HasPrefix(path, "/health") ||
       strings.HasPrefix(path, "/ready") || 
       strings.HasPrefix(path, "/metrics") ||
       strings.HasPrefix(path, "/repos") ||
       strings.HasPrefix(path, "/repo/") { // 排除 /repo/ 开头的路径
        return false
    }

    cleanPath := strings.TrimPrefix(path, "/")
    if cleanPath == "" {
        return false
    }

    log.Logger.Debugf("🔍 Direct filesystem access attempt: %s", cleanPath)

    // 🔥 新增：先尝试本地文件系统（保持原有性能）
    fullPath := filepath.Join(h.config.StoragePath, cleanPath)
    
    if info, err := os.Stat(fullPath); err == nil {
        log.Logger.Debugf("✅ Direct filesystem access: %s", fullPath)
        
        if info.IsDir() {
            // 智能目录处理
            h.handleSmartDirectoryListing(ctx, cleanPath, fullPath)
        } else {
            // 文件处理
            h.handleDirectFileServe(ctx, cleanPath, fullPath)
        }
        return true
    }
    
    // 🔥 新增：本地文件系统失败后，尝试对象存储
    log.Logger.Debugf("❌ Path not found in local filesystem: %s", fullPath)
    log.Logger.Debugf("🔍 Trying object storage for: %s", cleanPath)
    
    return h.tryObjectStorageAccess(ctx, cleanPath)
}

func (h *API) tryObjectStorageAccess(ctx *fasthttp.RequestCtx, cleanPath string) bool {
    log.Logger.Debugf("🔍 Checking object storage access for path: %s", cleanPath)

    keyword := strings.TrimPrefix(filepath.Clean(h.config.StoragePath), "/")
    
    if !strings.Contains(cleanPath, keyword) {
        log.Logger.Debugf("❌ Not a files repository (missing %s): %s", keyword, cleanPath)
        return false
    }
    
    log.Logger.Debugf("✅ Detected files repository path, attempting direct access: %s", cleanPath)
    
    return h.handleObjectStorageFile(ctx, "", cleanPath)
}

func (h *API) tryAccessRepository(ctx *fasthttp.RequestCtx, repoName, filePath string) bool {
    log.Logger.Debugf("🔍 Attempting to access repo=%s, file=%s", repoName, filePath)
    
    if filePath == "" {
        // 尝试目录访问
        if h.handleObjectStorageDirectory(ctx, repoName, repoName) {
            log.Logger.Debugf("✅ Successfully accessed directory for repo: %s", repoName)
            return true
        }
    } else {
        // 尝试文件访问
        if h.handleObjectStorageFile(ctx, repoName, filePath) {
            log.Logger.Debugf("✅ Successfully accessed file: repo=%s, file=%s", repoName, filePath)
            return true
        }
    }
    
    log.Logger.Debugf("❌ Failed to access repo=%s, file=%s", repoName, filePath)
    return false
}

func (h *API) handleObjectStorageDirectory(ctx *fasthttp.RequestCtx, repoName, displayPath string) bool {
    log.Logger.Debugf("🔍 Object storage directory: repo=%s", repoName)

    // 使用仓库服务获取文件列表
    packages, err := h.repoService.ListPackages(ctx, repoName)
    if err != nil {
        log.Logger.Debugf("❌ Failed to list packages for repo %s: %v", repoName, err)
        ctx.Error("Failed to access repository", fasthttp.StatusInternalServerError)
        return true
    }

    // 生成对象存储的目录列表HTML
    h.generateObjectStorageDirectoryHTML(ctx, repoName, displayPath, packages)
    return true
}

// 🔥 新增：处理对象存储文件
func (h *API) handleObjectStorageFile(ctx *fasthttp.RequestCtx, repoName, filePath string) bool {
    log.Logger.Debugf("🔍 Object storage file: repo=%s, path=%s", repoName, filePath)

    // 尝试下载文件
    reader, err := h.repoService.DownloadPackageFiles(ctx, repoName, filePath)
    if err != nil {
        log.Logger.Debugf("❌ Object storage file not found: repo=%s, path=%s, error=%v", repoName, filePath, err)
        ctx.Error("File not found", fasthttp.StatusNotFound)
        return true
    }
    defer reader.Close()

    // 设置适当的 Content-Type
    contentType := utils.GetContentTypeByExtension(filePath)
    ctx.Response.Header.Set("Content-Type", contentType)
    
    // 设置文件名
    filename := filepath.Base(filePath)
    ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
    
    ctx.SetBodyStream(reader, -1)
    return true
}

func (h *API) generateObjectStorageDirectoryHTML(ctx *fasthttp.RequestCtx, repoName, displayPath string, packages []types.PackageInfo) {
    ctx.SetContentType("text/html; charset=utf-8")
    ctx.SetBodyString(utils.GenerateObjectStorageDirectoryHTML(repoName, displayPath, packages))
}

func (h *API) handleSmartDirectoryListing(ctx *fasthttp.RequestCtx, cleanPath, fullPath string) {
    // 快速检查是否为仓库目录（不遍历所有仓库）
    repoType := utils.DetectRepoTypeByPath(fullPath)
    
    log.Logger.Debugf("🔍 Detected repo type for %s: %s", cleanPath, repoType)
    
    if repoType != "unknown" {
        // 是仓库目录，生成增强的HTML
        h.generateEnhancedDirectoryHTML(ctx, cleanPath, fullPath, repoType)
    } else {
        // 普通目录，使用基本HTML
        handleDirectoryListingNew(ctx, cleanPath, fullPath)
    }
}

func (h *API) generateEnhancedDirectoryHTML(ctx *fasthttp.RequestCtx, cleanPath, fullPath, repoType string) {
	str ,err := utils.GenerateEnhancedDirectoryHTML(cleanPath, fullPath, repoType)
	if err != nil {
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
		return
	}
    ctx.SetContentType("text/html; charset=utf-8")
    ctx.SetBodyString(str)
}

func (h *API) handleDirectFileServe(ctx *fasthttp.RequestCtx, cleanPath, fullPath string) {
    // 设置正确的 Content-Type
    if strings.Contains(cleanPath, "repodata/") {
        filename := filepath.Base(cleanPath)
        contentType := utils.GetContentType(filename)
        ctx.Response.Header.Set("Content-Type", contentType)
        ctx.Response.Header.Set("Cache-Control", "public, max-age=300")
    }
    
    // 对于包文件，设置下载头
    filename := filepath.Base(cleanPath)
    if strings.HasSuffix(filename, ".rpm") || strings.HasSuffix(filename, ".deb") {
        ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        metrics.IncrementDownloads()
    }
    
    fasthttp.ServeFile(ctx, fullPath)
}

// 新增：处理对象存储浏览
func (h *API) handleObjectStorageBrowse(ctx *fasthttp.RequestCtx, repoName, remainingPath string) bool {
	if remainingPath == "" {
		// 仓库根目录 - 显示仓库内容
		h.handleObjectStorageRepoList(ctx, repoName)
	} else {
		// 子路径 - 尝试下载文件
		h.handleObjectStorageFile(ctx, repoName, remainingPath)
	}
	return true
}

// 新增：处理本地存储浏览
func (h *API) handleLocalStorageBrowse(ctx *fasthttp.RequestCtx, repoName, remainingPath, cleanPath string) bool {
	// 构建存储路径
	var storagePath string
	if remainingPath == "" {
		storagePath = fmt.Sprintf("%s/%s", h.config.StoragePath, repoName)
	} else {
		storagePath = fmt.Sprintf("%s/%s/%s", h.config.StoragePath, repoName, remainingPath)
	}

	// 检查路径是否存在
	info, err := os.Stat(storagePath)
	if err != nil {
		log.Logger.Debugf("❌ Storage path not found: %s, error: %v", storagePath, err)
		return false
	}

	log.Logger.Debugf("✅ Local storage browse: repo=%s, path=%s, storage=%s", repoName, remainingPath, storagePath)

	if info.IsDir() {
		// 目录浏览 - 使用原有的目录列表函数
		handleDirectoryListingNew(ctx, cleanPath, storagePath)
	} else {
		// 文件下载
		fasthttp.ServeFile(ctx, storagePath)
	}
	
	return true
}

// 新增：处理对象存储仓库列表
func (h *API) handleObjectStorageRepoList(ctx *fasthttp.RequestCtx, repoName string) {
	log.Logger.Debugf("🔍 Object storage repository browse: repo=%s", repoName)

	// 使用仓库服务获取包列表
	packages, err := h.repoService.ListPackages(ctx, repoName)
	if err != nil {
		log.Logger.Debugf("❌ Failed to list packages for repo %s: %v", repoName, err)
		ctx.Error("Failed to access repository", fasthttp.StatusInternalServerError)
		return
	}

	// 构建简单的文件列表HTML
	html := utils.GenerateObjectStorageRepoHTML(repoName, packages)
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

// 仓库文件直接访问 (nginx 兼容方式)
func (h *API) handleRepoFileAccess(ctx *fasthttp.RequestCtx, _ fasthttp.RequestHandler) bool {
	path := string(ctx.Path())

	// 匹配 /repo/{repoName}/{filepath} 但排除 API 端点
	repoPathRegex := regexp.MustCompile(`^/repo/([^/]+)/(.+)$`)
	matches := repoPathRegex.FindStringSubmatch(path)

	if matches == nil {
		return false
	}

	repoName := matches[1]
	filePath := matches[2]

	// 排除 API 端点
	apiEndpoints := []string{"upload", "refresh", "repodata", "browse"}
	for _, endpoint := range apiEndpoints {
		if strings.HasPrefix(filePath, endpoint) {
			return false
		}
	}

	// 检查是否是直接文件访问
	fullPath := fmt.Sprintf("%s/%s/%s", h.config.StoragePath, repoName, filePath)
	if info, err := os.Stat(fullPath); err == nil {
		if info.IsDir() {
			// 目录访问 - 生成目录列表
			handleDirectoryListing(ctx, repoName, filePath, fullPath)
		} else {
			// 文件访问 - 直接服务文件
			fasthttp.ServeFile(ctx, fullPath)
		}
		return true
	}

	return false
}

// 仓库浏览模式处理
func (h *API) handleRepoBrowse(ctx *fasthttp.RequestCtx, browseRegex *regexp.Regexp) {
	path := string(ctx.Path())
	matches := browseRegex.FindStringSubmatch(path)

	if matches == nil {
		ctx.Error("Invalid browse path", fasthttp.StatusBadRequest)
		return
	}

	repoName := matches[1]
	subPath := matches[2]

	fullPath := fmt.Sprintf("%s/%s/%s", h.config.StoragePath, repoName, subPath)

	if info, err := os.Stat(fullPath); err != nil {
		ctx.Error("Path not found", fasthttp.StatusNotFound)
		return
	} else if info.IsDir() {
		handleDirectoryListing(ctx, repoName, subPath, fullPath)
	} else {
		fasthttp.ServeFile(ctx, fullPath)
	}
}

func (h *API) ServeMetadata(ctx *fasthttp.RequestCtx, repoName, filename string) {
	reader, err := h.repoService.GetMetadata(ctx, repoName, filename)
	if err != nil {
		ctx.Error("Metadata not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	contentType := utils.GetContentType(filename)
	ctx.Response.Header.Set("Content-Type", contentType)
	ctx.Response.Header.Set("Cache-Control", "public, max-age=300")

	ctx.SetBodyStream(reader, -1)
}

func (h *API) GetRepoInfo(ctx *fasthttp.RequestCtx, repoName string) {
	packages, err := h.repoService.ListPackages(ctx, repoName)
	if err != nil {
		log.Logger.Debugf("Get repo info failed for %s: %v", repoName, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to get repository info: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	// 新增：获取仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, repoName)
	if err != nil {
		log.Logger.Debugf("Failed to get repository type for %s: %v", repoName, err)
		repoType = "unknown" // 设置默认值而不是返回错误
	}

	// 统计信息
	var totalSize int64
	rpmCount := 0
	debCount := 0

	for _, pkg := range packages {
		totalSize += pkg.Size
		if strings.HasSuffix(pkg.Name, ".rpm") {
			rpmCount++
		} else if strings.HasSuffix(pkg.Name, ".deb") {
			debCount++
		}
	}

	h.sendJSONResponse(ctx, &types.RepoInfo{
		Status: types.Status{
			Status: "success"},
		Name:         repoName,
		Type:         repoType,        // 新增类型字段
		PackageCount: len(packages),
		RPMCount:     rpmCount,
		DEBCount:     debCount,
		TotalSize:    totalSize,
		Packages:     packages,
	}, fasthttp.StatusOK)
}

func (h *API) buildRepoTreeWithTypes(repos []string) map[string]*types.TreeNode {
	tree := make(map[string]*types.TreeNode)

	for _, repo := range repos {
		parts := strings.Split(repo, "/")
		current := tree

		for i, part := range parts {
			if _, exists := current[part]; !exists {
				if i == len(parts)-1 {
					// 叶子节点，获取仓库类型
					repoType, err := h.repoService.GetRepoType(context.Background(), repo)
					if err != nil {
						repoType = "unknown"
					}
					
					current[part] = &types.TreeNode{
						Type:     "repo",
						Path:     repo,
						RepoType: repoType, // 新增仓库类型字段
					}
				} else {
					// 中间节点
					current[part] = &types.TreeNode{
						Type:     "directory",
						Children: make(map[string]*types.TreeNode),
					}
				}
			}

			if i < len(parts)-1 {
				if node := current[part]; node != nil && node.Children != nil {
					current = node.Children
				}
			}
		}
	}

	return tree
}

func (h *API) ListRepos(ctx *fasthttp.RequestCtx) {
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Logger.Debugf("List repositories failed: %v", err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to list repositories: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	// 构建包含类型信息的层级结构
	repoTree := h.buildRepoTreeWithTypes(repos)

	h.sendJSONResponse(ctx, &types.RepoMeta{
		Status:       types.Status{Server: "Plus", Status: "success", Code: fasthttp.StatusOK},
		Repositories: repos,
		Tree:         repoTree,
		Count:        len(repos),
	}, fasthttp.StatusOK)
}

func (h *API) DeleteRepo(ctx *fasthttp.RequestCtx, repoName string) {
	err := h.repoService.DeleteRepo(ctx, repoName)
	if err != nil {
		log.Logger.Debugf("Delete repository failed for %s: %v", repoName, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to delete repository: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, "Repository deleted successfully")
}

func (h *API) ServeStatic(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	filename := strings.TrimPrefix(path, "/static/")

	// 安全检查，防止目录遍历攻击
	if strings.Contains(filename, "..") {
		ctx.Error("Forbidden", fasthttp.StatusForbidden)
		return
	}
	staticPath := filepath.Join("./static", filename)
	fasthttp.ServeFile(ctx, staticPath)
}

func (h *API) BatchUpload(ctx *fasthttp.RequestCtx) {
	// 解析 multipart form
	form, err := ctx.MultipartForm()
	if err != nil {
		ctx.Error("Failed to parse multipart form", fasthttp.StatusBadRequest)
		return
	}
	defer ctx.Request.RemoveMultipartFormFiles()

	// 获取仓库名称
	repoNames := form.Value["repository"]
	if len(repoNames) == 0 {
		ctx.Error("Repository name is required", fasthttp.StatusBadRequest)
		return
	}
	repoName := repoNames[0]

	// 获取文件列表
	files := form.File["files"]
	if len(files) == 0 {
		ctx.Error("No files uploaded", fasthttp.StatusBadRequest)
		return
	}

	response := &types.BatchUploadResponse{
		Total:   len(files),
		Results: make([]types.BatchUploadResult, 0, len(files)),
	}

	// 批量上传文件
	for _, fileHeader := range files {
		result := h.uploadSingleFile(ctx, repoName, fileHeader)
		response.Results = append(response.Results, result)

		if result.Status == "success" {
			response.Success++
		} else {
			response.Failed++
		}
	}

	// 检查是否需要自动刷新
	autoRefresh := form.Value["auto_refresh"]
	if len(autoRefresh) > 0 && autoRefresh[0] == "true" {
		if err := h.repoService.RefreshMetadata(ctx, repoName); err != nil {
			response.Status = "partial_success"
		} else {
			response.Status = "success"
		}
	} else {
		if response.Failed == 0 {
			response.Status = "success"
		} else if response.Success > 0 {
			response.Status = "partial_success"
		} else {
			response.Status = "failed"
		}
	}

	ctx.Response.Header.Set("Content-Type", "application/json")
	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

func (h *API) uploadSingleFile(ctx *fasthttp.RequestCtx, repoName string, fileHeader *multipart.FileHeader) types.BatchUploadResult {
	result := types.BatchUploadResult{
		Filename: fileHeader.Filename,
	}

	// 验证文件类型
	if !strings.HasSuffix(fileHeader.Filename, ".rpm") && !strings.HasSuffix(fileHeader.Filename, ".deb") {
		result.Status = "failed"
		result.Error = "Unsupported file type"
		return result
	}

	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("Failed to open file: %v", err)
		return result
	}
	defer file.Close()

	// 上传文件
	if err := h.repoService.UploadPackage(ctx, repoName, fileHeader.Filename, file); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("Upload failed: %v", err)
		return result
	}

	result.Status = "success"
	return result
}

func (h *API) CreateRepo(ctx *fasthttp.RequestCtx) {
	rt := &types.RepoTable{}
	if err := rt.UnmarshalJSON(ctx.PostBody()); err != nil {
		h.sendJSONError(ctx, "Invalid JSON format", fasthttp.StatusBadRequest)
		return
	}

	if rt.Name == "" {
		h.sendJSONError(ctx, "Repository name is required", fasthttp.StatusBadRequest)
		return
	}

	// 新增：验证仓库类型
	if rt.Type == "" {
		h.sendJSONError(ctx, "Repository type is required", fasthttp.StatusBadRequest)
		return
	}

	// 验证仓库类型是否有效
	validTypes := []string{"rpm", "deb", "files"}
	isValidType := false
	for _, validType := range validTypes {
		if rt.Type == validType {
			isValidType = true
			break
		}
	}
	if !isValidType {
		h.sendJSONError(ctx, "Invalid repository type. Must be one of: rpm, deb, files", fasthttp.StatusBadRequest)
		return
	}

	// 构建完整路径
	repoPath := rt.Name
	if rt.Path != "" {
		// 清理路径，移除前后斜杠
		cleanPath := strings.Trim(rt.Path, "/")
		if cleanPath != "" {
			repoPath = filepath.Join(rt.Name, cleanPath)
		}
	}

	// 验证路径格式
	if !utils.IsValidRepoName(repoPath) {
		h.sendJSONError(ctx, "Invalid repository path. Use only letters, numbers, hyphens, underscores and forward slashes", fasthttp.StatusBadRequest)
		return
	}

	err := h.repoService.CreateRepo(ctx, repoPath, rt.Type)
	if err != nil {
		log.Logger.Debugf("Create repository failed for %s (type: %s): %v", repoPath, rt.Type, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to create repository: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, fmt.Sprintf("Repository created successfully (type: %s)", rt.Type))
}

func (h *API) Upload(ctx *fasthttp.RequestCtx) {
	// 解析路径: /repo/{repoPath}/upload，支持多层路径
	path := string(ctx.Path())

	// 移除 /repo/ 前缀和 /upload 后缀
	if !strings.HasPrefix(path, "/repo/") || !strings.HasSuffix(path, "/upload") {
		h.sendJSONError(ctx, "Invalid upload path", fasthttp.StatusBadRequest)
		return
	}

	// 提取仓库路径
	repoPath := strings.TrimPrefix(path, "/repo/")
	repoPath = strings.TrimSuffix(repoPath, "/upload")

	if repoPath == "" {
		h.sendJSONError(ctx, "Repository path is required", fasthttp.StatusBadRequest)
		return
	}

	// 获取上传的文件
	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		h.sendJSONError(ctx, "No file uploaded", fasthttp.StatusBadRequest)
		return
	}

	// 新增：获取仓库类型并验证文件类型
	repoType, err := h.repoService.GetRepoType(ctx, repoPath)
	if err != nil {
		log.Logger.Debugf("Failed to get repository type for %s: %v", repoPath, err)
		h.sendJSONError(ctx, "Repository not found", fasthttp.StatusNotFound)
		return
	}

	// 验证文件类型与仓库类型的匹配
	if !utils.ValidateFileTypeForRepo(fileHeader.Filename, repoType) {
		h.sendJSONError(ctx, utils.GetFileTypeErrorMessage(repoType), fasthttp.StatusBadRequest)
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		h.sendJSONError(ctx, "Failed to open uploaded file", fasthttp.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 上传文件到指定路径
	err = h.repoService.UploadPackage(ctx, repoPath, fileHeader.Filename, file)
	if err != nil {
		log.Logger.Debugf("Upload failed for repo %s, file %s: %v", repoPath, fileHeader.Filename, err)
		h.sendJSONError(ctx, fmt.Sprintf("Upload failed: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, "Package uploaded successfully")
}

func (h *API) GetPackageChecksum(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	// 使用字符串操作解析路径
	if !strings.HasPrefix(path, "/repo/") || !strings.Contains(path, "/checksum/") {
		h.sendJSONError(ctx, "Invalid checksum path format", fasthttp.StatusBadRequest)
		return
	}

	// 移除 /repo/ 前缀
	pathWithoutPrefix := strings.TrimPrefix(path, "/repo/")

	// 查找 /checksum/ 的位置
	checksumIndex := strings.LastIndex(pathWithoutPrefix, "/checksum/")
	if checksumIndex == -1 {
		h.sendJSONError(ctx, "Invalid checksum path format", fasthttp.StatusBadRequest)
		return
	}

	// 提取仓库名和文件名
	repoName := pathWithoutPrefix[:checksumIndex]
	filename := pathWithoutPrefix[checksumIndex+10:] // 10 是 "/checksum/" 的长度

	if repoName == "" || filename == "" {
		h.sendJSONError(ctx, "Invalid checksum path format", fasthttp.StatusBadRequest)
		return
	}

	log.Logger.Debugf("🔍 Getting checksum for: repo=%s, file=%s", repoName, filename)

	// 调用服务层获取校验和
	checksum, err := h.repoService.GetPackageChecksum(ctx, repoName, filename)
	if err != nil {
		log.Logger.Debugf("❌ Failed to get checksum: repo=%s, file=%s, error=%v", repoName, filename, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to get checksum: %v", err), fasthttp.StatusNotFound)
		return
	}

	log.Logger.Debugf("✅ Found checksum for %s: %s", filename, checksum)

	// 构建响应
	response := &types.PackageChecksum{
		Status: types.Status{
			Status:  "success",
			Message: "Checksum retrieved successfully",
			Code:    fasthttp.StatusOK,
		},
		Filename: filename,
		SHA256:   checksum,
		Repo:     repoName,
	}

	h.sendJSONResponse(ctx, response, fasthttp.StatusOK)
}

func (h *API) DownloadPackage(ctx *fasthttp.RequestCtx, repoName, filename string) {
	log.Logger.Debugf("🔍 Download request: repo=%s, file=%s", repoName, filename)

	// 根据文件扩展名确定包类型
	var contentType string
	if strings.HasSuffix(filename, ".rpm") {
		contentType = "application/x-rpm"
		metrics.IncrementDownloads()
	} else if strings.HasSuffix(filename, ".deb") {
		contentType = "application/vnd.debian.binary-package"
		metrics.IncrementDownloads()
	} else {
		ctx.Error("Unsupported package type", fasthttp.StatusBadRequest)
		return
	}

	reader, err := h.repoService.DownloadPackage(ctx, repoName, filename)
	if err != nil {
		log.Logger.Debugf("❌ Package not found: repo=%s, file=%s, error=%v", repoName, filename, err)
		ctx.Error("Package not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	log.Logger.Debugf("✅ Serving package: %s/%s", repoName, filename)

	ctx.Response.Header.Set("Content-Type", contentType)
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	ctx.Response.Header.Set("Cache-Control", "public, max-age=3600")

	ctx.SetBodyStream(reader, -1)
}

func handleDirectoryListing(ctx *fasthttp.RequestCtx, repoName, subPath, fullPath string) {
	log.Logger.Debugf("🔍 Directory listing: repo=%s, subPath=%s, fullPath=%s", repoName, subPath, fullPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		log.Logger.Debugf("❌ Cannot read directory %s: %v", fullPath, err)
		ctx.Error("Cannot read directory", fasthttp.StatusInternalServerError)
		return
	}

	log.Logger.Debugf("📁 Found %d entries in directory %s", len(entries), fullPath)
	for _, entry := range entries {
		log.Logger.Debugf("  - %s (dir: %v)", entry.Name(), entry.IsDir())
	}

	// 生成 HTML 目录列表
	html := utils.GenerateDirectoryHTML(repoName, subPath, entries)

	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

func handleRepoEndpoints(ctx *fasthttp.RequestCtx, method, root, path string, patterns map[string]*regexp.Regexp, h *API) bool {
	log.Logger.Debugf("🔍 handleRepoEndpoints: method=%s, path=%s", method, path)

	// 特殊处理 /files/ 路径
	if strings.Contains(path, "/files/") {
		// 匹配 /repo/{repoPath}/files/{filePath}
		filesRegex := regexp.MustCompile(`^/repo/([^/]+(?:/[^/]+)*)/files/?(.*)$`)
		if matches := filesRegex.FindStringSubmatch(path); matches != nil {
			repoPath := matches[1] // 例如: "oe-release/x86_64"
			filePath := matches[2] // 例如: "repodata/repomd.xml"

			log.Logger.Debugf("✅ Matched files pattern: repo='%s', file='%s'", repoPath, filePath)

			if method == "GET" {
				handleRepoFiles(ctx, root, repoPath, filePath)
				return true
			}
		}
	}

	// 按优先级顺序检查模式
	priorityPatterns := []string{
		"upload", "refresh", "checksum", "download_rpm", "download_deb",
		"metadata", "deb_metadata", "repo_files", "repo_browse", "repo_info",
	}

	for _, patternName := range priorityPatterns {
		regex := patterns[patternName]
		if matches := regex.FindStringSubmatch(path); matches != nil {
			log.Logger.Debugf("✅ Matched pattern: %s for path: %s, matches: %v", patternName, path, matches)

			switch patternName {
			case "download_rpm", "download_deb":
				if method == "GET" {
					h.DownloadPackage(ctx, matches[1], matches[2])
					return true
				}
			case "metadata", "deb_metadata":
				if method == "GET" {
					h.ServeMetadata(ctx, matches[1], matches[2])
					return true
				}
			case "upload":
				if method == "POST" {
					h.Upload(ctx)
					return true
				}
			case "refresh":
				if method == "POST" {
					h.RefreshRepo(ctx)
					return true
				}
			case "checksum":
				if method == "GET" {
					h.GetPackageChecksum(ctx)
					return true
				}
			case "repo_files":
				if method == "GET" {
					log.Logger.Debugf("Handling repo_files: repo=%s, path=%s", matches[1], matches[2])
					handleRepoFiles(ctx, h.config.StoragePath, matches[1], matches[2])
					return true
				}
			case "repo_browse":
				if method == "GET" {
					log.Logger.Debugf("Handling repo_browse: repo=%s, path=%s", matches[1], matches[2])
					h.handleRepoBrowse(ctx, patterns["repo_browse"])
					return true
				}
			case "repo_info":
				// 确保不是特殊端点
				if !strings.Contains(matches[1], "/files") &&
					!strings.Contains(matches[1], "/browse") &&
					!strings.Contains(matches[1], "/upload") &&
					!strings.Contains(matches[1], "/refresh") {
					if method == "GET" {
						h.GetRepoInfo(ctx, matches[1])
						return true
					} else if method == "DELETE" {
						h.DeleteRepo(ctx, matches[1])
						return true
					}
				}
			}
		}
	}
	return false
}

func createExternalStaticHandler(root string) fasthttp.RequestHandler {
	fs := &fasthttp.FS{
		Root:               root,
		IndexNames:         []string{"index.html"},
		GenerateIndexPages: false,
		AcceptByteRange:    true,
	}
	return fs.NewRequestHandler()
}

func handleRepoFiles(ctx *fasthttp.RequestCtx, root, repoName, filePath string) {
	log.Logger.Debugf("handleRepoFiles called: repo=%s, path='%s'", repoName, filePath)

	// 构建完整路径
	var fullPath string
	if filePath == "" {
		fullPath = fmt.Sprintf("%s/%s", root, repoName)
	} else {
		fullPath = fmt.Sprintf("%s/%s/%s", root, repoName, filePath)
	}

	log.Logger.Debugf("Full path: %s", fullPath)

	// 检查路径是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		log.Logger.Debugf("Path not found: %s, error: %v", fullPath, err)
		ctx.Error("Path not found", fasthttp.StatusNotFound)
		return
	}

	if info.IsDir() {
		log.Logger.Debugf("Serving directory listing for: %s", fullPath)
		handleDirectoryListing(ctx, repoName, filePath, fullPath)
	} else {
		log.Logger.Debugf("Serving file: %s", fullPath)
		// 对于元数据文件，设置正确的 Content-Type
		if strings.Contains(filePath, "repodata/") {
			filename := filepath.Base(filePath)
			contentType := utils.GetContentType(filename)
			ctx.Response.Header.Set("Content-Type", contentType)
			ctx.Response.Header.Set("Cache-Control", "public, max-age=300")
		}
		fasthttp.ServeFile(ctx, fullPath)
	}
}

func createEmbeddedStaticHandler() fasthttp.RequestHandler {
	// 调试：列出所有嵌入的文件
	log.Logger.Info("=== Embedded files debug ===")
	err := fs.WalkDir(assets.StaticFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Logger.Debugf("Walk error: %v", err)
			return err
		}
		if d.IsDir() {
			log.Logger.Debugf("DIR:  %s/", path)
		} else {
			log.Logger.Debugf("FILE: %s", path)
		}
		return nil
	})
	if err != nil {
		log.Logger.Debugf("Failed to walk embedded files: %v", err)
	}
	log.Logger.Info("=== End embedded files debug ===")

	return func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())
		log.Logger.Debugf("🔍 Requested static path: %s", path)

		// 正确处理路径
		filePath := strings.TrimPrefix(path, "/static/")
		// 移除前导斜杠（如果有的话）
		filePath = strings.TrimPrefix(filePath, "/")

		// 如果是根路径，默认为 index.html
		if filePath == "" {
			filePath = "index.html"
		}

		// 构建完整路径，确保没有双斜杠
		fullPath := "static/" + filePath
		log.Logger.Debugf("🔍 Looking for embedded file: %s", fullPath)

		data, err := assets.StaticFiles.ReadFile(fullPath)
		if err != nil {
			log.Logger.Debugf("❌ File not found: %s, error: %v", fullPath, err)
			ctx.Error("File not found", fasthttp.StatusNotFound)
			return
		}

		log.Logger.Debugf("✅ Found file at: %s", fullPath)

		contentType := utils.GetStaticContentType(filePath)
		ctx.Response.Header.Set("Content-Type", contentType)
		ctx.SetBody(data)
		log.Logger.Debugf("✅ Served file: %s (%d bytes, %s)", filePath, len(data), contentType)
	}
}

func createRepoHandler(root string) fasthttp.RequestHandler {
	fs := &fasthttp.FS{
		Root:               root,
		GenerateIndexPages: true, // 启用目录索引
		AcceptByteRange:    true,
	}
	return fs.NewRequestHandler()
}

func handleRootPath(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(utils.HandleRootPath())
}

func handleAPIEndpoints(ctx *fasthttp.RequestCtx, method, path string, h *API) bool {
	switch path {
	case "/health":
		if method == "GET" {
			h.Health(ctx)
			return true
		}
	case "/ready":
		if method == "GET" {
			h.Ready(ctx)
			return true
		}
	case "/metrics":
		if method == "GET" {
			h.Metrics(ctx)
			return true
		}
	case "/repos":
		if method == "GET" {
			h.ListRepos(ctx)
			return true
		} else if method == "POST" {
			h.CreateRepo(ctx)
			return true
		}
	}
	return false
}

func handleWebStatic(ctx *fasthttp.RequestCtx, staticHandler fasthttp.RequestHandler) {
	originalPath := ctx.Path()
	newPath := strings.TrimPrefix(string(originalPath), "/static")
	ctx.URI().SetPath(newPath)
	staticHandler(ctx)
	ctx.URI().SetPath(string(originalPath))
}

func handleRepoListPage(ctx *fasthttp.RequestCtx, h *API) {
	// 获取仓库列表
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Logger.Debugf("Failed to list repositories: %v", err)
		ctx.Error("Failed to load repositories", fasthttp.StatusInternalServerError)
		return
	}

	// 生成包含类型信息的 HTML 页面
	html := utils.GenerateRepoListHTMLWithTypes(repos, h.repoService.GetRepoType)
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

func handleDirectoryListingNew(ctx *fasthttp.RequestCtx, repoPath, fullPath string) {
	log.Logger.Debugf("🔍 Direct directory listing: repoPath=%s, fullPath=%s", repoPath, fullPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		log.Logger.Debugf("❌ Cannot read directory %s: %v", fullPath, err)
		ctx.Error("Cannot read directory", fasthttp.StatusInternalServerError)
		return
	}

	log.Logger.Debugf("📁 Found %d entries in directory %s", len(entries), fullPath)

	// 生成新的 HTML 目录列表
	html := utils.GenerateDirectoryHTMLNew(repoPath, entries)

	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

func handleDirectBrowse(ctx *fasthttp.RequestCtx, path string, h *API) bool {
	// 排除特殊路径
	if path == "/" || strings.HasPrefix(path, "/static/") || 
	   strings.HasPrefix(path, "/health") ||
	   strings.HasPrefix(path, "/ready") || strings.HasPrefix(path, "/metrics") ||
	   strings.HasPrefix(path, "/repos") {
		return false
	}

	// 排除所有 /repo/ 开头的路径，这些由原有逻辑处理
	if strings.HasPrefix(path, "/repo/") {
		return false
	}

	// 移除前导斜杠
	cleanPath := strings.TrimPrefix(path, "/")
	if cleanPath == "" {
		return false
	}

	log.Logger.Debugf("🔍 Direct browse attempt: cleanPath=%s", cleanPath)

	// 检查是否是仓库路径
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Logger.Debugf("❌ Failed to get repos for path matching: %v", err)
		return false
	}

	// 查找匹配的仓库路径
	var matchedRepo string
	var remainingPath string
	
	for _, repo := range repos {
		if cleanPath == repo {
			matchedRepo = repo
			remainingPath = ""
			break
		} else if strings.HasPrefix(cleanPath, repo+"/") {
			matchedRepo = repo
			remainingPath = strings.TrimPrefix(cleanPath, repo+"/")
			break
		}
	}

	if matchedRepo == "" {
		log.Logger.Debugf("❌ No matching repository found for path: %s", cleanPath)
		return false
	}

	// 获取仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, matchedRepo)
	if err != nil {
		log.Logger.Debugf("❌ Failed to get repo type for %s: %v", matchedRepo, err)
		repoType = "unknown"
	}

	log.Logger.Debugf("✅ Matched repository: %s (type: %s), remaining path: %s", matchedRepo, repoType, remainingPath)

	// 根据仓库类型选择存储方式
	if utils.IsObjectStorage(repoType) {
		// 对象存储：使用仓库服务
		return h.handleObjectStorageBrowse(ctx, matchedRepo, remainingPath)
	} else {
		// 本地存储：使用文件系统
		return h.handleLocalStorageBrowse(ctx, matchedRepo, remainingPath, cleanPath)
	}
}