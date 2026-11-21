package handler

import (
	"archive/zip"
	"bytes"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"myblog/internal/auth"
	"myblog/internal/model"

	"github.com/go-chi/chi/v5"
)

const sessionDuration = 24 * time.Hour

// AdminLoginPage renders the login form.
func (h *AppHandler) AdminLoginPage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		SiteName string
		Error    string
	}{SiteName: "My Blog", Error: r.URL.Query().Get("error")}
	h.render(w, "admin_login", data)
}

// AdminLogin validates credentials against env vars and sets a JWT HttpOnly cookie.
func (h *AppHandler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.redirectLogin(w, r, "表单解析失败")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	// Constant-time comparisons to avoid timing attacks
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(h.cfg.AdminUsername)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(h.cfg.AdminPassword)) == 1

	if !userOK || !passOK {
		log.Printf("[AUTH] Failed login attempt for user %q from %s", username, r.RemoteAddr)
		h.redirectLogin(w, r, "用户名或密码错误")
		return
	}

	token, err := auth.GenerateToken(username, h.cfg.JWTSecret, sessionDuration)
	if err != nil {
		log.Printf("[AUTH] token generation failed: %v", err)
		h.redirectLogin(w, r, "登录令牌生成失败，请稍后重试")
		return
	}

	auth.SetAuthCookie(w, token, int(sessionDuration.Seconds()))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// AdminLogout clears the auth cookie and returns to the login page.
func (h *AppHandler) AdminLogout(w http.ResponseWriter, r *http.Request) {
	auth.ClearAuthCookie(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *AppHandler) redirectLogin(w http.ResponseWriter, r *http.Request, msg string) {
	// msg is user-facing; pass via query so the login page can display it
	http.Redirect(w, r, "/admin/login?error="+url.QueryEscape(msg), http.StatusSeeOther)
}

// AdminDashboard lists every post (published + drafts) for management.
func (h *AppHandler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	if page > 1_000_000 {
		page = 1_000_000
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	posts, total, err := h.store.ListAdminPosts(r.Context(), page, 20, search)
	if err != nil {
		log.Printf("admin list posts: %v", err)
		http.Error(w, "后台文章列表无法加载", http.StatusInternalServerError)
		return
	}

	publishedCount, draftCount := 0, 0
	for i := range posts {
		if posts[i].Published {
			publishedCount++
		} else {
			draftCount++
		}
	}

	const adminPerPage = 20
	totalPages := (total + adminPerPage - 1) / adminPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	base := h.newBaseData("后台仪表盘", "dashboard")
	base.Username = auth.GetUserFromContext(r.Context())
	data := struct {
		PageData
		Posts          []model.Post
		TotalPosts     int
		PublishedCount int
		DraftCount     int
		SearchQuery    string
		CurrentPage    int
		TotalPages     int
	}{
		PageData:       base,
		Posts:          posts,
		TotalPosts:     total,
		PublishedCount: publishedCount,
		DraftCount:     draftCount,
		SearchQuery:    search,
		CurrentPage:    page,
		TotalPages:     totalPages,
	}
	h.render(w, "admin_dashboard", data)
}

// AdminNewPost renders the blank editor form.
func (h *AppHandler) AdminNewPost(w http.ResponseWriter, r *http.Request) {
	base := h.newBaseData("新建文章", "new")
	base.Username = auth.GetUserFromContext(r.Context())
	data := struct {
		PageData
		IsNew bool
		Saved bool
		Post  model.Post
	}{
		PageData: base,
		IsNew:    true,
	}
	h.render(w, "admin_editor", data)
}

// AdminEditPost renders the editor form pre-filled with an existing post.
func (h *AppHandler) AdminEditPost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post, err := h.store.GetPostByID(r.Context(), id)
	if err != nil {
		log.Printf("admin get post %d: %v", id, err)
		http.Error(w, "文章暂时无法加载", http.StatusInternalServerError)
		return
	}
	if post == nil {
		http.NotFound(w, r)
		return
	}

	base := h.newBaseData("编辑文章", "edit")
	base.Username = auth.GetUserFromContext(r.Context())
	data := struct {
		PageData
		IsNew     bool
		Saved     bool
		Post      model.Post
		TagString string
	}{
		PageData:  base,
		IsNew:     false,
		Saved:     r.URL.Query().Get("saved") == "1",
		Post:      *post,
		TagString: strings.Join(post.Tags, ", "),
	}
	h.render(w, "admin_editor", data)
}

// CreateOrUpdatePost handles both POST /admin/posts (create) and POST /admin/posts/{id} (update).
func (h *AppHandler) CreateOrUpdatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单解析失败", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Redirect(w, r, "/admin/new?error="+url.QueryEscape("文章标题不能为空"), http.StatusSeeOther)
		return
	}
	contentMD := r.FormValue("content_md")
	slug := strings.TrimSpace(r.FormValue("slug"))
	if slug == "" {
		slug = model.GenerateSlug(title)
	} else {
		slug = model.GenerateSlug(slug) // normalize user-provided slug (spaces, casing)
		if slug == "" {
			slug = model.GenerateSlug(title)
		}
	}

	summary := strings.TrimSpace(r.FormValue("summary"))
	tags := model.CleanTags(r.FormValue("tags"))
	published := r.FormValue("published") == "true"

	post := &model.Post{
		Title:     title,
		Slug:      slug,
		Summary:   summary,
		ContentMD: contentMD,
		Tags:      tags,
		Published: published,
	}

	idParam := chi.URLParam(r, "id")
	if idParam != "" {
		// Update mode
		id, err := strconv.ParseInt(idParam, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		post.ID = id
		if err := h.store.UpdatePost(r.Context(), post); err != nil {
			log.Printf("admin update post %d: %v", id, err)
			http.Error(w, "保存文章失败", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/admin/edit/%d?saved=1", id), http.StatusSeeOther)
		return
	}

	// Create mode
	newID, err := h.store.CreatePost(r.Context(), post)
	if err != nil {
		log.Printf("admin create post: %v", err)
		http.Error(w, "创建文章失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/edit/%d?saved=1", newID), http.StatusSeeOther)
}

// AdminDeletePost removes a post (with confirmation handled on the client).
func (h *AppHandler) AdminDeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.store.DeletePost(r.Context(), id); err != nil {
		log.Printf("admin delete post %d: %v", id, err)
		http.Error(w, "删除文章失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// AdminExportPackages streams every post as a .zip of Markdown files with YAML frontmatter.
func (h *AppHandler) AdminExport(w http.ResponseWriter, r *http.Request) {
	posts, err := h.store.GetAllPostsForExport(r.Context())
	if err != nil {
		log.Printf("admin export: %v", err)
		http.Error(w, "导出备份失败", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	seen := make(map[string]int)
	for _, p := range posts {
		filename := p.Slug + ".md"
		if n, ok := seen[filename]; ok {
			n++
			seen[filename] = n
			filename = fmt.Sprintf("%s-%d.md", p.Slug, n)
		} else {
			seen[filename] = 1
		}

		fw, err := zw.Create(filename)
		if err != nil {
			log.Printf("admin export create zip entry: %v", err)
			http.Error(w, "导出备份失败", http.StatusInternalServerError)
			return
		}
		if _, err := fmt.Fprint(fw, postToMarkdown(&p)); err != nil {
			log.Printf("admin export write zip entry: %v", err)
			http.Error(w, "导出备份失败", http.StatusInternalServerError)
			return
		}
	}

	if err := zw.Close(); err != nil {
		log.Printf("admin export close zip: %v", err)
		http.Error(w, "导出备份失败", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("myblog-export-%s.zip", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeContent(w, r, filename, time.Now(), bytes.NewReader(buf.Bytes()))
}

// postToMarkdown builds a YAML frontmatter + markdown document for export.
func postToMarkdown(p *model.Post) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", yamlQuote(p.Title)))
	b.WriteString(fmt.Sprintf("slug: %s\n", yamlQuote(p.Slug)))
	if len(p.Tags) > 0 {
		b.WriteString("tags: [")
		for i, t := range p.Tags {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(yamlQuote(t))
		}
		b.WriteString("]\n")
	} else {
		b.WriteString("tags: []\n")
	}
	b.WriteString(fmt.Sprintf("created_at: %s\n", p.CreatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("updated_at: %s\n", p.UpdatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("published: %t\n", p.Published))
	if strings.TrimSpace(p.Summary) != "" {
		b.WriteString(fmt.Sprintf("summary: %s\n", yamlQuote(p.Summary)))
	}
	b.WriteString("---\n\n")
	b.WriteString(p.ContentMD)
	b.WriteString("\n")
	return b.String()
}

// yamlQuote wraps a value in double quotes and escapes embedded quotes/backslashes.
func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Ping is the health check endpoint (/health is reserved by the cloud platform).
func (h *AppHandler) Ping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbStatus := "ok"
	if err := h.store.Ping(ctx); err != nil {
		dbStatus = "unavailable"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","db":%q,"time":%q}`, dbStatus, time.Now().Format(time.RFC3339))
}
