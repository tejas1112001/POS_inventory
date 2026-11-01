import Pagination from '@/components/kit/Pagination';

// ListPagination: the shared Pagination for the older lists (client-side
// pages from TanStack Table).
export default function ListPagination({ table, dataLength }) {
  const { pageIndex, pageSize } = table.getState().pagination;
  return (
    <Pagination
      total={dataLength}
      page={pageIndex + 1}
      size={pageSize}
      pageSizes={[10, 25, 50, 100]}
      onPage={(p) => table.setPageIndex(p - 1)}
      onSize={(s) => {
        table.setPageSize(s);
        table.setPageIndex(0);
      }}
      className="rounded-b-lg"
    />
  );
}
