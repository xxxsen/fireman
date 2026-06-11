import type { ApiEnvelope, ApiErrorBody } from "@/types/api";
import { normalizeApiArrays } from "./normalize";

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";

export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, unknown>,
    public readonly status?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function apiRequest<T>(
  path: string,
  options?: RequestInit,
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

  const res = await fetch(url, { ...options, headers });

  if (!res.ok) {
    try {
      const errBody = (await res.json()) as ApiErrorBody;
      throw new ApiError(
        errBody.code,
        errBody.message,
        errBody.details,
        res.status,
      );
    } catch (e) {
      if (e instanceof ApiError) throw e;
      throw new ApiError("http_error", `HTTP ${res.status}: ${res.statusText}`, undefined, res.status);
    }
  }

  const body = (await res.json()) as ApiEnvelope<T>;
  if (body.code !== "ok") {
    throw new ApiError(body.code, body.message);
  }
  return normalizeApiArrays(body.data) as T;
}

export function apiGet<T>(path: string): Promise<T> {
  return apiRequest<T>(path);
}

export function apiPost<T>(path: string, data?: unknown, headers?: Record<string, string>): Promise<T> {
  return apiRequest<T>(path, {
    method: "POST",
    body: data !== undefined ? JSON.stringify(data) : undefined,
    headers,
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
