import { useEffect, useCallback, useRef } from 'react';
import {
  onEvent,
  subscribe,
  EVENT_STOCK_UPDATE,
  EVENT_ORDERBOOK_UPDATE,
  EVENT_TELEGRAPH_UPDATE,
  EVENT_MARKET_INDICES_UPDATE,
  EVENT_KLINE_UPDATE,
} from '../api/events';
import { callBackend } from '../api/runtime';
import { Stock, OrderBook, Telegraph, MarketIndex, KLineData } from '../types';

// K线推送数据结构
interface KLineUpdateData {
  code: string;
  period: string;
  data: KLineData[];
  incremental?: boolean; // 是否增量推送
}

interface UseMarketEventsOptions {
  onStockUpdate?: (stocks: Stock[]) => void;
  onOrderBookUpdate?: (orderBook: OrderBook) => void;
  onTelegraphUpdate?: (telegraph: Telegraph) => void;
  onMarketIndicesUpdate?: (indices: MarketIndex[]) => void;
  onKLineUpdate?: (data: KLineUpdateData) => void;
}

/**
 * 市场数据事件 Hook
 * 监听后端推送的实时市场数据
 *
 * 桌面模式走 Wails 事件，Web 模式走 SSE，
 * 此处通过 api/events 统一订阅，调用方无需区分。
 */
export function useMarketEvents(options: UseMarketEventsOptions) {
  const { onStockUpdate, onOrderBookUpdate, onTelegraphUpdate, onMarketIndicesUpdate, onKLineUpdate } = options;

  // 使用 ref 保存回调，避免重复注册
  const stockCallbackRef = useRef(onStockUpdate);
  const orderBookCallbackRef = useRef(onOrderBookUpdate);
  const telegraphCallbackRef = useRef(onTelegraphUpdate);
  const marketIndicesCallbackRef = useRef(onMarketIndicesUpdate);
  const klineCallbackRef = useRef(onKLineUpdate);

  // 更新 ref
  useEffect(() => {
    stockCallbackRef.current = onStockUpdate;
    orderBookCallbackRef.current = onOrderBookUpdate;
    telegraphCallbackRef.current = onTelegraphUpdate;
    marketIndicesCallbackRef.current = onMarketIndicesUpdate;
    klineCallbackRef.current = onKLineUpdate;
  }, [onStockUpdate, onOrderBookUpdate, onTelegraphUpdate, onMarketIndicesUpdate, onKLineUpdate]);

  // 注册事件监听
  useEffect(() => {
    // 监听股票数据更新
    const offStock = onEvent(EVENT_STOCK_UPDATE, (data) => {
      stockCallbackRef.current?.(data as Stock[]);
    });

    // 监听盘口数据更新
    const offOrderBook = onEvent(EVENT_ORDERBOOK_UPDATE, (data) => {
      orderBookCallbackRef.current?.(data as OrderBook);
    });

    // 监听快讯数据更新
    const offTelegraph = onEvent(EVENT_TELEGRAPH_UPDATE, (data) => {
      telegraphCallbackRef.current?.(data as Telegraph);
    });

    // 监听大盘指数更新
    const offIndices = onEvent(EVENT_MARKET_INDICES_UPDATE, (data) => {
      marketIndicesCallbackRef.current?.(data as MarketIndex[]);
    });

    // 监听K线数据更新
    const offKLine = onEvent(EVENT_KLINE_UPDATE, (data) => {
      klineCallbackRef.current?.(data as KLineUpdateData);
    });

    // 通知后端前端已准备好，循环调用直到成功
    const notifyReady = async () => {
      let success = false;
      while (!success) {
        try {
          await callBackend<null>('NotifyFrontendReady', '/api/frontend/ready', [], {
            httpMethod: 'GET',
          });
          success = true;
        } catch {
          await new Promise(resolve => setTimeout(resolve, 500));
        }
      }
    };
    notifyReady();

    // 清理函数
    return () => {
      offStock();
      offOrderBook();
      offTelegraph();
      offIndices();
      offKLine();
    };
  }, []);

  // 订阅股票
  const subscribeStocks = useCallback((codes: string[]) => {
    subscribe('/api/market/subscribe', { codes });
  }, []);

  // 订阅盘口（指定当前选中的股票）
  const subscribeOrderBook = useCallback((code: string) => {
    subscribe('/api/market/orderbook/subscribe', { code });
  }, []);

  // 订阅K线（指定股票代码和周期）
  const subscribeKLine = useCallback((code: string, period: string) => {
    subscribe('/api/market/kline/subscribe', { code, period });
  }, []);

  return { subscribe: subscribeStocks, subscribeOrderBook, subscribeKLine };
}
