import { redirect } from "next/navigation";

export default async function DashboardRedirect({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  redirect(`/plans/${id}/overview`);
}
