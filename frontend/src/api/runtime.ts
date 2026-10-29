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

/**
 * 桌面专属能力适配。
 *
 * 窗口最小化/最大化/关闭、获取窗口尺寸等操作只在桌面模式有意义。
 * Web 模式下这些能力被降级为安全的空操作：
 *   - WindowGetSize 返回浏览器视口尺寸（语义最接近）
 *   - 其余方法静默失败，不抛异常
 */

/** 获取窗口尺寸。Web 模式返回视口尺寸。 */
export async function windowGetSize(): Promise<{ w: number; h: number }> {
  if (isDesktop()) {
    const rt = (window as unknown as {
      runtime?: { WindowGetSize(): Promise<{ w: number; h: number }> };
    }).runtime;
    if (rt?.WindowGetSize) {
      return rt.WindowGetSize();
    }
  }
  return { w: window.innerWidth, h: window.innerHeight };
}

/** 设置窗口尺寸。Web 模式为空操作。 */
export async function windowSetSize(_w: number, _h: number): Promise<void> {
  if (!isDesktop()) return;
  const rt = (window as unknown as {
    runtime?: { WindowSetSize(w: number, h: number): Promise<void> };
  }).runtime;
  await rt?.WindowSetSize?.(_w, _h);
}

/** 查询窗口是否已最大化。Web 模式恒为 false。 */
export async function windowIsMaximised(): Promise<boolean> {
  if (!isDesktop()) return false;
  const rt = (window as unknown as {
    runtime?: { WindowIsMaximised(): Promise<boolean> };
  }).runtime;
  if (!rt?.WindowIsMaximised) return false;
  return rt.WindowIsMaximised();
}

/** 最小化窗口。Web 模式为空操作。 */
export async function windowMinimize(): Promise<void> {
  if (!isDesktop()) return;
  const app = wailsApp();
  await app?.WindowMinimize?.();
}

/** 切换窗口最大化。Web 模式为空操作。 */
export async function windowMaximize(): Promise<void> {
  if (!isDesktop()) return;
  const app = wailsApp();
  await app?.WindowMaximize?.();
}

/** 关闭窗口。Web 模式为空操作（由用户自行关闭标签页）。 */
export async function windowClose(): Promise<void> {
  if (!isDesktop()) return;
  const app = wailsApp();
  await app?.WindowClose?.();
}

/** 在默认浏览器中打开链接。Web 模式直接用新窗口打开。 */
export async function openURL(url: string): Promise<void> {
  if (isDesktop()) {
    const app = wailsApp();
    await app?.OpenURL?.(url);
    return;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}
