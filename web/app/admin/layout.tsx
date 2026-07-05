import type { Metadata } from "next";
import { AdminNav } from "@/components/admin/AdminNav";
import { PageHeader } from "@/components/ui/PageHeader";

export const metadata: Metadata = {
  title: "管理后台 · Fireman",
};

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="content-enter">
      <PageHeader
        title="管理后台"
        description="任务执行、回调与数据版本的系统观测。"
        className="mb-4"
      />
      <AdminNav />
      {children}
    </div>
  );
}
