import { redirect } from "next/navigation";

export default async function InstrumentsRedirect({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  redirect(`/plans/${id}/holdings`);
}
