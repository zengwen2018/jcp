import React, { useState, useEffect, useRef } from 'react';
import { Stock } from '../types';
import { searchStocks, StockSearchResult } from '../services/stockService';
import { Search, TrendingUp, X } from 'lucide-react';
import { windowClose } from '../api/runtime';
import { useTheme } from '../contexts/ThemeContext';
import logo from '../assets/images/logo.png';

interface WelcomePageProps {
  onAddStock: (stock: Stock) => void;
}

export const WelcomePage: React.FC<WelcomePageProps> = ({ onAddStock }) => {
  const { colors } = useTheme();
  const [searchTerm, setSearchTerm] = useState('');
  const [searchResults, setSearchResults] = useState<StockSearchResult[]>([]);
  const [showDropdown, setShowDropdown] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const searchRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  // 点击外部关闭下拉
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) {
        setShowDropdown(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // 搜索防抖
  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }

    if (!searchTerm.trim()) {
      setSearchResults([]);
      setShowDropdown(false);
      return;
    }

    setIsSearching(true);
    debounceRef.current = setTimeout(async () => {
      const results = await searchStocks(searchTerm);
      const safeResults = Array.isArray(results) ? results : [];
      setSearchResults(safeResults);
      setShowDropdown(safeResults.length > 0);
      setIsSearching(false);
    }, 300);

    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [searchTerm]);

  const handleSelectResult = (result: StockSearchResult) => {
    const newStock: Stock = {
      symbol: result.symbol,
      name: result.name,
      price: 0,
      change: 0,
      changePercent: 0,
      volume: 0,
      amount: 0,
      marketCap: '',
      sector: result.industry,
      open: 0,
      high: 0,
      low: 0,
      preClose: 0,
    };
    onAddStock(newStock);
  };

  return (
    <div className="h-screen w-screen flex flex-col items-center justify-center fin-app relative">
      {/* 右上角关闭按钮 */}
      <div className="absolute top-3 right-3" style={{ WebkitAppRegion: 'no-drag' } as React.CSSProperties}>
        <button
          onClick={() => windowClose()}
          className={`p-1.5 rounded hover:bg-red-500/80 transition-colors ${colors.isDark ? 'text-slate-400 hover:text-white' : 'text-slate-500 hover:text-white'}`}
          title="关闭"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Logo 和标题 */}
      <div className="flex items-center gap-3 mb-8">
        <img src={logo} alt="Logo" className="h-14 w-14 rounded-lg" />
        <div>
          <h1 className={`text-3xl font-bold ${colors.isDark ? 'text-white' : 'text-slate-800'}`}>
            韭菜盘 <span className="text-accent-2">AI</span>
          </h1>
          <p className={`text-sm ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>智能股票分析助手</p>
        </div>
      </div>

      {/* 搜索框 */}
      <div ref={searchRef} className="w-96 relative">
        <div className="relative">
          <Search className={`absolute left-4 top-3.5 h-5 w-5 ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`} />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            onFocus={() => searchResults.length > 0 && setShowDropdown(true)}
            placeholder="搜索股票代码或名称，添加自选股..."
            className={`w-full rounded-xl pl-12 pr-4 py-3 text-base focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent transition-all ${
              colors.isDark
                ? 'bg-slate-800/80 border border-slate-600 text-white placeholder-slate-500'
                : 'bg-white/90 border border-slate-300 text-slate-800 placeholder-slate-400'
            }`}
            autoFocus
          />
          {isSearching && (
            <div className="absolute right-4 top-3.5 h-5 w-5 border-2 border-accent border-t-transparent rounded-full animate-spin" />
          )}
        </div>

        {/* 搜索下拉结果 */}
        {showDropdown && (
          <div className={`absolute top-full left-0 right-0 mt-2 max-h-80 overflow-y-auto rounded-xl shadow-2xl ${
            colors.isDark
              ? 'bg-slate-800 border border-slate-600'
              : 'bg-white border border-slate-200'
          }`}>
            {searchResults.map((result) => (
              <div
                key={result.symbol}
                onClick={() => handleSelectResult(result)}
                className={`px-4 py-3 cursor-pointer first:rounded-t-xl last:rounded-b-xl ${
                  colors.isDark
                    ? 'hover:bg-slate-700 border-b border-slate-700 last:border-b-0'
                    : 'hover:bg-slate-100 border-b border-slate-200 last:border-b-0'
                }`}
              >
                <div className="flex justify-between items-center">
                  <div>
                    <span className={`font-medium ${colors.isDark ? 'text-white' : 'text-slate-800'}`}>{result.name}</span>
                    <span className="ml-2 font-mono text-accent-2 text-sm">{result.symbol}</span>
                  </div>
                  <span className={`text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{result.market}</span>
                </div>
                {result.industry && (
                  <div className={`text-xs mt-1 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{result.industry}</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* 提示文字 */}
      <div className={`mt-6 flex items-center gap-2 text-sm ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
        <TrendingUp className="h-4 w-4" />
        <span>搜索并添加您的第一只自选股开始使用</span>
      </div>
    </div>
  );
};
