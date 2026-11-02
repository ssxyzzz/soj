# 画板自托管 drawio webapp（不入库）

本目录存放 drawio 网页应用静态文件（118MB，`.gitignore`），由 docker-compose 只读挂载到
容器 `/data/drawio`，Go 侧经 `SOJ_DRAWIO_DIR` 启用 `/drawio/*` 静态路由——画板 iframe 同源，
宿主可控制编辑器内部（形状/格式面板开关、样式）。

## 获取（一次性）

```bash
docker pull docker.m.daocloud.io/jgraph/drawio:latest   # 国内走镜像加速源
docker create --name drawio-extract docker.m.daocloud.io/jgraph/drawio:latest
docker cp drawio-extract:/usr/local/tomcat/webapps/draw/. deploy/drawio/
docker rm drawio-extract
rm -rf deploy/drawio/WEB-INF deploy/drawio/META-INF   # Java servlet 段静态部署不需要
```

要求：`index.html` 必须在本目录根部。drawio 版本更新时重跑一遍上述命令即可（静态文件无
哈希版本号，前端静态缓存 1 天自动过期）。

## 回退

`SOJ_DRAWIO_DIR` 留空或目录为空 → `/drawio` 返回 404，DrawView 的 `DRAW_BASE` 改回
`https://embed.diagrams.net` 即回官方托管（同源按钮功能随跨域失效）。
