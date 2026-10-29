"""校验前端 service 层调用的路径与 HTTP 方法是否与后端路由表一致。

防止改了后端路由却忘了改前端（或反之）导致 404。
"""
import re, io, os, sys

os.chdir(r'D:\project-workspace\jcp')

# ---------- 后端路由表 ----------
routes = io.open('internal/webapi/routes.go', encoding='utf-8').read()
backend = {}   # path -> set(method)
pat = re.compile(
    r'\{\s*Method:\s*"(\w+)",\s*Path:\s*"([^"]+)"\s*(?:,\s*Args:\s*\[\]Arg\{.*?\}\s*)?'
    r'(?:,\s*HTTPMethod:\s*"(\w+)")?\s*\}', re.S)
for m in pat.finditer(routes):
    mname, path, http = m.group(1), m.group(2), m.group(3)
    if http is None:
        seg = m.group(0)
        http = 'POST' if 'B()' in seg else 'GET'
    backend.setdefault(path, set()).add(http)

# ---------- 前端调用 ----------
frontend = []
for d in ('frontend/src/services', 'frontend/src/hooks'):
    if not os.path.isdir(d):
        continue
    for fn in sorted(os.listdir(d)):
        if not fn.endswith('.ts'):
            continue
        content = io.open(os.path.join(d, fn), encoding='utf-8').read()
        # 逐个 callBackend 调用块解析（块内以 ')' 结束，不跨函数）
        for m in re.finditer(
                r"callBackend<[^>]*>\(\s*'([^']+)'\s*,\s*'([^']+)'([^;]*?)\);",
                content, re.S):
            mname, path, rest = m.group(1), m.group(2), m.group(3)
            hm = re.search(r"httpMethod:\s*'(\w+)'", rest)
            http = hm.group(1) if hm else None
            has_body = 'body:' in rest
            frontend.append((fn, mname, path, http, has_body))

print("后端路由: %d 条路径" % len(backend))
print("前端调用: %d 处\n" % len(frontend))

errs = []
for fn, mname, path, http, has_body in frontend:
    if path not in backend:
        errs.append("%s: %s 调用了后端不存在的路径 %s" % (fn, mname, path))
        continue
    if http is None:
        # 未显式指定：运行时按 body 存在与否决定 POST/GET
        http = 'POST' if has_body else 'GET'
    if http not in backend[path]:
        errs.append("%s: %s 用 %s 调用 %s，但后端该路径只支持 %s"
                    % (fn, mname, http, path, sorted(backend[path])))

print("=== 不一致 ===")
print("\n".join("  x " + e for e in errs) if errs else "  无")

sse_paths = {'/api/events', '/api/market/subscribe',
             '/api/market/orderbook/subscribe', '/api/market/kline/subscribe'}
extra = {p for _, _, p, _, _ in frontend} - set(backend) - sse_paths
print("\n=== 既不在路由表也不在 SSE 接口中 ===")
print("\n".join("  x " + p for p in extra) if extra else "  无")

sys.exit(1 if errs else 0)
