import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, ApiFailure } from './api';
import { eventTypeLabel, openRunStream, type StreamEvent, type StreamSession } from './stream';
import { SurfacePanel, type A2UISurfaceWire } from './surfaces';
import { Shell } from './ui/shell';
import {
  EmptyState, Field, Notice, PageHead, Panel, StatusLine, Table,
} from './ui/primitives';
import type {
  Artifact, Delivery, Execution, Generation, IndexStatus, Notification,
  Occurrence, RunInspector, Schedule, Source, Task,
} from './types';

type View = 'chat' | 'knowledge' | 'tasks' | 'schedules' | 'deliveries' | 'notifications' | 'runs' | 'surfaces';

interface ViewDef {
  id: View;
  label: string;
  title: string;
  kicker: string;
  index: string;
  meta: string;
}

// 单一数据源：导航、面包屑、巨型序号、页面标题都从这里取，避免三处文案漂移。
const VIEW_MAP: Record<View, ViewDef> = {
  chat: {
    id: 'chat', label: 'Execution stream', title: 'Execution stream',
    kicker: '01 / LIVE EXECUTION', index: '01', meta: 'AG-UI SSE\nLAST-EVENT-ID REPLAY',
  },
  knowledge: {
    id: 'knowledge', label: 'Knowledge', title: 'Knowledge index',
    kicker: '02 / INDEX & PROVENANCE', index: '02', meta: 'READ-ONLY\n/api/knowledge',
  },
  tasks: {
    id: 'tasks', label: 'Tasks', title: 'Tasks',
    kicker: '03 / REVISION-AWARE WRITES', index: '03', meta: 'EXPECTED_REVISION\nPATCH /api/tasks/{id}',
  },
  schedules: {
    id: 'schedules', label: 'Schedules', title: 'Schedules',
    kicker: '04 / CRON & OCCURRENCES', index: '04', meta: 'TZDATA · DST\n/api/schedules',
  },
  deliveries: {
    id: 'deliveries', label: 'Deliveries', title: 'Deliveries',
    kicker: '05 / WEBHOOK OUTBOX', index: '05', meta: 'READ-ONLY\n/api/deliveries',
  },
  notifications: {
    id: 'notifications', label: 'Notifications', title: 'Notifications',
    kicker: '06 / CHANNEL STATE', index: '06', meta: 'READ-ONLY\n/api/notifications',
  },
  runs: {
    id: 'runs', label: 'Run inspector', title: 'Run inspector',
    kicker: '07 / PLAN & EVIDENCE', index: '07', meta: 'PLAN · EVIDENCE\n/api/runs/{id}',
  },
  surfaces: {
    id: 'surfaces', label: 'Dynamic surfaces', title: 'Dynamic surfaces',
    kicker: '08 / SERVER-APPROVED WIRE', index: '08', meta: 'A2UI v0.9.1\nRESTRICTED CATALOG',
  },
};

const VIEWS: ViewDef[] = Object.values(VIEW_MAP);

// 视图用 hash 做深链：刷新后仍停在同一个面板，也便于外部直接打开某个视图。
// 不引入路由库，不改动任何 API 路径语义。
function viewFromHash(): View {
  const raw = window.location.hash.replace(/^#\/?/, '');
  return raw in VIEW_MAP ? (raw as View) : 'chat';
}

// A server-approved surface arrives as a declarative wire payload fetched from
// the read-only /api/surfaces/{id} endpoint, never as browser-authored
// component data. Until one is loaded, the panel stays in its empty state
// rather than fabricating a form.

function useLoad<T>(loader: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string>('');
  const reload = useCallback(() => {
    setError('');
    loader()
      .then(setData)
      .catch((cause: unknown) => setError(cause instanceof ApiFailure ? `${cause.code}: ${cause.message}` : String(cause)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(reload, [reload]);
  return { data, error, reload, loading: data === null && !error };
}

function failureText(cause: unknown): string {
  return cause instanceof ApiFailure ? `${cause.code}: ${cause.message}` : String(cause);
}

function Head({ def, children }: { def: ViewDef; children: ReactNode }) {
  return (
    <PageHead index={def.index} kicker={def.kicker} title={def.title} meta={def.meta}>
      {children}
    </PageHead>
  );
}

function ChatView() {
  const def = VIEW_MAP.chat;
  const [runId, setRunId] = useState('');
  const [threadId, setThreadId] = useState('corr');
  const [events, setEvents] = useState<StreamEvent[]>([]);
  const [diagnostic, setDiagnostic] = useState('');
  const [connected, setConnected] = useState(false);
  const sessionRef = useRef<StreamSession | null>(null);

  const disconnect = () => {
    sessionRef.current?.close();
    sessionRef.current = null;
    setConnected(false);
  };
  const connect = () => {
    disconnect();
    setEvents([]);
    setDiagnostic('');
    if (!runId) { return; }
    sessionRef.current = openRunStream(threadId, runId, {
      onEvent: (event) => setEvents((prev) => [...prev, event]),
      onSnapshot: (event) => setEvents((prev) => [...prev, event]),
      onError: setDiagnostic,
    });
    setConnected(true);
  };
  useEffect(() => () => sessionRef.current?.close(), []);

  return (
    <>
      <Head def={def}>
        Consumes the Milestone 9 AG-UI-compatible SSE projection. The browser reconnects with
        <code> Last-Event-ID</code> automatically; replay never restarts the run.
      </Head>
      <Panel title="Connection" meta="THREAD / RUN / TRANSPORT">
        <div className="controls">
          <Field label="Thread" value={threadId} onChange={setThreadId} />
          <Field label="Run ID" value={runId} onChange={setRunId} placeholder="execution id" />
          <div className="controls__actions">
            <button type="button" className="btn btn--primary" onClick={connect} disabled={!runId}>
              Connect <span className="btn__arrow" aria-hidden="true">↗</span>
            </button>
            <button type="button" className="btn btn--ghost" onClick={disconnect} disabled={!connected}>
              Disconnect
            </button>
          </div>
        </div>
        {diagnostic && <Notice tone="error" code="stream_interrupted" text={diagnostic} />}
      </Panel>
      <div className="event-log-wrap rv rv-5">
        <div className="event-log-head">
          <span>EVENT LOG / {String(events.length).padStart(3, '0')} ENTRIES</span>
          <span className={`stream-indicator${connected ? ' is-live' : ''}`}>
            <span className="pulse-dot" /> {connected ? 'CONNECTED' : 'IDLE'}
          </span>
        </div>
        {events.length === 0 ? (
          <EmptyState title={connected ? 'WAITING FOR FIRST EVENT' : 'NO STREAM EVENTS'}>
            {connected
              ? 'Connected. Waiting for the first snapshot or event. SSE is a long-lived stream; the client never polls the /api/ projections for live state.'
              : 'Enter a Run ID and connect to the /agent/ SSE projection. On reconnect the browser replays Last-Event-ID automatically, so replay never restarts the run.'}
          </EmptyState>
        ) : (
          <ol className="events">
            {events.map((event, index) => (
              <li className="event ev-in" key={`${event.id}-${index}`}>
                <span className="event__rail" aria-hidden="true" />
                <span className="event__id">{event.id || '—'}</span>
                <span className="event__kind">{eventTypeLabel(event.data)}</span>
                <code className="event__data">{event.data}</code>
              </li>
            ))}
          </ol>
        )}
      </div>
      <div className="boundary-strip rv rv-5">
        <div className="boundary-strip__cell">
          <span className="boundary-strip__num">01 / TRANSPORT</span>
          <h3 className="boundary-strip__title">AG-UI SSE</h3>
          <p className="boundary-strip__text">
            A native EventSource owns the stream. Last-Event-ID replay is automatic; the client never resumes a run.
          </p>
        </div>
        <div className="boundary-strip__cell">
          <span className="boundary-strip__num">02 / RENDERING</span>
          <h3 className="boundary-strip__title">Restricted catalog</h3>
          <p className="boundary-strip__text">
            Only server-registered A2UI v0.9.1 components render. Wire props stay untrusted plain text.
          </p>
        </div>
        <div className="boundary-strip__cell">
          <span className="boundary-strip__num">03 / BOUNDARY</span>
          <h3 className="boundary-strip__title">Loopback, same-origin</h3>
          <p className="boundary-strip__text">
            No external origin. Every state-changing request passes the server's same-origin guard before dispatch.
          </p>
        </div>
      </div>
    </>
  );
}

function KnowledgeView() {
  const def = VIEW_MAP.knowledge;
  const generations = useLoad<Generation[]>(() => api.generations(), []);
  const sources = useLoad<Source[]>(() => api.sources(), []);
  const status = useLoad<IndexStatus>(() => api.indexStatus(), []);
  const [artifacts, setArtifacts] = useState<Artifact[] | null>(null);
  const [artifactError, setArtifactError] = useState('');

  const showArtifacts = (id: number) => {
    setArtifactError('');
    api.artifacts(id)
      .then(setArtifacts)
      .catch((cause: unknown) => setArtifactError(failureText(cause)));
  };

  return (
    <>
      <Head def={def}>
        Committed generations, artifacts and source metadata. Every field is an allowlisted
        read-only projection — no raw payload or upstream envelope reaches the browser.
      </Head>
      {status.error && <Notice tone="error" text={status.error} />}
      {status.data && (
        <StatusLine items={[
          ['GENERATION', status.data.generation_id || 'none'],
          ['STATE', status.data.generation_status || 'n/a'],
          ['ARTIFACTS', status.data.artifact_count],
          ['SOURCES', status.data.source_count],
          ['CONTRACT', status.data.event_contract],
        ]} />
      )}
      <p className="panel__subtitle">Generations</p>
      {generations.error && <Notice tone="error" text={generations.error} />}
      <Table
        headers={['id', 'status', 'commit marker', 'created']}
        rows={(generations.data ?? []).map((g) => [g.id, g.status, g.commit_marker, g.created_at])}
        loading={generations.loading}
        footnote="BOUNDED LIST · /api/knowledge/generations"
      />
      {generations.data && generations.data.length > 0 && (
        <button type="button" className="btn btn--sm" onClick={() => showArtifacts(generations.data![0].id)}>
          Browse artifacts of latest generation <span className="btn__arrow" aria-hidden="true">↗</span>
        </button>
      )}
      {artifactError && <Notice tone="error" text={artifactError} />}
      {artifacts && (
        <>
          <p className="panel__subtitle">Artifacts</p>
          <Table
            headers={['id', 'kind', 'content hash']}
            rows={artifacts.map((a) => [a.id, a.kind, a.content_hash])}
            footnote="NO CONTENT OR LINEAGE IN PROJECTION"
          />
        </>
      )}
      <p className="panel__subtitle">Sources</p>
      {sources.error && <Notice tone="error" text={sources.error} />}
      <Table
        headers={['path', 'format', 'parser', 'provider', 'untrusted']}
        rows={(sources.data ?? []).map((s) => [s.path, s.format, s.parser_version, s.provider_mode, s.untrusted_source])}
        loading={sources.loading}
        footnote="SOURCE METADATA ONLY · /api/knowledge/sources"
      />
    </>
  );
}

function TasksView() {
  const def = VIEW_MAP.tasks;
  const tasks = useLoad<Task[]>(() => api.tasks(), []);
  const [taskId, setTaskId] = useState('');
  const [selected, setSelected] = useState<Task | null>(null);
  const [title, setTitle] = useState('');
  const [outcome, setOutcome] = useState<{ ok: boolean; code: string; text: string } | null>(null);

  const select = async (id: string) => {
    try {
      const task = await api.task(id);
      setSelected(task);
      setTitle(task.title);
      setOutcome(null);
    } catch (cause) {
      setOutcome({ ok: false, code: 'load_failed', text: failureText(cause) });
    }
  };
  const save = async () => {
    if (!selected) { return; }
    try {
      const updated = await api.updateTask(selected.id, selected.revision, { title });
      setSelected(updated);
      setOutcome({ ok: true, code: 'accepted', text: `Saved at revision ${updated.revision}.` });
      tasks.reload();
    } catch (cause) {
      // A stale revision surfaces the typed conflict instead of overwriting.
      const code = cause instanceof ApiFailure ? cause.code : 'write_failed';
      setOutcome({ ok: false, code, text: failureText(cause) });
    }
  };

  return (
    <>
      <Head def={def}>
        Updates carry <code>expected_revision</code>; a stale write displays a typed conflict
        (409) instead of silently overwriting a newer record.
      </Head>
      {tasks.error && <Notice tone="error" text={tasks.error} />}
      <Table
        headers={['id', 'title', 'status', 'revision']}
        rows={(tasks.data ?? []).map((t) => [t.id, t.title, t.status, t.revision])}
        loading={tasks.loading}
        footnote="READ-ONLY LIST · /api/tasks"
      />
      <Panel title="Load a task" meta="SINGLE RECORD / BY ID">
        <div className="controls">
          <Field label="Task ID" value={taskId} onChange={setTaskId} placeholder="task id" />
          <div className="controls__actions">
            <button type="button" className="btn btn--primary" disabled={!taskId} onClick={() => void select(taskId)}>
              Load <span className="btn__arrow" aria-hidden="true">↗</span>
            </button>
          </div>
        </div>
        {selected && (
          <div className="editor">
            <Field label="Title" value={title} onChange={setTitle} />
            <div className="editor__row">
              <button type="button" className="btn btn--primary" onClick={() => void save()}>
                Save with revision {selected.revision}
              </button>
              <span className="panel__meta">CURRENT REVISION {selected.revision}</span>
            </div>
          </div>
        )}
        {outcome && (
          <Notice tone={outcome.ok ? 'ok' : 'error'} code={outcome.code} text={outcome.text} />
        )}
      </Panel>
    </>
  );
}

function SchedulesView() {
  const def = VIEW_MAP.schedules;
  const schedules = useLoad<Schedule[]>(() => api.schedules(), []);
  const [occurrences, setOccurrences] = useState<Occurrence[] | null>(null);
  const [error, setError] = useState('');
  const history = (id: string) => {
    setError('');
    api.occurrences(id).then(setOccurrences).catch((cause: unknown) => setError(failureText(cause)));
  };
  return (
    <>
      <Head def={def}>
        Cron schedules with timezone-aware occurrence history. DST decisions are recorded
        per occurrence and surfaced as projection fields.
      </Head>
      {schedules.error && <Notice tone="error" text={schedules.error} />}
      <Table
        headers={['id', 'cron', 'timezone', 'misfire', 'enabled']}
        rows={(schedules.data ?? []).map((s) => [s.id, s.cron, s.timezone, s.misfire_policy, s.enabled])}
        loading={schedules.loading}
        footnote="READ-ONLY LIST · /api/schedules"
      />
      {schedules.data && schedules.data.length > 0 && (
        <button type="button" className="btn btn--sm" onClick={() => history(schedules.data![0].id)}>
          Execution history of first schedule <span className="btn__arrow" aria-hidden="true">↗</span>
        </button>
      )}
      {error && <Notice tone="error" text={error} />}
      {occurrences && (
        <>
          <p className="panel__subtitle">Occurrences</p>
          <Table
            headers={['occurrence', 'local time', 'dst', 'status', 'attempts']}
            rows={occurrences.map((o) => [o.occurrence_key, o.local_time, o.dst_decision, o.status, o.attempt_count])}
            footnote="BOUNDED HISTORY · /api/schedules/{id}/occurrences"
          />
        </>
      )}
    </>
  );
}

function SimpleListView<T>({ def, description, loader, headers, row, footnote }: {
  def: ViewDef;
  description: string;
  loader: () => Promise<T[]>;
  headers: string[];
  row: (item: T) => Array<string | number | boolean>;
  footnote: string;
}) {
  const state = useLoad<T[]>(loader, []);
  return (
    <>
      <Head def={def}>{description}</Head>
      {state.error && <Notice tone="error" text={state.error} />}
      <Table
        headers={headers}
        rows={(state.data ?? []).map(row)}
        loading={state.loading}
        footnote={footnote}
      />
    </>
  );
}

function DeliveriesView() {
  return (
    <SimpleListView<Delivery>
      def={VIEW_MAP.deliveries}
      description="Webhook outbox state and retry attempts. The projection deliberately omits target URLs and payload bodies."
      loader={() => api.deliveries()}
      headers={['id', 'status', 'attempts', 'last error', 'updated']}
      row={(d) => [d.id, d.status, d.attempt_count, d.last_error, d.updated_at]}
      footnote="NO URL OR PAYLOAD IN PROJECTION"
    />
  );
}

function NotificationsView() {
  return (
    <SimpleListView<Notification>
      def={VIEW_MAP.notifications}
      description="Notification channel state, keyed by revision. This is a point-in-time read, not a live feed."
      loader={() => api.notifications()}
      headers={['id', 'channel', 'status', 'revision']}
      row={(n) => [n.id, n.channel, n.status, n.revision]}
      footnote="READ-ONLY LIST · /api/notifications"
    />
  );
}

function RunsView() {
  const def = VIEW_MAP.runs;
  const runs = useLoad<Execution[]>(() => api.runs(), []);
  const [inspector, setInspector] = useState<RunInspector | null>(null);
  const [error, setError] = useState('');
  const inspect = (id: string) => {
    setError('');
    api.runInspector(id).then(setInspector).catch((cause: unknown) => setError(failureText(cause)));
  };
  return (
    <>
      <Head def={def}>
        Plan versions, checkpoints and context-projection digests for one execution.
        Native identities, raw results and failure blobs are intentionally absent.
      </Head>
      {runs.error && <Notice tone="error" text={runs.error} />}
      <Table
        headers={['id', 'status', 'mode', 'plan', 'revision']}
        rows={(runs.data ?? []).map((r) => [r.id, r.status, r.mode, r.plan_version, r.revision])}
        loading={runs.loading}
        footnote="READ-ONLY LIST · /api/runs"
      />
      {runs.data && runs.data.length > 0 && (
        <button type="button" className="btn btn--sm" onClick={() => inspect(runs.data![0].id)}>
          Inspect first run <span className="btn__arrow" aria-hidden="true">↗</span>
        </button>
      )}
      {error && <Notice tone="error" text={error} />}
      {inspector && (
        <div className="inspector rv rv-5">
          <StatusLine items={[
            ['EXECUTION', inspector.execution.id],
            ['PLANS', inspector.plans.length],
            ['EVIDENCE', inspector.evidence_count],
            ['EVENTS', inspector.event_count],
          ]} />
          <Table
            headers={['plan version', 'mode', 'rationale', 'authority expanded']}
            rows={inspector.plans.map((p) => [p.version, p.mode, p.rationale, p.authority_expanded])}
            footnote="PLAN VERSIONS · NO RAW RESULT"
          />
          {inspector.checkpoint && (
            <Notice tone="info" code="checkpoint" text={`${inspector.checkpoint.pause_reason} at ${inspector.checkpoint.current_step}`} />
          )}
          <Table
            headers={['context digest', 'source digest', 'scope']}
            rows={inspector.context_projections.map((c) => [c.digest, c.source_digest, c.continuity_scope])}
            footnote="DIGESTS ONLY · NOT STELE RECORDS"
          />
        </div>
      )}
    </>
  );
}

function SurfacesView() {
  const def = VIEW_MAP.surfaces;
  const [surfaceId, setSurfaceId] = useState('');
  const [executionId, setExecutionId] = useState('');
  const [taskRevision, setTaskRevision] = useState('1');
  const [wire, setWire] = useState<A2UISurfaceWire | null>(null);
  const [error, setError] = useState('');

  const load = () => {
    setError('');
    if (!surfaceId) { return; }
    api.surface(surfaceId)
      .then(setWire)
      .catch((cause: unknown) => {
        setWire(null);
        setError(failureText(cause));
      });
  };
  const revision = Number.parseInt(taskRevision, 10) || 1;

  return (
    <>
      <Head def={def}>
        Only server-registered A2UI 0.9.1 surfaces render here. A2UI 1.0 is not claimed;
        action submissions pass through the server gate with an idempotency key.
      </Head>
      <Panel title="Load a surface" meta="SERVER-APPROVED WIRE / READ-ONLY">
        <div className="controls">
          <Field label="Surface ID" value={surfaceId} onChange={setSurfaceId} placeholder="surface id" />
          <Field label="Execution ID" value={executionId} onChange={setExecutionId} placeholder="execution id" />
          <Field label="Task revision" value={taskRevision} onChange={setTaskRevision} />
          <div className="controls__actions">
            <button type="button" className="btn btn--primary" disabled={!surfaceId} onClick={load}>
              Load surface <span className="btn__arrow" aria-hidden="true">↗</span>
            </button>
          </div>
        </div>
        {error && <Notice tone="error" text={error} />}
        <SurfacePanel wire={wire} executionId={executionId} taskRevision={revision} />
      </Panel>
    </>
  );
}

export default function App() {
  const [view, setView] = useState<View>(viewFromHash);
  useEffect(() => {
    const sync = () => setView(viewFromHash());
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);
  const select = (id: string) => {
    setView(id as View);
    window.location.hash = `/${id}`;
  };
  const current = VIEW_MAP[view];
  return (
    <Shell
      views={VIEWS}
      active={view}
      onSelect={select}
      viewLabel={current.label}
    >
      {view === 'chat' && <ChatView />}
      {view === 'knowledge' && <KnowledgeView />}
      {view === 'tasks' && <TasksView />}
      {view === 'schedules' && <SchedulesView />}
      {view === 'deliveries' && <DeliveriesView />}
      {view === 'notifications' && <NotificationsView />}
      {view === 'runs' && <RunsView />}
      {view === 'surfaces' && <SurfacesView />}
    </Shell>
  );
}
