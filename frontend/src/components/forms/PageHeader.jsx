import React from 'react';
import { NavLink } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button } from '@/components/ui/button';

const PageHeader = ({ backPath, actionButtons }) => {
  if (!backPath && !actionButtons) return null;

  return (
    <div className="mb-2 flex items-center justify-end gap-2">
      {actionButtons}
      {backPath && (
        <Button variant="outline" size="sm" asChild>
          <NavLink to={backPath} className="flex items-center">
            <ArrowLeft className="mr-1.5 size-3.5" />
            Back
          </NavLink>
        </Button>
      )}
    </div>
  );
};

export default PageHeader;
