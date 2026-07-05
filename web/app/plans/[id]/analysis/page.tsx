import { redirect } from "next/navigation";

export default async function AnalysisRedirect({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  redirect(`/plans/${id}/settings?section=simulation`);
}
