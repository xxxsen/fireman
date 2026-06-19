"use client";

import { useEffect } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";

export default function HoldingsRedirectPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const searchParams = useSearchParams();

  useEffect(() => {
    const query = searchParams.toString();
    router.replace(
      query ? `/plans/${planId}/rebalance?${query}` : `/plans/${planId}/rebalance`,
    );
  }, [planId, router, searchParams]);

  return <p className="text-ink-muted">正在前往持仓预览…</p>;
}
