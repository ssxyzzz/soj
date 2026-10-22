# SOJ HTTPS 与域名上线手册（阿里云版）

> v0.13 交付。适用部署事实（2026-10-22 确认）：VPS 与域名均在阿里云（境内）；ICP 备案进行中；
> frp 纯 TCP remotePort 模式，现网链路 `学生 → VPS:8080（frps）→ 隧道 → frpc → SOJ:80（docker）`。
>
> **目标链路**：`学生 → https://域名（VPS Caddy :443，TLS 终止）→ 127.0.0.1:8080（frps）→ 隧道 → frpc → SOJ`。
>
> **节奏**：备案期间维持现状（IP:8080，域名不解析——管局要求备案完成前域名不得提供访问服务，提前开通有驳回风险）；
> 第 1–5 步备案期间全部做完并自测，**切换日（第 6 步）只花十分钟**。

## 0. 为什么是 Caddy + DNS-01

- 现链路全程明文 HTTP，学生密码与登录 token 在公网裸奔，HTTPS 是最高优先级安全项。
- 证书选 **DNS-01**（而非 HTTP-01）：验证只查阿里云 DNS 的 TXT 记录、不回连服务器端口，
  备案前后、任意端口都能签发续期；由 Caddy 带 alidns 插件全自动完成，无需 acme.sh/cron。
- SOJ 应用保持纯 HTTP，不内置证书逻辑；TLS 只在 VPS 边缘这一层。

## 1. 阿里云 RAM 子账号（仅 DNS 权限）

1. 阿里云控制台 → 访问控制 RAM → 用户 → 创建用户（如 `caddy-dns`），勾选 **OpenAPI 调用访问**。
2. 为该用户授权 **AliyunDNSFullAccess**（云解析 DNS 全部权限；证书签发需要添加/删除 TXT 记录）。
3. 为该用户创建 **AccessKey**，记下 AccessKey ID 与 Secret（只显示一次）。
   该子账号无 ECS/其他资源权限，即便泄漏影响面仅限 DNS 记录。

## 2. VPS 安装 Caddy（带 alidns 插件）

官方 apt 源的 Caddy **不带** alidns 插件，用官网定制构建：

1. 打开 https://caddyserver.com/download ，Platform 选 `linux-amd64`，
   Add modules 搜索并勾选 `github.com/caddy-dns/alidns`，Download。
2. 解压出 `caddy` 二进制放到 `/usr/local/bin/`，`chmod +x`。
3. 创建 systemd 服务 `/etc/systemd/system/caddy.service`：

   ```ini
   [Unit]
   Description=Caddy (SOJ HTTPS entry)
   After=network.target

   [Service]
   ExecStart=/usr/local/bin/caddy run --config /etc/caddy/Caddyfile
   ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
   Restart=always
   EnvironmentFile=/etc/caddy/alidns.env

   [Install]
   WantedBy=multi-user.target
   ```

## 3. 配置与密钥文件

1. `mkdir -p /etc/caddy`，放入仓库 `deploy/https/Caddyfile`，
   把 `soj.example.com` 改成实际域名、`email` 改成你的邮箱。
2. `/etc/caddy/alidns.env`（`chmod 600`）：

   ```
   ALICLOUD_ACCESS_KEY=LTAI...
   ALICLOUD_SECRET_KEY=...
   ```

## 4. 启动并验证证书签发（备案期间即可做）

```bash
systemctl daemon-reload && systemctl enable --now caddy
systemctl status caddy        # 应为 active
journalctl -u caddy -n 50     # 看到 certificate obtained（DNS-01）即签发成功
```

本地自测（域名尚未解析，用 --resolve 强制定向，不影响备案核查）：

```bash
curl -v --resolve 你的域名:443:127.0.0.1 https://你的域名/api/health
# 返回 {"code":0,...} 即 Caddy → frp → SOJ 全链路通
```

## 5. frp 隧道加密核查（顺手做）

1. `frpc -v` / frps 版本 **≥ 0.50**：frpc↔frps 默认已 TLS，无需改。
   低于 0.50：frpc 配置加 `transport.tls.enable = true` 后重启，建议直接升级。
2. frps 侧建议加 `transport.tls.force = true`（拒绝非 TLS 客户端）。
3. 确认 `auth.token` 是长随机串（参考配置见本目录 `frps.toml` / `frpc.toml`）。

## 6. 切换日清单（备案通过后，按序执行）

1. **DNS 解析**：阿里云云解析 DNS → 添加 A 记录（主机记录按需 `@` 或子域如 `oj`）→ VPS 公网 IP，TTL 默认即可。
2. **安全组**：VPS 实例安全组放行 **80/443**（80 供 HTTP→HTTPS 跳转与续期兜底）。
3. **验证**：手机流量（非内网）打开 `https://域名`，登录、提交一次判题；
   浏览器地址栏确认证书有效。
4. **真实 IP 验证**（v0.14 限流依赖）：`docker logs soj-soj-1 | tail`，
   gin 日志行首的 clientIP 应为学生公网 IP 而非 `172.x.x.x`——不对则检查
   docker-compose 的 `SOJ_TRUSTED_PROXIES` 是否覆盖 frpc 直连段。
5. **挂备案号**：管理后台 → 系统设置 → ICP 备案号（如 `皖ICP备2026XXXXXX号-1`）→ 保存，
   页脚立即展示并链接工信部备案系统（法规要求，阿里云会巡查）。
6. **公告**：首页公告通知学生改用域名访问。
7. **观察 1–2 天后关旧入口**：安全组**移除 8080**（防绕过 TLS 明文访问）。
   最终 VPS 只留 80/443、SSH、frps bindPort(7000)。

## 7. 回退（切换出问题时）

安全组重新放行 8080，学生回 `http://VPS公网IP:8080` 临时使用；`systemctl stop caddy`
不影响旧链路。修复后按第 6 步重走。

## 8. 日常维护

- 证书自动续期（Caddy 内置，到期前 30 天 DNS-01 重签），无需人工操作。
- Caddy 配置改动后 `systemctl reload caddy` 平滑生效。
- 升级 Caddy：重新下载同插件构建替换二进制后 restart。
