"use client";

import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import type { AssumptionProfileSummary } from "@/types/api";
import { statusBadge } from "./shared";

export interface ProfileListProps {
  profiles: AssumptionProfileSummary[];
  currentId?: string;
  currentVersion?: number;
  defaultId?: string;
  defaultVersion?: number;
  activatePending: boolean;
  onSelect: (id: string, version: number) => void;
  onActivate: (id: string, version: number) => void;
}

export function ProfileList({
  profiles,
  currentId,
  currentVersion,
  defaultId,
  defaultVersion,
  activatePending,
  onSelect,
  onActivate,
}: ProfileListProps) {
  return (
    <section className="rounded-lg border border-line bg-surface p-4">
      <h2 className="font-medium text-ink">假设 Profile</h2>
      <div className="mt-3 overflow-x-auto">
        <table className="min-w-full text-left text-sm">
          <caption className="sr-only">假设 Profile 列表</caption>
          <thead>
            <tr className="text-ink-muted">
              <th scope="col" className="pr-4 py-1">名称</th>
              <th scope="col" className="pr-4 py-1">ID@版本</th>
              <th scope="col" className="pr-4 py-1">归属</th>
              <th scope="col" className="pr-4 py-1">状态</th>
              <th scope="col" className="pr-4 py-1">默认</th>
              <th scope="col" className="pr-4 py-1" />
            </tr>
          </thead>
          <tbody>
            {profiles.map((p) => {
              const isDefault = defaultId === p.id && defaultVersion === p.version;
              const isCurrent = currentId === p.id && currentVersion === p.version;
              return (
                <tr key={`${p.id}@${p.version}`} className={`border-t ${isCurrent ? "bg-brand/5" : ""}`}>
                  <td className="py-1 pr-4">{p.name}</td>
                  <td className="py-1 pr-4 font-mono text-xs">
                    {p.id}@{p.version}
                  </td>
                  <td className="py-1 pr-4">{p.owner_scope === "system" ? "系统" : "自定义"}</td>
                  <td className="py-1 pr-4">{statusBadge(p.status)}</td>
                  <td className="py-1 pr-4">{isDefault ? <Badge variant="info">全局默认</Badge> : ""}</td>
                  <td className="py-1 pr-4">
                    <div className="flex gap-2">
                      <Button
                        variant="ghost"
                        className="px-2 py-1"
                        onClick={() => onSelect(p.id, p.version)}
                      >
                        查看
                      </Button>
                      {p.status === "draft" && (
                        <Button
                          variant="secondary"
                          className="px-2 py-1"
                          disabled={activatePending}
                          onClick={() => onActivate(p.id, p.version)}
                        >
                          激活
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
