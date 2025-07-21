package middleware

import (
	"strings"

	"github.com/valyala/fasthttp"
	"plus/internal/config"
)

func AuthMiddleware(config *config.Config) func(fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(ctx *fasthttp.RequestCtx) {
			// 如果认证未启用，直接通过
			if !config.Auth.Enabled {
				next(ctx)
				return
			}

			// 检查是否为只读操作
			method := string(ctx.Method())
			if method == "GET" || method == "HEAD" {
				// 只读操作可能不需要认证，根据配置决定
				if !config.Auth.RequireReadAuth {
					next(ctx)
					return
				}
			}

			// 获取 Authorization 头
			authHeader := string(ctx.Request.Header.Peek("Authorization"))
			if authHeader == "" {
				ctx.Error("Authorization required", fasthttp.StatusUnauthorized)
				ctx.Response.Header.Set("WWW-Authenticate", "Bearer")
				return
			}

			// 检查 Bearer token
			if !strings.HasPrefix(authHeader, "Bearer ") {
				ctx.Error("Invalid authorization format", fasthttp.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token != config.Auth.Token {
				ctx.Error("Invalid token", fasthttp.StatusUnauthorized)
				return
			}

			next(ctx)
		}
	}
}

// API Key 认证
func APIKeyMiddleware(config *config.Config) func(fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(ctx *fasthttp.RequestCtx) {
			if !config.Auth.Enabled {
				next(ctx)
				return
			}

			// 检查 X-API-Key 头
			apiKey := string(ctx.Request.Header.Peek("X-API-Key"))
			if apiKey == "" {
				// 也可以从查询参数获取
				apiKey = string(ctx.QueryArgs().Peek("api_key"))
			}

			if apiKey == "" {
				ctx.Error("API key required", fasthttp.StatusUnauthorized)
				return
			}

			// 验证 API key（这里简化为单个 key，实际可以支持多个）
			if apiKey != config.Auth.APIKey {
				ctx.Error("Invalid API key", fasthttp.StatusUnauthorized)
				return
			}

			next(ctx)
		}
	}
}
