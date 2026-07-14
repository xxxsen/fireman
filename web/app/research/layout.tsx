import { ResearchNav } from "@/components/research/ResearchNav";

export default function ResearchLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-5 lg:flex-row lg:gap-6">
      <ResearchNav />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
