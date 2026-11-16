import { useEffect, useState, type ReactNode } from 'react';

export interface NavView {
  id: string;
  label: string;
}

type Theme = 'dark' | 'light';

// ?theme=light / ?theme=dark 可以强制主题（用于深链和验收），优先级高于本地偏好。
function forcedTheme(): Theme | null {
  const value = new URLSearchParams(window.location.search).get('theme');
  return value === 'light' || value === 'dark' ? value : null;
}

function initialTheme(): Theme {
  const forced = forcedTheme();
  if (forced) { return forced; }
  try {
    return localStorage.getItem('danny-theme') === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

export function Shell({
  views, active, onSelect, viewLabel, children,
}: {
  views: NavView[];
  active: string;
  onSelect: (id: string) => void;
  viewLabel: string;
  children: ReactNode;
}): JSX.Element {
  const [theme, setTheme] = useState<Theme>(initialTheme);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    // 深链强制的主题只作用于本次会话，不改写用户保存的偏好。
    if (forcedTheme()) { return; }
    try { localStorage.setItem('danny-theme', theme); } catch { /* 隐私模式下忽略 */ }
  }, [theme]);

  return (
    <div className="shell">
      <aside className="rail rv rv-1">
        <div className="rail__brand">
          <div className="rail__mark" aria-hidden="true"><span /><span /><span /><span /></div>
          <div className="rail__wordmark">DANNY</div>
          <p className="rail__sub">personal agent / control surface</p>
        </div>
        <div className="rail__rule" />
        <p className="rail__section">NAVIGATION / {String(views.length).padStart(2, '0')}</p>
        <nav className="rail__nav" aria-label="Workbench views">
          {views.map((item, index) => (
            <button
              key={item.id}
              type="button"
              className={`rail__item${active === item.id ? ' is-active' : ''}`}
              aria-current={active === item.id ? 'page' : undefined}
              title={item.label}
              onClick={() => onSelect(item.id)}
            >
              <span className="rail__num">{String(index + 1).padStart(2, '0')}</span>
              <span className="rail__label">{item.label}</span>
              <span className="rail__chev" aria-hidden="true">›</span>
            </button>
          ))}
        </nav>
        <div className="rail__foot">
          <div className="rail__foot-row"><span className="rail__led" /> LOOPBACK ONLY</div>
          <div className="rail__foot-row">SAME-ORIGIN · /api · /agent</div>
          <div className="rail__foot-row">A2UI 0.9.1 · AG-UI SSE</div>
        </div>
      </aside>

      <div className="frame">
        <header className="topbar rv rv-2">
          <div className="topbar__crumb">
            <span className="topbar__root">WORKBENCH</span>
            <span className="topbar__sep" aria-hidden="true">/</span>
            <span className="topbar__here">{viewLabel}</span>
          </div>
          <div className="topbar__right">
            <div className="topbar__status"><span className="pulse-dot" /> LOCAL SERVICE</div>
            <span className="topbar__divider" aria-hidden="true" />
            <span className="topbar__host">LOOPBACK · 127.0.0.1</span>
            <button
              type="button"
              className="theme-toggle"
              aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            >
              <span className="theme-toggle__glyph" aria-hidden="true">◐</span>
              {theme === 'dark' ? 'LIGHT' : 'DARK'}
            </button>
          </div>
        </header>
        <div className="pulse-line" aria-hidden="true" />
        <main className="stage">
          <div className="stage__inner">{children}</div>
        </main>
      </div>
    </div>
  );
}
