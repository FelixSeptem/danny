import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiFailure } from './api';
import { eventTypeLabel, openRunStream, type StreamEvent, type StreamSession } from './stream';
import { SurfacePanel, type A2UISurfaceWire } from './surfaces';
import type {
  Artifact, Delivery, Execution, Generation, IndexStatus, Notification,
  Occurrence, RunInspector, Schedule, Source, Task,
} from './types';

type View = 'chat' | 'knowledge' | 'tasks' | 'schedules' | 'deliveries' | 'notifications' | 'runs' | 'surfaces';

const VIEWS: Array<{ id: View; label: string }> = [
  { id: 'chat', label: 'Chat / execution stream' },
  { id: 'knowledge', label: 'Knowledge' },
  { id: 'tasks', label: 'Tasks' },
  { id: 'schedules', label: 'Schedules' },
  { id: 'deliveries', label: 'Deliveries' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'runs', label: 'Run inspector' },
  { id: 'surfaces', label: 'Dynamic surfaces' },
];

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
  return { data, error, reload };
}

function Table({ headers, rows }: { headers: string[]; rows: Array<Array<string | number | boolean>> }) {
  return (
    <table className="grid">
      <thead><tr>{headers.map((h) => <th key={h}>{h}</th>)}</tr></thead>
      <tbody>
        {rows.length === 0
          ? <tr><td className="empty" colSpan={headers.length}>No records.</td></tr>
          : rows.map((row, i) => <tr key={i}>{row.map((cell, j) => <td key={j}>{String(cell)}</td>)}</tr>)}
      </tbody>
    </table>
  );
}

function ChatView() {
  const [runId, setRunId] = useState('');
  const [threadId, setThreadId] = useState('corr');
  const [events, setEvents] = useState<StreamEvent[]>([]);
  const [diagnostic, setDiagnostic] = useState('');
  const sessionRef = useRef<StreamSession | null>(null);

  const connect = () => {
    sessionRef.current?.close();
    setEvents([]);
    setDiagnostic('');
    if (!runId) { return; }
    sessionRef.current = openRunStream(threadId, runId, {
      onEvent: (event) => setEvents((prev) => [...prev, event]),
      onSnapshot: (event) => setEvents((prev) => [...prev, event]),
      onError: setDiagnostic,
    });
  };
  useEffect(() => () => sessionRef.current?.close(), []);

  return (
    <section>
      <h2>Execution stream</h2>
      <p className="hint">
        Consumes the Milestone 9 AG-UI-compatible SSE projection. The browser reconnects with
        <code> Last-Event-ID</code> automatically; replay never restarts the run.
      </p>
      <div className="controls">
        <label>Thread<input value={threadId} onChange={(e) => setThreadId(e.target.value)} /></label>
        <label>Run ID<input value={runId} onChange={(e) => setRunId(e.target.value)} placeholder="execution id" /></label>
        <button type="button" onClick={connect}>Connect</button>
        <button type="button" onClick={() => sessionRef.current?.close()}>Disconnect</button>
      </div>
      {diagnostic && <div className="typed-rejection" role="status">{diagnostic}</div>}
      <ol className="event-log">
        {events.length === 0 && <li className="empty">No stream events yet.</li>}
        {events.map((event, index) => (
          <li key={`${event.id}-${index}`}>
            <span className="event-id">{event.id || '—'}</span>
            <span className="event-kind">{eventTypeLabel(event.data)}</span>
            <code>{event.data}</code>
          </li>
        ))}
      </ol>
    </section>
  );
}

function KnowledgeView() {
  const generations = useLoad<Generation[]>(() => api.generations(), []);
  const sources = useLoad<Source[]>(() => api.sources(), []);
  const status = useLoad<IndexStatus>(() => api.indexStatus(), []);
  const [artifacts, setArtifacts] = useState<Artifact[] | null>(null);
  const [artifactError, setArtifactError] = useState('');

  const showArtifacts = (id: number) => {
    setArtifactError('');
    api.artifacts(id)
      .then(setArtifacts)
      .catch((cause: unknown) => setArtifactError(cause instanceof ApiFailure ? cause.code : String(cause)));
  };

  return (
    <section>
      <h2>Knowledge</h2>
      {status.error && <div className="typed-rejection">{status.error}</div>}
      {status.data && (
        <p className="hint">
          Generation {status.data.generation_id || 'none'} · {status.data.generation_status || 'n/a'} ·
          {status.data.artifact_count} artifacts · {status.data.source_count} sources
        </p>
      )}
      <h3>Generations</h3>
      {generations.error && <div className="typed-rejection">{generations.error}</div>}
      {generations.data && (
        <Table
          headers={['id', 'status', 'commit marker', 'created']}
          rows={generations.data.map((g) => [
            g.id, g.status, g.commit_marker, g.created_at,
            // Clicking a row reads a bounded list through the read-only API.
          ])}
        />
      )}
      {generations.data && generations.data.length > 0 && (
        <button type="button" onClick={() => showArtifacts(generations.data![0].id)}>
          Browse artifacts of latest generation
        </button>
      )}
      {artifactError && <div className="typed-rejection">{artifactError}</div>}
      {artifacts && <Table headers={['id', 'kind', 'content hash']} rows={artifacts.map((a) => [a.id, a.kind, a.content_hash])} />}
      <h3>Sources</h3>
      {sources.error && <div className="typed-rejection">{sources.error}</div>}
      {sources.data && (
        <Table headers={['path', 'format', 'parser', 'provider', 'untrusted']} rows={sources.data.map((s) => [s.path, s.format, s.parser_version, s.provider_mode, s.untrusted_source])} />
      )}
    </section>
  );
}

function TasksView() {
  const tasks = useLoad<Task[]>(() => api.tasks(), []);
  const [selected, setSelected] = useState<Task | null>(null);
  const [title, setTitle] = useState('');
  const [outcome, setOutcome] = useState<{ ok: boolean; text: string } | null>(null);

  const select = async (id: string) => {
    try {
      const task = await api.task(id);
      setSelected(task);
      setTitle(task.title);
      setOutcome(null);
    } catch (cause) {
      setOutcome({ ok: false, text: cause instanceof ApiFailure ? `${cause.code}: ${cause.message}` : String(cause) });
    }
  };
  const save = async () => {
    if (!selected) { return; }
    try {
      const updated = await api.updateTask(selected.id, selected.revision, { title });
      setSelected(updated);
      setOutcome({ ok: true, text: `Saved at revision ${updated.revision}.` });
      tasks.reload();
    } catch (cause) {
      // A stale revision surfaces the typed conflict instead of overwriting.
      setOutcome({ ok: false, text: cause instanceof ApiFailure ? `${cause.code}: ${cause.message}` : String(cause) });
    }
  };

  return (
    <section>
      <h2>Tasks</h2>
      <p className="hint">Updates carry <code>expected_revision</code>; a stale write displays a typed conflict.</p>
      {tasks.error && <div className="typed-rejection">{tasks.error}</div>}
      {tasks.data && (
        <Table headers={['id', 'title', 'status', 'revision']} rows={tasks.data.map((t) => [t.id, t.title, t.status, t.revision])} />
      )}
      <div className="controls">
        <label>Task ID<input id="task-id" placeholder="task id" /></label>
        <button type="button" onClick={() => { const el = document.getElementById('task-id') as HTMLInputElement | null; if (el) { void select(el.value); } }}>Load</button>
      </div>
      {selected && (
        <div className="editor">
          <label>Title<input value={title} onChange={(e) => setTitle(e.target.value)} /></label>
          <button type="button" onClick={() => void save()}>Save with revision {selected.revision}</button>
        </div>
      )}
      {outcome && <div className={outcome.ok ? 'typed-accepted' : 'typed-rejection'} role="status">{outcome.text}</div>}
    </section>
  );
}

function SchedulesView() {
  const schedules = useLoad<Schedule[]>(() => api.schedules(), []);
  const [occurrences, setOccurrences] = useState<Occurrence[] | null>(null);
  const [error, setError] = useState('');
  const history = (id: string) => {
    setError('');
    api.occurrences(id).then(setOccurrences).catch((cause: unknown) => setError(cause instanceof ApiFailure ? cause.code : String(cause)));
  };
  return (
    <section>
      <h2>Schedules</h2>
      {schedules.error && <div className="typed-rejection">{schedules.error}</div>}
      {schedules.data && (
        <Table headers={['id', 'cron', 'timezone', 'misfire', 'enabled']} rows={schedules.data.map((s) => [s.id, s.cron, s.timezone, s.misfire_policy, s.enabled])} />
      )}
      {schedules.data && schedules.data.length > 0 && (
        <button type="button" onClick={() => history(schedules.data![0].id)}>Execution history of first schedule</button>
      )}
      {error && <div className="typed-rejection">{error}</div>}
      {occurrences && <Table headers={['occurrence', 'local time', 'dst', 'status', 'attempts']} rows={occurrences.map((o) => [o.occurrence_key, o.local_time, o.dst_decision, o.status, o.attempt_count])} />}
    </section>
  );
}

function SimpleListView<T>({ title, loader, headers, row }: {
  title: string;
  loader: () => Promise<T[]>;
  headers: string[];
  row: (item: T) => Array<string | number | boolean>;
}) {
  const state = useLoad<T[]>(loader, []);
  return (
    <section>
      <h2>{title}</h2>
      {state.error && <div className="typed-rejection">{state.error}</div>}
      {state.data && <Table headers={headers} rows={state.data.map(row)} />}
    </section>
  );
}

function DeliveriesView() {
  return <SimpleListView<Delivery> title="Webhook deliveries" loader={() => api.deliveries()} headers={['id', 'status', 'attempts', 'last error', 'updated']} row={(d) => [d.id, d.status, d.attempt_count, d.last_error, d.updated_at]} />;
}

function NotificationsView() {
  return <SimpleListView<Notification> title="Notifications" loader={() => api.notifications()} headers={['id', 'channel', 'status', 'revision']} row={(n) => [n.id, n.channel, n.status, n.revision]} />;
}

function RunsView() {
  const runs = useLoad<Execution[]>(() => api.runs(), []);
  const [inspector, setInspector] = useState<RunInspector | null>(null);
  const [error, setError] = useState('');
  const inspect = (id: string) => {
    setError('');
    api.runInspector(id).then(setInspector).catch((cause: unknown) => setError(cause instanceof ApiFailure ? cause.code : String(cause)));
  };
  return (
    <section>
      <h2>Run inspector</h2>
      {runs.error && <div className="typed-rejection">{runs.error}</div>}
      {runs.data && <Table headers={['id', 'status', 'mode', 'plan', 'revision']} rows={runs.data.map((r) => [r.id, r.status, r.mode, r.plan_version, r.revision])} />}
      {runs.data && runs.data.length > 0 && <button type="button" onClick={() => inspect(runs.data![0].id)}>Inspect first run</button>}
      {error && <div className="typed-rejection">{error}</div>}
      {inspector && (
        <div className="inspector">
          <p className="hint">Plans {inspector.plans.length} · evidence {inspector.evidence_count} · events {inspector.event_count}</p>
          <Table headers={['plan version', 'mode', 'rationale', 'authority expanded']} rows={inspector.plans.map((p) => [p.version, p.mode, p.rationale, p.authority_expanded])} />
          {inspector.checkpoint && <p className="hint">Checkpoint: {inspector.checkpoint.pause_reason} at {inspector.checkpoint.current_step}</p>}
          <Table headers={['context digest', 'source digest', 'scope']} rows={inspector.context_projections.map((c) => [c.digest, c.source_digest, c.continuity_scope])} />
        </div>
      )}
    </section>
  );
}

function SurfacesView() {
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
        setError(cause instanceof ApiFailure ? `${cause.code}: ${cause.message}` : String(cause));
      });
  };
  const revision = Number.parseInt(taskRevision, 10) || 1;

  return (
    <section>
      <h2>Dynamic surfaces</h2>
      <p className="hint">
        Only server-registered A2UI 0.9.1 surfaces render here. A2UI 1.0 is not claimed;
        action submissions pass through the server gate with an idempotency key.
      </p>
      <div className="controls">
        <label>Surface ID<input value={surfaceId} onChange={(e) => setSurfaceId(e.target.value)} placeholder="surface id" /></label>
        <label>Execution ID<input value={executionId} onChange={(e) => setExecutionId(e.target.value)} placeholder="execution id" /></label>
        <label>Task revision<input value={taskRevision} onChange={(e) => setTaskRevision(e.target.value)} /></label>
        <button type="button" onClick={load}>Load server surface</button>
      </div>
      {error && <div className="typed-rejection" role="alert">{error}</div>}
      <SurfacePanel wire={wire} executionId={executionId} taskRevision={revision} />
    </section>
  );
}

export default function App() {
  const [view, setView] = useState<View>('chat');
  return (
    <div className="app">
      <aside className="sidebar">
        <h1>Danny</h1>
        <p className="boundary">loopback · same-origin</p>
        <nav>
          {VIEWS.map((item) => (
            <button key={item.id} type="button" className={view === item.id ? 'active' : ''} onClick={() => setView(item.id)}>
              {item.label}
            </button>
          ))}
        </nav>
      </aside>
      <main className="content">
        {view === 'chat' && <ChatView />}
        {view === 'knowledge' && <KnowledgeView />}
        {view === 'tasks' && <TasksView />}
        {view === 'schedules' && <SchedulesView />}
        {view === 'deliveries' && <DeliveriesView />}
        {view === 'notifications' && <NotificationsView />}
        {view === 'runs' && <RunsView />}
        {view === 'surfaces' && <SurfacesView />}
      </main>
    </div>
  );
}
