"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listInstruments } from "@/lib/api/instruments";
import {
  assetClassLabel,
  dataSourceLabel,
  instrumentStatusLabel,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";

export default function AssetsPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["instruments"],
    queryFn: listInstruments,
  });

  if (isLoading) return <p>加载资产资料库…</p>;
  if (error) return <p className="text-red-600">加载失败</p>;

  const userInstruments = data?.instruments.filter((i) => !i.is_system) ?? [];

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">资产资料库</h1>
        <Link
          href="/assets/import"
          className="rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white"
        >
          从 AKShare 录入标的
        </Link>
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left">
            <tr>
              <th className="px-3 py-2">代码</th>
              <th className="px-3 py-2">名称</th>
              <th className="px-3 py-2">市场</th>
              <th className="px-3 py-2">大类</th>
              <th className="px-3 py-2">地区</th>
              <th className="px-3 py-2">数据状态</th>
              <th className="px-3 py-2">数据来源</th>
              <th className="px-3 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {userInstruments.map((inst) => (
              <tr key={inst.id} className="border-t">
                <td className="px-3 py-2 font-mono">{inst.code}</td>
                <td className="px-3 py-2">{inst.name}</td>
                <td className="px-3 py-2">{inst.market}</td>
                <td className="px-3 py-2">{assetClassLabel(inst.asset_class)}</td>
                <td className="px-3 py-2">{regionLabel(inst.region)}</td>
                <td className="px-3 py-2">
                  {inst.status === "pending_fetch" || inst.status === "fetch_failed"
                    ? instrumentStatusLabel(inst.status)
                    : qualityStatusLabel(inst.quality_status ?? inst.status)}
                </td>
                <td className="px-3 py-2 text-xs text-slate-600">
                  {dataSourceLabel(inst.data_source_name)}
                </td>
                <td className="px-3 py-2 space-x-2">
                  <Link href={`/assets/${inst.id}`} className="underline">
                    详情
                  </Link>
                  {inst.status === "pending_fetch" && (
                    <Link href={`/assets/${inst.id}`} className="text-xs underline">
                      查看进度
                    </Link>
                  )}
                  {inst.status === "fetch_failed" && (
                    <Link href={`/assets/${inst.id}`} className="text-xs text-red-700 underline">
                      重试抓取
                    </Link>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!userInstruments.length && (
          <p className="p-8 text-center text-slate-500">尚无 AKShare 标的，请先录入。</p>
        )}
      </div>
    </div>
  );
}
