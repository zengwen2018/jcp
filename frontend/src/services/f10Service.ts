// F10 基本面服务 - 调用后端API
import { callBackend } from '../api/runtime';
import type { F10Overview, StockValuation } from '../types';

export const getF10Overview = async (code: string): Promise<F10Overview | null> => {
  if (!code) return null;
  return await callBackend<F10Overview>('GetF10Overview', '/api/f10/overview', [code], {
    query: { code },
  });
};

export const getF10Valuation = async (code: string): Promise<StockValuation | null> => {
  if (!code) return null;
  return await callBackend<StockValuation>('GetF10Valuation', '/api/f10/valuation', [code], {
    query: { code },
  });
};
