"use client";

import type { QuickFireYear } from "@/lib/api/quick-fire";
import { formatMoney } from "@/lib/format";

export function QuickFireYearTable({ years }: { years: QuickFireYear[] }) {
  return (
    <section aria-labelledby="quick-fire-year-title">
      <h2 id="quick-fire-year-title" className="text-lg font-medium text-ink">逐年现金流</h2>
      <div className="mt-3 hidden overflow-x-auto md:block">
        <table className="w-full border-collapse text-sm">
          <thead className="border-y border-line text-left text-xs text-ink-muted">
            <tr>
              <th className="px-2 py-2">年龄</th><th className="px-2 py-2">阶段</th><th className="px-2 py-2">期初资产</th>
              <th className="px-2 py-2">收入</th><th className="px-2 py-2">支出</th><th className="px-2 py-2">投资收益</th>
              <th className="px-2 py-2">期末资产</th><th className="px-2 py-2">真实期末资产</th><th className="px-2 py-2">所需资本</th>
            </tr>
          </thead>
          <tbody>
            {years.map((row) => <YearRow key={row.age} row={row} table />)}
          </tbody>
        </table>
      </div>
      <div className="mt-3 space-y-2 md:hidden">
        {years.map((row) => <YearRow key={row.age} row={row} />)}
      </div>
    </section>
  );
}

function YearRow({ row, table = false }: { row: QuickFireYear; table?: boolean }) {
  const values = [
    formatMoney(row.start_wealth_minor), formatMoney(row.income_minor), formatMoney(row.spending_minor),
    formatMoney(row.investment_gain_minor), formatMoney(row.end_wealth_minor),
    formatMoney(row.real_end_wealth_minor), formatMoney(row.required_wealth_minor),
  ];
  if (table) {
    return <tr className="border-b border-line/70"><td className="px-2 py-2">{row.age} 岁</td><td className="px-2 py-2">{row.phase === "accumulation" ? "积累" : "退休"}</td>{values.map((value, index) => <td className="px-2 py-2 font-mono-numeric" key={index}>{value}</td>)}</tr>;
  }
  return (
    <article className="rounded-md border border-line bg-surface p-3 text-sm">
      <div className="flex items-baseline justify-between"><h3 className="font-medium">{row.age} 岁 · {row.phase === "accumulation" ? "积累" : "退休"}</h3><span className="text-xs text-ink-muted">{row.months_in_period} 个月</span></div>
      <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs"><Pair label="期初" value={values[0]} /><Pair label="收入" value={values[1]} /><Pair label="支出" value={values[2]} /><Pair label="投资收益" value={values[3]} /><Pair label="期末" value={values[4]} /><Pair label="真实期末" value={values[5]} /><Pair label="所需资本" value={values[6]} /></dl>
    </article>
  );
}

function Pair({ label, value }: { label: string; value: string }) {
  return <div className="flex justify-between gap-2"><dt className="text-ink-muted">{label}</dt><dd className="font-mono-numeric text-ink">{value}</dd></div>;
}
