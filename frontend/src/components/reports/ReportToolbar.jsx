import { useState } from 'react';
import { FileSpreadsheet, FileText, Loader2, Printer } from 'lucide-react';
import FilterBar from '@/components/kit/FilterBar';
import { Button } from '@/components/ui/button';
import { DropdownRangeDatePicker } from '@/components/ui/dropdown-range-date-picker';
import { usePermission } from '@/context/PermissionContext';
import { exportToExcel } from '@/components/ListComponents/exportFunctions';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

// ReportActions: the one set of report buttons on every Reports tab:
// Excel, PDF and Print, in that order.
//   server: { path, qs, name, pdf = true, csv } downloads from the server (the user's
//   scope and the plan's export feature apply there, the file is complete).
//   rows / name without server: Excel of the rows shown, made here.
//   gated: false lets a list that is not a report (invoices) offer its
//   Excel without the reports:export permission.
export function ReportActions({ server, rows, name, print = true, disabled = false, gated = true }) {
  const { hasPermission } = usePermission();
  const [busy, setBusy] = useState('');
  const canExport = !gated || hasPermission('reports', 'export');

  const download = async (format) => {
    setBusy(format);
    const sep = server.qs ? '&' : '?';
    try {
      await api.download(`${server.path}${server.qs}${sep}format=${format}`, `${server.name}.${format}`, { quiet: true });
    } catch (err) {
      toast.error(err.code === 'FEATURE_NOT_IN_PLAN' ? 'Report export is not in your plan.' : err.message || 'The file could not be made.');
    } finally {
      setBusy('');
    }
  };
  const local = async () => {
    setBusy('xlsx');
    try {
      await exportToExcel(rows, name);
    } finally {
      setBusy('');
    }
  };
  const icon = (format, Icon) => (busy === format ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Icon className="size-4" aria-hidden="true" />);

  return (
    <>
      {canExport && (
        <Button type="button" variant="outline" disabled={!!busy || disabled || (!server && !rows?.length)} onClick={() => (server ? download('xlsx') : local())}>
          {icon('xlsx', FileSpreadsheet)} Excel
        </Button>
      )}
      {canExport && server && server.pdf !== false && (
        <Button type="button" variant="outline" disabled={!!busy || disabled} onClick={() => download('pdf')}>
          {icon('pdf', FileText)} PDF
        </Button>
      )}
      {canExport && server?.csv && (
        <Button type="button" variant="outline" disabled={!!busy || disabled} onClick={() => download('csv')}>
          {icon('csv', FileText)} CSV
        </Button>
      )}
      {print && (
        <Button type="button" variant="outline" onClick={() => window.print()}>
          <Printer className="size-4" aria-hidden="true" /> Print
        </Button>
      )}
    </>
  );
}

// ReportToolbar: the filter line of a report, the same as the other list
// pages: search, "Add filter" (labelled fields, applied ones as chips), the
// date range, and the report buttons on the right.
//   filters: [{ key, label, options: [{ value, label }] }], values: { key: value } ('' = all)
//   dates: { from, to, onChange({ from, to }), clearable, placeholder } or null
export default function ReportToolbar({ search, onSearch, placeholder, filters = [], values = {}, onFilter, onClear, dates, actions }) {
  const datesActive = dates && dates.clearable !== false ? (dates.from ? 1 : 0) : 0;
  return (
    <FilterBar
      search={search}
      onSearch={onSearch}
      placeholder={placeholder}
      filters={filters}
      values={values}
      onFilter={onFilter}
      onClear={onClear}
      extra={
        dates && (
          <DropdownRangeDatePicker
            placeholder={dates.placeholder || 'All dates'}
            clearable={dates.clearable !== false}
            className="h-10 text-[13px] sm:h-9"
            value={{ from: dates.from, to: dates.to }}
            onChange={dates.onChange}
          />
        )
      }
      extraActive={datesActive}
      actions={actions}
    />
  );
}
