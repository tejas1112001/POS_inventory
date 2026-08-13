import React from 'react';

/** @deprecated Use ListPageLayout filters prop instead */
export default function ListFilter({ children }) {
  return (
    <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => e.preventDefault()}>
      {children}
    </form>
  );
}