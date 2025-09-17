package middleware

import (
	"plus/internal/metrics"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

func MetricsMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
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