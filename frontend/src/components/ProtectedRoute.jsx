import React from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { usePos } from '../hooks/usePos';
import { usePermission } from '../context/PermissionContext';
import { NoAccess } from './errors/ErrorScreens';

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

  if (module && !hasPermission(module, action)) return <NoAccess />;

  return children;
}
