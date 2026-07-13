import { describe, expect, it } from "vitest";

import { ApiError } from "./api/client";
import { queryErrorMessage } from "./query-error";

describe("queryErrorMessage", () => {
  it("includes stable API diagnostics for support and log lookup", () => {
    const error = new ApiError(
      "internal_error",
      "internal server error",
      undefined,
      500,
      "req_123",
    );

    expect(queryErrorMessage(error)).toBe(
      "internal server error（错误代码：internal_error；请求 ID：req_123）",
    );
  });

  it("keeps useful messages for local errors", () => {
    expect(queryErrorMessage(new Error("本地校验失败"))).toBe("本地校验失败");
    expect(queryErrorMessage(null)).toBe("请求失败，请稍后重试。");
  });
});
