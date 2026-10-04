import React from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { usePos } from '../hooks/usePos';
import { usePermission } from '../context/PermissionContext';

export default function ProtectedRoute({ children, module, action = 'view' }) {
  const { currentUser, loading } = usePos();
  const { hasPermission, loadingPermissions } = usePermission();
  const location = useLocation();

  if (loading || loadingPermissions) {
    return (
      <div className="flex items-center justify-center py-20 text-muted-foreground">
        Loading session...
      </div>
    );
  }

  if (!currentUser) {
    // Redirect to signin, preserving the intended destination
    return <Navigate to="/signin" state={{ from: location }} replace />;
  }

  if (module && !hasPermission(module, action)) {
    return <Navigate to="/" replace />;
  }

  return children;
}
