package render

import (
	"bytes"
	"testing"
	"time"

	"myblog/internal/model"
)

func TestEngineParsesAllTemplates(t *testing.T) {
	if _, err := NewEngine(); err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
}

func TestRenderHomeAndPostAndAdminPages(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	post := model.Post{
		ID:        1,
		Title:     "测试 <文章>",
		Slug:      "hello-world",
		Summary:   "",
		ContentMD: "# 标题\n\n正文内容",
		Tags:      []string{"Go", "测试"},
		Published: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	html, err := NewMarkdownRenderer().ToHTML(post.ContentMD)
	if err != nil {
		t.Fatalf("markdown render failed: %v", err)
	}

	base := map[string]any{
		"SiteName":    "My Blog",
		"Title":       "页面标题",
		"Description": "",
		"CurrentYear": 2026,
		"ActiveNav":   "home",
		"Username":    "admin",
	}

	home := map[string]any{}
	for k, v := range base {
		home[k] = v
	}
	home["Posts"] = []model.Post{post}
	home["Tags"] = []string{"Go", "测试"}
	home["CurrentPage"] = 1
	home["TotalPages"] = 1
	home["TotalPosts"] = 1
	home["CurrentTag"] = "Go & <script>"
	home["SearchQuery"] = `q"uo'te`

	detail := map[string]any{}
	for k, v := range base {
		detail[k] = v
	}
	detail["Post"] = &post
	detail["RenderedContent"] = html

	about := map[string]any{}
	for k, v := range base {
		about[k] = v
	}
	about["CustomContent"] = nil

	dashboard := map[string]any{}
	for k, v := range base {
		dashboard[k] = v
	}
	dashboard["Posts"] = []model.Post{post}
	dashboard["TotalPosts"] = 1
	dashboard["PublishedCount"] = 1
	dashboard["DraftCount"] = 0
	dashboard["SearchQuery"] = ""
	dashboard["CurrentPage"] = 1
	dashboard["TotalPages"] = 3

	editor := map[string]any{}
	for k, v := range base {
		editor[k] = v
	}
	editor["IsNew"] = false
	editor["Saved"] = true
	editor["Post"] = post
	editor["TagString"] = "Go, 测试"

	login := map[string]any{"SiteName": "My Blog", "Error": "用户名或密码错误"}

	cases := []struct {
		name string
		data map[string]any
	}{
		{"home", home},
		{"post", detail},
		{"about", about},
		{"admin_dashboard", dashboard},
		{"admin_editor", editor},
		{"admin_login", login},
	}

	for _, tc := range cases {
		var buf bytes.Buffer
		if err := engine.Render(&buf, tc.name, tc.data); err != nil {
			t.Errorf("render %s failed: %v", tc.name, err)
			continue
		}
		if buf.Len() < 200 {
			t.Errorf("render %s produced suspiciously small output (%d bytes)", tc.name, buf.Len())
		}
	}
}
