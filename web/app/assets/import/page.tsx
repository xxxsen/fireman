"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { importInstrument, previewImport } from "@/lib/api/instruments";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, dataSourceLabel, formatPercent } from "@/lib/format";

const MARKETS = [
  { value: "CN", label: "中国市场" },
  { value: "HK", label: "香港市场" },
  { value: "US", label: "美国市场" },
];

const TYPES: Record<string, { value: string; label: string }[]> = {
  CN: [
    { value: "cn_exchange_fund", label: "场内 ETF / LOF" },
    { value: "cn_exchange_stock", label: "A 股" },
    { value: "cn_mutual_fund", label: "公募基金" },
  ],
  HK: [
    { value: "hk_etf", label: "香港 ETF" },
    { value: "hk_stock", label: "港股" },
  ],
  US: [
    { value: "us_etf", label: "美国 ETF" },
    { value: "us_stock", label: "美国股票" },
  ],
};

const CODE_HINTS: Record<string, string> = {
  cn_exchange_fund: "例如 510300",
  cn_exchange_stock: "例如 600519",
  cn_mutual_fund: "例如 000001",
  hk_etf: "例如 02800",
  hk_stock: "例如 00700",
  us_etf: "例如 SPY",
  us_stock: "例如 AAPL",
};

type Stage = "search" | "preview" | "error";

export default function ImportAssetPage() {
  const router = useRouter();
  const [stage, setStage] = useState<Stage>("search");
  const [market, setMarket] = useState("CN");
  const [instrumentType, setInstrumentType] = useState("cn_exchange_fund");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<Record<string, unknown> | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchPreview = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await previewImport({ market, instrument_type: instrumentType, code });
      setPreview(data);
      setStage("preview");
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "AKShare 请求失败");
      setStage("error");
    } finally {
      setLoading(false);
    }
  };

  const confirmImport = async () => {
    setLoading(true);
    setError(null);
    try {
      const inst = await importInstrument({ market, instrument_type: instrumentType, code });
      router.push(`/assets/${inst.id}`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "录入失败");
      setStage("error");
    } finally {
      setLoading(false);
    }
  };

  const inst = (preview?.instrument as Record<string, unknown>) ?? {};
  const quality = (preview?.quality_status as string) ?? "";
  const canSave =
    quality === "available" ||
    quality === "insufficient_history";
  const blockSave =
    quality === "classification_failed" ||
    quality === "metadata_conflict" ||
    quality === "data_anomaly";

  const simWindow = preview?.simulation_window as
    | {
        inclusion_date?: string;
        complete_year_count?: number;
        modeled_annual_return?: number;
        annual_volatility?: number;
        max_drawdown?: number;
        excluded_years?: number[];
      }
    | undefined;

  return (
    <div className="mx-auto max-w-2xl">
      <Link href="/assets" className="text-sm underline">
        ← 资产资料库
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">AKShare 标的录入</h1>

      {stage === "search" && (
        <div className="mt-6 space-y-4 rounded-lg border p-6">
          <h2 className="font-medium">1. 查询标的</h2>
          <p className="text-sm text-slate-600">
            仅允许选择市场、标的类型并输入代码。名称、大类、收益风险均由 AKShare 或后端计算。
          </p>
          <label className="block text-sm">
            市场
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={market}
              onChange={(e) => {
                setMarket(e.target.value);
                setInstrumentType(TYPES[e.target.value]?.[0]?.value ?? "");
              }}
            >
              {MARKETS.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </select>
          </label>
          <label className="block text-sm">
            标的类型
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={instrumentType}
              onChange={(e) => setInstrumentType(e.target.value)}
            >
              {(TYPES[market] ?? []).map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
          </label>
          <label className="block text-sm">
            代码
            <input
              className="mt-1 w-full rounded-md border px-3 py-2 font-mono"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={CODE_HINTS[instrumentType]}
            />
          </label>
          <button
            type="button"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={!code.trim() || loading}
            onClick={() => void fetchPreview()}
          >
            从 AKShare 获取
          </button>
        </div>
      )}

      {stage === "preview" && preview && (
        <div className="mt-6 space-y-4 rounded-lg border p-6">
          <h2 className="font-medium">2. 核对 AKShare 数据（只读）</h2>
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-slate-500">名称</dt>
              <dd>{String(inst.name ?? "—")}</dd>
            </div>
            <div>
              <dt className="text-slate-500">代码</dt>
              <dd>{code}</dd>
            </div>
            <div>
              <dt className="text-slate-500">大类</dt>
              <dd>{assetClassLabel(String(inst.asset_class ?? ""))}</dd>
            </div>
            <div>
              <dt className="text-slate-500">币种</dt>
              <dd>{String(inst.currency ?? "—")}</dd>
            </div>
            <div>
              <dt className="text-slate-500">抓取数据源</dt>
              <dd>
                {dataSourceLabel(String(preview.source_name ?? ""))}
                {preview.source_name ? (
                  <span className="ml-1 font-mono text-xs text-slate-400">
                    ({String(preview.source_name)})
                  </span>
                ) : null}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">数据截止</dt>
              <dd>{String(preview.data_as_of ?? "—")}</dd>
            </div>
            <div>
              <dt className="text-slate-500">数据质量</dt>
              <dd>{quality}</dd>
            </div>
            <div>
              <dt className="text-slate-500">费率状态</dt>
              <dd>
                {String(inst.expense_ratio_status ?? "—")}
                <MetricHelp termKey="fee_included" />
              </dd>
            </div>
          </dl>
          {simWindow && (
            <div className="rounded-md border border-slate-200 bg-slate-50 p-3 text-sm">
              <h3 className="font-medium">纳入计划后的模拟窗口预览</h3>
              <p className="mt-1 text-slate-600">
                纳入日 {simWindow.inclusion_date} · 完整年度 {simWindow.complete_year_count ?? 0} 个
              </p>
              <dl className="mt-2 grid gap-2 sm:grid-cols-3">
                <div>
                  <dt className="flex items-center text-slate-500">
                    年化收益
                    <MetricHelp termKey="annual_return" />
                  </dt>
                  <dd>{formatPercent(simWindow.modeled_annual_return ?? 0)}</dd>
                </div>
                <div>
                  <dt className="flex items-center text-slate-500">
                    年化波动
                    <MetricHelp termKey="annual_volatility" />
                  </dt>
                  <dd>{formatPercent(simWindow.annual_volatility ?? 0)}</dd>
                </div>
                <div>
                  <dt className="flex items-center text-slate-500">
                    最大回撤
                    <MetricHelp termKey="max_drawdown" />
                  </dt>
                  <dd>{formatPercent(simWindow.max_drawdown ?? 0)}</dd>
                </div>
              </dl>
              {simWindow.excluded_years && simWindow.excluded_years.length > 0 && (
                <p className="mt-2 text-xs text-slate-600">
                  排除的不完整年度：{simWindow.excluded_years.join("、")}
                </p>
              )}
            </div>
          )}
          {Array.isArray(preview.annual_returns) && (
            <div className="max-h-48 overflow-auto rounded border text-xs">
              <table className="w-full">
                <thead className="bg-slate-50">
                  <tr>
                    <th className="px-2 py-1">年份</th>
                    <th className="px-2 py-1">收益</th>
                  </tr>
                </thead>
                <tbody>
                  {(preview.annual_returns as { year: number; annual_return: number }[])
                    .slice(-20)
                    .map((r) => (
                      <tr key={r.year} className="border-t">
                        <td className="px-2 py-1">{r.year}</td>
                        <td className="px-2 py-1">{formatPercent(r.annual_return)}</td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
          )}
          <h3 className="font-medium">3. 确认录入</h3>
          {quality === "insufficient_history" && (
            <p className="text-sm text-amber-800">
              历史不足：可保存为不可用资料，暂不能用于计划模拟。
            </p>
          )}
          {blockSave ? (
            <p className="text-sm text-red-700">数据质量未通过，禁止保存。</p>
          ) : (
            <button
              type="button"
              data-testid="confirm-import"
              className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
              disabled={!canSave || loading}
              onClick={() => void confirmImport()}
            >
              {quality === "insufficient_history"
                ? "保存并等待更多历史数据"
                : "确认录入资料库"}
            </button>
          )}
          <button
            type="button"
            className="ml-3 text-sm underline"
            onClick={() => setStage("search")}
          >
            修改查询条件
          </button>
        </div>
      )}

      {stage === "error" && (
        <div className="mt-6 rounded-lg border border-red-200 bg-red-50 p-6">
          <p className="text-red-800">{error}</p>
          <div className="mt-4 flex gap-3">
            <button
              type="button"
              className="rounded-md border px-3 py-2 text-sm"
              onClick={() => void fetchPreview()}
            >
              重试
            </button>
            <button
              type="button"
              className="text-sm underline"
              onClick={() => {
                setStage("search");
                setError(null);
              }}
            >
              修改查询条件
            </button>
            <Link href="/assets" className="text-sm underline">
              返回资料库
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}
