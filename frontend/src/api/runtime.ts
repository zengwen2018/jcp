/**
 * 运行时适配层。
 *
 * 同一份前端代码需要同时支持两种运行形态：
 *   - 桌面模式：Wails 注入的 Go 方法，通过 window.go.main.App.* 直接调用；
 *   - Web 模式：HTTP + SSE，走 api/client.ts。
 *
 * 通过检测 window.go 是否存在来选择调用路径，
 * 使业务代码无需关心当前处于哪种形态。
 */

import { apiGet, apiPost, apiPut, apiDelete } from './client';

/** Wails 注入的 Go 绑定在 window 上的挂载点。 */
interface WailsBindings {
  main?: {
    App?: Record<string, (...args: unknown[]) => Promise<unknown>>;
  };
}

/** 判断当前是否运行在 Wails 桌面环境中。 */
export function isDesktop(): boolean {
  const bindings = (window as unknown as { go?: WailsBindings }).go;
  return Boolean(bindings?.main?.App);
}

/** 获取 Wails 绑定对象。 */
function wailsApp(): Record<string, (...args: unknown[]) => Promise<unknown>> {
  const bindings = (window as unknown as { go?: WailsBindings }).go;
  const app = bindings?.main?.App;
  if (!app) {
    throw new Error('Wails 运行时不可用');
  }
  return app;
}

/**
 * 调用后端方法：桌面模式走 Wails 绑定，Web 模式走 HTTP。
 *
 * @param method 后端方法名
 * @param path   Web 模式下的 HTTP 路径
 * @param args   位置参数（桌面模式）
 * @param options Web 模式下的请求配置
 */
export async function callBackend<T>(
  method: string,
  path: string,
  args: unknown[] = [],
  options?: {
    query?: Record<string, unknown>;
    body?: unknown;
    httpMethod?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  },
): Promise<T> {
  if (isDesktop()) {
    const fn = wailsApp()[method];
    if (!fn) {
      throw new Error(`后端方法不存在：${method}`);
    }
    return (await fn(...args)) as T;
  }

  // Web 模式：显式指定方法时优先。
  const opts = options ?? {};
  const httpMethod = opts.httpMethod;
  if (httpMethod === 'DELETE') {
    return apiDelete<T>(path, opts.query);
  }
  if (httpMethod === 'PUT') {
    return apiPut<T>(path, opts.body);
  }
  if (httpMethod === 'POST' || opts.body !== undefined) {
    return apiPost<T>(path, opts.body);
  }
  return apiGet<T>(path, opts.query);
}
