import { useState } from 'react';
import { A2UI_WIRE_VERSION, submitSurfaceAction } from './api';

// Restricted renderer for A2UI v0.9.1 declarative surfaces.
//
// The authoritative catalog is registered on the server
// (internal/agentui.DefaultCatalog). This client-side set is a READ-ONLY
// rendering guard: it lists the components the workbench knows how to draw.
// A surface referencing any other component is refused, never executed, and
// the browser cannot add an entry to this set or to the server catalog.

export const RENDERABLE_COMPONENTS = Object.freeze(['Card', 'ChoiceChips', 'Text'] as const);

export interface WireComponent {
  type: string;
  props?: Record<string, unknown>;
  children?: WireComponent[];
}

export interface WireAction {
  id: string;
  policy_ref: string;
}

export interface A2UISurfaceWire {
  a2ui_version: string;
  surface_id: string;
  revision: number;
  catalog_version: string;
  components: WireComponent[];
  actions: WireAction[];
}

export interface SurfaceRejection {
  code: string;
  message: string;
}

// validateSurface is the fail-closed gate before any rendering happens.
export function validateSurface(wire: A2UISurfaceWire): SurfaceRejection | null {
  if (wire.a2ui_version !== A2UI_WIRE_VERSION) {
    return { code: 'unsupported_version', message: `Only A2UI ${A2UI_WIRE_VERSION} is supported.` };
  }
  if (wire.catalog_version !== 'danny.catalog.v1') {
    return { code: 'unknown_catalog', message: 'Surface references an unregistered catalog.' };
  }
  const check = (nodes: WireComponent[]): SurfaceRejection | null => {
    for (const node of nodes) {
      if (!RENDERABLE_COMPONENTS.includes(node.type as (typeof RENDERABLE_COMPONENTS)[number])) {
        return { code: 'unregistered_component', message: `Component ${node.type} is not server-registered.` };
      }
      if (node.children) {
        const nested = check(node.children);
        if (nested) { return nested; }
      }
    }
    return null;
  };
  return check(wire.components ?? []);
}

function textProp(props: Record<string, unknown> | undefined, key: string): string {
  const value = props?.[key];
  return typeof value === 'string' ? value : '';
}

function RenderNode({ node }: { node: WireComponent }): JSX.Element {
  // Props are untrusted data. React escapes them as text; they are never
  // interpreted as component names, action IDs, policies, or handlers.
  switch (node.type) {
    case 'Card':
      return (
        <section className="surface-card">
          <h3>{textProp(node.props, 'title')}</h3>
          {(node.children ?? []).map((child, index) => <RenderNode key={index} node={child} />)}
        </section>
      );
    case 'Text':
      return <p className="surface-text">{textProp(node.props, 'value')}</p>;
    case 'ChoiceChips':
      return <p className="surface-choices">{textProp(node.props, 'options')}</p>;
    default:
      return <p className="surface-error">unregistered component</p>;
  }
}

export interface SurfacePanelProps {
  wire: A2UISurfaceWire | null;
  executionId: string;
  taskRevision: number;
}

export function SurfacePanel({ wire, executionId, taskRevision }: SurfacePanelProps): JSX.Element {
  const [result, setResult] = useState<{ ok: boolean; code: string; message: string } | null>(null);
  const [choice, setChoice] = useState('');

  if (!wire) {
    return <p className="empty">No server-approved surface is active for this run.</p>;
  }
  const rejection = validateSurface(wire);
  if (rejection) {
    return (
      <div className="typed-rejection" role="alert">
        <strong>{rejection.code}</strong>: {rejection.message}
      </div>
    );
  }

  const submit = async (actionId: string) => {
    // The submission carries only a registered action ID, user activation,
    // and a plain data payload. Component props never become a mutation.
    const outcome = await submitSurfaceAction({
      contract_version: 'danny.surface.v1',
      execution_id: executionId,
      surface_id: wire.surface_id,
      surface_revision: wire.revision,
      task_revision: taskRevision,
      idempotency_key: crypto.randomUUID(),
      actions: [{ action_id: actionId, user_activation: true, payload: choice ? { choice } : {} }],
    });
    setResult(outcome);
  };

  return (
    <div className="surface-panel">
      <p className="surface-meta">
        A2UI {wire.a2ui_version} · surface {wire.surface_id} · revision {wire.revision}
      </p>
      {(wire.components ?? []).map((node, index) => <RenderNode key={index} node={node} />)}
      <label className="surface-input">
        Choice
        <input value={choice} onChange={(event) => setChoice(event.target.value)} placeholder="approve / reject" />
      </label>
      <div className="surface-actions">
        {wire.actions.map((action) => (
          <button key={action.id} type="button" onClick={() => void submit(action.id)}>
            {action.id}
          </button>
        ))}
      </div>
      {result && (
        <div className={result.ok ? 'typed-accepted' : 'typed-rejection'} role="status">
          <strong>{result.code}</strong>: {result.message}
        </div>
      )}
      <p className="surface-note">
        Actions are validated and executed by the server gate. This client holds no write authority.
      </p>
    </div>
  );
}
