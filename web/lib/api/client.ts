import type { ApiEnvelope, ApiErrorBody } from "@/types/api";
import { normalizeApiArrays } from "./normalize";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";

export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, unknown>,
    public readonly status?: number,
    public readonly requestId?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export type ApiRequestOptions = RequestInit & {
  timeoutMs?: number;
};

function mergeAbortSignals(
  userSignal: AbortSignal | null | undefined,
  timeoutMs: number | undefined,
): { signal: AbortSignal | undefined; timedOut: () => boolean } {
  if (!timeoutMs) {
    return { signal: userSignal ?? undefined, timedOut: () => false };
  }
  if (!userSignal) {
    return { signal: AbortSignal.timeout(timeoutMs), timedOut: () => true };
  }
  let timedOutFlag = false;
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  timeoutSignal.addEventListener("abort", () => {
    timedOutFlag = true;
  });
  if (!userSignal) {
    return { signal: timeoutSignal, timedOut: () => timedOutFlag };
  }
  const controller = new AbortController();
  const abort = () => controller.abort();
  userSignal.addEventListener("abort", abort);
  timeoutSignal.addEventListener("abort", abort);
  return { signal: controller.signal, timedOut: () => timedOutFlag };
}

function mapFetchError(
  error: unknown,
  timedOut: () => boolean,
  operation: string,
): never {
  if (error instanceof ApiError) {
    throw error;
  }
  if (error instanceof DOMException && error.name === "AbortError") {
    if (timedOut()) {
      console.warn(
        `market provider timeout operation=${operation} layer=web`,
      );
      throw new ApiError(
        "market_provider_timeout",
        "上游市场数据服务响应超时，请稍后重试",
      );
    }
    throw new ApiError("request_aborted", "请求已取消");
  }
  if (error instanceof Error) {
    throw new ApiError("network_error", error.message);
  }
  throw new ApiError("network_error", "网络请求失败");
}

export async function apiRequest<T>(
  path: string,
  options?: ApiRequestOptions,
): Promise<T> {
  const url = `${API_BASE}${path}`;
  const rawHeaders = options?.headers;
  const headers: Record<string, string> = {};
  if (rawHeaders) {
    if (rawHeaders instanceof Headers) {
      rawHeaders.forEach((v, k) => {
        headers[k] = v;
      });
    } else if (Array.isArray(rawHeaders)) {
      for (const [k, v] of rawHeaders) {
        headers[k] = v;
      }
    } else {
      Object.assign(headers, rawHeaders);
    }
  }
  const hasContentType = Object.keys(headers).some(
    (k) => k.toLowerCase() === "content-type",
  );
  if (options?.body && !hasContentType) {
    headers["Content-Type"] = "application/json";
  }

  const { timeoutMs, ...fetchOptions } = options ?? {};
  const { signal, timedOut } = mergeAbortSignals(fetchOptions.signal, timeoutMs);

  let res: Response;
  try {
    res = await fetch(url, { ...fetchOptions, headers, signal });
  } catch (error) {
    mapFetchError(error, timedOut, path);
  }

  if (!res!.ok) {
    try {
      const errBody = (await res!.json()) as ApiErrorBody;
      throw new ApiError(
        errBody.code,
        errBody.message,
        errBody.details,
        res!.status,
        errBody.request_id,
      );
    } catch (e) {
      if (e instanceof ApiError) throw e;
      throw new ApiError("http_error", `HTTP ${res!.status}: ${res!.statusText}`, undefined, res!.status);
    }
  }

  const body = (await res!.json()) as ApiEnvelope<T>;
  if (body.code !== "ok") {
    throw new ApiError(body.code, body.message);
  }
  // Backend may omit JSON null before fix; React Query rejects undefined query results.
  return normalizeApiArrays(body.data ?? null) as T;
}

export function apiGet<T>(path: string, options?: ApiRequestOptions): Promise<T> {
  return apiRequest<T>(path, options);
}

export function apiPost<T>(
  path: string,
  data?: unknown,
  headers?: Record<string, string>,
  options?: Omit<ApiRequestOptions, "method" | "body" | "headers">,
): Promise<T> {
  return apiRequest<T>(path, {
    method: "POST",
    body: data !== undefined ? JSON.stringify(data) : undefined,
    headers,
    ...options,
  });
}

export function apiPut<T>(path: string, data: unknown): Promise<T> {
  return apiRequest<T>(path, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export function apiPatch<T>(path: string, data: unknown): Promise<T> {
  return apiRequest<T>(path, {
    method: "PATCH",
    body: JSON.stringify(data),
  });
}

export function apiDelete<T>(path: string): Promise<T> {
  return apiRequest<T>(path, { method: "DELETE" });
}
