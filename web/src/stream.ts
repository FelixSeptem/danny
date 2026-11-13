// AG-UI-compatible execution projection consumer (Milestone 9 boundary).
// A native EventSource is deliberate: the browser automatically replays the
// Last-Event-ID header on reconnect, so the client never restarts or resumes
// an execution itself. Live execution state comes only from this stream;
// /api/ projections are point-in-time reads.

export interface StreamEvent {
  id: string;
  kind: string;
  data: string;
}

export interface StreamHandlers {
  onEvent: (event: StreamEvent) => void;
  onSnapshot: (event: StreamEvent) => void;
  onError: (message: string) => void;
}

export interface StreamSession {
  close: () => void;
  lastEventId: () => string;
}

// runEventsPath is the unchanged Milestone 9 endpoint mounted by the assembled
// service. threadId is advisory: the boundary keys the projection by run ID.
export function runEventsPath(threadId: string, runId: string): string {
  return `/agent/threads/${encodeURIComponent(threadId)}/runs/${encodeURIComponent(runId)}/events`;
}

export function openRunStream(threadId: string, runId: string, handlers: StreamHandlers): StreamSession {
  const source = new EventSource(runEventsPath(threadId, runId));
  let lastId = '';

  const capture = (event: MessageEvent<string>) => {
    if (event.lastEventId) { lastId = event.lastEventId; }
  };

  source.addEventListener('snapshot', (event) => {
    const message = event as MessageEvent<string>;
    capture(message);
    handlers.onSnapshot({ id: message.lastEventId, kind: 'snapshot', data: message.data });
  });

  source.addEventListener('event', (event) => {
    const message = event as MessageEvent<string>;
    capture(message);
    handlers.onEvent({ id: message.lastEventId, kind: 'event', data: message.data });
  });

  // A typed SSE comment or transport failure is surfaced as a diagnostic,
  // never converted into a fake RunError for the run itself.
  source.onerror = () => {
    handlers.onError('stream interrupted; the browser will reconnect with the last event id');
  };

  return {
    close: () => source.close(),
    lastEventId: () => lastId,
  };
}

// Parse one AG-UI wire event's type for display. Payloads remain opaque data:
// the client never interprets them as instructions or executable definitions.
export function eventTypeLabel(data: string): string {
  try {
    const parsed = JSON.parse(data) as { type?: string };
    return parsed.type ?? 'Custom';
  } catch {
    return 'Custom';
  }
}
