/**
 * 事件订阅模块。
 *
 * 桌面模式：使用 Wails 的 EventsOn/EventsOff；
 * Web 模式：使用 EventSource 订阅 /api/events 的 SSE 流。
 *
 * 对外暴露统一的 onEvent / emitEvent 接口，
 * 使 useMarketEvents 等业务 Hook 无需感知运行形态。
 */

import { isDesktop } from './runtime';
import { getToken } from './client';

/**
 * 事件处理函数。
 *
 * Wails 的 EventsOn 回调可能收到多个参数（如 (msg, extra)），
 * Web 模式的信封只有单个 data 字段，因此这里统一为「第一个参数 + 剩余参数」。
 */
type EventHandler<T = unknown> = (data: T, ...rest: unknown[]) => void;

/** 事件名常量，与后端保持一致。 */
export const EVENT_STOCK_UPDATE = 'market:stock:update';
export const EVENT_ORDERBOOK_UPDATE = 'market:orderbook:update';
export const EVENT_TELEGRAPH_UPDATE = 'market:telegraph:update';
export const EVENT_MARKET_INDICES_UPDATE = 'market:indices:update';
export const EVENT_KLINE_UPDATE = 'market:kline:update';

/** 桌面模式下的 Wails 运行时接口。 */
interface WailsRuntime {
  EventsOn(name: string, handler: (...data: unknown[]) => void): () => void;
  EventsOff(name: string): void;
  EventsEmit(name: string, ...data: unknown[]): void;
}

function wailsRuntime(): WailsRuntime | null {
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  if (!rt || typeof rt.EventsOn !== 'function') {
    return null;
  }
  return rt;
}

/**
 * 订阅后端事件。
 *
 * @param name 事件名
 * @param handler 事件处理函数
 * @returns 取消订阅的函数
 */
export function onEvent<T = unknown>(name: string, handler: EventHandler<T>): () => void {
  if (isDesktop()) {
    const rt = wailsRuntime();
    if (!rt) {
      return () => {};
    }
    const off = rt.EventsOn(name, (...data: unknown[]) => {
      handler((data.length > 1 ? data : data[0]) as T);
    });
    return () => {
      if (typeof off === 'function') {
        off();
      } else {
        rt.EventsOff(name);
      }
    };
  }
  return webOnEvent(name, handler);
}

/**
 * Web 模式下向服务端发送订阅请求。
 *
 * @param path 订阅接口路径
 * @param body 订阅内容
 */
export async function subscribe(path: string, body: unknown): Promise<void> {
  if (isDesktop()) {
    return;
  }
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  await fetch(path, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  });
}

/** Web 模式的事件分发器，按事件名维护处理器集合。 */
class WebEventBus {
  private source: EventSource | null = null;
  private handlers = new Map<string, Set<EventHandler>>();

  /** 惰性建立 SSE 连接。 */
  private ensureSource(): EventSource {
    if (this.source) {
      return this.source;
    }
    const token = getToken();
    // EventSource 无法自定义请求头，令牌通过查询参数传递。
    const url = token
      ? `/api/events?token=${encodeURIComponent(token)}`
      : '/api/events';
    const source = new EventSource(url);
    this.source = source;

    // 服务端统一使用信封格式 {event, data} 且不写 event: 行，
    // 因此全部事件都从 onmessage 接收，事件名从信封中读取。
    // 这样可以支持会议事件这类带动态后缀的事件名。
    source.onmessage = (evt: MessageEvent) => {
      let envelope: { event?: string; data?: unknown };
      try {
        envelope = JSON.parse(evt.data);
      } catch {
        return;
      }
      const name = envelope.event;
      if (!name) return;
      const set = this.handlers.get(name);
      if (!set || set.size === 0) return;
      set.forEach((handler) => {
        try {
          handler(envelope.data);
        } catch (err) {
          console.error(`事件处理器异常 [${name}]`, err);
        }
      });
    };

    source.onerror = () => {
      // 浏览器会自动重连；这里仅记录状态。
      console.warn('SSE 连接异常，正在重连…');
    };

    return source;
  }

  /** 注册处理器。 */
  add(name: string, handler: EventHandler): () => void {
    this.ensureSource();
    let set = this.handlers.get(name);
    if (!set) {
      set = new Set();
      this.handlers.set(name, set);
    }
    set.add(handler);
    return () => {
      set!.delete(handler);
    };
  }
}

/** 全局 Web 事件总线实例。 */
const webBus = new WebEventBus();

/** Web 模式下的事件订阅实现。 */
function webOnEvent<T = unknown>(name: string, handler: EventHandler<T>): () => void {
  return webBus.add(name, handler as EventHandler);
}
