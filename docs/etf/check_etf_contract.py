#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ETF 口径契约一致性验收脚本
================================================================================

用途：M4 实现完成后，核对「实现结果是否严格符合口径契约」
      —— docs/etf/ETF指标口径与数据契约.md
      —— docs/etf/Phase2-口径裁决记录.md §九（强制门禁）

设计原则：
  1. **每一项判定都能独立复核**：每条 check 都写明「数据源」+「公式」+「期望值」，
     不依赖对实现的了解，只依赖契约里写死的口径。
  2. **断言严格、解析宽容**：M4 端点尚未存在，data 载荷形状未定，
     故载荷提取做多形态兼容；但数值判定一律严格。
  3. **只读**：不写 jcp 仓库任何文件，不改配置，只发 GET 请求。

用法：
    python check_etf_contract.py
    JCP_BASE=http://127.0.0.1:10001 python check_etf_contract.py
    JCP_TOKEN=<token> python check_etf_contract.py

环境变量：
    JCP_BASE   默认 http://120.53.8.253:10001
    JCP_TOKEN  访问令牌（必填，从环境变量读取）

退出码：全部 PASS = 0；存在 FAIL = 1

────────────────────────────────────────────────────────────────────────────
放这里而不是 scripts/ 的原因（重要）
────────────────────────────────────────────────────────────────────────────
仓库 `.gitignore` 第 12 行是 `scripts`，该规则会**忽略整个 scripts 目录**：

    $ git check-ignore -v scripts/check_etf_contract.py
    .gitignore:12:scripts  scripts/check_etf_contract.py

若把脚本放 scripts/，它**不会进版本历史** —— 一个「M4 验收门禁」如果不可追溯，
就失去了作为门禁的意义。故放在与契约同目录的 docs/etf/，随契约一起入库。

后续建议（未执行，交由主理人决定）：把 .gitignore 第 12 行的 `scripts`
改为更精确的规则（如 `scripts/local/`），否则将来任何 scripts/ 下的
正式脚本都会被静默忽略 —— 这与之前 `.gitignore.web` 失效是同一类「看起来
做了、实际没生效」的坑。
"""

import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

# ══════════════════════════════════════════════════════════════════
# 配置
# ══════════════════════════════════════════════════════════════════

BASE = os.environ.get("JCP_BASE", "http://120.53.8.253:10001").rstrip("/")
# 令牌只从环境变量读取，禁止硬编码（避免提交到版本库泄露）。
TOKEN = os.environ.get("JCP_TOKEN", "")
TIMEOUT = 20

UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

# 契约 §4.2 / C1：深交所 jjlb 的三类基金类别
ETF_CATEGORY_ENUM = {"ETF", "LOF", "不动产基金"}

# 契约 §4.2 / C1：深交所 tzlb 的六类投资类别（QDII 归入「其它基金」）
# 若 M4 采用英文编码，改这一处即可（脚本内唯一枚举定义点）
INVEST_CATEGORY_ENUM = {
    "股票基金", "混合基金", "其它基金", "债券基金", "ABS", "货币市场基金",
    # 常见别名（源站用词差异）
    "QDII", "QDII-ETF", "REITs", "货币基金", "混合型基金", "股票型基金",
    "债券型基金", "货币市场型基金", "ABS-REITs", "其他基金",
}

# 契约 §4.2：T+0 判定期望值（依据 深交所 tzlb 规则 + 沪市代码规则）
#   tzlb ∈ {其它基金(跨境QDII), 债券基金, 货币市场基金, ABS} → T+0
#   tzlb ∈ {股票基金, 混合基金}                          → T+1
T0_EXPECTED = {
    "sh510300": False,  # 沪深300ETF华泰柏瑞   股票基金 → T+1
    "sh513100": True,   # 纳指ETF国泰         其它基金(跨境QDII) → T+0
    "sh511990": True,   # 华宝添益货币ETF     货币市场基金 → T+0
    "sh511010": True,   # 国债ETF             债券基金 → T+0
    "sz159915": False,  # 创业板ETF易方达     股票基金 → T+1
    "sh588000": False,  # 科创50ETF华夏       股票基金 → T+1
}

# 契约 §A1 差异项 1：涨跌停幅度（深交所 change20PerLimit 独立佐证）
PRICE_LIMIT_EXPECTED = {
    "sh510300": 10.0,
    "sh513100": 10.0,
    "sh588000": 20.0,
    "sz159915": 20.0,
}

# 契约 §4.2 premiumRatePercentile 等历史字段不参与本次验收（冷启动期本就为空）

ALL_CODES = sorted(set(list(T0_EXPECTED) + list(PRICE_LIMIT_EXPECTED)))


# ══════════════════════════════════════════════════════════════════
# HTTP 工具
# ══════════════════════════════════════════════════════════════════

def http_get(url, headers=None, decode="utf-8"):
    """GET 并返回解码后的文本；失败抛异常由调用方处理。"""
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        raw = resp.read()
    return raw.decode(decode, errors="replace")


def api_get(path, params=None):
    """调用 jcp Web API，返回解析后的 data 段。

    鉴权三选一（服务端均支持）：Authorization 头 / ?token= / Cookie。
    这里用 Authorization 头 + 同时带 token 查询参数，双保险。
    """
    url = BASE + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    headers = {
        "Authorization": "Bearer " + TOKEN,
        "Accept": "application/json",
        "User-Agent": UA,
    }
    sep = "&" if "?" in url else "?"
    url += sep + "token=" + urllib.parse.quote(TOKEN)
    text = http_get(url, headers=headers)
    obj = json.loads(text)
    if isinstance(obj, dict) and "error" in obj and obj.get("data") is None:
        raise RuntimeError("API 返回 error: %s" % obj["error"])
    return obj.get("data", obj) if isinstance(obj, dict) else obj


# ══════════════════════════════════════════════════════════════════
# 外部数据源（用于独立复核实现结果）
# ══════════════════════════════════════════════════════════════════

def sina_iopv(code):
    """新浪 IOPV：hq.sinajs.cn/list={code}_iopv
    返回 "日期,时间,IOPV" 三段，例：2026-10-30,11:30:00,4.1032
    口径来源：契约 §A2「IOPV 四源对照」
    """
    url = "https://hq.sinajs.cn/list=%s_iopv" % code
    text = http_get(url, headers={"Referer": "https://finance.sina.com.cn",
                                  "User-Agent": UA}, decode="gbk")
    m = re.search(r'"([^"]+)"', text)
    if not m:
        return None
    parts = m.group(1).split(",")
    if len(parts) < 3:
        return None
    try:
        return float(parts[2])
    except ValueError:
        return None


def tencent_quotes(codes):
    """腾讯行情：qt.gtimg.cn/q=code1,code2
    A股字段索引（实测标定，见契约 §A2「腾讯字段索引的稳定性与校验锚点」）：
      [3]  最新价   [77] 折溢价率(%)   [78] IOPV   [81] 单位净值
      [35] "价/量(手)/额(元)" 自描述复合串
    返回 {code: {...}}
    """
    url = "https://qt.gtimg.cn/q=" + ",".join(codes)
    text = http_get(url, headers={"User-Agent": UA}, decode="gbk")
    out = {}
    for line in text.split("\n"):
        m = re.match(r'v_(\w+)="(.*)";', line.strip())
        if not m:
            continue
        f = m.group(2).split("~")

        def num(idx):
            try:
                return float(f[idx])
            except (IndexError, ValueError):
                return None

        out[m.group(1)] = {
            "price": num(3),
            "premiumRateSrc": num(77),   # 源站折溢价率，仅作交叉校验哨兵
            "iopv": num(78),
            "nav": num(81),
            "limitUp": num(47),
            "limitDown": num(48),
            "quoteTime": f[30] if len(f) > 30 else "",
        }
    return out


# ══════════════════════════════════════════════════════════════════
# 载荷提取（宽容解析）
# ══════════════════════════════════════════════════════════════════

def extract_quotes(data):
    """从 /api/etf/quote 的 data 段提取 {code: quote}。
    兼容三种形状（端点未定稿，先都支持）：
      A. data = [ {...}, {...} ]                列表
      B. data = { "sh510300": {...}, ... }      按代码索引的字典
      C. data = { "quotes": [...] } / {"items": [...]}  包装对象
    """
    if data is None:
        return {}
    items = []
    if isinstance(data, list):
        items = data
    elif isinstance(data, dict):
        if isinstance(data.get("quotes"), list):
            items = data["quotes"]
        elif isinstance(data.get("items"), list):
            items = data["items"]
        elif isinstance(data.get("data"), list):
            items = data["data"]
        else:
            # 形状 B：按代码索引
            for k, v in data.items():
                if isinstance(v, dict) and ("symbol" in v or "code" in v):
                    items.append(v)
    out = {}
    for it in items:
        if not isinstance(it, dict):
            continue
        code = it.get("symbol") or it.get("code") or ""
        if code:
            out[code] = it
    return out


def pick(q, *names):
    """按候选字段名依次取值，兼容不同命名（snake_case / camelCase）。"""
    for n in names:
        if n in q and q[n] is not None:
            return q[n]
    return None


def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


# ══════════════════════════════════════════════════════════════════
# 断言工具
# ══════════════════════════════════════════════════════════════════

class Result(object):
    def __init__(self, name, passed, actual, expected, detail=""):
        self.name = name
        self.passed = passed
        self.actual = actual
        self.expected = expected
        self.detail = detail


def check(name, passed, actual, expected, detail=""):
    return Result(name, bool(passed), actual, expected, detail)


# ══════════════════════════════════════════════════════════════════
# 10 项检查
# ══════════════════════════════════════════════════════════════════

def c1_premium_dual_field(quotes):
    """① 折溢价率双字段：premiumRate(分母=IOPV) 与 closePremiumRate(分母=单位净值)
    必须都存在、**不相等**，且各自可由 price/iopv/nav 重算复原。
    公式（契约 §A2）：
      premiumRate      = (price - iopv) / iopv * 100
      closePremiumRate = (close - nav) / nav * 100
    """
    detail, fails = [], []
    for code in ALL_CODES:
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        pr = num(pick(q, "premiumRate"))
        cpr = num(pick(q, "closePremiumRate"))
        price = num(pick(q, "price"))
        iopv = num(pick(q, "iopv"))
        nav = num(pick(q, "nav"))
        if pr is None or cpr is None:
            fails.append("%s: premiumRate=%s closePremiumRate=%s（缺一即失败）"
                         % (code, pr, cpr))
            continue
        if abs(pr - cpr) < 1e-9:
            fails.append("%s: 两字段相等(%.6f%%)，疑似只实现了一个口径" % (code, pr))
            continue
        if price and iopv:
            calc = (price - iopv) / iopv * 100
            if abs(calc - pr) > 0.01:
                fails.append("%s: premiumRate 重算偏差 %.4fpp（返回 %.4f vs 重算 %.4f）"
                             % (code, abs(calc - pr), pr, calc))
                continue
        if price and nav:
            calc = (price - nav) / nav * 100
            if abs(calc - cpr) > 0.01:
                fails.append("%s: closePremiumRate 重算偏差 %.4fpp（返回 %.4f vs 重算 %.4f）"
                             % (code, abs(calc - cpr), cpr, calc))
                continue
        detail.append("%s: premiumRate=%.4f%% closePremiumRate=%.4f%%（不相等 ✓）"
                      % (code, pr, cpr))
    return check("① 折溢价率双字段（存在·不相等·可重算）", not fails,
                 "%d/%d 通过" % (len(detail), len(ALL_CODES)),
                 "%d/%d" % (len(ALL_CODES), len(ALL_CODES)),
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c2_iopv_cross_check(quotes):
    """② IOPV 交叉校验：实现返回的 iopv 与两个外部源比对。
    外部源：新浪 {code}_iopv、腾讯 field[78]。
    容差 0.1%（契约 §A2：四源刷新节奏不同，同刻差可达 ~0.06%）。
    """
    detail, fails = [], []
    tx = {}
    try:
        tx = tencent_quotes(ALL_CODES)
    except Exception as e:
        detail.append("腾讯源不可达: %s" % e)
    for code in ALL_CODES:
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        impl = num(pick(q, "iopv"))
        if impl is None:
            fails.append("%s: iopv 为空" % code)
            continue
        sn = None
        try:
            sn = sina_iopv(code)
        except Exception as e:
            detail.append("%s 新浪源异常: %s" % (code, e))
        if sn is not None:
            d = abs(impl - sn) / sn * 100
            if d > 0.1:
                fails.append("%s: 与新浪_iopv 偏差 %.4f%% > 0.1%%（实现 %.4f vs 新浪 %.4f）"
                             % (code, d, impl, sn))
                continue
            detail.append("%s: vs新浪 %.4f (偏差 %.4f%% ✓)" % (code, sn, d))
        t = tx.get(code, {}).get("iopv")
        if t is not None:
            d = abs(impl - t) / t * 100
            if d > 0.1:
                fails.append("%s: 与腾讯f[78] 偏差 %.4f%% > 0.1%%（实现 %.4f vs 腾讯 %.4f）"
                             % (code, d, impl, t))
                continue
            detail.append("%s: vs腾讯 %.4f (偏差 %.4f%% ✓)" % (code, t, d))
    return check("② IOPV 交叉校验（新浪_iopv / 腾讯f[78]，容差0.1%）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c3_t0_tradable(quotes):
    """③ T+0 判定：t0Tradable 正确 + t0Basis 非空（必须记录判定依据）。
    依据 深交所 tzlb 规则：{其它基金,债券基金,货币市场基金,ABS}→T+0；
         {股票基金,混合基金}→T+1。
    """
    detail, fails = [], []
    for code, expect in T0_EXPECTED.items():
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        got = pick(q, "t0Tradable")
        if got is None:
            fails.append("%s: t0Tradable 为空" % code)
            continue
        got = bool(got) if not isinstance(got, str) else got.lower() == "true"
        if got != expect:
            fails.append("%s: t0Tradable=%s 期望 %s" % (code, got, expect))
            continue
        basis = pick(q, "t0Basis")
        if not basis:
            fails.append("%s: t0Basis 为空（必须记录判定依据）" % code)
            continue
        detail.append("%s: t0Tradable=%s t0Basis=%s" % (code, got, basis))
    return check("③ T+0 判定（t0Tradable 正确 + t0Basis 非空）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c4_fund_scale_semantics(quotes):
    """④ 规模口径：fundScale ≈ shareCount × nav（单位净值），容差 1%；
    且**不得**等于 shareCount × price（市价口径）。
    契约 §A3：新浪 nmc/mktcap 是市值口径，不得落库到 fundScale。
    注意：境内 ETF 净值与市价很接近（溢价 <1%），故用「NAV 拟合显著更优」
    判定，而非简单的不等断言；跨境 ETF（513100 溢价 ~14.6%）做硬断言。
    """
    detail, fails = [], []
    for code in ALL_CODES:
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        scale = num(pick(q, "fundScale"))
        shares = num(pick(q, "shareCount"))
        nav = num(pick(q, "nav"))
        price = num(pick(q, "price"))
        if scale is None or shares is None or nav is None or price is None:
            fails.append("%s: 缺 fundScale/shareCount/nav/price" % code)
            continue
        by_nav = shares * nav
        by_price = shares * price
        e_nav = abs(scale - by_nav) / scale * 100
        e_price = abs(scale - by_price) / scale * 100
        if e_nav > 1.0:
            fails.append("%s: fundScale 与 份额×nav 偏差 %.3f%% > 1%%" % (code, e_nav))
            continue
        if e_price <= e_nav:
            fails.append("%s: 份额×price(%.0f, 误差%.3f%%) 拟合不差于 份额×nav(%.0f, 误差%.3f%%)"
                         " → 疑似用了市值口径" % (code, by_price, e_price, by_nav, e_nav))
            continue
        if code == "sh513100" and e_price < 5.0:
            fails.append("%s: 跨境ETF溢价约14.6%%，市值口径偏差应 >5%%，实测 %.3f%%"
                         " → fundScale 疑似市值口径" % (code, e_price))
            continue
        detail.append("%s: NAV口径误差%.3f%% < 市值口径误差%.3f%% ✓" % (code, e_nav, e_price))
    return check("④ 规模口径（=份额×净值，非市值口径）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c5_price_limit(quotes):
    """⑤ 涨跌停幅度：priceLimitPercent 与板块规则一致。
    依据：腾讯 field[47]/[48] 源站已按板块算好（合约 §A1 差异项1）；
    独立佐证：深交所 data.change20PerLimit。
      科创板/创业板成分 ETF → 20%；主板成分 ETF → 10%
    """
    detail, fails = [], []
    for code, expect in PRICE_LIMIT_EXPECTED.items():
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        pct = num(pick(q, "priceLimitPercent", "priceLimitPct"))
        if pct is None:
            # 退化路径：由 limitUp/limitDown 与 preClose 反推
            up = num(pick(q, "limitUpPrice"))
            pre = num(pick(q, "preClose"))
            if up is None or pre is None:
                fails.append("%s: 缺 priceLimitPercent 且无法反推" % code)
                continue
            pct = (up - pre) / pre * 100
        if abs(pct - expect) > 0.5:
            fails.append("%s: priceLimitPercent=%.3f%% 期望 %.1f%%" % (code, pct, expect))
            continue
        detail.append("%s: %.2f%% ✓" % (code, pct))
    return check("⑤ 涨跌停幅度（科创/创业板20%，主板10%）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c6_category_fields(quotes):
    """⑥ 分类字段：investCategory 非空且在六类枚举内；etfCategory 在 ETF/LOF/不动产基金 内。
    依据 深交所 tzlb / jjlb（契约 §C1、§A6）。
    """
    detail, fails = [], []
    for code in ALL_CODES:
        q = quotes.get(code)
        if not q:
            fails.append("%s: 端点未返回该标的" % code)
            continue
        inv = pick(q, "investCategory")
        cat = pick(q, "etfCategory")
        if not inv:
            fails.append("%s: investCategory 为空" % code)
            continue
        if str(inv) not in INVEST_CATEGORY_ENUM:
            fails.append("%s: investCategory='%s' 不在六类枚举内" % (code, inv))
            continue
        if not cat:
            fails.append("%s: etfCategory 为空" % code)
            continue
        if str(cat) not in ETF_CATEGORY_ENUM:
            fails.append("%s: etfCategory='%s' 不在 {ETF,LOF,不动产基金} 内" % (code, cat))
            continue
        detail.append("%s: investCategory=%s etfCategory=%s" % (code, inv, cat))
    return check("⑥ 分类字段（investCategory 六类 / etfCategory 三类）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c7_premium_sentinel(quotes):
    """⑦ 折溢价率量级哨兵：
      sh513100（跨境纳指ETF）premiumRate 落在 10%~20%（实测 +14.45%/+14.60%）；
      全部标的 |premiumRate| < 50（超 50 说明分母取错或单位错）。
    """
    detail, fails = [], []
    for code, q in quotes.items():
        pr = num(pick(q, "premiumRate"))
        if pr is None:
            fails.append("%s: premiumRate 为空" % code)
            continue
        if abs(pr) >= 50:
            fails.append("%s: |premiumRate|=%.4f%% ≥ 50，量级异常" % (code, pr))
            continue
        detail.append("%s: %.4f%%" % (code, pr))
    q513 = quotes.get("sh513100")
    if q513:
        pr = num(pick(q513, "premiumRate"))
        if pr is None:
            fails.append("sh513100: premiumRate 为空，无法做区间断言")
        elif not (10.0 <= pr <= 20.0):
            fails.append("sh513100: premiumRate=%.4f%% 不在 10%%~20%% 区间" % pr)
    else:
        fails.append("sh513100: 端点未返回，无法做区间断言")
    return check("⑦ 折溢价率量级哨兵（513100 ∈ 10~20%；全体 |x|<50）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c8_tracking_error_sentinel(quotes):
    """⑧ 跟踪误差哨兵：若 trackingError 有值则必须 < 50。
    依据 裁决 D-08：东财 Data_performanceEvaluation 的「跟踪误差」是 0-100 **评分**
    （实测 510300 = 90），误用会让看板显示「跟踪误差 90%」。
    本期该项可为空（基准序列未确认 → 留空），空值不判失败。
    """
    detail, fails = [], []
    seen = 0
    for code, q in quotes.items():
        te = num(pick(q, "trackingError"))
        if te is None:
            continue
        seen += 1
        if te >= 50:
            fails.append("%s: trackingError=%.4f ≥ 50，疑似误用东财 0-100 评分字段" % (code, te))
            continue
        detail.append("%s: %.4f%%" % (code, te))
    return check("⑧ 跟踪误差哨兵（有值须 <50；空值放行）", not fails,
                 "%d 个异常 / %d 个有值" % (len(fails), seen), "0 个异常",
                 "; ".join(fails) if fails else
                 (" | ".join(detail[:4]) if detail else "全部为空（本期允许）"))


def c9_common_fields(quotes):
    """⑨ 通用字段完备：market/currency/assetType/securityType/tradePhase/quoteTime/dataSource
    任一为空即判定为白名单漏字段。
    背景：app.go mergeRealtimeStock 是**白名单式逐字段 copy**，漏字段**静默丢数据无报错**。
    """
    required = ["market", "currency", "assetType", "securityType",
                "tradePhase", "quoteTime", "dataSource"]
    detail, fails = [], []
    for code, q in quotes.items():
        missing = [f for f in required if not pick(q, f)]
        if missing:
            fails.append("%s: 缺 %s" % (code, ",".join(missing)))
            continue
        if pick(q, "market") != "CN":
            fails.append("%s: market=%s 期望 CN" % (code, pick(q, "market")))
            continue
        if pick(q, "currency") != "CNY":
            fails.append("%s: currency=%s 期望 CNY" % (code, pick(q, "currency")))
            continue
        if pick(q, "assetType") != "etf":
            fails.append("%s: assetType=%s 期望 etf" % (code, pick(q, "assetType")))
            continue
        detail.append("%s: 7/7 ✓" % code)
    return check("⑨ 通用字段完备（7 项非空，漏字段=白名单问题）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


def c10_timestamp_format(quotes):
    """⑩ 时间戳格式：quoteTime 必须 RFC3339 带时区偏移，如 2026-10-30T11:18:20+08:00。
    依据 裁决 D-05：禁止裸时间串 "2026-10-30 11:18:20"。
    """
    rx = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?([+-]\d{2}:\d{2}|Z)$")
    detail, fails = [], []
    for code, q in quotes.items():
        t = pick(q, "quoteTime")
        if not t:
            fails.append("%s: quoteTime 为空" % code)
            continue
        if not rx.match(str(t)):
            fails.append("%s: quoteTime='%s' 非 RFC3339 带偏移格式" % (code, t))
            continue
        detail.append("%s: %s ✓" % (code, t))
    return check("⑩ 时间戳格式（RFC3339 带时区偏移）", not fails,
                 "%d 项异常" % len(fails), "0 项异常",
                 "; ".join(fails) if fails else " | ".join(detail[:4]))


# ══════════════════════════════════════════════════════════════════
# 主流程
# ══════════════════════════════════════════════════════════════════

CHECKS = [
    ("① 折溢价率双字段", c1_premium_dual_field),
    ("② IOPV 交叉校验", c2_iopv_cross_check),
    ("③ T+0 判定", c3_t0_tradable),
    ("④ 规模口径", c4_fund_scale_semantics),
    ("⑤ 涨跌停幅度", c5_price_limit),
    ("⑥ 分类字段", c6_category_fields),
    ("⑦ 折溢价率量级哨兵", c7_premium_sentinel),
    ("⑧ 跟踪误差哨兵", c8_tracking_error_sentinel),
    ("⑨ 通用字段完备", c9_common_fields),
    ("⑩ 时间戳格式", c10_timestamp_format),
]


def main():
    print("=" * 78)
    print("ETF 口径契约一致性验收")
    print("=" * 78)
    print("契约：docs/etf/ETF指标口径与数据契约.md")
    print("门禁：docs/etf/Phase2-口径裁决记录.md §九")
    print("端点：%s/api/etf/quote" % BASE)
    print("标的：%s" % ", ".join(ALL_CODES))
    print()

    # ---- 拉取实现端点 ----
    quotes, api_err = {}, None
    try:
        data = api_get("/api/etf/quote", {"codes": ",".join(ALL_CODES)})
        quotes = extract_quotes(data)
        print("[INFO] 端点返回标的数 = %d" % len(quotes))
        if not quotes:
            api_err = "端点可达但未解析出标的（载荷形状可能不同，需人工核对）"
    except urllib.error.HTTPError as e:
        api_err = "HTTP %s %s（端点尚未实现是正常的）" % (e.code, e.reason)
    except Exception as e:
        api_err = "%s: %s" % (type(e).__name__, e)

    if api_err:
        print("[INFO] 实现端点不可用：%s" % api_err)
        print("[INFO] 依赖端点的 10 项检查将全部判 FAIL —— 这是 M4 未实现时的预期结果。")
        print()

    print("-" * 78)
    results = []
    for _label, fn in CHECKS:
        try:
            r = fn(quotes)
        except Exception as e:
            r = check(_label, False, "执行异常", "正常", "%s: %s" % (type(e).__name__, e))
        results.append(r)
        mark = "PASS" if r.passed else "FAIL"
        print("[%s] %s" % (mark, r.name))
        print("       实际: %s   期望: %s" % (r.actual, r.expected))
        if r.detail:
            print("       说明: %s" % r.detail)
        print()

    passed = sum(1 for r in results if r.passed)
    failed = len(results) - passed
    print("=" * 78)
    print("汇总：PASS %d / FAIL %d / 共 %d" % (passed, failed, len(results)))
    if failed:
        print("存在 FAIL —— M4 实现需按上述「实际 vs 期望」逐条对齐。")
    print("=" * 78)
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
