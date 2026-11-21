# My Blog — 轻量个人博客系统

一个使用 **纯 Go** 编写、可直接部署到云容器（Uppcoder / Zeabur / Railway / 自建 Docker 等）的个人博客系统。
零 Node.js、零打包链、零 ORM —— 所有模板与静态资源通过 `//go:embed` 编译进**单一二进制文件**，启动即用。

> **技术栈**：Go 1.27 · chi 路由 · pgx 原生 SQL · PostgreSQL · goldmark（GFM）· JWT HttpOnly Cookie · Tailwind CSS Play CDN + Typography · Highlight.js · marked.js 实时预览

---

## ✨ 功能特性

| 公开页面 | 后台管理 |
| :--- | :--- |
| 首页文章列表（分页 / 关键词搜索标题与正文 / 标签筛选） | 账号密码登录（JWT + HttpOnly Cookie） |
| 文章详情（阅读时长、标签、发布/更新时间、代码高亮 + 一键复制） | 仪表盘（草稿 / 已发布状态一目了然，二次确认删除） |
| 关于我（可由数据库 `slug=about` 的文章覆盖内容） | 左右双栏 Markdown 编辑器（marked.js 实时预览 + 格式化工具栏） |
| 深/浅色主题一键切换（localStorage 记忆，无闪烁） | 一键导出全部文章为 `.zip`（每篇一个带 YAML Frontmatter 的 `.md`） |
| 移动端自适应、极简现代 UI | 启动自动建表（Auto Migration），无需手动导 SQL |

**其他亮点**

- **零构建前端**：Tailwind Play CDN + Typography `prose` 排版，改样式即改模板。
- **PostgreSQL 原生数组**：`tags TEXT[]` + GIN 索引，`$1 = ANY(tags)` 标签筛选。
- **规范的健康检查**：云平台保留的 `/health` 不被占用，本系统使用 **`/ping`**。
- **首次启动自动播种**：数据库为空时自动创建一篇欢迎示例文章，部署后不再是白屏。

---

## 📁 项目结构

```
.
├── main.go                      # 入口：环境变量、路由、优雅停机、种子数据
├── go.mod / go.sum
├── .env.example                 # 环境变量示例
├── Dockerfile                   # 多阶段构建（静态链接 + Alpine 运行）
├── internal/
│   ├── config/                  # 环境变量读取（支持本地 .env）
│   ├── db/                      # pgxpool 连接池、Auto Migration、全部 SQL
│   ├── handler/                 # public.go（前台）/ admin.go（后台）/ handler.go（公共类型）
│   ├── auth/                    # JWT 签发与校验、HttpOnly Cookie、路由中间件
│   ├── model/                   # Post 模型、Slug 生成、标签清洗、摘要截取
│   └── render/                  # goldmark Markdown 渲染 + html/template 引擎
├── templates/                   # 全部页面模板（go:embed 嵌入）
│   ├── embed.go
│   ├── layout.html  home.html  post.html  about.html
│   └── admin/  layout.html  login.html  dashboard.html  editor.html
└── static/                      # 静态资源（go:embed 嵌入）
    └── favicon.svg
```

---

## 🚀 本地运行

### 1. 准备 PostgreSQL

任选其一：

```bash
# 本机已有 PostgreSQL
createdb blog

# 或使用 Docker 临时启动一个
docker run -d --name blog-pg -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=blog postgres:16-alpine
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env，至少确认 DATABASE_URL 正确
```

### 3. 启动

```bash
go run .
```

控制台输出形如：

```
[DB] Connecting to PostgreSQL (attempt 1/5)...
[DB] Successfully connected to PostgreSQL.
[DB] Running auto-migration...
[HTTP] Listening on http://0.0.0.0:8080
```

打开 <http://localhost:8080> 即可看到博客首页，后台地址 <http://localhost:8080/admin>。

> 默认账号 `admin` / `admin123`（请务必通过环境变量修改，启动时会打印安全警告）。

---

## 🧭 路由一览

### 公开

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| GET | `/` | 首页（`?page=` 分页、`?q=` 搜索、`?tag=` 标签筛选） |
| GET | `/post/{slug}` | 文章详情 |
| GET | `/about` | 关于我 |
| GET | `/ping` | 健康检查（JSON，可直连数据库探活） |

### 后台（需登录）

| 方法 | 路径 | 说明 |
| :--- | :--- | :--- |
| GET / POST | `/admin/login` | 登录页 / 提交登录 |
| POST | `/admin/logout` | 退出登录（清除 Cookie） |
| GET | `/admin` | 仪表盘（含草稿，支持搜索与分页） |
| GET | `/admin/new` | 新建文章 |
| POST | `/admin/posts` | 创建文章 |
| GET | `/admin/edit/{id}` | 编辑文章 |
| POST | `/admin/posts/{id}` | 更新文章 |
| POST | `/admin/delete/{id}` | 删除文章（前端二次确认） |
| GET | `/admin/export` | 导出全部文章为 `.zip` 备份 |

---

## 🗄️ 数据库表结构

启动时自动执行 `CREATE TABLE IF NOT EXISTS`，无需手动导入。

```sql
CREATE TABLE IF NOT EXISTS posts (
    id         SERIAL PRIMARY KEY,
    title      TEXT NOT NULL,
    slug       TEXT UNIQUE NOT NULL,
    summary    TEXT DEFAULT '',
    content_md TEXT NOT NULL,
    tags       TEXT[] DEFAULT '{}',
    published  BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_posts_slug              ON posts(slug);
CREATE INDEX IF NOT EXISTS idx_posts_published_created ON posts(published, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_tags              ON posts USING GIN (tags);
```

**Slug 规则**：留空时由标题自动生成（保留中英文，空白转 `-`，纯符号标题则回退为时间戳随机串）；
与已有文章冲突时自动追加 `-1`、`-2`…，不会报错。

**摘要规则**：摘要留空时，列表页自动剥离 Markdown 标记后截取正文前 120 字。

---

## 📦 编译部署

### 方式一：直接编译 Linux 二进制

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o myblog .

# 上传 myblog 到服务器后：
./myblog
```

产物为单一静态二进制（约 10MB 出头），无需任何运行时文件。

### 方式二：Docker（推荐云容器平台）

```bash
docker build -t myblog .
docker run -d -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/blog?sslmode=require" \
  -e ADMIN_USERNAME=admin \
  -e ADMIN_PASSWORD='强密码' \
  -e JWT_SECRET="$(openssl rand -hex 32)" \
  myblog
```

### 部署到 Uppcoder 等云平台

1. 在平台创建 PostgreSQL 实例，拿到连接串。
2. 新建 Web 服务，构建方式选择 **Dockerfile**（或上传 Linux 二进制直接运行）。
3. 在控制台配置环境变量：`DATABASE_URL`、`ADMIN_USERNAME`、`ADMIN_PASSWORD`、`JWT_SECRET`。
4. 端口无需写死：程序读取平台注入的 `PORT`，并监听 `0.0.0.0:$PORT`。
5. 健康检查路径请填 **`/ping`**（`/health` 为平台保留路径，本程序未占用）。

---

## ⚙️ 环境变量

| 变量 | 必填 | 默认值 | 说明 |
| :--- | :---: | :--- | :--- |
| `PORT` | 否 | `8080` | 监听 `0.0.0.0:$PORT` |
| `DATABASE_URL` | **是** | — | PostgreSQL 连接串 |
| `ADMIN_USERNAME` | 否 | `admin` | 后台用户名 |
| `ADMIN_PASSWORD` | 否 | `admin123` | 后台密码（**生产必须修改**，启动有警告） |
| `JWT_SECRET` | 建议 | 随机生成 | 留空则每次启动随机生成（重启后登录态失效） |

> 本地开发时程序会自动读取项目根目录的 `.env`；云平台请直接注入环境变量（`.env` 不会被读取也无需打包）。

---

## 🔐 安全说明

- 登录态使用 **JWT + HttpOnly Cookie**（`SameSite=Lax`），前端 JS 无法读取令牌。
- 登录失败采用**常数时间比较**（`crypto/subtle`），避免时序攻击。
- 所有 SQL 均为 **参数化查询**（pgx 原生协议），无 SQL 注入面。
- Markdown 渲染放开了原始 HTML（便于嵌入视频/徽章）；**如果允许他人投稿，请改用 goldmark 默认的安全渲染**（移除 `html.WithUnsafe()` 即可）。
- 部署在 HTTPS 环境下（云平台默认如此），建议同时将 `auth.SetAuthCookie` 中的 `Secure` 置为 `true`。

---

## 🧪 验证

```bash
go build ./...
go vet ./...

# 仅运行模板渲染相关测试（本项目唯一的测试）
go test ./internal/render -run "TestEngineParsesAllTemplates|TestRenderHomeAndPostAndAdminPages"
```
