import re, io, os

os.chdir(r'D:\project-workspace\jcp')

# ---------- 收集 App 方法 ----------
app = io.open('app.go', encoding='utf-8').read()
methods = {}
# 支持有返回值与无返回值两种签名
pat = re.compile(r'^func \(a \*App\) ([A-Z]\w*)\(([^)]*)\)(?:\s+([^\{]+))?\{', re.M)
for m in pat.finditer(app):
    name, params = m.group(1), m.group(2)
    ret = (m.group(3) or '').strip()
    ptypes = []
    if params.strip():
        for p in params.split(','):
            toks = p.strip().split()
            ptypes.append(toks[-1] if len(toks) > 1 else toks[0])
    methods[name] = (ptypes, ret)

# ---------- 收集路由表 ----------
routes = io.open('internal/webapi/routes.go', encoding='utf-8').read()
route_entries = []
# 两种格式都要匹配：
#   {Method: "X", Path: "Y", Args: []Arg{...}}
#   {Method: "X", Path: "Y"}
pat_with_args = re.compile(
    r'\{\s*Method:\s*"(\w+)",\s*Path:\s*"([^"]+)"\s*,\s*Args:\s*\[\]Arg\{(.*?)\}\s*\}',
    re.S)
pat_no_args = re.compile(
    r'\{\s*Method:\s*"(\w+)",\s*Path:\s*"([^"]+)"\s*\}')

for m in pat_with_args.finditer(routes):
    name, path, args = m.group(1), m.group(2), m.group(3)
    n = len(re.findall(r'\b[QB]\s*\(', args))
    route_entries.append((name, path, n, args.strip()))

for m in pat_no_args.finditer(routes):
    name, path = m.group(1), m.group(2)
    route_entries.append((name, path, 0, ''))

print("App 导出方法: %d 个 | 路由表: %d 条" % (len(methods), len(route_entries)))

errs = []
seen = set()
path_map = {}
for name, path, n, args in route_entries:
    if name in seen:
        errs.append("重复方法名: %s" % name)
    seen.add(name)
    path_map.setdefault(path, []).append(name)

    if name not in methods:
        errs.append("路由指向不存在的方法: %s (%s)" % (name, path))
        continue
    ptypes, ret = methods[name]
    if len(ptypes) != n:
        errs.append("参数数量不匹配 %s: 方法 %d 个 vs 路由 %d 个 -> %s" %
                    (name, len(ptypes), n, ptypes))
    # body 参数应与结构体类型匹配
    if 'B()' in args:
        has_struct = any('struct' in t or t.startswith('models.') or
                         t.endswith('Request') for t in ptypes)
        if not has_struct:
            errs.append("%s 声明了 body 参数但方法签名无结构体: %s" % (name, ptypes))

print("\n=== 错误 ===")
print("\n".join("  x " + e for e in errs) if errs else "  无")

missing = sorted(set(methods) - seen)
print("\n=== 未纳入路由表的方法 ===")
for m in missing:
    print("  - %s(%s) -> %s" % (m, methods[m][0], methods[m][1] or "void"))

print("\n=== 同一路径挂多个方法（需确认 HTTP 方法不冲突）===")
for p, v in path_map.items():
    if len(v) > 1:
        print("  %s <- %s" % (p, v))

print("\n=== 路径总数: %d ===" % len(path_map))
