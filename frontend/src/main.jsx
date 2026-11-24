import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import Root from './Root.jsx'

// Links from the old hash-based app (/#/sales) keep working.
if (window.location.hash.startsWith('#/')) {
  window.history.replaceState(null, '', window.location.hash.slice(1))
}

// Dark mode is set before the first paint (no light flash). The public
// shop pages always render light.
try {
  const publicPage = /^\/($|product\/|shop\/)/.test(window.location.pathname)
  if (!publicPage && localStorage.getItem('mintobill-mode') === 'dark') {
    document.documentElement.classList.add('dark')
  }
} catch {
  /* storage blocked: light */
}

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <Root />
  </StrictMode>,
)
