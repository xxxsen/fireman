import { ApiError } from "./api/client";

/** Convert unknown query/mutation errors into user-facing text. */
export function queryErrorMessage(error: unknown, fallback = "请求失败，请稍后重试。"): string {
  if (error instanceof ApiError) {
    const diagnostics = [`错误代码：${error.code}`];
    if (error.requestId) diagnostics.push(`请求 ID：${error.requestId}`);
    return `${error.message}（${diagnostics.join("；")}）`;
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  if (typeof error === "string" && error.trim()) {
    return error;
  }
  return fallback;
}
