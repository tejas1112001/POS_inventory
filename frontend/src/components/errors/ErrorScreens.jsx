import { useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, Compass, Lock, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { errorDetails, isChunkLoadError } from './ErrorBoundary';

function Panel({ icon: Icon, tone = 'muted', title, children, actions }) {
  const tones = {
    muted: 'bg-muted text-muted-foreground',
    danger: 'bg-destructive/10 text-destructive',
  };
  return (
    <section className="mx-auto flex max-w-md flex-col items-center px-4 py-16 text-center" aria-labelledby="state-title">
      <div className={`mb-4 flex size-14 items-center justify-center rounded-full ${tones[tone]}`}>
        <Icon className="size-7" aria-hidden="true" />
      </div>
      <h1 id="state-title" className="text-xl font-semibold text-foreground">{title}</h1>
      <div className="mt-2 text-sm text-muted-foreground">{children}</div>
      {actions && <div className="mt-6 flex flex-wrap justify-center gap-2">{actions}</div>}
    </section>
  );
}

function CopyDetails({ error }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(errorDetails(error));
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Button type="button" variant="ghost" onClick={copy}>
      {copied ? 'Details copied' : 'Copy details'}
    </Button>
  );
}

// PageError replaces only the page area: the menu stays usable.
export function PageError({ error, reset }) {
  if (isChunkLoadError(error)) return <NewVersion />;
  return (
    <Panel
      icon={AlertTriangle}
      tone="danger"
      title="Something went wrong on this page"
      actions={
        <>
          <Button type="button" onClick={reset}>
            <RefreshCw className="size-4" aria-hidden="true" /> Try again
          </Button>
          <Button type="button" variant="outline" asChild>
            <Link to="/dashboard">Go to Dashboard</Link>
          </Button>
          <CopyDetails error={error} />
        </>
      }
    >
      The rest of the app still works. If this keeps happening, copy the details and send them to support.
    </Panel>
  );
}

// AppError is the last resort when the whole app fails to start.
export function AppError({ error }) {
  if (isChunkLoadError(error)) return <NewVersion fullPage />;
  return (
    <div className="flex min-h-dvh items-center bg-background">
      <Panel
        icon={AlertTriangle}
        tone="danger"
        title="Something went wrong"
        actions={
          <>
            <Button type="button" onClick={() => window.location.reload()}>
              <RefreshCw className="size-4" aria-hidden="true" /> Reload
            </Button>
            <Button type="button" variant="outline" onClick={() => window.location.assign('/dashboard')}>
              Go to Dashboard
            </Button>
            <CopyDetails error={error} />
          </>
        }
      >
        The app could not show this page. Reloading usually helps.
      </Panel>
    </div>
  );
}

function NewVersion({ fullPage = false }) {
  const body = (
    <Panel
      icon={RefreshCw}
      title="A new version is available"
      actions={
        <Button type="button" onClick={() => window.location.reload()}>
          Reload
        </Button>
      }
    >
      The app was updated while this page was open. Reload to continue.
    </Panel>
  );
  return fullPage ? <div className="flex min-h-dvh items-center bg-background">{body}</div> : body;
}

export function NotFound() {
  return (
    <Panel
      icon={Compass}
      title="Page not found"
      actions={
        <Button asChild>
          <Link to="/dashboard">Go to Dashboard</Link>
        </Button>
      }
    >
      This address does not exist. Check the link, or use the menu.
    </Panel>
  );
}

export function NoAccess() {
  return (
    <Panel
      icon={Lock}
      title="You don't have access to this page"
      actions={
        <Button asChild>
          <Link to="/dashboard">Back to Dashboard</Link>
        </Button>
      }
    >
      Your role does not include this page. Ask the shop owner if you need it.
    </Panel>
  );
}
