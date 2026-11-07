import { createContext, useCallback, useContext, useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';

const ConfirmContext = createContext(null);

// ConfirmProvider gives every page useConfirm(): a promise-based replacement
// for window.confirm. Options: title, body, confirmLabel, cancelLabel,
// destructive (red button), typeToConfirm (a word the user must type).
export function ConfirmProvider({ children }) {
  const [state, setState] = useState(null);
  const [typed, setTyped] = useState('');

  const confirm = useCallback(
    (options) =>
      new Promise((resolve) => {
        setTyped('');
        setState({ confirmLabel: 'Confirm', cancelLabel: 'Cancel', ...options, resolve });
      }),
    []
  );

  const close = (answer) => {
    state?.resolve(answer);
    setState(null);
  };

  const blocked = !!state?.typeToConfirm && typed.trim() !== state.typeToConfirm;

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <Dialog open={!!state} onOpenChange={(open) => !open && close(false)}>
        {state && (
          <DialogContent sheet className="sm:max-w-md" onOpenAutoFocus={(e) => state.typeToConfirm && e.preventDefault()}>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                {state.destructive && <AlertTriangle className="size-5 text-danger" aria-hidden="true" />}
                {state.title}
              </DialogTitle>
              {state.body && <DialogDescription>{state.body}</DialogDescription>}
            </DialogHeader>
            {state.typeToConfirm && (
              <label className="grid gap-1.5 text-sm">
                <span>
                  Type <strong className="font-mono">{state.typeToConfirm}</strong> to confirm
                </span>
                <input
                  className="h-11 rounded-md border border-input bg-transparent px-3 text-base sm:h-9 sm:text-sm"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  autoComplete="off"
                  autoFocus
                />
              </label>
            )}
            <DialogFooter className="gap-2">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => close(false)}>
                {state.cancelLabel}
              </Button>
              <Button
                type="button"
                variant={state.destructive ? 'destructive' : 'default'}
                className="max-sm:h-11"
                disabled={blocked}
                onClick={() => close(true)}
              >
                {state.confirmLabel}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </ConfirmContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components -- hook kept next to its provider
export function useConfirm() {
  const confirm = useContext(ConfirmContext);
  if (!confirm) throw new Error('useConfirm must be used inside ConfirmProvider');
  return confirm;
}
