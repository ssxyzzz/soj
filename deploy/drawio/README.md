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

## 生产（VPS 直出，不走隧道）

**注意：sync 脚本的 rsync 目标是家里生产机，不是 VPS**——webapp 需要从开发机单独推一份
到 VPS `/var/www/soj-drawio`（标准 www 目录，caddy 服务用户天然可读，无权限坑）。开发机执行：

```bash
ssh root@ssxyzzz.cn "mkdir -p /var/www/soj-drawio"
rsync -a --delete /home/ssxyz/soj/deploy/drawio/ root@ssxyzzz.cn:/var/www/soj-drawio/
```

让 VPS 上的 Caddy 对 `/drawio/*` 本地发文件（不走 frp 隧道回源）：消掉每文件隧道 RTT
（首载 10s 级→2-3s 级）与家宽上行占用，全班并发首载由 VPS 云带宽承担。deploy/https/Caddyfile
已含现成块：

```
ssxyzzz.cn {
    encode zstd gzip
    handle_path /drawio/* {
        root * /var/www/soj-drawio
        @html path *.html
        @assets not path *.html
        header @html Cache-Control "public, max-age=86400"
        header @assets Cache-Control "public, max-age=2592000"
        file_server
    }
    reverse_proxy 127.0.0.1:8080
}
```

**encode 必须有**（2026-11-02 实锤）：漏配时 drawio 资产裸传（app.min.js 10.8MB 不压缩），
叠加 VPS 带宽顶（按带宽计费实例常见 3Mbps≈0.4MB/s）→ 画板首载 35s；开压缩后传输量降 ~4 倍。
VPS 带宽是直出后的硬天花板（全班共享），课堂并发场景视带宽决定是否升级或走 SW 预热错峰。

上线两步：①整块按上面替换站点块；②`sudo systemctl reload caddy`。

**验证直出与压缩生效**（必做）：

```bash
# 直出：max-age=2592000 → Caddy 本地盘 ✓；max-age=86400 → 仍走隧道（Go 发的）
curl -sI https://ssxyzzz.cn/drawio/js/app.min.js | grep -i cache-control
# 压缩：下载量应为 ~2MB 级（裸传是 10.8MB）
curl -s -o /dev/null -w '%{size_download} 字节 / %{time_total}s\n' \
  -H 'Accept-Encoding: gzip' https://ssxyzzz.cn/drawio/js/app.min.js
```

同源不变（同域名），DrawView 零改动。缓存策略：资产 30 天、HTML 入口 1 天——第二次起近零拉取。

**升级/更新 webapp**：本地重跑「获取」命令后，再执行上面两条 rsync 推送命令即刷新 VPS。
文件名无哈希，学生旧资产缓存最长 30 天不刷新，期间新旧混用有风险——升级当天通知学生
Ctrl+F5 强刷一次（embed 协议本身跨版本稳定兜底）。

## 回退

`SOJ_DRAWIO_DIR` 留空或目录为空 → `/drawio` 返回 404，DrawView 的 `DRAW_BASE` 改回
`https://embed.diagrams.net` 即回官方托管（同源按钮功能随跨域失效）。
