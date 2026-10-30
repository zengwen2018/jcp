export interface Stock {
  symbol: string;
  name: string;
  price: number;
  change: number;
  changePercent: number;
  volume: number;
  amount: number;
  marketCap: string;
  sector: string;
  open: number;
  high: number;
  low: number;
  preClose: number;
}

// 股票持仓信息
export interface StockPosition {
  shares: number;    // 持仓数量
  costPrice: number; // 成本价
}

export interface KLineData {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  amount?: number;
  avg?: number; // For intraday average price line
  // 均线数据
  ma5?: number;
  ma10?: number;
  ma20?: number;
}

export interface OrderBookItem {
  price: number;
  size: number;
  total: number;
  percent: number; // For visual bar depth
}

export interface OrderBook {
  bids: OrderBookItem[];
  asks: OrderBookItem[];
}

export enum AgentRole {
  BULL = '多头分析师',
  BEAR = '空头怀疑论者',
  QUANT = '技术量化专家',
  MACRO = '宏观经济学家',
  NEWS = '市场情报员'
}

export interface Agent {
  id: string;
  name: string;
  role: AgentRole;
  avatar: string;
  color: string;
}

export interface ChatMessage {
  id: string;
  agentId: string;
  agentName?: string;
  role?: string;
  content: string;
  timestamp: number;
  replyTo?: string;
  mentions?: string[];
  round?: number;        // 讨论轮次
  msgType?: MsgType;     // 消息类型
}

// 消息类型
export type MsgType = 'opening' | 'opinion' | 'summary';

export type TimePeriod = '1m' | '1d' | '1w' | '1mo';

// 快讯数据结构
export interface Telegraph {
  time: string;
  content: string;
  url: string;
}

// MCP 传输类型
export type MCPTransportType = 'http' | 'sse' | 'command';

// MCP 服务器配置
export interface MCPServerConfig {
  id: string;
  name: string;
  transportType: MCPTransportType;
  endpoint: string;
  command: string;
  args: string[];
  toolFilter: string[];
  enabled: boolean;
}

// 大盘指数数据
export interface MarketIndex {
  code: string;          // 指数代码
  name: string;          // 指数名称
  price: number;         // 当前点位
  change: number;        // 涨跌点数
  changePercent: number; // 涨跌幅(%)
  volume: number;        // 成交量(手)
  amount: number;        // 成交额(万元)
}

// 市场状态
export interface MarketStatus {
  status: string;        // trading, closed, pre_market, lunch_break
  statusText: string;    // 中文状态描述
  isTradeDay: boolean;   // 是否交易日
  holidayName: string;   // 节假日名称
}

export interface StockPeer {
  symbol: string;
  name: string;
  market?: string;
}

export interface FinancialStatements {
  income?: Record<string, any>[];
  balance?: Record<string, any>[];
  cashflow?: Record<string, any>[];
}

export interface PerformanceEvents {
  forecast?: Record<string, any>[];
  express?: Record<string, any>[];
  schedule?: Record<string, any>[];
}

export interface FundFlowSeries {
  fields?: string[];
  lines?: string[][];
  labels?: Record<string, string>;
  latest?: Record<string, any>;
}

export interface InstitutionalHoldings {
  topHolders?: Record<string, any>[];
  controller?: Record<string, any>;
}

export interface IndustryCompare {
  industry?: string;
  peers?: StockPeer[];
}

export interface BonusFinancing {
  dividend?: Record<string, any>[];
  annual?: Record<string, any>[];
  financing?: Record<string, any>[];
  allotment?: Record<string, any>[];
}

export interface BusinessAnalysis {
  scope?: Record<string, any>[];
  composition?: Record<string, any>[];
  review?: Record<string, any>[];
}

export interface ShareholderNumbers {
  records?: Record<string, any>[];
  latest?: Record<string, any>;
}

export interface EquityPledge {
  records?: Record<string, any>[];
  latest?: Record<string, any>;
}

export interface LockupRelease {
  records?: Record<string, any>[];
  latest?: Record<string, any>;
}

export interface ShareholderChanges {
  records?: Record<string, any>[];
  latest?: Record<string, any>;
}

export interface StockBuyback {
  records?: Record<string, any>[];
  latest?: Record<string, any>;
}

export interface F10OperationsRequired {
  latestIndicators?: Record<string, any>;
  latestIndicatorsExtra?: Record<string, any>;
  latestIndicatorsQuote?: Record<string, any>;
  eventReminders?: Record<string, any>[];
  news?: Record<string, any>[];
  announcements?: Record<string, any>[];
  shareholderAnalysis?: Record<string, any>[];
  dragonTigerList?: Record<string, any>[];
  blockTrades?: Record<string, any>[];
  marginTrading?: Record<string, any>[];
  mainIndicators?: Record<string, any>[];
  sectorTags?: Record<string, any>[];
  coreThemes?: Record<string, any>[];
  institutionForecast?: Record<string, any>[];
  forecastChart?: Record<string, any>[];
  reportSummary?: Record<string, any>[];
  researchReports?: Record<string, any>[];
  forecastRevisionTrack?: Record<string, any>[];
}

export interface F10Management {
  managementList?: Record<string, any>[];
  salaryDetails?: Record<string, any>[];
  holdingChanges?: Record<string, any>[];
}

export interface F10CapitalOperation {
  raiseSources?: Record<string, any>[];
  projectProgress?: Record<string, any>[];
}

export interface F10EquityStructure {
  latest?: Record<string, any>[];
  history?: Record<string, any>[];
  composition?: Record<string, any>[];
}

export interface F10RelatedStocks {
  industryRankings?: Record<string, any>[];
  conceptRelations?: Record<string, any>[];
}

export interface F10CoreThemes {
  boardTypes?: Record<string, any>[];
  themes?: Record<string, any>[];
  history?: Record<string, any>[];
  selectedBoardReasons?: Record<string, any>[];
  popularLeaders?: Record<string, any>[];
}

export interface F10IndustryCompareMetrics {
  valuation?: Record<string, any>[];
  performance?: Record<string, any>[];
  growth?: Record<string, any>[];
}

export interface F10MainIndicators {
  latest?: Record<string, any>[];
  yearly?: Record<string, any>[];
  quarterly?: Record<string, any>[];
}

export interface F10ValuationTrend {
  source?: string;
  range?: string;
  requestedRange?: string;
  fallback?: boolean;
  dateType?: number;
  labels?: Record<string, string>;
  pe?: Record<string, any>[];
  pb?: Record<string, any>[];
  ps?: Record<string, any>[];
  pcf?: Record<string, any>[];
}

export interface StockValuation {
  price?: number;
  peTtm?: number;
  pb?: number;
  totalMarketCap?: number;
  floatMarketCap?: number;
  turnoverRate?: number;
  amplitude?: number;
  totalShares?: number;
  floatShares?: number;
}

export interface F10Overview {
  code: string;
  updatedAt?: string;
  source?: string;
  company?: Record<string, any>;
  financials?: FinancialStatements;
  performance?: PerformanceEvents;
  fundFlow?: FundFlowSeries;
  institutions?: InstitutionalHoldings;
  industry?: IndustryCompare;
  bonus?: BonusFinancing;
  business?: BusinessAnalysis;
  shareholders?: ShareholderNumbers;
  pledge?: EquityPledge;
  lockup?: LockupRelease;
  holderChange?: ShareholderChanges;
  buyback?: StockBuyback;
  valuation?: StockValuation;
  operations?: F10OperationsRequired;
  coreThemes?: F10CoreThemes;
  industryMetrics?: F10IndustryCompareMetrics;
  mainIndicators?: F10MainIndicators;
  management?: F10Management;
  capitalOperation?: F10CapitalOperation;
  equityStructure?: F10EquityStructure;
  relatedStocks?: F10RelatedStocks;
  valuationTrend?: F10ValuationTrend;
  errors?: Record<string, string>;
}

// ===== A股 ETF 相关类型（对齐 internal/models/etf.go）=====

// ETF 实时行情聚合（二级市价 + IOPV + 折溢价 + 涨跌停 + T+0）
export interface ETFQuote {
  symbol: string;        // 带市场前缀代码，如 sh510300 / sz159915
  name: string;
  exchange: string;      // sh / sz
  marketPrice: number;   // 二级市场最新价
  preClose: number;
  open: number;
  high: number;
  low: number;
  volume: number;        // 成交量（份）
  amount: number;        // 成交额（元）
  change: number;        // 涨跌额
  changePercent: number; // 涨跌幅（%）
  iopv: number;          // 基金份额参考净值（交易所直连）
  nav?: number;          // 单位净值（T-1，可选）
  premiumRate: number;   // 折溢价率（%，正=溢价）
  limitUpPrice: number;  // 涨停价
  limitDownPrice: number;// 跌停价
  t0Flag: boolean;       // 是否 T+0 回转交易
  orderBook: OrderBook;  // 五档盘口
  dateTime: string;      // 行情时间
}

// 单条净值记录（东财 lsjz）
export interface ETFNavPoint {
  date: string;             // yyyy-MM-dd
  unitNav: number;          // 单位净值
  accumNav: number;         // 累计净值
  changePercent: number;    // 日涨跌（%）
  subscribeStatus: string;  // 申购状态
  redeemStatus: string;     // 赎回状态
}

// ETF 净值历史序列
export interface ETFNavSeries {
  code: string;
  name: string;
  points: ETFNavPoint[];    // 按日期升序
}

// IOPV 分时单点（仅深交所提供）
export interface ETFIOPVPoint {
  time: string;   // HH:MM
  value: number;  // IOPV 值
}

// ETF 清单条目（多源合并）
export interface ETFListEntry {
  code: string;        // 6 位代码
  name: string;
  exchange: string;    // sh / sz
  fundType: string;    // 基金类别（ETF / LOF / 不动产基金）
  investType: string;  // 投资类别（股票基金 / 债券基金 / 其它基金 ...）
  scaleWan: string;    // 当前规模（万份，原始字符串；沪市为空）
  manager: string;     // 基金管理人（沪市为空）
  listDate: string;    // 上市日期
  t0Flag: boolean;     // 是否 T+0
}

// 分页 ETF 目录结果
export interface ETFListResult {
  total: number;
  page: number;
  size: number;
  items: ETFListEntry[];
}

// ETF 静态画像
export interface ETFProfile {
  code: string;
  name: string;
  exchange: string;
  fundType: string;
  investType: string;
  scaleWan: string;
  manager: string;
  listDate: string;
  t0Flag: boolean;
  navSeries?: ETFNavPoint[];  // 近期净值（可选）
}
