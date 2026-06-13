"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function PlanScenariosRedirectPage() {
  const router = useRouter();

  useEffect(() => {
    router.replace("/scenarios");
  }, [router]);

  return <p className="text-slate-600">正在前往场景配置…</p>;
}
