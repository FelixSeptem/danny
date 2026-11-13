// Danny-owned projection contracts. These mirrors exist only to type-check the
// client; every field is an allowlisted projection field served by /api/, never
// a Baymax event type, Stele envelope, or raw payload blob.

export interface Task {
  id: string;
  title: string;
  description: string;
  status: string;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface Schedule {
  id: string;
  cron: string;
  timezone: string;
  action_type: string;
  instruction: string;
  enabled: boolean;
  retry_limit: number;
  misfire_policy: string;
  backfill_limit: number;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface Occurrence {
  id: string;
  schedule_id: string;
  occurrence_key: string;
  local_time: string;
  resolved_at: string;
  timezone: string;
  tzdata_version: string;
  dst_decision: string;
  status: string;
  attempt_count: number;
  execution_id: string;
  created_at: string;
  updated_at: string;
}

export interface Delivery {
  id: string;
  execution_id: string;
  occurrence_id: string;
  status: string;
  attempt_count: number;
  next_attempt_at: string;
  last_error: string;
  created_at: string;
  updated_at: string;
}

export interface Notification {
  id: string;
  execution_id: string;
  channel: string;
  status: string;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface Generation {
  id: number;
  status: string;
  commit_marker: string;
  created_at: string;
  committed_at: string;
}

export interface Artifact {
  id: string;
  generation_id: number;
  kind: string;
  content_hash: string;
  created_at: string;
}

export interface Source {
  id: string;
  path: string;
  content_hash: string;
  format: string;
  parser_version: string;
  provider_mode: string;
  untrusted_source: boolean;
  created_at: string;
  updated_at: string;
}

export interface IndexStatus {
  generation_id: number;
  generation_status: string;
  commit_marker: string;
  artifact_count: number;
  source_count: number;
  event_contract: string;
}

export interface Execution {
  id: string;
  task_id: string;
  status: string;
  revision: number;
  mode: string;
  plan_version: string;
  parent_run_id: string;
  correlation_id: string;
  created_at: string;
  updated_at: string;
}

export interface PlanVersion {
  version: string;
  parent_version: string;
  mode: string;
  rationale: string;
  replan_reason: string;
  authority_expanded: boolean;
  created_at: string;
}

export interface Checkpoint {
  current_step: string;
  pause_reason: string;
  plan_version: string;
  task_revision: number;
}

export interface ContextProjectionRef {
  digest: string;
  source_digest: string;
  continuity_scope: string;
  created_at: string;
}

export interface RunInspector {
  execution: Execution;
  plans: PlanVersion[];
  checkpoint?: Checkpoint;
  context_projections: ContextProjectionRef[];
  evidence_count: number;
  event_count: number;
}
