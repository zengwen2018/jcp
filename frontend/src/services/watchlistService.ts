// 自选股服务 - 调用后端API
import { callBackend } from '../api/runtime';
import type { Stock } from '../types';

export const getWatchlist = async (): Promise<Stock[]> => {
  return await callBackend<Stock[]>('GetWatchlist', '/api/watchlist');
};

export const addToWatchlist = async (stock: Stock): Promise<string> => {
  return await callBackend<string>('AddToWatchlist', '/api/watchlist', [stock], {
    body: stock,
    httpMethod: 'POST',
  });
};

export const removeFromWatchlist = async (symbol: string): Promise<string> => {
  return await callBackend<string>('RemoveFromWatchlist', '/api/watchlist', [symbol], {
    query: { symbol },
    httpMethod: 'DELETE',
  });
};
