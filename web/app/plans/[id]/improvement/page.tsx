import { ImprovementPage } from "@/components/plans/improvement/ImprovementPage";

export default async function PlanImprovementPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ImprovementPage planId={id} />;
}
