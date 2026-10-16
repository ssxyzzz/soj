# 前端产物由宿主机构建（web-building.sh → web/dist），经 compose additional_contexts
# 以 "web" 名义传入；web/.dockerignore 只放行 dist/。镜像构建不含前端工具链，
# 因此直接 docker compose build 前必须已有 web/dist（make up 会自动先构建）。

# 后端构建阶段：前端产物嵌入二进制
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src

# 缓存挂载（A）：模块与编译缓存在多次构建间持久——嵌入前端变化导致的必然重编译
# 只补编受影响包，25s 冷编译变秒级
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web ./dist ./internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/soj ./cmd/soj

# 运行阶段
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/soj /app/soj
ENTRYPOINT ["/app/soj"]
