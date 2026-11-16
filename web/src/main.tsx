import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

// 字体随构建产物打包进 web/dist/assets。服务是 loopback-only 且不假设有外网，
// 因此不使用任何 CDN 字体；只加载真正用到的字重与 latin 子集。
import '@fontsource/archivo-black/latin-400.css';
import '@fontsource/ibm-plex-sans/latin-400.css';
import '@fontsource/ibm-plex-sans/latin-500.css';
import '@fontsource/ibm-plex-sans/latin-600.css';
import '@fontsource/ibm-plex-mono/latin-400.css';
import '@fontsource/ibm-plex-mono/latin-500.css';
import '@fontsource/ibm-plex-mono/latin-600.css';

import './styles/tokens.css';
import './styles/base.css';
import './styles/components.css';

import App from './App';

const container = document.getElementById('root');
if (!container) {
  throw new Error('workbench root element is missing');
}
createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
