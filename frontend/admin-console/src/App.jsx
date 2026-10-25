import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { SessionProvider, useSession } from './session';
import { StepUpProvider } from './stepup';
import Layout from './components/Layout';
import SignIn from './pages/SignIn';
import Tenants from './pages/Tenants';

function Routed() {
  const { loading, user } = useSession();
  if (loading) return <p className="empty">Loading…</p>;
  if (!user) return <SignIn />;
  return (
    <Layout>
      <Routes>
        <Route path="/tenants" element={<Tenants />} />
        <Route path="*" element={<Navigate to="/tenants" replace />} />
      </Routes>
    </Layout>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <SessionProvider>
        <StepUpProvider>
          <Routed />
        </StepUpProvider>
      </SessionProvider>
    </BrowserRouter>
  );
}
