import type {
  Artifact, Delivery, Execution, Generation, IndexStatus, Notification,
  Occurrence, RunInspector, Schedule, Source, Task,
} from './types';
import type { A2UISurfaceWire } from './surfaces';

// A2UI 1.0 is NOT claimed. The workbench consumes only the v0.9.1 wire
// version shipped by the Milestone 9 boundary. The separate
// `a2ui-1-0-upgrade` change is BLOCKED pending a real v1.0 React renderer.
export const A2UI_WIRE_VERSION = '0.9.1';

export interface ApiError {
  error: string;
  message: string;
  status: number;
}

export class ApiFailure extends Error {
  readonly code: string;
  readonly status: number;
  constructor(failure: ApiError) {
    super(failure.message || failure.error);
    this.code = failure.error;
    this.status = failure.status;
  }
}

// All calls are same-origin relative paths. State-changing requests rely on
// the browser's automatic Origin / Sec-Fetch-Site headers, which the server's
// fail-closed guard verifies; the client never sends a cross-origin mutation.
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json', ...(init.headers ?? {}) } : init?.headers,
  });
  const text = await response.text();
  let payload: unknown = null;
  if (text) {
    try { payload = JSON.parse(text); } catch { payload = null; }
  }
  if (!response.ok) {
    const body = (payload ?? {}) as Partial<ApiError>;
    throw new ApiFailure({
      error: body.error ?? 'request_failed',
      message: body.message ?? `request failed with status ${response.status}`,
      status: response.status,
    });
  }
  return payload as T;
}

export const api = {
  health: () => request<{ status: string; boundary: string }>('/api/health'),
  tasks: () => request<Task[]>('/api/tasks'),
  task: (id: string) => request<Task>(`/api/tasks/${encodeURIComponent(id)}`),
  updateTask: (id: string, expectedRevision: number, patch: { title?: string; description?: string; status?: string }) =>
    request<Task>(`/api/tasks/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ expected_revision: expectedRevision, ...patch }),
    }),
  schedules: () => request<Schedule[]>('/api/schedules'),
  schedule: (id: string) => request<Schedule>(`/api/schedules/${encodeURIComponent(id)}`),
  occurrences: (id: string) => request<Occurrence[]>(`/api/schedules/${encodeURIComponent(id)}/occurrences`),
  deliveries: () => request<Delivery[]>('/api/deliveries'),
  delivery: (id: string) => request<Delivery>(`/api/deliveries/${encodeURIComponent(id)}`),
  notifications: () => request<Notification[]>('/api/notifications'),
  generations: () => request<Generation[]>('/api/knowledge/generations'),
  artifacts: (generationId: number) => request<Artifact[]>(`/api/knowledge/artifacts?generation_id=${generationId}`),
  sources: () => request<Source[]>('/api/knowledge/sources'),
  indexStatus: () => request<IndexStatus>('/api/knowledge/status'),
  runs: () => request<Execution[]>('/api/runs'),
  runInspector: (id: string) => request<RunInspector>(`/api/runs/${encodeURIComponent(id)}`),
  // The wire is rendered server-side from a stored, catalog-approved surface.
  // The client only consumes it; it never supplies a component tree.
  surface: (id: string) => request<A2UISurfaceWire>(`/api/surfaces/${encodeURIComponent(id)}`),
};

// Surface action submission goes through the Milestone 9 server-validated
// endpoint. The client sends an idempotency key and user activation, and
// displays the typed acceptance or rejection verbatim. It never maps
// browser-provided component data to a mutation of its own.
export interface SurfaceSubmission {
  contract_version: 'danny.surface.v1';
  execution_id: string;
  surface_id: string;
  surface_revision: number;
  task_revision: number;
  idempotency_key: string;
  actions: Array<{ action_id: string; user_activation: boolean; payload: Record<string, unknown> }>;
}

export async function submitSurfaceAction(submission: SurfaceSubmission): Promise<{ ok: boolean; code: string; message: string }> {
  try {
    await request<unknown>(`/agent/surfaces/${encodeURIComponent(submission.surface_id)}/actions`, {
      method: 'POST',
      body: JSON.stringify(submission),
    });
    return { ok: true, code: 'accepted', message: 'Action accepted by the server gate.' };
  } catch (error) {
    if (error instanceof ApiFailure) {
      return { ok: false, code: error.code, message: error.message };
    }
    return { ok: false, code: 'transport_failed', message: String(error) };
  }
}
