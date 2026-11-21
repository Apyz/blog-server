package render

import (
	"fmt"
	"html/template"
	"io"
	"net/url"
	"strings"
	"time"

	"myblog/templates"
)

type Engine struct {
	templates map[string]*template.Template
}

func NewEngine() (*Engine, error) {
	funcMap := template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"join": func(items []string, sep string) string {
			return strings.Join(items, sep)
		},
		"urlq": func(s string) string {
			return url.QueryEscape(s)
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"formatDate": func(t time.Time) string {
			return t.Format("2006-01-02")
		},
	}

	pages := map[string][]string{
		"home":            {"layout.html", "home.html"},
		"post":            {"layout.html", "post.html"},
		"about":           {"layout.html", "about.html"},
		"admin_login":     {"admin/login.html"},
		"admin_dashboard": {"admin/layout.html", "admin/dashboard.html"},
		"admin_editor":    {"admin/layout.html", "admin/editor.html"},
	}

	compiled := make(map[string]*template.Template)

	for name, files := range pages {
		tmpl := template.New("").Funcs(funcMap)
		parsed, err := tmpl.ParseFS(templates.FS, files...)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s (%v): %w", name, files, err)
		}
		compiled[name] = parsed
	}

	return &Engine{templates: compiled}, nil
}

// Render executes a compiled template set by name to the given writer.
func (e *Engine) Render(w io.Writer, name string, data any) error {
	tmpl, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}

	var rootName string
	switch name {
	case "home", "post", "about":
		rootName = "layout.html"
	case "admin_login":
		rootName = "login.html"
	case "admin_dashboard", "admin_editor":
		rootName = "layout.html"
	default:
		rootName = name
	}

	return tmpl.ExecuteTemplate(w, rootName, data)
}
