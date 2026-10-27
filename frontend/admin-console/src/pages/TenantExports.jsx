import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useStepUp } from '../stepup';
import { Button, Check, Modal, Notice, StatusBadge, Table } from '../components/ui';
import { bytes, formatDateTime } from '../lib/format';

// TenantExports: encrypted per-tenant exports and restore tests
// (OPERATIONS §6.2–6.3, tenants:export). Requesting an export needs
// step-up; the worker runs it. Restore tests are queued here and run by
// "posctl tenant restore-tests", the only process with the private key.
// Archives are never downloadable from the console.
export default function TenantExports({ tenant }) {
  const withStepUp = useStepUp();
  const base = `/tenants/${encodeURIComponent(tenant.id)}`;
  const [exports, setExports] = useState(null);
  const [restores, setRestores] = useState(null);
  const [notice, setNotice] = useState(null);
  const [asking, setAsking] = useState(false);
  const [withFiles, setWithFiles] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api.get(`${base}/exports`).then((r) => setExports(r?.data || [])).catch((err) => setNotice({ kind: 'error', text: err.message }));
    api.get(`${base}/restore-tests`).then((r) => setRestores(r?.data || [])).catch(() => setRestores([]));
  }, [base]);
  useEffect(load, [load]);

  // Refresh while something is queued or running.
  useEffect(() => {
    const active = [...(exports || []), ...(restores || [])].some((x) => x.status === 'queued' || x.status === 'running');
    if (!active) return undefined;
    const t = setTimeout(load, 5000);
    return () => clearTimeout(t);
  }, [exports, restores, load]);

  const requestExport = async () => {
    setBusy(true);
    try {
      await withStepUp(() => api.post(`${base}/exports`, { includeFiles: withFiles }));
      setAsking(false);
      setNotice({ kind: 'ok', text: 'Export queued. It runs in the background; this list refreshes.' });
      load();
    } catch (err) {
      if (err.code === 'CANCELLED') return;
      setAsking(false);
      setNotice({ kind: 'error', text: err.code === 'CONFLICT' ? 'An export of this tenant is already queued or running.' : err.message });
    } finally {
      setBusy(false);
    }
  };

  const restoreTest = async (exportId) => {
    try {
      await api.post(`${base}/restore-tests`, exportId ? { exportId } : {});
      setNotice({ kind: 'ok', text: 'Restore test queued. It runs with "posctl tenant restore-tests" (see below).' });
      load();
    } catch (err) {
      setNotice({ kind: 'error', text: err.code === 'CONFLICT' ? 'A restore of this tenant is already queued or running.' : err.message });
    }
  };

  return (
    <div className="stack">
      <Notice kind={notice?.kind}>{notice?.text}</Notice>
      <div className="row-between">
        <h2>Exports</h2>
        <Button variant="primary" onClick={() => setAsking(true)}>
          Request export
        </Button>
      </div>
      <Table
        loading={!exports}
        rows={exports || []}
        empty="No exports yet."
        columns={[
          { key: 'createdAt', label: 'Requested', render: (r) => formatDateTime(r.createdAt), className: 'nowrap' },
          { key: 'kind', label: 'Kind' },
          { key: 'status', label: 'Status', render: (r) => <StatusBadge status={r.status} /> },
          { key: 'requestedBy', label: 'By' },
          { key: 'size', label: 'Size', render: (r) => (r.sizeBytes != null ? bytes(r.sizeBytes) : '') },
          { key: 'rows', label: 'Tables / rows / files', render: (r) => (r.tableCount != null ? `${r.tableCount} / ${r.rowCount} / ${r.fileCount ?? 0}` : '') },
          { key: 'sha256', label: 'SHA-256', render: (r) => <span className="mono small">{r.sha256 ? `${r.sha256.slice(0, 16)}…` : ''}</span> },
          { key: 'error', label: 'Note', render: (r) => r.error || '' },
          {
            key: 'test',
            label: '',
            render: (r) =>
              r.status === 'succeeded' ? (
                <Button onClick={() => restoreTest(r.id)} aria-label={`Test restore of the export from ${formatDateTime(r.createdAt)}`}>
                  Test restore
                </Button>
              ) : null,
          },
        ]}
      />

      <h2>Restore tests</h2>
      <p className="muted small">
        A restore test loads an export into a scratch schema, checks every table&apos;s rows and checksum against the export, and drops it.
        Queued tests run on the server with <span className="mono">posctl tenant restore-tests</span>.
      </p>
      <Table
        loading={!restores}
        rows={restores || []}
        empty="No restore tests yet."
        columns={[
          { key: 'createdAt', label: 'Requested', render: (r) => formatDateTime(r.createdAt), className: 'nowrap' },
          { key: 'mode', label: 'Mode' },
          { key: 'status', label: 'Status', render: (r) => <StatusBadge status={r.status} /> },
          { key: 'verified', label: 'Verified', render: (r) => (r.verified == null ? '' : r.verified ? 'Rows and checksums match' : 'MISMATCH') },
          { key: 'rows', label: 'Rows', render: (r) => r.rows ?? '' },
          { key: 'requestedBy', label: 'By' },
          { key: 'finishedAt', label: 'Finished', render: (r) => formatDateTime(r.finishedAt, '') },
          { key: 'error', label: 'Error', render: (r) => r.error || '' },
        ]}
      />

      {asking && (
        <Modal title="Request export" onClose={() => setAsking(false)}>
          <p>An encrypted copy of this tenant&apos;s data is written to the export store. Login tokens are never exported.</p>
          <Check label="Include files (logos, product images, receipts)" checked={withFiles} onChange={setWithFiles} />
          <div className="row right">
            <Button onClick={() => setAsking(false)}>Cancel</Button>
            <Button variant="primary" busy={busy} onClick={requestExport}>
              Request export
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
