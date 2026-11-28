# go-judge 沙箱会 bind-mount 镜像自身的 /usr、/lib 到判题容器中，
# 因此编译器/解释器必须装在这个镜像里。
# 基础镜像: https://github.com/criyle/go-judge
FROM criyle/go-judge:latest

# 国内网络加速：Debian 源替换为阿里云镜像（兼容新旧两种源配置格式；清华源部分网络 403，
# 2026-10-31 迁移实测）
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' \
      /etc/apt/sources.list /etc/apt/sources.list.d/*.sources /etc/apt/sources.list.d/*.list \
    2>/dev/null || true

# 按需扩展判题语言（新增语言后同步修改 server/internal/lang/lang.go）
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc g++ python3 \
    && rm -rf /var/lib/apt/lists/*

# testlib.h 内置（与 Codeforces / 洛谷等主流 OJ 一致）：自定义校验器可以直接
# #include "testlib.h"，不必自包含。装进 /usr/include——go-judge 把镜像自身的 /usr
# 挂进判题容器，g++ 默认搜索路径就能找到，校验器的编译参数不用加 -I。
# 文件随仓库入库（上游 MikeMirzayanov/testlib，VERSION 0.9.52），升级时整份替换，
# 文件头的许可声明保留。
# 改动本文件后沙箱镜像要单独重建：docker compose build sandbox && docker compose up -d sandbox
# （例行 make up 只重建 soj / judge 两个镜像）。
COPY testlib.h /usr/include/testlib.h
