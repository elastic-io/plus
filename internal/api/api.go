package api

import (
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

	err := h.repoService.RefreshMetadata(ctx, repoPath)
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

	repoHandler := createRepoHandler("./storage")

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
					if handleRepoEndpoints(ctx, method, path, patterns, h) {
						return
					}

					// 6. 仓库文件直接访问 - 最后匹配
					if method == "GET" && strings.HasPrefix(path, "/repo/") {
						if handleRepoFileAccess(ctx, repoHandler) {
							return
						}
					}

					ctx.Error("Not Found", fasthttp.StatusNotFound)
				},
			),
		),
	)
}

// 处理仓库列表页面
func handleRepoListPage(ctx *fasthttp.RequestCtx, h *API) {
	// 获取仓库列表
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Printf("Failed to list repositories: %v", err)
		ctx.Error("Failed to load repositories", fasthttp.StatusInternalServerError)
		return
	}

	// 生成 HTML 页面
	html := generateRepoListHTML(repos)
	ctx.SetContentType("text/html; charset=utf-8")
	ctx.SetBodyString(html)
}

// 生成仓库列表 HTML
func generateRepoListHTML(repos []string) string {
	var html strings.Builder // 创建一个字符串构建器用于高效拼接HTML内容

	// 写入HTML文档的基础结构，包括DOCTYPE声明和head部分
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

	// 检查仓库列表是否为空
	if len(repos) == 0 {
		html.WriteString(`        <li>No repositories found.</li>`) // 如果没有仓库，显示提示信息
	} else {
		// 遍历所有仓库，为每个仓库生成对应的HTML列表项
		for _, repo := range repos {
			// 使用格式化字符串为每个仓库生成包含浏览和信息链接的HTML结构
			html.WriteString(fmt.Sprintf(`
        <li>
            <div class="repo-item">
                <div>
                    <a href="/repo/%s/files/" class="repo-name">📁 %s</a>
                </div>
                <div class="repo-links">
                    <a href="/repo/%s/files/">Browse</a>
                    <a href="/repo/%s">Info</a>
                </div>
            </div>
        </li>`, repo, repo, repo, repo))
		}
	}

	// 写入HTML文档的结束部分，包括页脚信息和闭合标签
	html.WriteString(`    </ul>
    <hr>
    <p><em>Generated by Plus Artifacts Server</em></p>
</body>
</html>`)

	return html.String() // 返回构建完成的HTML字符串
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
func handleRepoFileAccess(ctx *fasthttp.RequestCtx, repoHandler fasthttp.RequestHandler) bool {
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
	fullPath := fmt.Sprintf("./storage/%s/%s", repoName, filePath)
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
func handleRepoBrowse(ctx *fasthttp.RequestCtx, browseRegex *regexp.Regexp) {
	path := string(ctx.Path())
	matches := browseRegex.FindStringSubmatch(path)

	if matches == nil {
		ctx.Error("Invalid browse path", fasthttp.StatusBadRequest)
		return
	}

	repoName := matches[1]
	subPath := matches[2]

	fullPath := fmt.Sprintf("./storage/%s/%s", repoName, subPath)

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
        <a href="/repo/">📁 Browse Files</a>
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
		PackageCount: len(packages),
		RPMCount:     rpmCount,
		DEBCount:     debCount,
		TotalSize:    totalSize,
		Packages:     packages,
	}, fasthttp.StatusOK)
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

	err := h.repoService.CreateRepo(ctx, repoPath)
	if err != nil {
		log.Printf("Create repository failed for %s: %v", repoPath, err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to create repository: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	h.sendSuccess(ctx, "Repository created successfully")
}

func (h *API) ListRepos(ctx *fasthttp.RequestCtx) {
	repos, err := h.repoService.ListRepos(ctx)
	if err != nil {
		log.Printf("List repositories failed: %v", err)
		h.sendJSONError(ctx, fmt.Sprintf("Failed to list repositories: %v", err), fasthttp.StatusInternalServerError)
		return
	}

	// 构建层级结构
	repoTree := buildRepoTree(repos)

	h.sendJSONResponse(ctx, &types.RepoMeta{
		Status:       types.Status{Server: "Plus", Status: "success", Code: fasthttp.StatusOK},
		Repositories: repos,
		Tree:         repoTree,
		Count:        len(repos),
	}, fasthttp.StatusOK)
}

func (h *API) DownloadRPM(ctx *fasthttp.RequestCtx) {
	// 解析路径: /repo/{repoName}/rpm/{filename}
	path := string(ctx.Path())
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 4 {
		ctx.Error("Invalid path", fasthttp.StatusBadRequest)
		return
	}

	// 支持多层路径
	repoName := strings.Join(parts[1:len(parts)-2], "/")
	filename := parts[len(parts)-1]

	// 获取文件
	reader, err := h.repoService.DownloadPackage(ctx, repoName, filename)
	if err != nil {
		ctx.Error("Package not found", fasthttp.StatusNotFound)
		return
	}
	defer reader.Close()

	// 设置响应头
	ctx.Response.Header.Set("Content-Type", "application/x-rpm")
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// 流式传输文件
	ctx.SetBodyStream(reader, -1)
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
func (h *API) UploadRPM(ctx *fasthttp.RequestCtx) {
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

func handleRepoFiles(ctx *fasthttp.RequestCtx, repoName, filePath string) {
	log.Printf("handleRepoFiles called: repo=%s, path='%s'", repoName, filePath)

	// 构建完整路径
	var fullPath string
	if filePath == "" {
		fullPath = fmt.Sprintf("./storage/%s", repoName)
	} else {
		fullPath = fmt.Sprintf("./storage/%s/%s", repoName, filePath)
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

// 修复目录列表生成函数
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

func handleRepoEndpoints(ctx *fasthttp.RequestCtx, method, path string, patterns map[string]*regexp.Regexp, h *API) bool {
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
				handleRepoFiles(ctx, repoPath, filePath)
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
					h.UploadRPM(ctx)
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
					handleRepoFiles(ctx, matches[1], matches[2])
					return true
				}
			case "repo_browse":
				if method == "GET" {
					log.Printf("Handling repo_browse: repo=%s, path=%s", matches[1], matches[2])
					handleRepoBrowse(ctx, patterns["repo_browse"])
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
