import { Component } from 'react';

// ErrorBoundary: a crash while rendering shows a message instead of an
// empty page. resetKey (the address) clears it. Plain text and CSS
// classes only (strict CSP: no inline styles).
export default class ErrorBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error('[console-error]', error, info?.componentStack);
  }

  componentDidUpdate(prev) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null });
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    const chunk = /dynamically imported module|ChunkLoadError/i.test(String(error?.message));
    return (
      <section className="error-state" role="alert">
        <h1>{chunk ? 'A new version is available' : 'Something went wrong on this page'}</h1>
        <p className="muted">{chunk ? 'Reload to continue.' : 'The rest of the console still works. Reload, or go back to Tenants.'}</p>
        <div className="row">
          <button type="button" className="btn btn-primary" onClick={() => window.location.reload()}>
            Reload
          </button>
          {!chunk && (
            <a className="btn" href="/tenants">
              Go to Tenants
            </a>
          )}
        </div>
      </section>
    );
  }
}
