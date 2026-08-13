import React from 'react';
import { cn } from '@/lib/utils';
import ListTable from './ListTable';
import ListPagination from './ListPagination';

export default function ListPageLayout({
  title: _title,
  description,
  filters,
  filterRow,
  controlButtons,
  primaryButtons,
  extraContent,
  afterTable,
  table,
  emptyState,
  dataLength,
  isLoading,
  className,
  tableProps = {},
}) {
  return (
    <div className={cn('flex min-h-0 flex-1 flex-col overflow-hidden', className)}>
      <div className="shrink-0 pb-2">
        <div className="flex flex-col gap-2">
          <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
            {description && (
              <p className="shrink-0 text-xs text-muted-foreground">{description}</p>
            )}

            <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center lg:justify-end">
              {filters && (
                <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-end [&>*]:w-full sm:[&>*]:w-auto">
                  {filters}
                </div>
              )}

              {(controlButtons || primaryButtons) && (
                <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                  {controlButtons}
                  {primaryButtons}
                </div>
              )}
            </div>
          </div>

          {filterRow && <div className="border-t border-border pt-2">{filterRow}</div>}
        </div>
      </div>

      {extraContent && <div className="shrink-0 pb-2">{extraContent}</div>}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <ListTable
          table={table}
          emptyState={emptyState}
          isLoading={isLoading}
          className="min-h-0 flex-1"
          {...tableProps}
        />
      </div>

      {afterTable && <div className="shrink-0">{afterTable}</div>}

      <ListPagination table={table} dataLength={dataLength} />
    </div>
  );
}
