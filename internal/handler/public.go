package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"myblog/internal/model"

	"github.com/go-chi/chi/v5"
)

const postsPerPage = 10

// Home renders the public post list with search, tag filtering, and pagination.
func (h *AppHandler) Home(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	if page > 1_000_000 {
		page = 1_000_000
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))

	posts, total, err := h.store.ListPublicPosts(r.Context(), page, postsPerPage, tag, search)
	if err != nil {
		log.Printf("list public posts: %v", err)
		http.Error(w, "文章列表暂时无法加载", http.StatusInternalServerError)
		return
	}
	tags, err := h.store.GetAllTags(r.Context())
	if err != nil {
		log.Printf("list public tags: %v", err)
		tags = nil
	}
	totalPages := (total + postsPerPage - 1) / postsPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	data := struct {
		PageData
		Posts       []model.Post
		Tags        []string
		CurrentPage int
		TotalPages  int
		TotalPosts  int
		CurrentTag  string
		SearchQuery string
	}{
		PageData:    h.newBaseData("首页", "home"),
		Posts:       posts,
		Tags:        tags,
		CurrentPage: page,
		TotalPages:  totalPages,
		TotalPosts:  total,
		CurrentTag:  tag,
		SearchQuery: search,
	}
	h.render(w, "home", data)
}

// Post displays a published post by slug.
func (h *AppHandler) Post(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chiURLParam(r, "slug"))
	post, err := h.store.GetPostBySlug(r.Context(), slug)
	if err != nil {
		log.Printf("get post %q: %v", slug, err)
		http.Error(w, "文章暂时无法加载", http.StatusInternalServerError)
		return
	}
	if post == nil || !post.Published {
		http.NotFound(w, r)
		return
	}
	content, err := h.md.ToHTML(post.ContentMD)
	if err != nil {
		log.Printf("render markdown for %q: %v", slug, err)
		http.Error(w, "文章内容无法渲染", http.StatusInternalServerError)
		return
	}

	data := struct {
		PageData
		Post            *model.Post
		RenderedContent any
	}{
		PageData:        h.newBaseData(post.Title, ""),
		Post:            post,
		RenderedContent: content,
	}
	h.render(w, "post", data)
}

// About renders a short profile page. A published post with slug "about" can override its body.
func (h *AppHandler) About(w http.ResponseWriter, r *http.Request) {
	var customContent any
	post, err := h.store.GetPostBySlug(r.Context(), "about")
	if err != nil {
		log.Printf("get about post: %v", err)
	} else if post != nil && post.Published {
		customContent, err = h.md.ToHTML(post.ContentMD)
		if err != nil {
			log.Printf("render about markdown: %v", err)
			customContent = nil
		}
	}
	data := struct {
		PageData
		CustomContent any
	}{PageData: h.newBaseData("关于", "about"), CustomContent: customContent}
	h.render(w, "about", data)
}

func (h *AppHandler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.Render(w, name, data); err != nil {
		log.Printf("render template %q: %v", name, err)
		// If the template failed before writing a response, the status is still writable.
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
	}
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
