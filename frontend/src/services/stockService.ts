// 市场数据服务 - 调用后端API
import { callBackend } from '../api/runtime';
import type { Stock, KLineData, OrderBook } from '../types';

// 股票搜索结果类型
export interface StockSearchResult {
  symbol: string;
  name: string;
  industry: string;
  market: string;
}

export const getStockRealTimeData = async (codes: string[]): Promise<Stock[]> => {
  return await callBackend<Stock[]>('GetStockRealTimeData', '/api/stock/realtime', [codes], {
    query: { codes: codes.join(',') },
  });
};

export const getKLineData = async (code: string, period: string, days: number): Promise<KLineData[]> => {
  return await callBackend<KLineData[]>('GetKLineData', '/api/stock/kline', [code, period, days], {
    query: { code, period, days },
  });
};

// 获取真实五档盘口数据
export const getOrderBook = async (code: string): Promise<OrderBook> => {
  return await callBackend<OrderBook>('GetOrderBook', '/api/stock/orderbook', [code], {
    query: { code },
  });
};

// 搜索股票
export const searchStocks = async (keyword: string): Promise<StockSearchResult[]> => {
  if (!keyword.trim()) return [];
  return await callBackend<StockSearchResult[]>('SearchStocks', '/api/stock/search', [keyword], {
    query: { keyword },
  });
};
