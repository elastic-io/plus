package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"plus/assets"
	"plus/internal/config"
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

	log.Printf("🔄 Refreshing repository: %s", repoPath)

	// 检查仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, repoPath)
	if err != nil {
		log.Printf("Failed to get repository type for %s: %v", repoPath, err)
		h.sendJSONError(ctx, "Repository not found", fasthttp.StatusNotFound)
		return
	}

	// Files 类型仓库不需要刷新元数据
	if repoType == "files" {
		log.Printf("Repository %s is files type, no metadata refresh needed", repoPath)
		h.sendJSONError(ctx, "Files repositories do not require metadata refresh", fasthttp.StatusBadRequest)
		return
	}

	err = h.repoService.RefreshMetadata(ctx, repoPath)
	if err != nil {
		log.Printf("Refresh metadata failed for repo %s: %v", repoPath, err)
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
		log.Printf("Failed to encode JSON response: %v", err)
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
		log.Printf("Failed to encode JSON error response: %v", err)
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
		log.Println("Using external static files (development mode)")
	} else {
		// 生产模式：使用嵌入文件
		staticHandler = createEmbeddedStaticHandler()
		log.Println("Using embedded static files (production mode)")
	}

	repoHandler := createRepoHandler(h.config.StoragePath)

	return middleware.CORSMiddleware(
		middleware.LoggingMiddleware(
			metricsMiddleware(
				func(ctx *fasthttp.RequestCtx) {
					path := string(ctx.Path())
					method := string(ctx.Method())

					log.Printf("🔍 Request: %s %s", method, path)

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
					if method == "GET" && handleDirectBrowse(ctx, path, h) {
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

func (h *API) isObjectStorage(repoType string) bool {
	switch repoType {
	case "files":
		return true  // files 类型使用对象存储
	case "rpm", "deb":
		return false // rpm 和 deb 类型使用本地存储
	default:
		return false // 默认本地存储
	}
}
/*
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

    log.Printf("🔍 Direct browse attempt: cleanPath=%s", cleanPath)

    // 检查是否是仓库路径
    repos, err := h.repoService.ListRepos(ctx)
    if err != nil {
        log.Printf("❌ Failed to get repos for path matching: %v", err)
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
        log.Printf("❌ No matching repository found for path: %s", cleanPath)
        return false
    }

    // 新增：如果是精确的仓库路径且Accept头包含JSON，返回API响应
    if remainingPath == "" {
        accept := string(ctx.Request.Header.Peek("Accept"))
        if strings.Contains(accept, "application/json") {
            log.Printf("🔍 Direct repo info API: repo=%s", matchedRepo)
            h.GetRepoInfo(ctx, matchedRepo)
            return true
        }
    }

    // 获取仓库类型
    repoType, err := h.repoService.GetRepoType(ctx, matchedRepo)
    if err != nil {
        log.Printf("❌ Failed to get repo type for %s: %v", matchedRepo, err)
        repoType = "unknown"
    }

    log.Printf("✅ Matched repository: %s (type: %s), remaining path: %s", matchedRepo, repoType, remainingPath)

    // 根据仓库类型选择存储方式
    if h.isObjectStorage(repoType) {
        // 对象存储：使用仓库服务
        return h.handleObjectStorageBrowse(ctx, matchedRepo, remainingPath)
    } else {
        // 本地存储：使用文件系统
        return h.handleLocalStorageBrowse(ctx, matchedRepo, remainingPath, cleanPath)
    }
}
*/

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

	log.Printf("🔍 Direct browse attempt: cleanPath=%s", cleanPath)

	// 检查是否是仓库路径
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Printf("❌ Failed to get repos for path matching: %v", err)
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
		log.Printf("❌ No matching repository found for path: %s", cleanPath)
		return false
	}

	// 获取仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, matchedRepo)
	if err != nil {
		log.Printf("❌ Failed to get repo type for %s: %v", matchedRepo, err)
		repoType = "unknown"
	}

	log.Printf("✅ Matched repository: %s (type: %s), remaining path: %s", matchedRepo, repoType, remainingPath)

	// 根据仓库类型选择存储方式
	if h.isObjectStorage(repoType) {
		// 对象存储：使用仓库服务
		return h.handleObjectStorageBrowse(ctx, matchedRepo, remainingPath)
	} else {
		// 本地存储：使用文件系统
		return h.handleLocalStorageBrowse(ctx, matchedRepo, remainingPath, cleanPath)
	}
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
		log.Printf("❌ Storage path not found: %s, error: %v", storagePath, err)
		return false
	}

	log.Printf("✅ Local storage browse: repo=%s, path=%s, storage=%s", repoName, remainingPath, storagePath)

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
	log.Printf("🔍 Object storage repository browse: repo=%s", repoName)

	// 使用仓库服务获取包列表
	packages, err := h.repoService.ListPackages(ctx, repoName)
	if err != nil {
		log.Printf("❌ Failed to list packages for repo %s: %v", repoName, err)
		ctx.Error("Failed to access repository", fasthttp.StatusInternalServerError)
		return
	}

	// 构建简单的文件列表HTML
	html := h.generateObjectStorageRepoHTML(repoName, packages)
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

// 新增：处理对象存储文件访问
func (h *API) handleObjectStorageFile(ctx *fasthttp.RequestCtx, repoName, filePath string) {
	log.Printf("🔍 Object storage file access: repo=%s, path=%s", repoName, filePath)

	// 尝试下载文件
	reader, err := h.repoService.DownloadPackage(ctx, repoName, filePath)
	if err != nil {
		log.Printf("❌ File not found: repo=%s, path=%s, error=%v", repoName, filePath, err)
		ctx.Error("File not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	// 设置适当的 Content-Type
	contentType := h.getContentTypeByExtension(filePath)
	ctx.Response.Header.Set("Content-Type", contentType)
	
	// 设置文件名
	filename := filepath.Base(filePath)
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	
	ctx.SetBodyStream(reader, -1)
}

// 新增：生成对象存储仓库HTML
func (h *API) generateObjectStorageRepoHTML(repoName string, packages []types.PackageInfo) string {
	var html strings.Builder

	currentPath := "/" + repoName

	html.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Repository: %s</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { border-bottom: 1px solid #ccc; }
        .file-list { list-style: none; padding: 0; }
        .file-list li { padding: 5px 0; }
        .file-list a { text-decoration: none; color: #0066cc; }
        .file-list a:hover { text-decoration: underline; }
        .parent { color: #999; }
        .file-info { display: flex; justify-content: space-between; align-items: center; }
        .file-name { flex: 1; }
        .file-meta { color: #666; font-size: 0.9em; }
    </style>
</head>
<body>
    <h1>📁 Repository: %s</h1>
    <ul class="file-list">`, repoName, repoName))

	// 父目录链接
	html.WriteString(`        <li><a href="/repo/" class="parent">../</a></li>`)

	// 添加文件
	for _, pkg := range packages {
		linkPath := fmt.Sprintf("%s/%s", currentPath, pkg.Name)
		size := formatFileSize(pkg.Size)
		icon := getFileIcon(pkg.Name)

		html.WriteString(fmt.Sprintf(`        <li>
			<div class="file-info">
				<div class="file-name"><a href="%s">%s %s</a></div>
				<div class="file-meta">%s</div>
			</div>
		</li>`, linkPath, icon, pkg.Name, size))
	}

	html.WriteString(`    </ul>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String()
}

// 新增：获取文件类型
func (h *API) getContentTypeByExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".rpm":
		return "application/x-rpm"
	case ".deb":
		return "application/vnd.debian.binary-package"
	case ".gz":
		return "application/gzip"
	case ".zip":
		return "application/zip"
	case ".sql":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func handleDirectoryListingNew(ctx *fasthttp.RequestCtx, repoPath, fullPath string) {
	log.Printf("🔍 Direct directory listing: repoPath=%s, fullPath=%s", repoPath, fullPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		log.Printf("❌ Cannot read directory %s: %v", fullPath, err)
		ctx.Error("Cannot read directory", fasthttp.StatusInternalServerError)
		return
	}

	log.Printf("📁 Found %d entries in directory %s", len(entries), fullPath)

	// 生成新的 HTML 目录列表
	html := generateDirectoryHTMLNew(repoPath, entries)

	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

func generateDirectoryHTMLNew(repoPath string, entries []os.DirEntry) string {
	var html strings.Builder

	currentPath := "/" + repoPath

	html.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Index of %s</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { border-bottom: 1px solid #ccc; }
        .file-list { list-style: none; padding: 0; }
        .file-list li { padding: 5px 0; }
        .file-list a { text-decoration: none; color: #0066cc; }
        .file-list a:hover { text-decoration: underline; }
        .dir { font-weight: bold; }
        .size { color: #666; margin-left: 20px; }
        .parent { color: #999; }
        .file-info { display: flex; justify-content: space-between; align-items: center; }
        .file-name { flex: 1; }
        .file-meta { color: #666; font-size: 0.9em; }
    </style>
</head>
<body>
    <h1>📁 Repository: %s</h1>
    <ul class="file-list">`, currentPath, repoPath))

	// 父目录链接
	var parentPath string
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	if len(parts) > 1 {
		// 返回上一级
		parentParts := parts[:len(parts)-1]
		parentPath = "/" + strings.Join(parentParts, "/")
	} else {
		// 返回仓库列表
		parentPath = "/repo/"
	}

	html.WriteString(fmt.Sprintf(`        <li><a href="%s" class="parent">../</a></li>`, parentPath))

	// 添加文件和目录
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()

		if entry.IsDir() {
			linkPath := fmt.Sprintf("%s/%s", currentPath, name)
			html.WriteString(fmt.Sprintf(`        <li>
				<div class="file-info">
					<div class="file-name"><a href="%s" class="dir">📁 %s/</a></div>
					<div class="file-meta">Directory</div>
				</div>
			</li>`, linkPath, name))
		} else {
			linkPath := fmt.Sprintf("%s/%s", currentPath, name)
			size := formatFileSize(info.Size())
			icon := getFileIcon(name)
			modTime := info.ModTime().Format("2006-01-02 15:04:05")

			html.WriteString(fmt.Sprintf(`        <li>
				<div class="file-info">
					<div class="file-name"><a href="%s">%s %s</a></div>
					<div class="file-meta">%s | %s</div>
				</div>
			</li>`, linkPath, icon, name, size, modTime))
		}
	}

	html.WriteString(`    </ul>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String()
}

// 修改 generateRepoListHTML 函数，需要传入仓库类型信息
func (h *API) generateRepoListHTMLWithTypes(repos []string) string {
	var html strings.Builder

	html.WriteString(`<!DOCTYPE html>
<html>
<head>
    <title>Repository List</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { border-bottom: 1px solid #ccc; color: #333; }
        .repo-list { list-style: none; padding: 0; }
        .repo-list li { padding: 10px 0; border-bottom: 1px solid #eee; }
        .repo-list a { text-decoration: none; color: #0066cc; font-size: 16px; }
        .repo-list a:hover { text-decoration: underline; }
        .repo-item { display: flex; justify-content: space-between; align-items: center; }
        .repo-name { font-weight: bold; }
        .repo-links { font-size: 14px; }
        .repo-links a { margin-left: 10px; color: #666; }
        .repo-links button { margin-left: 10px; padding: 2px 8px; font-size: 12px; }
        .back-link { margin-bottom: 20px; }
        .back-link a { color: #999; }
    </style>
</head>
<body>
    <div class="back-link">
        <a href="/">← Back to Home</a>
    </div>
    <h1>📁 All Repositories</h1>
    <ul class="repo-list">`)
	
	if len(repos) == 0 {
		html.WriteString(`        <li>No repositories found.</li>`)
	} else {
		for _, repo := range repos {
			// 获取仓库类型
			repoType, err := h.repoService.GetRepoType(context.Background(), repo)
			if err != nil {
				repoType = "unknown"
			}
			
			// 根据类型决定是否显示 refresh 按钮
			refreshButton := ""
			if repoType != "files" {
				refreshButton = fmt.Sprintf(`<button onclick="refreshRepo('%s')">Refresh</button>`, repo)
			}
			
			typeIcon := h.getRepoTypeIcon(repoType)
			
			html.WriteString(fmt.Sprintf(`
        <li>
            <div class="repo-item">
                <div>
                    <a href="/%s" class="repo-name">%s %s (%s)</a>
                </div>
                <div class="repo-links">
                    <a href="/%s">Browse</a>
                    <a href="/repo/%s">Info</a>
                    %s
                </div>
            </div>
        </li>`, repo, typeIcon, repo, repoType, repo, repo, refreshButton))
		}
	}

	html.WriteString(`    </ul>
    <script>
        function refreshRepo(repoName) {
            if (confirm('Refresh metadata for repository: ' + repoName + '?')) {
                fetch('/repo/' + encodeURIComponent(repoName) + '/refresh', {
                    method: 'POST'
                })
                .then(response => response.json())
                .then(data => {
                    if (data.status === 'success') {
                        alert('Repository refreshed successfully');
                    } else {
                        alert('Refresh failed: ' + (data.message || 'Unknown error'));
                    }
                })
                .catch(error => {
                    alert('Refresh failed: ' + error.message);
                });
            }
        }
    </script>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String()
}

// 修改 handleRepoListPage 使用新的函数
func handleRepoListPage(ctx *fasthttp.RequestCtx, h *API) {
	// 获取仓库列表
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Printf("Failed to list repositories: %v", err)
		ctx.Error("Failed to load repositories", fasthttp.StatusInternalServerError)
		return
	}

	// 生成包含类型信息的 HTML 页面
	html := h.generateRepoListHTMLWithTypes(repos)
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}


// 生成仓库列表 HTML
func generateRepoListHTML(repos []string) string {
	var html strings.Builder

	html.WriteString(`<!DOCTYPE html>
<html>
<head>
    <title>Repository List</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { border-bottom: 1px solid #ccc; color: #333; }
        .repo-list { list-style: none; padding: 0; }
        .repo-list li { padding: 10px 0; border-bottom: 1px solid #eee; }
        .repo-list a { text-decoration: none; color: #0066cc; font-size: 16px; }
        .repo-list a:hover { text-decoration: underline; }
        .repo-item { display: flex; justify-content: space-between; align-items: center; }
        .repo-name { font-weight: bold; }
        .repo-links { font-size: 14px; }
        .repo-links a { margin-left: 10px; color: #666; }
        .back-link { margin-bottom: 20px; }
        .back-link a { color: #999; }
    </style>
</head>
<body>
    <div class="back-link">
        <a href="/">← Back to Home</a>
    </div>
    <h1>📁 All Repositories</h1>
    <ul class="repo-list">`)
	
	if len(repos) == 0 {
		html.WriteString(`        <li>No repositories found.</li>`)
	} else {
		for _, repo := range repos {
			html.WriteString(fmt.Sprintf(`
        <li>
            <div class="repo-item">
                <div>
                    <a href="/%s" class="repo-name">📁 %s</a>
                </div>
                <div class="repo-links">
                    <a href="/%s">Browse</a>
                    <a href="/repo/%s">Info</a>
                </div>
            </div>
        </li>`, repo, repo, repo, repo))
		}
	}

	html.WriteString(`    </ul>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String()
}

func getFileIcon(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".rpm":
		return "📦"
	case ".deb":
		return "📦"
	case ".xml":
		return "📄"
	case ".gz", ".xz":
		return "🗜️"
	case ".txt":
		return "📝"
	case ".json":
		return "🔧"
	case ".log":
		return "📋"
	default:
		return "📄"
	}
}

// Web UI 静态文件处理 (现代方式)
func handleWebStatic(ctx *fasthttp.RequestCtx, staticHandler fasthttp.RequestHandler) {
	originalPath := ctx.Path()
	newPath := strings.TrimPrefix(string(originalPath), "/static")
	ctx.URI().SetPath(newPath)
	staticHandler(ctx)
	ctx.URI().SetPath(string(originalPath))
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

// 根路径处理
func handleRootPath(ctx *fasthttp.RequestCtx) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Plus Artifacts Server</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; }
        .option { margin: 20px; padding: 20px; border: 1px solid #ddd; border-radius: 5px; display: inline-block; }
        .option a { text-decoration: none; color: #0066cc; font-size: 18px; }
        .option:hover { background-color: #f5f5f5; }
    </style>
</head>
<body>
    <h1>Plus Artifacts Server</h1>
    <p>Choose your preferred interface:</p>
    
    <div class="option">
        <a href="/static/">📱 Modern Web UI</a>
        <p>Feature-rich web interface for package management</p>
    </div>
    
    <div class="option">
        <a href="/repo/">📁 Browse Repositories</a>
        <p>Traditional file browser (nginx-style)</p>
    </div>
    
    <div class="option">
        <a href="/repos">🔧 API Endpoints</a>
        <p>JSON API for programmatic access</p>
    </div>
</body>
</html>`

	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

// API 端点处理
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

// 创建仓库文件处理器
func createRepoHandler(root string) fasthttp.RequestHandler {
	fs := &fasthttp.FS{
		Root:               root,
		GenerateIndexPages: true, // 启用目录索引
		AcceptByteRange:    true,
	}
	return fs.NewRequestHandler()
}

func metricsMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		metrics.IncrementRequests()
		metrics.IncrementActiveRequests()

		defer func() {
			metrics.DecrementActiveRequests()
			metrics.RecordResponseTime(time.Since(start))
		}()

		next(ctx)

		// 记录特定操作的指标
		path := string(ctx.Path())
		if strings.Contains(path, "/upload") {
			metrics.IncrementUploads()
		} else if strings.Contains(path, "/rpm/") || strings.Contains(path, "/deb/") {
			metrics.IncrementDownloads()
		}

		if ctx.Response.StatusCode() >= 400 {
			metrics.IncrementErrors()
		}
	}
}

func (h *API) ServeMetadata(ctx *fasthttp.RequestCtx, repoName, filename string) {
	reader, err := h.repoService.GetMetadata(ctx, repoName, filename)
	if err != nil {
		ctx.Error("Metadata not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	contentType := getContentType(filename)
	ctx.Response.Header.Set("Content-Type", contentType)
	ctx.Response.Header.Set("Cache-Control", "public, max-age=300")

	ctx.SetBodyStream(reader, -1)
}

func (h *API) GetRepoInfo(ctx *fasthttp.RequestCtx, repoName string) {
	packages, err := h.repoService.ListPackages(ctx, repoName)
	if err != nil {
		log.Printf("Get repo info failed for %s: %v", repoName, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to get repository info: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	// 新增：获取仓库类型
	repoType, err := h.repoService.GetRepoType(ctx, repoName)
	if err != nil {
		log.Printf("Failed to get repository type for %s: %v", repoName, err)
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

// 修改：构建仓库树结构，包含类型信息
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

// 修改 ListRepos 方法使用新的构建函数
func (h *API) ListRepos(ctx *fasthttp.RequestCtx) {
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Printf("List repositories failed: %v", err)
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

func (h *API) renderRepoTree(tree map[string]*types.TreeNode, level int) string {
	var html strings.Builder
	
	for name, node := range tree {
		if node.Type == "repo" {
			// 根据仓库类型显示不同图标
			typeIcon := h.getRepoTypeIcon(node.RepoType)
			html.WriteString(fmt.Sprintf(`
				<div class="repo-item" style="margin-left: %dpx;">
					<div class="repo-name">%s %s <span class="repo-type">(%s)</span> <span class="repo-path">(%s)</span></div>
					<div class="repo-actions">
						<button class="btn-refresh" onclick="repoManager.refreshRepository('%s')">
							Refresh Metadata
						</button>
						<button class="btn-info" onclick="repoManager.showRepositoryInfo('%s')">
							Info
						</button>
					</div>
				</div>`, level*20, typeIcon, name, node.RepoType, node.Path, node.Path, node.Path))
		} else if node.Type == "directory" && node.Children != nil {
			html.WriteString(fmt.Sprintf(`
				<div class="repo-directory" style="margin-left: %dpx;">
					<div class="directory-name">📁 %s/</div>
					%s
				</div>`, level*20, name, h.renderRepoTree(node.Children, level+1)))
		}
	}
	
	return html.String()
}

// 新增：根据仓库类型返回对应图标
func (h *API) getRepoTypeIcon(repoType string) string {
	switch repoType {
	case "rpm":
		return "📦" // RPM 包
	case "deb":
		return "📋" // DEB 包
	case "files":
		return "📁" // 文件
	default:
		return "❓" // 未知类型
	}
}

func (h *API) DeleteRepo(ctx *fasthttp.RequestCtx, repoName string) {
	err := h.repoService.DeleteRepo(ctx, repoName)
	if err != nil {
		log.Printf("Delete repository failed for %s: %v", repoName, err)
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
		log.Printf("Create repository failed for %s (type: %s): %v", repoPath, rt.Type, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to create repository: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, fmt.Sprintf("Repository created successfully (type: %s)", rt.Type))
}

// 构建仓库树结构
func buildRepoTree(repos []string) map[string]*types.TreeNode {
	tree := make(map[string]*types.TreeNode)

	for _, repo := range repos {
		parts := strings.Split(repo, "/")
		current := tree

		for i, part := range parts {
			if _, exists := current[part]; !exists {
				if i == len(parts)-1 {
					// 叶子节点，存储完整路径
					current[part] = &types.TreeNode{
						Type: "repo",
						Path: repo,
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

// 修改上传路径解析
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
		log.Printf("Failed to get repository type for %s: %v", repoPath, err)
		h.sendJSONError(ctx, "Repository not found", fasthttp.StatusNotFound)
		return
	}

	// 验证文件类型与仓库类型的匹配
	if !h.validateFileTypeForRepo(fileHeader.Filename, repoType) {
		h.sendJSONError(ctx, h.getFileTypeErrorMessage(repoType), fasthttp.StatusBadRequest)
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
		log.Printf("Upload failed for repo %s, file %s: %v", repoPath, fileHeader.Filename, err)
		h.sendJSONError(ctx, fmt.Sprintf("Upload failed: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, "Package uploaded successfully")
}

// 新增：验证文件类型与仓库类型的匹配
func (h *API) validateFileTypeForRepo(filename, repoType string) bool {
	filename = strings.ToLower(filename)
	
	switch repoType {
	case "rpm":
		return strings.HasSuffix(filename, ".rpm")
	case "deb":
		return strings.HasSuffix(filename, ".deb")
	case "files":
		return true // files 类型接受任何文件
	default:
		return false
	}
}

// 新增：获取文件类型错误消息
func (h *API) getFileTypeErrorMessage(repoType string) string {
	switch repoType {
	case "rpm":
		return "This RPM repository only accepts .rpm files"
	case "deb":
		return "This DEB repository only accepts .deb files"
	case "files":
		return "Invalid file type"
	default:
		return "Invalid file type for this repository"
	}
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

	log.Printf("🔍 Getting checksum for: repo=%s, file=%s", repoName, filename)

	// 验证文件名
	if !strings.HasSuffix(filename, ".rpm") {
		h.sendJSONError(ctx, "Only RPM files are supported", fasthttp.StatusBadRequest)
		return
	}

	// 调用服务层获取校验和
	checksum, err := h.repoService.GetPackageChecksum(ctx, repoName, filename)
	if err != nil {
		log.Printf("❌ Failed to get checksum: repo=%s, file=%s, error=%v", repoName, filename, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to get checksum: %v", err), fasthttp.StatusNotFound)
		return
	}

	log.Printf("✅ Found checksum for %s: %s", filename, checksum)

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

func getContentType(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".xml"):
		return "application/xml"
	case strings.HasSuffix(filename, ".xml.gz"):
		return "application/gzip"
	case strings.HasSuffix(filename, ".sqlite"):
		return "application/x-sqlite3"
	default:
		return "application/octet-stream"
	}
}

// 添加缓存头函数
func setCacheHeaders(ctx *fasthttp.RequestCtx, filename string) {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".rpm", ".deb":
		// 包文件长期缓存
		ctx.Response.Header.Set("Cache-Control", "public, max-age=86400")
	case ".xml", ".gz", ".xz":
		// 元数据文件短期缓存
		ctx.Response.Header.Set("Cache-Control", "public, max-age=300")
	default:
		ctx.Response.Header.Set("Cache-Control", "public, max-age=1800")
	}
}

// 格式化文件大小
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func createEmbeddedStaticHandler() fasthttp.RequestHandler {
	// 调试：列出所有嵌入的文件
	log.Println("=== Embedded files debug ===")
	err := fs.WalkDir(assets.StaticFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("Walk error: %v", err)
			return err
		}
		if d.IsDir() {
			log.Printf("DIR:  %s/", path)
		} else {
			log.Printf("FILE: %s", path)
		}
		return nil
	})
	if err != nil {
		log.Printf("Failed to walk embedded files: %v", err)
	}
	log.Println("=== End embedded files debug ===")

	return func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())
		log.Printf("🔍 Requested static path: %s", path)

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
		log.Printf("🔍 Looking for embedded file: %s", fullPath)

		data, err := assets.StaticFiles.ReadFile(fullPath)
		if err != nil {
			log.Printf("❌ File not found: %s, error: %v", fullPath, err)
			ctx.Error("File not found", fasthttp.StatusNotFound)
			return
		}

		log.Printf("✅ Found file at: %s", fullPath)

		contentType := getStaticContentType(filePath)
		ctx.Response.Header.Set("Content-Type", contentType)
		ctx.SetBody(data)
		log.Printf("✅ Served file: %s (%d bytes, %s)", filePath, len(data), contentType)
	}
}

func getStaticContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

// 创建外部静态文件处理器（开发模式）
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
	log.Printf("handleRepoFiles called: repo=%s, path='%s'", repoName, filePath)

	// 构建完整路径
	var fullPath string
	if filePath == "" {
		fullPath = fmt.Sprintf("%s/%s", root, repoName)
	} else {
		fullPath = fmt.Sprintf("%s/%s/%s", root, repoName, filePath)
	}

	log.Printf("Full path: %s", fullPath)

	// 检查路径是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		log.Printf("Path not found: %s, error: %v", fullPath, err)
		ctx.Error("Path not found", fasthttp.StatusNotFound)
		return
	}

	if info.IsDir() {
		log.Printf("Serving directory listing for: %s", fullPath)
		handleDirectoryListing(ctx, repoName, filePath, fullPath)
	} else {
		log.Printf("Serving file: %s", fullPath)
		// 对于元数据文件，设置正确的 Content-Type
		if strings.Contains(filePath, "repodata/") {
			filename := filepath.Base(filePath)
			contentType := getContentType(filename)
			ctx.Response.Header.Set("Content-Type", contentType)
			ctx.Response.Header.Set("Cache-Control", "public, max-age=300")
		}
		fasthttp.ServeFile(ctx, fullPath)
	}
}

func generateDirectoryHTML(repoName, subPath string, entries []os.DirEntry) string {
	var html strings.Builder

	currentPath := fmt.Sprintf("/repo/%s/files", repoName)
	if subPath != "" {
		currentPath = fmt.Sprintf("/repo/%s/files/%s", repoName, subPath)
	}

	html.WriteString(fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Index of %s</title>
    <style>
        body { font-family: monospace; margin: 20px; }
        h1 { border-bottom: 1px solid #ccc; }
        .file-list { list-style: none; padding: 0; }
        .file-list li { padding: 5px 0; }
        .file-list a { text-decoration: none; color: #0066cc; }
        .file-list a:hover { text-decoration: underline; }
        .dir { font-weight: bold; }
        .size { color: #666; margin-left: 20px; }
        .parent { color: #999; }
        .file-info { display: flex; justify-content: space-between; align-items: center; }
        .file-name { flex: 1; }
        .file-meta { color: #666; font-size: 0.9em; }
    </style>
</head>
<body>
    <h1>📁 Repository: %s/%s</h1>
    <ul class="file-list">`, currentPath, repoName, subPath))

	// 修改父目录链接逻辑
	var parentPath string
	if subPath != "" {
		// 如果在子目录中，返回上一级
		cleanSubPath := strings.Trim(subPath, "/")
		if !strings.Contains(cleanSubPath, "/") {
			// 单级子目录，返回仓库根目录
			parentPath = fmt.Sprintf("/repo/%s/files/", repoName)
		} else {
			// 多级子目录，返回上一级
			parts := strings.Split(cleanSubPath, "/")
			parentSubPath := strings.Join(parts[:len(parts)-1], "/")
			parentPath = fmt.Sprintf("/repo/%s/files/%s/", repoName, parentSubPath)
		}
	} else {
		// 如果在仓库根目录，返回所有仓库列表
		parentPath = "/repo/"
	}

	html.WriteString(fmt.Sprintf(`        <li><a href="%s" class="parent">../</a></li>`, parentPath))

	// 添加文件和目录
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()

		if entry.IsDir() {
			linkPath := fmt.Sprintf("%s/%s/", currentPath, name)
			html.WriteString(fmt.Sprintf(`        <li>
				<div class="file-info">
					<div class="file-name"><a href="%s" class="dir">📁 %s/</a></div>
					<div class="file-meta">Directory</div>
				</div>
			</li>`, linkPath, name))
		} else {
			linkPath := fmt.Sprintf("%s/%s", currentPath, name)
			size := formatFileSize(info.Size())
			icon := getFileIcon(name)
			modTime := info.ModTime().Format("2006-01-02 15:04:05")

			html.WriteString(fmt.Sprintf(`        <li>
				<div class="file-info">
					<div class="file-name"><a href="%s">%s %s</a></div>
					<div class="file-meta">%s | %s</div>
				</div>
			</li>`, linkPath, icon, name, size, modTime))
		}
	}

	html.WriteString(`    </ul>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String()
}

func handleRepoEndpoints(ctx *fasthttp.RequestCtx, method, root, path string, patterns map[string]*regexp.Regexp, h *API) bool {
	log.Printf("🔍 handleRepoEndpoints: method=%s, path=%s", method, path)

	// 特殊处理 /files/ 路径
	if strings.Contains(path, "/files/") {
		// 匹配 /repo/{repoPath}/files/{filePath}
		filesRegex := regexp.MustCompile(`^/repo/([^/]+(?:/[^/]+)*)/files/?(.*)$`)
		if matches := filesRegex.FindStringSubmatch(path); matches != nil {
			repoPath := matches[1] // 例如: "oe-release/x86_64"
			filePath := matches[2] // 例如: "repodata/repomd.xml"

			log.Printf("✅ Matched files pattern: repo='%s', file='%s'", repoPath, filePath)

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
			log.Printf("✅ Matched pattern: %s for path: %s, matches: %v", patternName, path, matches)

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
					log.Printf("Handling repo_files: repo=%s, path=%s", matches[1], matches[2])
					handleRepoFiles(ctx, h.config.StoragePath, matches[1], matches[2])
					return true
				}
			case "repo_browse":
				if method == "GET" {
					log.Printf("Handling repo_browse: repo=%s, path=%s", matches[1], matches[2])
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

func (h *API) DownloadPackage(ctx *fasthttp.RequestCtx, repoName, filename string) {
	log.Printf("🔍 Download request: repo=%s, file=%s", repoName, filename)

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
		log.Printf("❌ Package not found: repo=%s, file=%s, error=%v", repoName, filename, err)
		ctx.Error("Package not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	log.Printf("✅ Serving package: %s/%s", repoName, filename)

	ctx.Response.Header.Set("Content-Type", contentType)
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	ctx.Response.Header.Set("Cache-Control", "public, max-age=3600")

	ctx.SetBodyStream(reader, -1)
}

func handleDirectoryListing(ctx *fasthttp.RequestCtx, repoName, subPath, fullPath string) {
	log.Printf("🔍 Directory listing: repo=%s, subPath=%s, fullPath=%s", repoName, subPath, fullPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		log.Printf("❌ Cannot read directory %s: %v", fullPath, err)
		ctx.Error("Cannot read directory", fasthttp.StatusInternalServerError)
		return
	}

	log.Printf("📁 Found %d entries in directory %s", len(entries), fullPath)
	for _, entry := range entries {
		log.Printf("  - %s (dir: %v)", entry.Name(), entry.IsDir())
	}

	// 生成 HTML 目录列表
	html := generateDirectoryHTML(repoName, subPath, entries)

	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}