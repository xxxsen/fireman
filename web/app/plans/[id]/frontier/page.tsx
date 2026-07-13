import { FrontierPage } from "@/components/plans/frontier/FrontierPage";

export default async function PlanFrontierPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <FrontierPage planId={id} />;
}
