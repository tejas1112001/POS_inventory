import React, { createContext, useContext } from 'react';
import { toast } from '../../shared/toast/store';

// ErrorContext keeps the old useError().showError API; every message now
// goes through the shared toast system (docs/UI_REDESIGN_PLAN.md §2).
// The <Toaster /> itself is mounted once in Root.jsx.
const ErrorContext = createContext({ showError: (m) => toast.error(m) });

// eslint-disable-next-line react-refresh/only-export-components -- hook kept next to its provider
export const useError = () => useContext(ErrorContext);

const value = { showError: (message) => toast.error(message) };

export const ErrorProvider = ({ children }) => (
  <ErrorContext.Provider value={value}>
    {children}
  </ErrorContext.Provider>
);

export { toast };
