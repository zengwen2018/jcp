/**
 * 统一的 HTTP API 客户端。
 *
 * 桌面模式（Wails）与 Web 模式（HTTP + SSE）共用这一层，
 * 服务层函数只需关心数据本身，无需感知运行形态。
 *
 * 鉴权：
 *   令牌优先从 localStorage 读取（键 jcp_token），
 *   也可通过 URL 查询参数 ?token=xxx 传入（EventSource 无法自定义请求头）。
 */

const TOKEN_STORAGE_KEY = 'jcp_token';

/** 读取访问令牌。 */
export function getToken(): string {
  try {
    const fromQuery = new URLSearchParams(window.location.search).get('token');
    if (fromQuery) {
      localStorage.setItem(TOKEN_STORAGE_KEY, fromQuery);
      return fromQuery;
    }
    return localStorage.getItem(TOKEN_STORAGE_KEY) || '';
  } catch {
    return '';
  }
}

/** 保存访问令牌。 */
export function setToken(token: string): void {
  try {
    if (token) {
      localStorage.setItem(TOKEN_STORAGE_KEY, token);
    } else {
      localStorage.removeItem(TOKEN_STORAGE_KEY);
    }
  } catch {
    /* localStorage 不可用时静默降级 */
  }
}

/** API 请求错误。 */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/** 接口响应包裹格式。 */
interface ApiEnvelope<T> {
  data?: T;
  error?: string;
}

/**
 * 发起 GET 请求。
 *
 * @param path 接口路径，如 /api/watchlist
 * @param params 查询参数
 */
export async function apiGet<T>(path: string, params?: Record<string, unknown>): Promise<T> {
  const url = new URL(path, window.location.origin);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value === undefined || value === null || value === '') continue;
      url.searchParams.set(key, String(value));
    }
  }
  return request<T>(url.toString(), { method: 'GET' });
}

/**
 * 发起 POST 请求。
 *
 * @param path 接口路径
 * @param body 请求体（会被序列化为 JSON）
 */
export async function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/**
 * 发起 PUT 请求。
 *
 * @param path 接口路径
 * @param body 请求体（会被序列化为 JSON）
 */
export async function apiPut<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/**
 * 发起 DELETE 请求。
 *
 * @param path 接口路径
 * @param params 查询参数
 */
export async function apiDelete<T>(path: string, params?: Record<string, unknown>): Promise<T> {
  const url = new URL(path, window.location.origin);
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value === undefined || value === null || value === '') continue;
      url.searchParams.set(key, String(value));
    }
  }
  return request<T>(url.toString(), { method: 'DELETE' });
}

/** 执行请求并解包响应。 */
async function request<T>(url: string, init: RequestInit): Promise<T> {
  const headers = { ...(init.headers as Record<string, string> | undefined) };
  const token = getToken();
  if (token && !headers.Authorization) {
    headers.Authorization = `Bearer ${token}`;
  }

  let response: Response;
  try {
    response = await fetch(url, { ...init, headers });
  } catch (err) {
    throw new ApiError(`网络请求失败：${(err as Error).message}`, 0);
  }

  const text = await response.text();
  let payload: ApiEnvelope<T> | null = null;
  if (text) {
    try {
      payload = JSON.parse(text) as ApiEnvelope<T>;
    } catch {
      // 非 JSON 响应（例如反向代理返回的 HTML 错误页）
      if (!response.ok) {
        throw new ApiError(`请求失败（HTTP ${response.status}）`, response.status);
      }
      throw new ApiError('服务端返回了非 JSON 数据', response.status);
    }
  }

  if (!response.ok) {
    throw new ApiError(payload?.error || `请求失败（HTTP ${response.status}）`, response.status);
  }
  if (payload && payload.error) {
    throw new ApiError(payload.error, response.status);
  }
  return payload?.data as T;
}
