# 前端构建阶段：web 目录经 compose additional_contexts 以 "web" 名义传入
FROM node:22-alpine AS webbuild
WORKDIR /web
COPY --from=web ./package.json ./package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY --from=web . .
RUN npm run build && cp -r node_modules/vditor/dist dist/vditor

# 后端构建阶段：前端产物嵌入二进制
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/soj ./cmd/soj

# 运行阶段
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/soj /app/soj
ENTRYPOINT ["/app/soj"]
