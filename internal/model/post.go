package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Post struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug"`
	Summary   string    `json:"summary"`
	ContentMD string    `json:"content_md"`
	Tags      []string  `json:"tags"`
	Published bool      `json:"published"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FormattedCreatedAt returns a readable date string.
func (p *Post) FormattedCreatedAt() string {
	return p.CreatedAt.Format("2006-01-02")
}

// FormattedUpdatedAt returns a readable date string.
func (p *Post) FormattedUpdatedAt() string {
	return p.UpdatedAt.Format("2006-01-02")
}

// ReadingTimeMinutes calculates the estimated reading time (approx 300 words/runes per minute).
func (p *Post) ReadingTimeMinutes() int {
	runes := utf8.RuneCountInString(p.ContentMD)
	mins := runes / 300
	if mins < 1 {
		return 1
	}
	return mins
}

var (
	mdLinkRegex   = regexp.MustCompile(`\[(.*?)\]\(.*?\)`)
	mdCodeRegex   = regexp.MustCompile("`{1,3}[^`]*`{1,3}")
	mdHeaderRegex = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	mdSymbolRegex = regexp.MustCompile(`[*_~>#\-+|]`)
	multiSpace    = regexp.MustCompile(`\s+`)
)

// Excerpt returns the post summary, or the first 120 clean characters from ContentMD if summary is empty.
func (p *Post) Excerpt() string {
	if strings.TrimSpace(p.Summary) != "" {
		return strings.TrimSpace(p.Summary)
	}

	// Strip markdown artifacts for excerpt preview
	text := mdCodeRegex.ReplaceAllString(p.ContentMD, "")
	text = mdLinkRegex.ReplaceAllString(text, "$1")
	text = mdHeaderRegex.ReplaceAllString(text, "")
	text = mdSymbolRegex.ReplaceAllString(text, "")
	text = multiSpace.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	runes := []rune(text)
	if len(runes) > 120 {
		return string(runes[:120]) + "..."
	}
	if len(runes) == 0 {
		return "暂无文章摘要..."
	}
	return string(runes)
}

// GenerateSlug generates a clean URL slug from the title.
// If the title contains only non-ASCII characters or symbols, generates a clean post timestamp slug.
func GenerateSlug(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return generateRandomSlug()
	}

	// Normalize and lowercase
	var b strings.Builder
	lastDash := false

	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r <= unicode.MaxASCII {
				b.WriteRune(unicode.ToLower(r))
				lastDash = false
			} else {
				// Non-ASCII rune (like CJK): keep if valid letter/digit
				b.WriteRune(unicode.ToLower(r))
				lastDash = false
			}
		} else if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return generateRandomSlug()
	}

	return slug
}

func generateRandomSlug() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("post-%s-%s", time.Now().Format("200601021504"), hex.EncodeToString(b))
}

// CleanTags parses a comma or Chinese comma separated tag string into trimmed, non-empty tags.
func CleanTags(tagInput string) []string {
	normalized := strings.ReplaceAll(tagInput, "，", ",")
	parts := strings.Split(normalized, ",")
	tags := make([]string, 0, len(parts))
	seen := make(map[string]bool)

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			tags = append(tags, trimmed)
		}
	}
	return tags
}
