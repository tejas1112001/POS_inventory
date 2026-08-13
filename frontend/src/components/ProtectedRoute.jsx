import React from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { usePos } from '../hooks/usePos';

export default function ProtectedRoute({ children }) {
  const { currentUser, loading } = usePos();
  const location = useLocation();

  if (loading) {
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

  return children;
}
