import React from 'react';
import ListPageLayout from './ListPageLayout';

/** @deprecated Use ListPageLayout directly */
export default function ListContainer({ children, extraElement, className }) {
  return (
    <div className={className}>
      {extraElement}
      {children}
    </div>
  );
}

export { ListPageLayout };
