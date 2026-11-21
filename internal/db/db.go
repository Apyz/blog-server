package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"myblog/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// InitPool initializes a PostgreSQL connection pool with startup retry for container environments.
func InitPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is empty")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	// Sensible pool defaults for lightweight containers
	config.MaxConns = 15
	config.MinConns = 2
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	// Serverless databases (Neon/Supabase) can take several seconds to wake up,
	// and managed proxies add latency — give TCP+TLS+AUTH ample time.
	if config.ConnConfig.ConnectTimeout < 10*time.Second {
		config.ConnConfig.ConnectTimeout = 10 * time.Second
	}

	target := fmt.Sprintf("%s:%d/%s", config.ConnConfig.Host, config.ConnConfig.Port, config.ConnConfig.Database)
	log.Printf("[DB] Target: %s (user=%s, sslmode=%s)", target, config.ConnConfig.User, tlsMode(config.ConnConfig.TLSConfig))

	var pool *pgxpool.Pool
	var lastErr error

	// Retry loop for cloud container startup
	const maxAttempts = 5
	for i := 1; i <= maxAttempts; i++ {
		log.Printf("[DB] Connecting to PostgreSQL (attempt %d/%d)...", i, maxAttempts)
		pool, err = pgxpool.NewWithConfig(ctx, config)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				log.Println("[DB] Successfully connected to PostgreSQL.")
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		if i < maxAttempts {
			log.Printf("[DB] Connection attempt %d failed: %v. Retrying in 3s...", i, err)
			time.Sleep(3 * time.Second)
		}
	}

	return nil, fmt.Errorf("could not connect to %s after %d attempts: %w%s", target, maxAttempts, lastErr, dbErrorHint(lastErr))
}

// tlsMode returns "enabled"/"disabled" for log output.
func tlsMode(tlsCfg any) string {
	if tlsCfg == nil {
		return "disabled"
	}
	return "enabled"
}

// dbErrorHint appends an actionable hint for the most common failure modes.
func dbErrorHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "password authentication failed"),
		strings.Contains(msg, "role") && strings.Contains(msg, "does not exist"):
		return "\n  [HINT] 用户名或密码错误，请检查 DATABASE_URL 中的账号密码。"
	case strings.Contains(msg, "timed out"), strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "context canceled"):
		return "\n  [HINT] 连接超时：数据库地址不可达或被代理/防火墙拦截。请确认主机端口可访问，并留意本地代理（TUN 模式）是否放行了该连接。"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"):
		return "\n  [HINT] DNS 解析失败，请检查 DATABASE_URL 中的主机名是否正确。"
	case strings.Contains(msg, "does not exist"):
		return "\n  [HINT] 数据库不存在，请先在 PostgreSQL 中创建 DATABASE_URL 指定的数据库。"
	case strings.Contains(msg, "ssl"):
		return "\n  [HINT] TLS 握手失败，请尝试在 DATABASE_URL 末尾添加 ?sslmode=disable（自建库）或 ?sslmode=require（云数据库）。"
	}
	return ""
}

// ListPublicPosts returns published posts with pagination, search, and tag filters.
func (s *Store) ListPublicPosts(ctx context.Context, page, limit int, tag, search string) ([]model.Post, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	whereClauses := []string{"published = true"}
	args := []any{}
	argIdx := 1

	if tag != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("$%d = ANY(tags)", argIdx))
		args = append(args, tag)
		argIdx++
	}

	if search != "" {
		searchPattern := "%" + search + "%"
		whereClauses = append(whereClauses, fmt.Sprintf("(title ILIKE $%d OR content_md ILIKE $%d OR summary ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, searchPattern)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	// Count total
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM posts WHERE %s", whereSQL)
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count query failed: %w", err)
	}

	// Select rows
	selectQuery := fmt.Sprintf(`
		SELECT id, title, slug, summary, content_md, tags, published, created_at, updated_at
		FROM posts
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("select query failed: %w", err)
	}
	defer rows.Close()

	var posts []model.Post
	for rows.Next() {
		var p model.Post
		err := rows.Scan(
			&p.ID,
			&p.Title,
			&p.Slug,
			&p.Summary,
			&p.ContentMD,
			&p.Tags,
			&p.Published,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan post failed: %w", err)
		}
		posts = append(posts, p)
	}

	return posts, total, nil
}

// ListAdminPosts returns all posts (drafts and published) for the admin dashboard.
func (s *Store) ListAdminPosts(ctx context.Context, page, limit int, search string) ([]model.Post, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	whereClauses := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if search != "" {
		searchPattern := "%" + search + "%"
		whereClauses = append(whereClauses, fmt.Sprintf("(title ILIKE $%d OR slug ILIKE $%d OR content_md ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, searchPattern)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM posts WHERE %s", whereSQL)
	var total int
	err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("admin count query failed: %w", err)
	}

	selectQuery := fmt.Sprintf(`
		SELECT id, title, slug, summary, content_md, tags, published, created_at, updated_at
		FROM posts
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, selectQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("admin select query failed: %w", err)
	}
	defer rows.Close()

	var posts []model.Post
	for rows.Next() {
		var p model.Post
		err := rows.Scan(
			&p.ID,
			&p.Title,
			&p.Slug,
			&p.Summary,
			&p.ContentMD,
			&p.Tags,
			&p.Published,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan admin post failed: %w", err)
		}
		posts = append(posts, p)
	}

	return posts, total, nil
}

// GetPostBySlug retrieves a single post by its slug.
func (s *Store) GetPostBySlug(ctx context.Context, slug string) (*model.Post, error) {
	query := `
		SELECT id, title, slug, summary, content_md, tags, published, created_at, updated_at
		FROM posts
		WHERE slug = $1
		LIMIT 1
	`
	var p model.Post
	err := s.pool.QueryRow(ctx, query, slug).Scan(
		&p.ID,
		&p.Title,
		&p.Slug,
		&p.Summary,
		&p.ContentMD,
		&p.Tags,
		&p.Published,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPostByID retrieves a single post by its ID.
func (s *Store) GetPostByID(ctx context.Context, id int64) (*model.Post, error) {
	query := `
		SELECT id, title, slug, summary, content_md, tags, published, created_at, updated_at
		FROM posts
		WHERE id = $1
		LIMIT 1
	`
	var p model.Post
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.Title,
		&p.Slug,
		&p.Summary,
		&p.ContentMD,
		&p.Tags,
		&p.Published,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// CreatePost inserts a new post. If the slug collides, an incrementing or random suffix is appended.
func (s *Store) CreatePost(ctx context.Context, post *model.Post) (int64, error) {
	post.Slug = s.ensureUniqueSlug(ctx, post.Slug, 0)
	if post.Tags == nil {
		post.Tags = []string{}
	}

	query := `
		INSERT INTO posts (title, slug, summary, content_md, tags, published, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	var id int64
	err := s.pool.QueryRow(ctx, query,
		post.Title,
		post.Slug,
		post.Summary,
		post.ContentMD,
		post.Tags,
		post.Published,
	).Scan(&id, &post.CreatedAt, &post.UpdatedAt)

	if err != nil {
		return 0, fmt.Errorf("create post failed: %w", err)
	}

	post.ID = id
	return id, nil
}

// UpdatePost updates an existing post by ID.
func (s *Store) UpdatePost(ctx context.Context, post *model.Post) error {
	post.Slug = s.ensureUniqueSlug(ctx, post.Slug, post.ID)
	if post.Tags == nil {
		post.Tags = []string{}
	}

	query := `
		UPDATE posts
		SET title = $1, slug = $2, summary = $3, content_md = $4, tags = $5, published = $6, updated_at = NOW()
		WHERE id = $7
	`
	cmdTag, err := s.pool.Exec(ctx, query,
		post.Title,
		post.Slug,
		post.Summary,
		post.ContentMD,
		post.Tags,
		post.Published,
		post.ID,
	)
	if err != nil {
		return fmt.Errorf("update post failed: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return errors.New("post not found")
	}
	return nil
}

// DeletePost removes a post by ID.
func (s *Store) DeletePost(ctx context.Context, id int64) error {
	query := `DELETE FROM posts WHERE id = $1`
	cmdTag, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete post failed: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return errors.New("post not found")
	}
	return nil
}

// GetAllPostsForExport retrieves all posts sorted by ID for ZIP export.
func (s *Store) GetAllPostsForExport(ctx context.Context) ([]model.Post, error) {
	query := `
		SELECT id, title, slug, summary, content_md, tags, published, created_at, updated_at
		FROM posts
		ORDER BY id ASC
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("export query failed: %w", err)
	}
	defer rows.Close()

	var posts []model.Post
	for rows.Next() {
		var p model.Post
		err := rows.Scan(
			&p.ID,
			&p.Title,
			&p.Slug,
			&p.Summary,
			&p.ContentMD,
			&p.Tags,
			&p.Published,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan export post failed: %w", err)
		}
		posts = append(posts, p)
	}
	return posts, nil
}

// GetAllTags retrieves all distinct tags from published posts.
func (s *Store) GetAllTags(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT unnest(tags) AS tag
		FROM posts
		WHERE published = true
		ORDER BY tag ASC
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil && t != "" {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

// Ping verifies the database connection is healthy.
func (s *Store) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.pool.Ping(pingCtx)
}

// CountPosts returns the total number of posts in the database.
func (s *Store) CountPosts(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&count)
	return count, err
}

// ensureUniqueSlug verifies if a slug is available, otherwise appends an index or timestamp.
func (s *Store) ensureUniqueSlug(ctx context.Context, baseSlug string, currentID int64) string {
	slug := baseSlug
	if strings.TrimSpace(slug) == "" {
		slug = model.GenerateSlug("")
	}

	counter := 1
	for {
		var existingID int64
		err := s.pool.QueryRow(ctx, "SELECT id FROM posts WHERE slug = $1 LIMIT 1", slug).Scan(&existingID)
		if errors.Is(err, pgx.ErrNoRows) {
			// Slug is unique
			return slug
		}
		if err == nil && existingID == currentID {
			// Slug belongs to the same post being updated
			return slug
		}
		// Collided, append counter
		slug = fmt.Sprintf("%s-%d", baseSlug, counter)
		counter++
		if counter > 100 {
			// Fallback with timestamp
			slug = fmt.Sprintf("%s-%d", baseSlug, time.Now().UnixNano()%10000)
			return slug
		}
	}
}
