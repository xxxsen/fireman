import { redirect } from "next/navigation";

export default async function HoldingsRedirect({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { id } = await params;
  const entries = await searchParams;
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(entries)) {
    if (value === undefined) continue;
    for (const v of Array.isArray(value) ? value : [value]) {
      query.append(key, v);
    }
  }
  const suffix = query.toString();
  redirect(
    suffix ? `/plans/${id}/rebalance?${suffix}` : `/plans/${id}/rebalance`,
  );
}
