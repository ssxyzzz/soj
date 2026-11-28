#!/usr/bin/env python3
"""校验器约定迁移（v0.101：自定义校验器对齐 testlib 系「输入、考生输出、答案」）。

把站内所有自定义校验器按「旧源码指纹 → 新源码」整份替换，读写都走站内 HTTP 接口
（清单 GET /api/problems/checkers，回写 POST /api/problems/:id/checker），不碰库和数据卷。

  plan    只读：拉全站校验器清单，备份原源码，产出逐题处置报告
  apply   回写：映射命中的题上传新源码（默认演练只打印，加 --write 才真写）

映射目录（--map DIR）：每个文件是一份新源码，文件名 = 它要替换的旧源码的 sha256。
站点与登录态：--base 站点根地址；SOJ_TOKEN（Bearer），或 SOJ_USER + SOJ_PASSWORD 让脚本登录。
需要「编辑任意题目」权限。目标站点只认 http / https、不跟随重定向；解析到私网或环回
地址（如本机彩排的 localhost）须加 --allow-private。

逐题处置：
  待迁移      旧源码指纹命中映射 → apply 回写
  已迁移      现有源码正是映射里的某份新源码（重跑安全）
  testlib     含 registerTestlibCmd 的 checker 本就是新约定，无需迁移
  未映射      其余——人工确认该 checker 用的是哪种参数顺序
  读取失败    站点读不到源码文件（清单接口该项带 error）

旧题包重导会把旧约定的 checker 带回来：导入后重跑一次本脚本即可（按指纹匹配，幂等）。
"""

import argparse
import base64
import hashlib
import ipaddress
import json
import os
import re
import socket
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

TIMEOUT = 30  # 单请求超时（秒）
MAX_SOURCE = 512 * 1024  # 单份校验器源码上限（与站点读取上限一致）
TESTLIB_SIGN = re.compile(rb"registerTestlib(?:Cmd)?\s*\(")
ALLOW_PRIVATE = False  # --allow-private：放行解析到私网 / 环回地址的目标（本机彩排用）


class ApiError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """不跟随重定向：目标主机以 --base 为准，换主机一律拒绝。"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ApiError(f"站点返回重定向到 {newurl}，已拒绝")


def gate(base):
    """出网闸门：只放行 http / https、显式主机、无内嵌凭据的根地址；解析主机名，
    落到非公网地址的目标须显式 --allow-private。返回 (规范化根地址, 是否公网站点)。"""
    u = urllib.parse.urlsplit(base)
    if u.scheme not in ("http", "https") or not u.hostname:
        raise SystemExit(f"--base 只认 http/https 且须带主机名：{base}")
    if u.username is not None or u.password is not None or u.query or u.fragment:
        raise SystemExit(f"--base 不允许内嵌凭据 / 查询串 / 片段：{base}")
    if u.path not in ("", "/"):
        raise SystemExit(f"--base 只填站点根地址，不带路径：{base}")
    port = u.port or (443 if u.scheme == "https" else 80)
    try:
        infos = socket.getaddrinfo(u.hostname, port, proto=socket.IPPROTO_TCP)
    except socket.gaierror as e:
        raise SystemExit(f"解析不了主机 {u.hostname}：{e}")
    public = True
    for info in infos:
        if not ipaddress.ip_address(info[4][0]).is_global:
            public = False
            if not ALLOW_PRIVATE:
                raise SystemExit(
                    f"{u.hostname} 解析到非公网地址 {info[4][0]}：本机 / 内网彩排请加 --allow-private")
    return f"{u.scheme}://{u.netloc}", public


class Client:
    """目标站点的 HTTP 客户端：根地址过出网闸门，不跟随重定向，请求超时有界。"""

    def __init__(self, base, token=""):
        self.base, self.public = gate(base)
        self.token = token
        self.opener = urllib.request.build_opener(NoRedirect)

    def _open(self, method, path, body, headers, limit):
        """发一次请求并解析站点信封 {code, data | message}；path 只能是本脚本拼出的站内路径。"""
        if not path.startswith("/api/") or path.startswith("//"):
            raise ApiError(f"非法路径：{path}")
        req = urllib.request.Request(self.base + path, data=body, method=method)
        for k, v in headers.items():
            req.add_header(k, v)
        try:
            with self.opener.open(req, timeout=TIMEOUT) as resp:
                raw = resp.read(limit)
        except urllib.error.HTTPError as e:
            raw = e.read(4096)
            try:
                msg = json.loads(raw).get("message") or raw[:200].decode(errors="replace")
            except ValueError:
                msg = raw[:200].decode(errors="replace")
            raise ApiError(f"HTTP {e.code}: {msg}")
        except urllib.error.URLError as e:
            raise ApiError(f"连不上站点：{e.reason}")
        try:
            env = json.loads(raw)
        except ValueError:
            raise ApiError(f"响应不是 JSON：{raw[:200]!r}")
        if env.get("code") != 0:
            raise ApiError(str(env.get("message")))
        return env.get("data")

    def call(self, method, path, body=None, content_type=None):
        headers = {"Authorization": "Bearer " + self.token}
        if content_type:
            headers["Content-Type"] = content_type
        return self._open(method, path, body, headers, limit=64 << 20)

    @classmethod
    def login(cls, base, user, password):
        client = cls(base)
        body = json.dumps({"username": user, "password": password}).encode()
        data = client._open("POST", "/api/auth/login", body,
                            {"Content-Type": "application/json"}, limit=1 << 20)
        token = (data or {}).get("token")
        if not token:
            raise ApiError("登录响应缺 token")
        client.token = token
        return client


def load_map(map_dir):
    """映射目录 → {旧源码 sha256: (新源码路径, 新源码字节, 新源码 sha256)}。"""
    mapping = {}
    for p in sorted(map_dir.iterdir()):
        if not p.is_file() or p.name.startswith("."):
            continue
        if not re.fullmatch(r"[0-9a-f]{64}", p.stem):
            print(f"!! 跳过文件名不是 64 位指纹的文件：{p.name}", file=sys.stderr)
            continue
        content = p.read_bytes()
        mapping[p.stem] = (p, content, hashlib.sha256(content).hexdigest())
    if not mapping:
        raise SystemExit(f"映射目录里没有可用文件：{map_dir}")
    return mapping


def classify(row, mapping, done):
    """返回 (处置, 原源码字节, 说明)。"""
    if row.get("error"):
        return "读取失败", None, row["error"]
    try:
        src = base64.b64decode(row.get("source_b64") or "", validate=True)
    except (ValueError, TypeError):
        return "读取失败", None, "源码不是合法 base64"
    if len(src) > MAX_SOURCE:
        return "读取失败", None, "源码超过读取上限"
    sha = hashlib.sha256(src).hexdigest()
    if row.get("sha") and row["sha"] != sha:
        return "读取失败", None, "站点指纹与源码对不上（传输不完整）"
    if sha in mapping:
        return "待迁移", src, mapping[sha][2]
    if sha in done:
        return "已迁移", src, ""
    if TESTLIB_SIGN.search(src):
        return "testlib", src, ""
    return "未映射", src, ""


def plan(client, mapping, out_dir):
    data = client.call("GET", "/api/problems/checkers")
    rows = (data or {}).get("checkers") or []
    if (data or {}).get("truncated"):
        print("!! 清单触顶（truncated），站点校验器题超过单次上限，以下结果不完整", file=sys.stderr)
    done = {new_sha for _, _, new_sha in mapping.values()}
    before = out_dir / "before"
    before.mkdir(parents=True, exist_ok=True)
    items = []
    for row in rows:
        status, src, note = classify(row, mapping, done)
        ext = Path(row.get("file") or "").suffix or ".cpp"
        item = {
            "id": row["id"], "title": row.get("title", ""), "file": row.get("file", ""),
            "sha": row.get("sha", ""), "status": status, "note": note,
        }
        if src is not None:
            backup = before / f"{row['id']}{ext}"
            backup.write_bytes(src)
            item["backup"] = str(backup)
        items.append(item)
    (out_dir / "inventory.json").write_text(
        json.dumps({"base": client.base, "at": time.strftime("%Y-%m-%d %H:%M:%S"), "items": items},
                   ensure_ascii=False, indent=1))
    return items


def report(items, out_dir, write):
    counts = {}
    for it in items:
        counts[it["status"]] = counts.get(it["status"], 0) + 1
    lines = [f"{'题号':<8}{'处置':<8}{'旧指纹':<14}{'→ 新指纹':<14}标题 / 说明"]
    for it in items:
        new = it["note"][:12] if it["status"] == "待迁移" else ""
        tail = it["title"] + (f" ｜ {it['note']}" if it["status"] in ("读取失败", "回写失败") else "")
        lines.append(f"{it['id']:<8}{it['status']:<8}{it['sha'][:12]:<14}{new:<14}{tail}")
    text = "\n".join(lines)
    print(text)
    (out_dir / ("report-apply.txt" if write else "report-plan.txt")).write_text(text + "\n")
    print(f"\n合计 {len(items)} 题：" + "，".join(f"{k} {v}" for k, v in sorted(counts.items())))
    return counts


def upload(client, pid, name, content):
    boundary = "sojchecker" + uuid.uuid4().hex
    head = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"checker\"; "
            f"filename=\"{name}\"\r\nContent-Type: text/x-c++src\r\n\r\n").encode()
    body = head + content + f"\r\n--{boundary}--\r\n".encode()
    return client.call("POST", f"/api/problems/{int(pid)}/checker", body,
                       f"multipart/form-data; boundary={boundary}")


def main():
    global ALLOW_PRIVATE
    ap = argparse.ArgumentParser(description="校验器约定迁移（v0.101）")
    ap.add_argument("action", choices=["plan", "apply"])
    ap.add_argument("--base", required=True, help="站点根地址，如 http://localhost")
    ap.add_argument("--map", required=True, type=Path, help="映射目录：文件名 = 旧源码 sha256")
    ap.add_argument("--out", type=Path, default=None, help="备份与报告目录（默认 ./checker-migrate-out/<时间>）")
    ap.add_argument("--only", default="", help="只处理这些题号（逗号分隔）")
    ap.add_argument("--write", action="store_true", help="apply 真回写（默认只演练）")
    ap.add_argument("--allow-private", action="store_true", help="目标解析到私网 / 环回地址时放行（本机彩排）")
    ap.add_argument("--yes", action="store_true", help="向非本机站点回写前的确认")
    args = ap.parse_args()
    ALLOW_PRIVATE = args.allow_private

    mapping = load_map(args.map)
    out_dir = args.out or Path("checker-migrate-out") / time.strftime("%Y%m%d-%H%M%S")
    out_dir.mkdir(parents=True, exist_ok=True)

    token = os.environ.get("SOJ_TOKEN")
    if token:
        client = Client(args.base, token)
    else:
        user, password = os.environ.get("SOJ_USER"), os.environ.get("SOJ_PASSWORD")
        if not (user and password):
            raise SystemExit("需要登录态：设 SOJ_TOKEN，或 SOJ_USER + SOJ_PASSWORD")
        client = Client.login(args.base, user, password)
    if args.action == "apply" and args.write and client.public and not args.yes:
        raise SystemExit(f"即将向公网站点 {client.base} 回写校验器：确认无误后加 --yes")

    items = plan(client, mapping, out_dir)
    only = {int(x) for x in args.only.split(",") if x.strip()}
    if only:
        items = [it for it in items if it["id"] in only]
    todo = [it for it in items if it["status"] == "待迁移"]
    print(f"\n站点 {client.base} ｜ 映射 {len(mapping)} 份 ｜ 备份与报告 {out_dir}/")

    if args.action == "plan" or not args.write:
        counts = report(items, out_dir, write=False)
        if args.action == "apply":
            print(f"\n演练：未写任何数据。真回写将上传 {len(todo)} 题："
                  + " ".join(str(it["id"]) for it in todo))
        return 1 if any(k in counts for k in ("未映射", "读取失败")) else 0

    failed = []
    for it in todo:
        _, content, new_sha = mapping[it["sha"]]
        try:
            data = upload(client, it["id"], mapping[it["sha"]][0].name, content)
            got = (data or {}).get("sha")
            if got != new_sha:
                raise ApiError(f"站点回显指纹 {got} 与上传内容不符")
            it["status"], it["note"] = "已回写", new_sha
        except ApiError as e:
            it["status"], it["note"] = "回写失败", str(e)
            failed.append(it)
        print(f"  {it['id']:<8}{it['status']:<8}→ {new_sha[:12]}  {it['title']}" +
              (f"  ｜ {it['note']}" if it["status"] == "回写失败" else ""))
    counts = report(items, out_dir, write=True)
    print(f"\n回写完成 {len(todo) - len(failed)}/{len(todo)}，备份在 {out_dir}/before/")
    return 1 if failed or any(k in counts for k in ("未映射", "读取失败")) else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except ApiError as e:
        print(f"!! {e}", file=sys.stderr)
        sys.exit(2)
