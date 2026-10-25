import { useState } from 'react';
import { FileSpreadsheet, FileText, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';

// ReportExportButtons downloads the report as Excel or PDF from the server
// (GET <path>?format=xlsx|pdf with the same filters). The server applies
// the user's scope and the plan's report-export feature, and never cuts
// the file short.
export default function ReportExportButtons({ path, qs, name }) {
  const { hasPermission } = usePermission();
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  if (!hasPermission('reports', 'export')) return null;

  const get = async (format) => {
    setBusy(format);
    setError('');
    const sep = qs ? '&' : '?';
    try {
      await api.download(`${path}${qs}${sep}format=${format}`, `${name}.${format}`, { quiet: true });
    } catch (err) {
      setError(err.code === 'FEATURE_NOT_IN_PLAN' ? 'Report export is not in your plan.' : err.message);
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="flex flex-col items-start gap-1 md:col-span-2">
      <span className="text-sm font-medium text-gray-700">Download</span>
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={() => get('xlsx')} disabled={!!busy}>
          {busy === 'xlsx' ? <Loader2 className="size-4 animate-spin" /> : <FileSpreadsheet className="size-4" />} Excel
        </Button>
        <Button type="button" variant="outline" onClick={() => get('pdf')} disabled={!!busy}>
          {busy === 'pdf' ? <Loader2 className="size-4 animate-spin" /> : <FileText className="size-4" />} PDF
        </Button>
      </div>
      {error && <span role="alert" className="text-xs text-destructive">{error}</span>}
    </div>
  );
}
