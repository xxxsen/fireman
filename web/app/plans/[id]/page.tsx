import { redirect } from "next/navigation";

export default async function PlanRedirect({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  redirect(`/plans/${id}/overview`);
}
