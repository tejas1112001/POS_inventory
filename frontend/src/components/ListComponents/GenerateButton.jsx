import React from 'react';
import { Button } from '@/components/ui/button';

export default function GenerateButton({ onClick, label = 'Generate' }) {
  return (
    <div className="flex items-end md:col-span-1">
      <Button type="button" onClick={onClick} className="w-full">
        {label}
      </Button>
    </div>
  );
}
