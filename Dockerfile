# ---------- 构建阶段 ----------
FROM golang:1.27-alpine AS builder

WORKDIR /build

# 依赖层缓存
COPY go.mod go.sum ./
RUN go mod download

# 编译：静态链接、裁剪符号表，产出单一二进制
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /build/myblog .

# ---------- 运行阶段 ----------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=builder /build/myblog /app/myblog

USER app
ENV PORT=8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- "http://127.0.0.1:${PORT}/ping" >/dev/null || exit 1

ENTRYPOINT ["/app/myblog"]
