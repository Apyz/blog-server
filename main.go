package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"myblog/internal/auth"
	"myblog/internal/config"
	"myblog/internal/db"
	"myblog/internal/handler"
	"myblog/internal/model"
	"myblog/internal/render"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed static
var staticAssets embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("========================================")
	log.Println("  My Blog - 轻量个人博客系统")
	log.Println("========================================")

	cfg := config.Load()

	// Root context cancelled on SIGINT/SIGTERM so shutdown is graceful.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Database ---
	pool, err := db.InitPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database initialization failed: %v", err)
	}
	defer pool.Close()

	if err := db.AutoMigrate(ctx, pool); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	store := db.NewStore(pool)
	if err := seedWelcomePost(ctx, store); err != nil {
		log.Printf("[WARN] could not seed welcome post: %v", err)
	}

	// --- Template engine & Markdown renderer ---
	tmplEngine, err := render.NewEngine()
	if err != nil {
		log.Fatalf("[FATAL] Template engine initialization failed: %v", err)
	}
	mdRenderer := render.NewMarkdownRenderer()

	h := handler.NewAppHandler(cfg, store, mdRenderer, tmplEngine)

	// --- Router ---
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Embedded static assets
	staticSub, err := fs.Sub(staticAssets, "static")
	if err != nil {
		log.Fatalf("[FATAL] static assets: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Public routes.
	// NOTE: /health is reserved by the cloud platform, so the health endpoint is /ping.
	r.Get("/", h.Home)
	r.Get("/post/{slug}", h.Post)
	r.Get("/about", h.About)
	r.Get("/ping", h.Ping)
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.svg", http.StatusMovedPermanently)
	})

	// Admin routes
	r.Route("/admin", func(r chi.Router) {
		r.Get("/login", h.AdminLoginPage)
		r.Post("/login", h.AdminLogin)
		r.Post("/logout", h.AdminLogout)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth(cfg.JWTSecret))

			r.Get("/", h.AdminDashboard)
			r.Get("/new", h.AdminNewPost)
			r.Get("/edit/{id}", h.AdminEditPost)
			r.Get("/export", h.AdminExport)
			r.Post("/posts", h.CreateOrUpdatePost)
			r.Post("/posts/{id}", h.CreateOrUpdatePost)
			r.Post("/delete/{id}", h.AdminDeletePost)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>404 - 页面不存在</title></head>
<body style="margin:0;display:flex;min-height:100vh;align-items:center;justify-content:center;font-family:-apple-system,'Segoe UI',sans-serif;background:#f9fafb;color:#111827">
<div style="text-align:center;padding:24px">
<div style="font-size:72px;font-weight:800;background:linear-gradient(135deg,#6366f1,#818cf8);-webkit-background-clip:text;background-clip:text;color:transparent">404</div>
<p style="color:#6b7280;margin:8px 0 24px">你访问的页面不存在或已被移动</p>
<a href="/" style="display:inline-block;padding:10px 20px;border-radius:12px;background:#4f46e5;color:#fff;text-decoration:none;font-size:14px;font-weight:600">返回首页</a>
</div></body></html>`)
	})

	// --- HTTP Server ---
	addr := "0.0.0.0:" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("[HTTP] Listening on http://%s", addr)
		log.Printf("[HTTP] 管理员账号: %s (请通过 ADMIN_USERNAME / ADMIN_PASSWORD 环境变量修改)", cfg.AdminUsername)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[FATAL] HTTP server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("[HTTP] Shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[WARN] graceful shutdown failed: %v", err)
	}
	log.Println("[HTTP] Server stopped.")
}

// seedWelcomePost inserts a demo post when the database is completely empty,
// so a freshly deployed instance is not a blank page.
func seedWelcomePost(ctx context.Context, store *db.Store) error {
	count, err := store.CountPosts(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	post := &model.Post{
		Title:     "欢迎来到我的博客",
		Slug:      "welcome",
		Summary:   "这是一篇自动生成的欢迎文章，用于演示博客系统的基本功能，你可以随时在后台修改或删除它。",
		Tags:      []string{"公告", "Go", "开始"},
		Published: true,
		ContentMD: strings.TrimSpace(`
欢迎使用这个使用 **Go 语言** 编写的轻量博客系统！这篇文章是首次启动时自动创建的示例内容，你可以登录后台随时编辑或删除。

## 技术特点

- 使用 Go 标准库 ` + "`html/template`" + ` 服务端渲染（SSR）
- PostgreSQL 原生数组存储标签，无需 ORM
- 模板与静态资源通过 ` + "`//go:embed`" + ` 嵌入单一二进制
- Markdown 由 [Goldmark](https://github.com/yuin/goldmark) 解析，支持 GFM 扩展

## 代码高亮示例

` + "```go" + `
package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
` + "```" + `

## 表格示例

| 功能 | 状态 | 说明 |
| :--- | :---: | ---: |
| Markdown 渲染 | ✅ | GFM 表格 / 删除线 / 自动链接 |
| 标签筛选 | ✅ | PostgreSQL 原生数组 |
| 全文搜索 | ✅ | 标题 + 正文 |

> 提示：登录后台后，点击「新建文章」即可开始你的写作之旅。

祝写作愉快！
	`),
	}

	_, err = store.CreatePost(ctx, post)
	if err == nil {
		log.Println("[DB] Empty database detected — seeded a welcome post.")
	}
	return err
}
