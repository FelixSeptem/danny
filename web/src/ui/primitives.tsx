import type { CSSProperties, ReactNode } from 'react';

/* 纯展示组件。这里没有任何数据获取、状态写入或对后端契约的解释，
 * 输入的字符串一律当作不可信文本交给 React 转义。 */

export type Tone = 'ok' | 'warn' | 'danger' | 'info' | 'neutral';

const TONE_WORDS: Record<Exclude<Tone, 'neutral'>, string[]> = {
  ok: ['ok', 'accepted', 'active', 'committed', 'complete', 'completed', 'done', 'enabled', 'ready', 'running', 'succeeded', 'success', 'true', 'healthy'],
  warn: ['backfill', 'deferred', 'misfire', 'paused', 'pending', 'queued', 'retry', 'retrying', 'skipped', 'stale', 'waiting'],
  danger: ['conflict', 'disabled', 'error', 'failed', 'failure', 'false', 'rejected', 'unavailable'],
  info: ['draft', 'in_progress', 'planned', 'projected', 'read_only', 'snapshot'],
};

export function toneForStatus(value: string): Tone {
  const normalized = value.trim().toLowerCase();
  for (const tone of ['ok', 'warn', 'danger', 'info'] as const) {
    if (TONE_WORDS[tone].includes(normalized)) { return tone; }
  }
  return 'neutral';
}

export function Badge({ value, tone }: { value: string; tone?: Tone }): JSX.Element {
  return <span className={`badge badge--${tone ?? toneForStatus(value)}`}>{value || '—'}</span>;
}

export function Notice({
  tone = 'error', text, code, role,
}: {
  tone?: 'error' | 'ok' | 'info';
  text: string;
  code?: string;
  role?: 'alert' | 'status';
}): JSX.Element {
  const glyph = tone === 'ok' ? '✓' : tone === 'error' ? '!' : 'i';
  return (
    <div className={`notice notice--${tone}`} role={role ?? (tone === 'error' ? 'alert' : 'status')}>
      <span className="notice__glyph" aria-hidden="true">{glyph}</span>
      <div>
        {code && <span className="notice__code">{code}</span>}
        <p className="notice__msg">{text}</p>
      </div>
    </div>
  );
}

export function EmptyState({
  title = 'NO RECORDS', children,
}: {
  title?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="empty-state">
      <div className="empty-state__ticks" aria-hidden="true">
        {Array.from({ length: 9 }, (_, i) => <i key={i} />)}
      </div>
      <div>
        <p className="empty-state__title">{title}</p>
        <p className="empty-state__text">{children}</p>
      </div>
    </div>
  );
}

export function PageHead({
  index, kicker, title, meta, children,
}: {
  index: string;
  kicker: string;
  title: string;
  meta?: ReactNode;
  children?: ReactNode;
}): JSX.Element {
  return (
    <header className="page-head rv rv-3">
      <span className="page-head__index" aria-hidden="true">{index}</span>
      <div className="page-head__body">
        <p className="page-head__kicker">{kicker}</p>
        <h1 className="page-head__title">{title}</h1>
        {children && <p className="page-head__desc">{children}</p>}
      </div>
      {meta && <div className="page-head__meta">{meta}</div>}
    </header>
  );
}

export function Panel({
  title, meta, children, className = '',
}: {
  title: string;
  meta?: string;
  children: ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <section className={`panel rv rv-4 ${className}`}>
      <div className="panel__head">
        <h2 className="panel__title">{title}</h2>
        {meta && <p className="panel__meta">{meta}</p>}
      </div>
      <div className="panel__body">{children}</div>
    </section>
  );
}

export function Field({
  label, value, onChange, placeholder,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
}): JSX.Element {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} />
    </label>
  );
}

export function StatusLine({ items }: { items: Array<[string, string | number]> }): JSX.Element {
  return (
    <div className="status-line">
      {items.map(([key, value]) => (
        <div className="status-line__item" key={key}>
          <span className="status-line__key">{key}</span>
          <span className="status-line__val">{value}</span>
        </div>
      ))}
    </div>
  );
}

type Cell = string | number | boolean;

// 长文本列不适合等宽排版，切回正文字体。其余列默认等宽数字/标识符。
const TEXT_COLUMN = /(title|rationale|error|instruction|path|description|message|reason)/i;

function cellText(value: Cell): string {
  if (typeof value === 'boolean') { return value ? 'true' : 'false'; }
  return String(value ?? '');
}

export interface TableProps {
  headers: string[];
  rows: Cell[][];
  loading?: boolean;
  statusColumns?: number[];
  monoColumns?: number[];
  textColumns?: number[];
  footnote?: string;
  emptyTitle?: string;
  emptyMessage?: string;
}

export function Table({
  headers, rows, loading = false, statusColumns, monoColumns, textColumns,
  footnote = 'READ-ONLY PROJECTION', emptyTitle, emptyMessage,
}: TableProps): JSX.Element {
  // 表头里叫 status / state 的列自动转成带文字的状态徽章（不只靠颜色表意）。
  const inferredStatus = headers
    .map((header, index) => (/^(status|state)$/i.test(header.trim()) ? index : -1))
    .filter((index) => index >= 0);
  const statusCols = statusColumns ?? inferredStatus;
  const booleanCols = headers
    .map((header, index) => (/^(enabled|untrusted|authority expanded)$/i.test(header.trim()) ? index : -1))
    .filter((index) => index >= 0);
  const textCols = textColumns ?? headers
    .map((header, index) => (TEXT_COLUMN.test(header) ? index : -1))
    .filter((index) => index >= 0);
  const monoCols = monoColumns ?? [];

  const renderCell = (value: Cell, column: number) => {
    const text = cellText(value);
    if (statusCols.includes(column) || booleanCols.includes(column)) {
      return <Badge value={text} />;
    }
    if (!text) { return <span className="cell-empty">—</span>; }
    const className = textCols.includes(column) && !monoCols.includes(column) ? 'cell-text' : undefined;
    return <span className={className}>{text}</span>;
  };

  return (
    <div className="data rv rv-5">
      <div className="data__scroll">
        <table className="grid">
          <thead>
            <tr>{headers.map((header) => <th key={header} scope="col">{header}</th>)}</tr>
          </thead>
          <tbody>
            {loading && Array.from({ length: 3 }, (_, row) => (
              <tr key={`skeleton-${row}`} aria-hidden="true">
                {headers.map((header, column) => (
                  <td key={header}>
                    <span
                      className="skeleton__line"
                      style={{ '--w': `${42 + ((row * 17 + column * 23) % 46)}%` } as CSSProperties}
                    />
                  </td>
                ))}
              </tr>
            ))}
            {!loading && rows.length === 0 && (
              <tr>
                <td className="cell-blank" colSpan={headers.length}>
                  <EmptyState title={emptyTitle}>
                    {emptyMessage ?? 'This projection has no records yet. Data comes from the read-only /api/ endpoints; lists are bounded to 200 rows.'}
                  </EmptyState>
                </td>
              </tr>
            )}
            {!loading && rows.map((row, rowIndex) => (
              <tr key={rowIndex}>
                {row.map((cell, columnIndex) => (
                  <td key={columnIndex} className={textCols.includes(columnIndex) && !monoCols.includes(columnIndex) ? 'cell-text' : undefined}>
                    {renderCell(cell, columnIndex)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="data__foot">
        <span>{loading ? 'LOADING…' : `${rows.length} RECORD${rows.length === 1 ? '' : 'S'} · MAX 200`}</span>
        <span>{footnote}</span>
      </div>
    </div>
  );
}
