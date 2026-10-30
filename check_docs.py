#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""
文档一致性校验脚本。

用途：改完代码/配置后跑一遍，确认 docs/ 里的描述没有落后于代码实现。

检查项：
  1. 代码里的关键实现特征 → 文档是否有对应描述（正向核对）
  2. 文档里的关键断言     → 代码里是否真实存在（反向核对）
  3. 文档间链接的文件是否存在
  4. 文档内锚点链接是否有效
  5. Markdown 代码块是否闭合
  6. 文档中的数量断言（工具数、端点数、组件数等）是否与代码一致

用法：
  python check_docs.py
退出码：0 = 全部通过，1 = 有偏差
"""

import io
import os
import re
import glob
import sys

DOCS = 'docs'


def doc_files():
    """返回全部文档文件（递归包含子目录）。

    必须递归：docs/etf/ 等子目录下的文档同样需要校验，
    否则新增的文档会游离在守门脚本之外。
    """
    return sorted(glob.glob(os.path.join(DOCS, '**', '*.md'), recursive=True))

# ══════════════════════════════════════════════════════════
# 1 & 2. 语义断言表
#    每条 = (说明, 代码侧判定函数, 文档侧判定函数)
#    判定函数返回 (命中数, 详情列表)
# ══════════════════════════════════════════════════════════

def count_in(path, pattern):
    """统计文件中匹配 pattern 的行数"""
    if not os.path.exists(path):
        return 0, ['文件不存在: %s' % path]
    s = io.open(path, encoding='utf-8').read()
    n = len(re.findall(pattern, s, re.M))
    return n, []


def count_multi(paths, pattern):
    """统计多个文件中的匹配总数"""
    total = 0
    details = []
    for p in paths:
        n, err = count_in(p, pattern)
        total += n
        details.extend(err)
    return total, details


def read_doc(name):
    p = os.path.join(DOCS, name)
    if not os.path.exists(p):
        return ''
    return io.open(p, encoding='utf-8').read()


# (描述, 代码判定, 文档判定, 期望关系)
#   关系 'both'    : 两边都应命中
#   关系 'code_only': 代码有但文档无 → 文档落后，需补
#   关系 'doc_only' : 文档有但代码无 → 文档超前或代码回退，需查
CHECKS = [
    (
        '路由注册使用独立的 registered 集合（非 len==1 / 非栈式 pendingPatterns）',
        lambda: count_in('internal/webapi/router.go', r'registered\[pattern\] = true'),
        lambda: count_in(os.path.join(DOCS, '10-Web化改造.md'), r'registered map\[string\]bool'),
        'both',
    ),
    (
        'favicon.ico 在鉴权白名单内',
        lambda: count_in('internal/webapi/auth.go', r'path == "/favicon\.ico"'),
        lambda: count_in(os.path.join(DOCS, '09-安全模型.md'), r'path == "/favicon\.ico"'),
        'both',
    ),
    (
        '前端有桌面专属能力适配层（windowGetSize 等）',
        lambda: count_in('frontend/src/api/runtime.ts', r'^export async function window'),
        lambda: count_in(os.path.join(DOCS, '10-Web化改造.md'), r'windowGetSize\(\)'),
        'both',
    ),
    (
        '事件回调兼容 Wails 多参数签名',
        lambda: count_in('frontend/src/api/events.ts', r'\.\.\.rest: unknown\[\]'),
        lambda: count_in(os.path.join(DOCS, '10-Web化改造.md'), r'\.\.\.rest: unknown\[\]'),
        'both',
    ),
    (
        '东财接口有备用域名回退（emFallbackHosts）',
        lambda: count_in('internal/services/market_service.go', r'eastmoney\.com"'),
        lambda: count_in(os.path.join(DOCS, '10-Web化改造.md'), r'push2his'),
        'both',
    ),
    (
        '榜单结构带 Error 字段（前端可透出失败原因）',
        lambda: count_in('internal/models/market_fundflow.go', r'Error string `json:"error,omitempty"`'),
        lambda: count_in(os.path.join(DOCS, '05-API-接口.md'), r'data\.error'),
        'both',
    ),
    (
        '前端不再直接引用 Wails 运行时（应为 0）',
        lambda: count_multi(
            glob.glob('frontend/src/**/*.ts', recursive=True) +
            glob.glob('frontend/src/**/*.tsx', recursive=True),
            r'from\s+[\'"].*wailsjs/(runtime|go/main)',
        ),
        lambda: (0, []),
        'both',
    ),
    (
        '路由实现中已无 pendingPatterns 标识符（注释中的反面教材不计，应为 0）',
        # 只匹配代码行，排除以 // 开头的注释行
        lambda: count_in('internal/webapi/router.go', r'^\s*[^/\s].*pendingPatterns'),
        lambda: (0, []),
        'both',
    ),
]


# ══════════════════════════════════════════════════════════
# 3. 数量断言：文档里写的数字必须与代码实际一致
# ══════════════════════════════════════════════════════════
def count_assertions():
    checks = []

    # 内置工具数
    n_tools, _ = count_in('internal/adk/tools/registry.go', r'r\.registerTool\("')
    checks.append(('内置工具数', n_tools, r'37 个内置工具'))

    # 路由端点数
    n_routes, _ = count_in('internal/webapi/routes.go', r'\{Method:')
    checks.append(('HTTP 端点数', n_routes, r'60 个 HTTP 端点|60 个端点'))

    # 前端 service 文件数
    svc = glob.glob('frontend/src/services/*.ts')
    checks.append(('前端 service 文件数', len(svc), r'8 个服务模块|8 个 service|service 8'))

    # React 组件数
    comps = glob.glob('frontend/src/components/*.tsx')
    checks.append(('React 组件数', len(comps), r'15 个 React 组件|components/\s+15'))

    return checks


def main():
    errors = []
    warns = []

    print('=' * 62)
    print('文档一致性校验')
    print('=' * 62)

    # ---- 1 & 2. 语义断言 ----
    print('\n【1】语义断言核对（代码 ↔ 文档）')
    for desc, code_fn, doc_fn, rel in CHECKS:
        cn, cerr = code_fn()
        dn, derr = doc_fn()
        cerr = [e for e in cerr if '文件不存在' not in e]
        derr = [e for e in derr if '文件不存在' not in e]

        if rel == 'both':
            # 双方都是"应为 0"的断言时，只要代码侧为 0 就算通过
            expect_zero = cn == 0 and dn == 0 and '应为 0' in desc
            if expect_zero or (cn > 0 and dn > 0):
                print('  ✓ %s' % desc)
                continue
            msg = '代码命中 %d，文档命中 %d' % (cn, dn)
            if cn > 0 and dn == 0:
                errors.append('%s —— 代码已改，文档未同步' % desc)
            elif cn == 0 and dn > 0:
                errors.append('%s —— 文档有描述，代码里没有' % desc)
            else:
                errors.append('%s —— 两边都没找到' % desc)
            print('  ✗ %s  (%s)' % (desc, msg))
        for e in cerr + derr:
            warns.append('%s: %s' % (desc, e))

    # ---- 3. 数量断言 ----
    print('\n【2】文档中的数量断言')
    doc_all = ''
    for f in doc_files():
        doc_all += io.open(f, encoding='utf-8').read()

    for name, actual, doc_patterns in count_assertions():
        hit = False
        for pat in doc_patterns.split('|'):
            if re.search(pat, doc_all):
                hit = True
                m = re.search(pat, doc_all)
                # 从文档里抽出实际写的数字
                ctx = doc_all[max(0, m.start() - 30):m.end() + 5]
                nums = re.findall(r'\d+', ctx)
                if nums and str(actual) not in nums:
                    errors.append('%s：文档写 %s，代码实际 %d' % (name, nums[0], actual))
                    print('  ✗ %s —— 文档数字与代码不符（实际 %d）' % (name, actual))
                else:
                    print('  ✓ %s = %d' % (name, actual))
                break
        if not hit:
            warns.append('%s：文档中未找到对应表述（实际 %d）' % (name, actual))

    # ---- 4. 链接 ----
    print('\n【3】文档间链接')
    bad_link = 0
    for f in doc_files():
        s = io.open(f, encoding='utf-8').read()
        base_dir = os.path.dirname(f)
        for m in re.finditer(r'\[([^\]]+)\]\(([^)#]+\.md)(#[^)]*)?\)', s):
            # 链接目标相对于当前文档所在目录解析（支持子目录间互链）
            target = os.path.normpath(os.path.join(base_dir, m.group(2)))
            if not os.path.exists(target):
                errors.append('%s -> %s：目标文件不存在' % (os.path.basename(f), m.group(2)))
                bad_link += 1
    print('  %s 文件链接' % ('✗ %d 处失效' % bad_link if bad_link else '✓ 全部有效'))

    # ---- 5. 锚点 ----
    print('\n【4】文档内锚点')
    def slug(t):
        t = t.strip().lower()
        t = re.sub(r'[^\w\u4e00-\u9fff\s-]', '', t)
        return re.sub(r'\s+', '-', t)

    anchors = {}
    for f in doc_files():
        s = io.open(f, encoding='utf-8').read()
        anchors[os.path.basename(f)] = {
            slug(m.group(1)) for m in re.finditer(r'^#{2,4}\s+(.+)$', s, re.M)
        }
    bad_anchor = 0
    for f in doc_files():
        s = io.open(f, encoding='utf-8').read()
        for m in re.finditer(r'\[([^\]]+)\]\(([^)#]+\.md)#([^)]+)\)', s):
            base, anc = os.path.basename(m.group(2)), m.group(3)
            if base in anchors and anc not in anchors[base]:
                errors.append('%s -> %s#%s：锚点不存在' % (os.path.basename(f), base, anc))
                bad_anchor += 1
    print('  %s 锚点' % ('✗ %d 处失效' % bad_anchor if bad_anchor else '✓ 全部有效'))

    # ---- 6. 代码块闭合 ----
    print('\n【5】Markdown 代码块闭合')
    bad_block = 0
    for f in sorted(doc_files()):
        s = io.open(f, encoding='utf-8').read()
        n = s.count('```')
        if n % 2 != 0:
            errors.append('%s：代码块未闭合（``` 计数 %d）' % (os.path.basename(f), n))
            bad_block += 1
    print('  %s 代码块' % ('✗ %d 处未闭合' % bad_block if bad_block else '✓ 全部闭合'))

    # ---- 汇总 ----
    print('\n' + '=' * 62)
    if warns:
        print('提示 %d 条：' % len(warns))
        for w in warns:
            print('  · %s' % w)
    if errors:
        print('偏差 %d 条（需修正）：' % len(errors))
        for e in errors:
            print('  ✗ %s' % e)
        print('=' * 62)
        return 1
    print('全部通过：文档与代码语义一致')
    print('=' * 62)
    return 0


if __name__ == '__main__':
    sys.exit(main())
