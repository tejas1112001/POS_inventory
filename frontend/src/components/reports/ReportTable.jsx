import ListTable from '@/components/ListComponents/ListTable';
import ListPagination from '@/components/ListComponents/ListPagination';

// ReportTable: a report's rows (TanStack table, pages made here) with the
// pager under it. It never gets shorter than about ten rows; on a short
// screen the page scrolls instead (ListPageShell scroll).
export default function ReportTable({ table, loading, emptyState, dataLength, footer }) {
  return (
    <div className="flex min-h-[26rem] flex-1 flex-col">
      <ListTable table={table} emptyState={emptyState} isLoading={loading} className="min-h-0 flex-1" />
      {footer}
      <ListPagination table={table} dataLength={dataLength} />
    </div>
  );
}
