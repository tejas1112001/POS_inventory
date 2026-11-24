import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource-variable/manrope';
import '../../shared/tokens.css';
import '../../shared/toast/toast.css';
import './global.css';
import { Provider } from 'react-redux';
import { store } from './store';
import App from './App';

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <Provider store={store}>
      <App />
    </Provider>
  </StrictMode>
);
