import React from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { usePos } from '../hooks/usePos';
import { usePermission } from '../context/PermissionContext';

export default function ProtectedRoute({ children, module, action = 'view' }) {
  const { currentUser, loading } = usePos();
  const { hasPermission } = usePermission();
  const location = useLocation();

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20 text-muted-foreground">
        Loading session...
      </div>
    );
  }

  if (!currentUser) {
    // Redirect to the sign-in, preserving the intended destination
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  if (module && !hasPermission(module, action)) {
    if (location.pathname === '/dashboard') {
      return <div className="py-20 text-center text-muted-foreground">You do not have access to this page.</div>;
    }
    return <Navigate to="/dashboard" replace />;
  }

  return children;
}
