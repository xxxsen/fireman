import type { Task, WorkerTaskStatus } from "@/types/api";
import { ApiError, apiGet } from "./client";

export type { WorkerTaskStatus } from "@/types/api";
export type WorkerType = "go_worker" | "sidecar_worker";

export function isTaskActive(
  status: WorkerTaskStatus | string | null | undefined,
): boolean {
  return (
    status === "pending" || status === "running" || status === "pre_complete"
  );
}

export function isTaskTerminal(
  status: WorkerTaskStatus | string | null | undefined,
): boolean {
  return (
    status === "complete" || status === "failed" || status === "canceled"
  );
}

export const taskQueryKey = (taskId: string | null | undefined) =>
  ["worker-task", taskId] as const;

export interface TaskListParams {
  worker_type?: WorkerType;
  type?: string;
  status?: WorkerTaskStatus | "active";
  scope_type?: string;
  scope_id?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export interface TaskListResponse {
  items: Task[];
  total: number;
}

export function listTasks(params: TaskListParams): Promise<TaskListResponse> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") search.set(key, String(value));
  }
  return apiGet(`/api/v1/tasks?${search.toString()}`);
}

export interface ActiveTaskConflictRef {
  taskId: string;
  resourceId?: string;
}

export function activeTaskConflictRef(error: unknown): ActiveTaskConflictRef | null {
  if (!(error instanceof ApiError) || error.code !== "task_already_active") {
    return null;
  }
  const taskId = error.details?.task_id;
  if (typeof taskId !== "string" || !taskId) return null;
  const resourceId = error.details?.resource_id;
  return {
    taskId,
    resourceId:
      typeof resourceId === "string" && resourceId ? resourceId : undefined,
  };
}
