import { Component } from 'react';

// isChunkLoadError tells a failed download of a code chunk (usually a new
// version was deployed while the page was open) from a real crash.
export function isChunkLoadError(error) {
  const msg = String(error?.message || error || '');
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|ChunkLoadError/i.test(msg);
}

// errorDetails is the text behind "Copy details": enough for support to
// find the problem, no stack trace on screen.
export function errorDetails(error) {
  return [
    `Error: ${String(error?.message || error)}`,
    `Page: ${window.location.pathname}`,
    `Time: ${new Date().toISOString()}`,
    `Browser: ${navigator.userAgent}`,
  ].join('\n');
}

// ErrorBoundary catches a crash while rendering its children and shows
// fallback({ error, reset }) instead of an empty page. A change of
// resetKey (for example the page address) clears the error.
export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
    this.reset = () => this.setState({ error: null });
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error('[ui-error]', error, info?.componentStack);
  }

  componentDidUpdate(prev) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.reset();
  }

  render() {
    if (this.state.error) return this.props.fallback({ error: this.state.error, reset: this.reset });
    return this.props.children;
  }
}
