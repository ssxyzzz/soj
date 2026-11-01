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
