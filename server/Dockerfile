# 构建阶段：编译 web / judge 两个二进制
FROM golang:1.26-alpine AS build
WORKDIR /src

# 默认走国内模块代理，海外环境可用 --build-arg GOPROXY=https://proxy.golang.org,direct 覆盖
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/judge ./cmd/judge

# 运行阶段：judge 服务由 compose 的 entrypoint 覆盖为 /app/judge
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 soj
COPY --from=build /out/ /app/
USER soj
ENTRYPOINT ["/app/web"]
