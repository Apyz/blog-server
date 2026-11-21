package handler

import (
	"myblog/internal/config"
	"myblog/internal/db"
	"myblog/internal/render"
)

type AppHandler struct {
	cfg   *config.Config
	store *db.Store
	md    *render.MarkdownRenderer
	tmpl  *render.Engine
}

func NewAppHandler(cfg *config.Config, store *db.Store, md *render.MarkdownRenderer, tmpl *render.Engine) *AppHandler {
	return &AppHandler{
		cfg:   cfg,
		store: store,
		md:    md,
		tmpl:  tmpl,
	}
}

// Common template data
type PageData struct {
	SiteName    string
	Title       string
	Description string
	CurrentYear int
	ActiveNav   string
	Username    string
}

func (h *AppHandler) newBaseData(title, activeNav string) PageData {
	return PageData{
		SiteName:    "My Blog",
		Title:       title,
		Description: "个人技术与思考博客，记录编程探索与生活随笔",
		CurrentYear: 2026,
		ActiveNav:   activeNav,
	}
}
