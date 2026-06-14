"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import {
  candidateIdentity,
  importAsync,
  isSameCandidate,
  resolveImport,
  type ResolveCandidate,
} from "@/lib/api/instruments";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, regionLabel } from "@/lib/format";

const ASSET_CLASSES = [
  { value: "equity", label: "股票 / 权益" },
  { value: "bond", label: "债券" },
  { value: "cash", label: "现金 / 货币" },
] as const;

const REGIONS = [
  { value: "domestic", label: "国内" },
  { value: "foreign", label: "国外" },
] as const;

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
  cn_exchange_fund: "例如 510300 或 sh510300",
  cn_exchange_stock: "例如 600519",
  cn_mutual_fund: "例如 000001",
  hk_etf: "例如 02800",
  hk_stock: "例如 00700",
  us_etf: "例如 SPY",
  us_stock: "例如 AAPL",
};

function isCandidateCompatible(candidate: ResolveCandidate): boolean {
  return candidate.is_importable ?? false;
}

type Stage = "search" | "disambiguate" | "confirm" | "error";

export default function ImportAssetPage() {
  const router = useRouter();
  const [stage, setStage] = useState<Stage>("search");
  const [market, setMarket] = useState("CN");
  const [instrumentType, setInstrumentType] = useState("cn_exchange_fund");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [candidates, setCandidates] = useState<ResolveCandidate[]>([]);
  const [selected, setSelected] = useState<ResolveCandidate | null>(null);
  const [assetClass, setAssetClass] = useState("");
  const [region, setRegion] = useState("");
  const [error, setError] = useState<string | null>(null);

  const beginConfirm = (candidate: ResolveCandidate) => {
    setSelected(candidate);
    setAssetClass("");
    setRegion("");
    setStage("confirm");
  };

  const handleResolve = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await resolveImport({ market, instrument_type: instrumentType, code });
      if (data.ambiguous && data.candidates?.length) {
        setCandidates(data.candidates);
        setSelected(null);
        setStage("disambiguate");
        return;
      }
      if (data.resolved) {
        beginConfirm(data.resolved);
        return;
      }
      setError("未找到匹配的标的");
      setStage("error");
    } catch (e) {
      if (e instanceof ApiError && e.code === "instrument_type_mismatch") {
        const suggested = e.details?.suggested_instrument_type;
        if (suggested === "cn_mutual_fund") {
          setInstrumentType("cn_mutual_fund");
          setError("该代码属于场外公募基金，已切换为「公募基金」，请再次查询");
        } else {
          setError(e.message);
        }
      } else if (e instanceof ApiError && e.code === "market_provider_timeout") {
        setError("数据源响应超时，请重试");
      } else {
        setError(e instanceof ApiError ? e.message : "解析失败");
      }
      setStage("error");
    } finally {
      setLoading(false);
    }
  };

  const handleImport = async () => {
    if (!selected) return;
    setLoading(true);
    setError(null);
    try {
      if (!selected.ticket_id) {
        setError("缺少 resolution ticket，请重新解析标的");
        setStage("error");
        return;
      }
      if (!assetClass) {
        setError("请选择资产类别");
        return;
      }
      if (!region) {
        setError("请选择投资地区");
        return;
      }
      const result = await importAsync({
        ticket_id: selected.ticket_id,
        asset_class: assetClass,
        region,
      });
      router.push(`/assets/${result.instrument_id}`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "录入失败");
      setStage("error");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="mx-auto max-w-2xl">
      <Link href="/assets" className="text-sm underline">
        ← 资产资料库
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">AKShare 标的录入</h1>

      {stage === "search" && (
        <div className="mt-6 space-y-4 rounded-lg border p-6">
          <h2 className="font-medium">1. 解析标的</h2>
          <p className="text-sm text-slate-600">
            输入代码后轻量解析名称与交易所；确认后异步抓取全量历史，无需等待。
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
            data-testid="resolve-button"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={!code.trim() || loading}
            onClick={() => void handleResolve()}
          >
            {loading ? "正在查询…" : "解析标的"}
          </button>
        </div>
      )}

      {stage === "disambiguate" && (
        <div className="mt-6 space-y-4 rounded-lg border p-6">
          <h2 className="font-medium">2. 选择真实标的</h2>
          <p className="text-sm text-amber-800">该代码存在多个候选，请选择正确的一项。</p>
          <div className="space-y-2" role="radiogroup" aria-label="候选标的">
            {candidates.map((c) => {
              const compatible = isCandidateCompatible(c);
              const identity = candidateIdentity(c);
              return (
              <label
                key={identity}
                data-testid={`candidate-${identity}`}
                data-compatible={compatible ? "true" : "false"}
                className={`flex items-start gap-3 rounded-md border p-3 ${
                  compatible
                    ? "cursor-pointer hover:bg-slate-50"
                    : "cursor-not-allowed bg-slate-50 opacity-50"
                }`}
              >
                <input
                  type="radio"
                  name="candidate"
                  disabled={!compatible}
                  checked={compatible && isSameCandidate(selected, c)}
                  onChange={() => compatible && setSelected(c)}
                />
                <span>
                  <span className="font-mono font-medium">{c.code}</span>
                  <span className="ml-2">{c.name}</span>
                  <span className="ml-2 text-xs text-slate-500">
                    {c.exchange} · {c.instrument_kind}
                  </span>
                  {!compatible && (
                    <span className="ml-2 text-xs text-amber-700">与所选类型不匹配</span>
                  )}
                </span>
              </label>
            );
            })}
          </div>
          <div className="flex gap-3">
            <button
              type="button"
              className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
              disabled={!selected || !isCandidateCompatible(selected)}
              onClick={() => selected && beginConfirm(selected)}
            >
              下一步
            </button>
            <button type="button" className="text-sm underline" onClick={() => setStage("search")}>
              返回修改
            </button>
          </div>
        </div>
      )}

      {stage === "confirm" && selected && (
        <div className="mt-6 space-y-4 rounded-lg border p-6">
          <h2 className="font-medium">3. 确认并开始抓取</h2>
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-slate-500">名称</dt>
              <dd>{selected.name}</dd>
            </div>
            <div>
              <dt className="text-slate-500">完整代码</dt>
              <dd className="font-mono">{selected.code}</dd>
            </div>
            <div>
              <dt className="text-slate-500">交易所</dt>
              <dd>{selected.exchange || "—"}</dd>
            </div>
            <div>
              <dt className="text-slate-500">类型</dt>
              <dd>{selected.instrument_kind}</dd>
            </div>
          </dl>
          <label className="block text-sm">
            资产类别
            <span className="ml-1 text-slate-500">（用于组合配置，请手动确认）</span>
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={assetClass}
              onChange={(e) => setAssetClass(e.target.value)}
              data-testid="asset-class-select"
            >
              <option value="">请选择…</option>
              {ASSET_CLASSES.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          {assetClass && (
            <p className="text-sm text-slate-600">
              已选择：{assetClassLabel(assetClass)}
              {instrumentType === "cn_mutual_fund" && assetClass === "bond" && (
                <span>（短债/纯债等场外公募基金通常归入债券）</span>
              )}
              {instrumentType === "cn_mutual_fund" && assetClass === "equity" && (
                <span>（标普500、纳指等联接基金若投资海外市场，地区请选「国外」）</span>
              )}
            </p>
          )}
          <label className="block text-sm">
            投资地区
            <span className="ml-1 text-slate-500">（用于国内/国外权重配置，请手动确认）</span>
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
              data-testid="region-select"
            >
              <option value="">请选择…</option>
              {REGIONS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          {region && (
            <p className="text-sm text-slate-600">已选择：{regionLabel(region)}</p>
          )}
          <p className="text-sm text-slate-600">
            确认后将创建占位记录并在后台抓取历史数据；详情页可查看进度。
          </p>
          <button
            type="button"
            data-testid="confirm-import"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={loading || !assetClass || !region}
            onClick={() => void handleImport()}
          >
            {loading ? "提交中…" : "开始抓取"}
          </button>
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
              onClick={() => void handleResolve()}
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
