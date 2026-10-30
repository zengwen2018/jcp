import React, { useEffect, useMemo, useState } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';
import { OrderBook as OrderBookComponent } from './OrderBook';
import { apiGet } from '../api/client';
import type { ETFQuote, ETFProfile, ETFIOPVPoint, ETFNavPoint, ETFNavSeries } from '../types';

// 数据层：统一走 jcp 的 HTTP API 客户端（自动带 Bearer 鉴权）。
// 与 jcp-src 的 Wails 绑定版本相比，只替换这一层，组件渲染逻辑完全一致。
const getETFQuote = (codes: string[]) => apiGet<ETFQuote[]>('/api/etf/quote', { codes: codes.join(',') });
const getETFProfile = (code: string) => apiGet<ETFProfile>('/api/etf/profile', { code });
const getETFIOPVSeries = (code: string) => apiGet<ETFIOPVPoint[]>('/api/etf/iopv-series', { code });
const getETFNav = (code: string, days: number) => apiGet<ETFNavSeries>('/api/etf/nav', { code, days });

interface ETFPanelProps {
  /** 带市场前缀的代码，如 sh510300 / sz159915 */
  code: string;
}

// ===== 展示用格式化工具 =====

/** ETF 价格普遍为 3 位小数（如 4.105），货基 ETF 为 3 位整数 */
const formatPrice = (value?: number): string => {
  if (value === undefined || value === null || Number.isNaN(value)) return '--';
  return value.toFixed(3);
};

/** IOPV/净值保留 4 位小数，与交易所播报精度一致 */
const formatNav = (value?: number): string => {
  if (value === undefined || value === null || Number.isNaN(value)) return '--';
  return value.toFixed(4);
};

/** 百分比始终带符号，正数显式加 + */
const formatPercent = (value?: number): string => {
  if (value === undefined || value === null || Number.isNaN(value)) return '--';
  return `${value >= 0 ? '+' : ''}${value.toFixed(2)}%`;
};

const formatVolume = (volume?: number): string => {
  if (!volume || Number.isNaN(volume)) return '--';
  if (volume >= 1e8) return `${(volume / 1e8).toFixed(2)}亿份`;
  if (volume >= 1e4) return `${(volume / 1e4).toFixed(2)}万份`;
  return `${volume}份`;
};

const formatAmount = (amount?: number): string => {
  if (!amount || Number.isNaN(amount)) return '--';
  if (amount >= 1e8) return `${(amount / 1e8).toFixed(2)}亿`;
  if (amount >= 1e4) return `${(amount / 1e4).toFixed(2)}万`;
  return amount.toFixed(2);
};

/** 空字符串统一显示为 -- */
const orDash = (value?: string): string => (value && value.trim() ? value : '--');

/**
 * 选择净值序列。
 *
 * 东财 lsjz 按 6 位代码查询，沪深都可用，因此优先使用 GetETFNav 返回的完整序列；
 * 仅当它为空（网络失败或该基金无净值记录）时，才回退画像里附带的近期净值。
 */
const pickNavPoints = (
  primary?: ETFNavPoint[] | null,
  fallback?: ETFNavPoint[] | null,
): ETFNavPoint[] => {
  if (primary && primary.length > 0) return primary;
  if (fallback && fallback.length > 0) return fallback;
  return [];
};

// ===== 折线图（净值 / IOPV 分时共用）=====

interface SparklineProps {
  values: number[];
  stroke: string;
  height?: number;
  showArea?: boolean;
}

const Sparkline: React.FC<SparklineProps> = ({ values, stroke, height = 72, showArea = true }) => {
  const { width, linePath, areaPath } = useMemo(() => {
    const viewWidth = 100;
    if (values.length < 2) {
      return { width: viewWidth, linePath: '', areaPath: '' };
    }
    const min = Math.min(...values);
    const max = Math.max(...values);
    const span = max - min || 1;
    const points = values.map((value, index) => {
      const x = (index / (values.length - 1)) * viewWidth;
      const y = height - ((value - min) / span) * (height - 6) - 3;
      return { x, y };
    });
    const line = points
      .map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(2)},${p.y.toFixed(2)}`)
      .join(' ');
    const area = `${line} L${viewWidth},${height} L0,${height} Z`;
    return { width: viewWidth, linePath: line, areaPath: area };
  }, [values, height]);

  if (!linePath) {
    return (
      <div className="text-[11px] text-slate-500 flex items-center justify-center" style={{ height }}>
        暂无数据
      </div>
    );
  }

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="w-full"
      style={{ height }}
    >
      {showArea && <path d={areaPath} fill={stroke} fillOpacity={0.12} stroke="none" />}
      <path d={linePath} fill="none" stroke={stroke} strokeWidth={1.2} vectorEffect="non-scaling-stroke" />
    </svg>
  );
};

// ===== 折溢价率条 =====

interface PremiumBarProps {
  rate: number;
  isDark: boolean;
  upColor: string;
  downColor: string;
  upClass: string;
  downClass: string;
}

const PremiumBar: React.FC<PremiumBarProps> = ({ rate, isDark, upColor, downColor, upClass, downClass }) => {
  // 显示范围钳制在 ±10%，跨境 ETF 溢价可能远超，直接截断避免条溢出
  const CLAMP = 10;
  const clamped = Math.max(-CLAMP, Math.min(CLAMP, rate));
  const widthPercent = (Math.abs(clamped) / CLAMP) * 50;
  const isPremium = rate >= 0;
  const barColor = isPremium ? upColor : downColor;

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-[11px]">
        <span className={isDark ? 'text-slate-400' : 'text-slate-500'}>折溢价率</span>
        <span className={`font-mono font-semibold ${isPremium ? upClass : downClass}`}>
          {formatPercent(rate)}
          <span className="ml-1 text-[10px] font-normal opacity-70">{isPremium ? '溢价' : '折价'}</span>
        </span>
      </div>
      <div
        className={`relative h-2 rounded-sm overflow-hidden ${isDark ? 'bg-slate-800' : 'bg-slate-200'}`}
        title={`折溢价率 ${formatPercent(rate)}（相对 IOPV）`}
      >
        {/* 0 值中线 */}
        <div
          className={`absolute top-0 bottom-0 w-px ${isDark ? 'bg-slate-600' : 'bg-slate-400'}`}
          style={{ left: '50%' }}
        />
        <div
          className="absolute top-0 bottom-0 transition-all duration-300"
          style={{
            backgroundColor: barColor,
            width: `${Math.max(widthPercent, 0.6)}%`,
            left: isPremium ? '50%' : `${50 - Math.max(widthPercent, 0.6)}%`,
          }}
        />
      </div>
      <div className={`flex justify-between text-[10px] ${isDark ? 'text-slate-600' : 'text-slate-400'}`}>
        <span>折价 -{CLAMP}%</span>
        <span>0</span>
        <span>+{CLAMP}% 溢价</span>
      </div>
    </div>
  );
};

// ===== 指标格子 =====

interface StatCellProps {
  label: string;
  value: string;
  valueClass?: string;
  hint?: string;
}

const StatCell: React.FC<StatCellProps> = ({ label, value, valueClass, hint }) => {
  const { colors } = useTheme();
  return (
    <div className="px-2 py-1.5" title={hint}>
      <div className={`text-[10px] ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{label}</div>
      <div className={`text-xs font-mono ${valueClass ?? (colors.isDark ? 'text-slate-200' : 'text-slate-700')}`}>
        {value}
      </div>
    </div>
  );
};

// ===== 主面板 =====

export const ETFPanel: React.FC<ETFPanelProps> = ({ code }) => {
  const { colors } = useTheme();
  const cc = useCandleColor();

  const [quote, setQuote] = useState<ETFQuote | null>(null);
  const [profile, setProfile] = useState<ETFProfile | null>(null);
  const [iopvSeries, setIopvSeries] = useState<ETFIOPVPoint[]>([]);
  const [navPoints, setNavPoints] = useState<ETFNavPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      setLoading(true);
      setError('');
      setQuote(null);
      setProfile(null);
      setIopvSeries([]);
      setNavPoints([]);

      try {
        // 行情、画像、净值序列是三条独立请求，并行发起。
        // 净值走东财 lsjz（按 6 位代码查询），沪深都可用，因此不分交易所。
        const [quotes, profileData, navData] = await Promise.all([
          getETFQuote([code]),
          getETFProfile(code),
          getETFNav(code, 120),
        ]);
        if (cancelled) return;

        const current = quotes?.[0] ?? null;
        setQuote(current);
        setProfile(profileData);

        if (!current) {
          setError('未获取到该 ETF 的行情数据');
          return;
        }

        // 净值优先用 lsjz 的完整序列，为空时回退画像附带的近期净值
        setNavPoints(pickNavPoints(navData?.points, profileData?.navSeries));

        // IOPV 分时只有深交所提供；沪市仅有瞬时快照值
        if ((current.exchange || '').toLowerCase() === 'sz') {
          const iopvData = await getETFIOPVSeries(code);
          if (cancelled) return;
          setIopvSeries(iopvData ?? []);
        } else {
          setIopvSeries([]);
        }
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : '加载 ETF 数据失败');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    void load();
    return () => {
      cancelled = true;
    };
  }, [code]);

  const isUp = (quote?.change ?? 0) >= 0;
  const changeClass = cc.getColorClass(isUp);

  const navValues = useMemo(
    () => navPoints.map((p) => p.unitNav).filter((v) => Number.isFinite(v) && v > 0),
    [navPoints],
  );
  const iopvValues = useMemo(
    () => iopvSeries.map((p) => p.value).filter((v) => Number.isFinite(v) && v > 0),
    [iopvSeries],
  );

  // 净值首末对比，用于曲线旁的区间涨跌提示
  const navRangeChange = useMemo(() => {
    if (navValues.length < 2) return null;
    const first = navValues[0];
    const last = navValues[navValues.length - 1];
    if (!first) return null;
    return ((last - first) / first) * 100;
  }, [navValues]);

  if (loading) {
    return (
      <div className="h-full fin-panel p-3 space-y-2 animate-pulse">
        <div className={`h-5 w-48 rounded ${colors.isDark ? 'bg-slate-800' : 'bg-slate-200'}`} />
        <div className={`h-10 w-32 rounded ${colors.isDark ? 'bg-slate-800' : 'bg-slate-200'}`} />
        <div className={`h-16 rounded ${colors.isDark ? 'bg-slate-800' : 'bg-slate-200'}`} />
        <div className={`h-24 rounded ${colors.isDark ? 'bg-slate-800' : 'bg-slate-200'}`} />
      </div>
    );
  }

  if (error) {
    return (
      <div className="h-full fin-panel flex flex-col items-center justify-center gap-2 text-xs">
        <div className="text-rose-400">{error}</div>
        <div className={colors.isDark ? 'text-slate-500' : 'text-slate-400'}>
          请确认该代码为场内 ETF（如 sh510300 / sz159915）
        </div>
      </div>
    );
  }

  if (!quote) {
    return null;
  }

  const cardBg = colors.isDark ? 'bg-slate-900/40' : 'bg-slate-50';
  const sectionTitle = `text-[11px] font-medium ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`;

  return (
    <div className="h-full fin-panel overflow-y-auto text-xs">
      {/* 头部：名称 / 代码 / ETF 标签 / T+0 徽章 */}
      <div className={`px-3 py-2 border-b fin-divider-soft flex items-center gap-2 flex-wrap`}>
        <span className={`text-sm font-bold ${colors.isDark ? 'text-white' : 'text-slate-800'}`}>
          {quote.name || quote.symbol}
        </span>
        <span className={`text-[11px] font-mono ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
          {quote.symbol}
        </span>
        <span
          className={`px-1.5 py-0.5 rounded text-[10px] border ${
            colors.isDark
              ? 'border-slate-600 text-slate-300 bg-slate-800/60'
              : 'border-slate-300 text-slate-600 bg-slate-100'
          }`}
        >
          ETF
        </span>
        <span
          className={`px-1.5 py-0.5 rounded text-[10px] font-medium border ${
            quote.t0Flag
              ? colors.isDark
                ? 'border-amber-500/60 text-amber-300 bg-amber-500/15'
                : 'border-amber-500 text-amber-700 bg-amber-100'
              : colors.isDark
                ? 'border-slate-600 text-slate-400 bg-slate-800/40'
                : 'border-slate-300 text-slate-500 bg-slate-100'
          }`}
          title={quote.t0Flag ? 'T+0：当日买入可当日卖出' : 'T+1：当日买入次日可卖出'}
        >
          {quote.t0Flag ? 'T+0' : 'T+1'}
        </span>
        <span className={`ml-auto text-[10px] ${colors.isDark ? 'text-slate-600' : 'text-slate-400'}`}>
          {orDash(quote.dateTime)}
        </span>
      </div>

      {/* 价格区 */}
      <div className="px-3 py-2 border-b fin-divider-soft flex items-end gap-4">
        <div className={`text-2xl font-mono font-bold ${changeClass}`}>
          {formatPrice(quote.marketPrice)}
        </div>
        <div className={`flex items-center gap-3 pb-1 font-mono text-sm ${changeClass}`}>
          <span>
            {quote.change >= 0 ? '+' : ''}
            {quote.change.toFixed(3)}
          </span>
          <span>{formatPercent(quote.changePercent)}</span>
        </div>
      </div>

      {/* 折溢价率条 */}
      <div className="px-3 py-2 border-b fin-divider-soft">
        <PremiumBar
          rate={quote.premiumRate}
          isDark={colors.isDark}
          upColor={cc.upColor}
          downColor={cc.downColor}
          upClass={cc.upClass}
          downClass={cc.downClass}
        />
      </div>

      {/* 指标网格 */}
      <div className={`grid grid-cols-4 border-b fin-divider-soft ${cardBg}`}>
        <StatCell label="IOPV" value={formatNav(quote.iopv)} hint="交易所实时播报的基金份额参考净值" />
        <StatCell
          label="单位净值"
          value={formatNav(quote.nav)}
          hint="T-1 单位净值（东财），沪市可能滞后或无数据"
        />
        <StatCell
          label="涨停价"
          value={formatPrice(quote.limitUpPrice)}
          valueClass={cc.upClass}
          hint="按品种规则计算（股票 ETF ±10%，创业板/科创板成分 ETF ±20%）"
        />
        <StatCell
          label="跌停价"
          value={formatPrice(quote.limitDownPrice)}
          valueClass={cc.downClass}
          hint="按品种规则计算（股票 ETF ±10%，创业板/科创板成分 ETF ±20%）"
        />
        <StatCell label="今开" value={formatPrice(quote.open)} />
        <StatCell label="昨收" value={formatPrice(quote.preClose)} />
        <StatCell label="最高" value={formatPrice(quote.high)} valueClass={cc.upClass} />
        <StatCell label="最低" value={formatPrice(quote.low)} valueClass={cc.downClass} />
        <StatCell label="成交量" value={formatVolume(quote.volume)} />
        <StatCell label="成交额" value={formatAmount(quote.amount)} />
        <StatCell label="交易所" value={(quote.exchange || '--').toUpperCase()} />
        <StatCell
          label="溢价基准"
          value={quote.iopv > 0 ? 'IOPV' : '--'}
          hint="折溢价率 = (市价 - IOPV) / IOPV × 100%"
        />
      </div>

      {/* 分类与画像 */}
      <div className="px-3 py-2 border-b fin-divider-soft space-y-1.5">
        <div className={sectionTitle}>产品信息</div>
        <div className="flex flex-wrap items-center gap-1.5">
          {profile?.fundType ? (
            <span
              className={`px-1.5 py-0.5 rounded text-[10px] ${
                colors.isDark ? 'bg-slate-800 text-slate-300' : 'bg-slate-200 text-slate-600'
              }`}
            >
              {profile.fundType}
            </span>
          ) : null}
          {profile?.investType ? (
            <span
              className={`px-1.5 py-0.5 rounded text-[10px] ${
                colors.isDark ? 'bg-slate-800 text-slate-300' : 'bg-slate-200 text-slate-600'
              }`}
            >
              {profile.investType}
            </span>
          ) : null}
          {!profile?.fundType && !profile?.investType ? (
            <span className={colors.isDark ? 'text-slate-600' : 'text-slate-400'}>分类信息暂不可得</span>
          ) : null}
        </div>
        <div className="grid grid-cols-3 gap-1">
          <StatCell label="规模" value={orDash(profile?.scaleWan)} hint="沪市交易所接口不提供规模，显示 --" />
          <StatCell label="管理人" value={orDash(profile?.manager)} hint="沪市交易所接口不提供管理人，显示 --" />
          <StatCell label="上市日" value={orDash(profile?.listDate)} />
        </div>
      </div>

      {/* 净值曲线 */}
      <div className="px-3 py-2 border-b fin-divider-soft">
        <div className="flex items-center justify-between mb-1">
          <span className={sectionTitle}>单位净值走势（近 {navValues.length} 个交易日）</span>
          {navRangeChange !== null ? (
            <span className={`font-mono text-[11px] ${cc.getColorClass(navRangeChange >= 0)}`}>
              区间 {formatPercent(navRangeChange)}
            </span>
          ) : null}
        </div>
        <Sparkline values={navValues} stroke={navRangeChange !== null && navRangeChange < 0 ? cc.downColor : cc.upColor} />
      </div>

      {/* IOPV 分时（仅深市） */}
      <div className="px-3 py-2 border-b fin-divider-soft">
        <div className="flex items-center justify-between mb-1">
          <span className={sectionTitle}>
            IOPV 分时
            {(quote.exchange || '').toLowerCase() !== 'sz' ? '（沪市仅提供瞬时值，无分时序列）' : ''}
          </span>
          {iopvValues.length > 0 ? (
            <span className={`font-mono text-[11px] ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
              {formatNav(iopvValues[iopvValues.length - 1])}
            </span>
          ) : null}
        </div>
        {(quote.exchange || '').toLowerCase() === 'sz' ? (
          <Sparkline values={iopvValues} stroke={colors.accent2} showArea={false} height={56} />
        ) : (
          <div className={`text-[11px] ${colors.isDark ? 'text-slate-600' : 'text-slate-400'}`}>
            深交所接口提供 IOPV 分时，沪交所仅提供快照值
          </div>
        )}
      </div>

      {/* 五档盘口 */}
      <div className="px-3 py-2">
        <div className={`${sectionTitle} mb-1`}>五档盘口</div>
        <div className="h-48 border fin-divider-soft rounded overflow-hidden">
          <OrderBookComponent data={quote.orderBook} />
        </div>
      </div>
    </div>
  );
};

export default ETFPanel;
