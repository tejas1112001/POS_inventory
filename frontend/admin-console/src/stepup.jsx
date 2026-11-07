import { createContext, useCallback, useContext, useRef, useState } from 'react';
import { ApiError, api } from './api';
import { Button, Modal, Notice, TextInput } from './components/ui';

const StepUpContext = createContext(null);

// StepUpProvider: destructive actions (suspend, delete, slug change,
// exports, cancelling a subscription …) need a fresh authenticator code.
// withStepUp(fn) runs fn; if the server answers STEP_UP_REQUIRED it asks
// for a code, confirms it (valid 5 minutes) and runs fn once more.
export function StepUpProvider({ children }) {
  const [asking, setAsking] = useState(false);
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const pending = useRef(null);

  const ask = () =>
    new Promise((resolve, reject) => {
      pending.current = { resolve, reject };
      setCode('');
      setError('');
      setAsking(true);
    });

  const withStepUp = useCallback(async (fn) => {
    try {
      return await fn();
    } catch (err) {
      if (err?.code !== 'STEP_UP_REQUIRED') throw err;
      await ask();
      return fn();
    }
  }, []);

  const cancel = () => {
    setAsking(false);
    pending.current?.reject(new ApiError('Cancelled.', { code: 'CANCELLED' }));
    pending.current = null;
  };

  const confirm = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.post('/auth/step-up', { code: code.trim() });
      setAsking(false);
      pending.current?.resolve();
      pending.current = null;
    } catch (err) {
      setError(err.code === 'RATE_LIMITED' ? 'Too many tries. Wait a minute.' : 'That code did not work. Use the newest code from your authenticator.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <StepUpContext.Provider value={withStepUp}>
      {children}
      {asking && (
        <Modal title="Confirm it is you" onClose={cancel}>
          <form onSubmit={confirm} className="stack">
            <p>This action needs a fresh code from your authenticator app. It stays confirmed for 5 minutes.</p>
            <TextInput label="Authenticator code" value={code} onChange={setCode} inputMode="numeric" autoComplete="one-time-code" maxLength={6} />
            <Notice kind="error">{error}</Notice>
            <div className="row right">
              <Button onClick={cancel}>Cancel</Button>
              <Button type="submit" variant="primary" busy={busy} disabled={code.trim().length !== 6}>
                Confirm
              </Button>
            </div>
          </form>
        </Modal>
      )}
    </StepUpContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components -- hook kept with its provider
export function useStepUp() {
  return useContext(StepUpContext);
}
