"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MetricHelp } from "@/components/ui/MetricHelp";
import {
  deleteInstrument,
  getAnnualReturns,
  getInstrument,
  refreshInstrument,
} from "@/lib/api/instruments";
import {
  assetClassLabel,
  formatPercent,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";

export default function AssetDetailPage() {
  const id = useParams().id as string;
  const router = useRouter();
  const qc = useQueryClient();

  const { data: inst, isLoading } = useQuery({
    queryKey: ["instrument", id],
    queryFn: () => getInstrument(id),
  });
  const returnsQ = useQuery({
    queryKey: ["annual-returns", id],
    queryFn: () => getAnnualReturns(id),
  });

  const refreshMut = useMutation({
    mutationFn: () => refreshInstrument(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["instrument", id] });
      void qc.invalidateQueries({ queryKey: ["annual-returns", id] });
    },
  });

  const deleteMut = useMutation({
    mutationFn: () => deleteInstrument(id),
    onSuccess: () => router.push("/assets"),
  });

  if (isLoading || !inst) return <p>加载资产详情…</p>;

  return (
    <div className="max-w-3xl">
      <Link href="/assets" className="text-sm underline">
        ← 资料库
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">
        {inst.name} <span className="font-mono text-lg text-slate-500">({inst.code})</span>
      </h1>

      <dl className="mt-6 grid gap-3 sm:grid-cols-2 text-sm">
        <div>
          <dt className="text-slate-500">市场 / 类型</dt>
          <dd>
            {inst.market} / {inst.instrument_type}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">大类 / 地区</dt>
          <dd>
            {assetClassLabel(inst.asset_class)} / {regionLabel(inst.region)}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">币种</dt>
          <dd>{inst.currency}</dd>
        </div>
        <div>
          <dt className="text-slate-500">数据截至</dt>
          <dd>{inst.data_as_of || "—"}</dd>
        </div>
        <div>
          <dt className="text-slate-500">数据状态</dt>
          <dd>{qualityStatusLabel(inst.quality_status ?? inst.status)}</dd>
        </div>
        <div>
          <dt className="text-slate-500">费率处理</dt>
          <dd>
            {inst.fee_treatment}
            <MetricHelp termKey="fee_included" />
          </dd>
        </div>
      </dl>

      <div className="mt-6 flex gap-3">
        <button
          type="button"
          className="rounded-md border px-3 py-2 text-sm"
          disabled={refreshMut.isPending || inst.is_system}
          onClick={() => refreshMut.mutate()}
        >
          刷新 AKShare 数据
        </button>
        {!inst.is_system && (
          <button
            type="button"
            className="rounded-md border border-red-200 px-3 py-2 text-sm text-red-700"
            onClick={() => {
              if (window.confirm("确定删除此标的？")) deleteMut.mutate();
            }}
          >
            删除
          </button>
        )}
      </div>

      <h2 className="mt-8 font-medium">年度收益（只读）</h2>
      <div className="mt-2 max-h-96 overflow-auto rounded-lg border">
        <table className="w-full text-sm">
          <thead className="sticky top-0 bg-slate-50">
            <tr>
              <th className="px-3 py-2 text-left">年份</th>
              <th className="px-3 py-2 text-right">
                年化收益
                <MetricHelp termKey="annual_return" />
              </th>
            </tr>
          </thead>
          <tbody>
            {(returnsQ.data?.annual_returns as { year: number; annual_return: number }[] | undefined)?.map(
              (r) => (
                <tr key={r.year} className="border-t">
                  <td className="px-3 py-2">{r.year}</td>
                  <td className="px-3 py-2 text-right">{formatPercent(r.annual_return)}</td>
                </tr>
              ),
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
