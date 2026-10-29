#!/usr/bin/env python3
"""洛谷题包前置脚本（v0.31 衔接，原 plan §2.37 第 6 条）。

扫描 data/test/luogu 下各题包 problem.md 里的 cdn.luogu.com.cn 外链图：
下载入各包 assets/（沿用 URL 原名，导入时再内容寻址重命名）→ md 引用改写为
assets/ 相对路径。零 Mongo 写入；单图失败保留外链（导入期转存兜底）；重跑幂等
（已相对化/文件已在则跳过）。产物报告：stdout 摘要 + .crawl/prefetch-report.json。

出网闸门（本脚本运行在开发机，钉死目标）：仅 https + 主机恒为 cdn.luogu.com.cn、
DNS 解析后全部 IP 须为公网（拒环回/私网/链路本地）、禁重定向、响应魔数校验。

用法: python3 scripts/luogu-prefetch.py [--root data/test/luogu] [--dry]
"""

import argparse
import concurrent.futures
import ipaddress
import json
import pathlib
import re
import socket
import sys
import urllib.error
import urllib.request

UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 SOJ-LuoguPrefetch/1.0"
PINNED_HOST = "cdn.luogu.com.cn"
IMG_URL_RE = re.compile(r'(?:!\[[^\]]*\]\(\s*)?(https://cdn\.luogu\.com\.cn/[^\s)"]+)')
MAGIC = [(b"\x89PNG", "png"), (b"\xff\xd8\xff", "jpg"), (b"GIF8", "gif")]


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, *_a, **_k):  # CDN 直链不应重定向；出现即拒绝
        raise urllib.error.URLError("拒绝重定向（钉死 cdn.luogu.com.cn 直链）")


OPENER = urllib.request.build_opener(NoRedirect)


def sniff_image(data: bytes) -> str:
    for magic, ext in MAGIC:
        if data.startswith(magic):
            return ext
    if len(data) > 11 and data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        return "webp"
    return ""


def assert_public_ips(host: str) -> None:
    infos = socket.getaddrinfo(host, 443, proto=socket.IPPROTO_TCP)
    for info in infos:
        ip = ipaddress.ip_address(info[4][0])
        if not ip.is_global:
            raise ValueError(f"DNS 解析到非公网地址 {ip}，拒绝连接")


def download(url: str):
    host = urllib.parse.urlsplit(url).hostname or ""
    if not url.startswith("https://") or host != PINNED_HOST:
        raise ValueError("仅允许 https://" + PINNED_HOST + " 直链")
    assert_public_ips(host)
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with OPENER.open(req, timeout=20) as resp:
        data = resp.read(6 << 20)  # 与站内单图限额同口径
    ext = sniff_image(data)
    if not ext:
        raise ValueError("内容不是白名单图片（svg 或未知类型）")
    return data, ext


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default="data/test/luogu")
    ap.add_argument("--dry", action="store_true", help="只统计不下载不改写")
    args = ap.parse_args()
    root = pathlib.Path(args.root)

    pkgs = []  # (dir, urls)
    for md in sorted(root.glob("*/*/*/problem.md")):  # csp-j|csp-s / 分类 / 题包
        text = md.read_text(encoding="utf-8")
        urls, seen = [], set()
        for m in IMG_URL_RE.finditer(text):
            u = m.group(1)
            if u not in seen:
                seen.add(u)
                urls.append(u)
        if urls:
            pkgs.append((md.parent, urls))
    total_urls = sum(len(u) for _, u in pkgs)
    print(f"含外链图题包 {len(pkgs)} 个 / 外链图 {total_urls} 张")
    if args.dry:
        for d, urls in pkgs:
            print(f"  {d.relative_to(root)}: {len(urls)} 图")
        return 0

    report, failures, done = [], [], 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        for pkg_dir, urls in pkgs:
            entry = {"dir": str(pkg_dir.relative_to(root)), "ok": [], "fail": []}
            futures = {pool.submit(download, u): u for u in urls}
            contents = {}
            for fut in concurrent.futures.as_completed(futures):
                u = futures[fut]
                try:
                    contents[u] = fut.result()
                except Exception as e:  # noqa: BLE001 —— 单图失败降级保外链
                    entry["fail"].append({"url": u, "err": str(e)})
                    failures.append((str(pkg_dir), u, str(e)))
            if contents:
                text = (pkg_dir / "problem.md").read_text(encoding="utf-8")
                assets = pkg_dir / "assets"
                assets.mkdir(exist_ok=True)
                for u, (data, _ext) in sorted(contents.items()):
                    name = "assets/" + u.rsplit("/", 1)[-1]
                    (pkg_dir / name).write_bytes(data)  # 幂等：同名覆盖同内容
                    text = text.replace(u, name)
                    entry["ok"].append(name)
                    done += 1
                (pkg_dir / "problem.md").write_text(text, encoding="utf-8")
            report.append(entry)
            mark = "✓" if not entry["fail"] else "△"
            print(f"  {mark} {entry['dir']}: 落库 {len(entry['ok'])} / 失败 {len(entry['fail'])}")

    print(f"\n完成：下载落包 {done} 张 / 失败保留外链 {len(failures)} 张")
    for d, u, e in failures:
        print(f"  ✗ {d}: {u} ({e})")
    out = root / ".crawl" / "prefetch-report.json"
    out.write_text(json.dumps({"packages": report, "downloaded": done,
                               "failures": [{"dir": d, "url": u, "err": e} for d, u, e in failures]},
                              ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"报告: {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
