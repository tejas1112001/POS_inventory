import { useState } from 'react';
import { useStepUp } from '../stepup';
import { Button, Check, Modal, Notice, Select, TextArea, TextInput } from './ui';

// ActionDialog collects a few values and runs one console action. The
// action goes through step-up when the server asks for it, and server
// field errors appear under their fields.
//
// fields: [{key, label, type: text|textarea|number|select|check, hint,
//           options, required, minLength, initial}]
// confirmWord: the user must type this word (for example the subdomain).
export default function ActionDialog({ title, intro, fields = [], confirmWord, submitLabel = 'Confirm', danger, onSubmit, onClose, onDone }) {
  const withStepUp = useStepUp();
  const [values, setValues] = useState(() => Object.fromEntries(fields.map((f) => [f.key, f.initial ?? (f.type === 'check' ? false : '')])));
  const [typed, setTyped] = useState('');
  const [errors, setErrors] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setValues((s) => ({ ...s, [k]: v }));

  const submit = async (e) => {
    e.preventDefault();
    const local = {};
    for (const f of fields) {
      const v = values[f.key];
      if (f.required && (v === '' || v === null)) local[f.key] = 'Required.';
      else if (f.minLength && String(v).trim().length < f.minLength) local[f.key] = `At least ${f.minLength} characters.`;
    }
    setErrors(local);
    if (Object.keys(local).length) return;
    setBusy(true);
    setError('');
    try {
      const result = await withStepUp(() => onSubmit(values));
      onDone?.(result);
    } catch (err) {
      if (err.code === 'CANCELLED') return;
      setErrors(err.fields || {});
      setError(err.fields ? 'Please correct the marked fields.' : err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={submit} className="stack" noValidate>
        {intro && <p>{intro}</p>}
        {fields.map((f) => {
          const common = { label: f.label, hint: f.hint, error: errors[f.key], value: values[f.key], onChange: set(f.key) };
          if (f.type === 'textarea') return <TextArea key={f.key} {...common} maxLength={f.maxLength} />;
          if (f.type === 'select') return <Select key={f.key} {...common} options={f.options} />;
          if (f.type === 'check') return <Check key={f.key} label={f.label} checked={values[f.key]} onChange={set(f.key)} />;
          return <TextInput key={f.key} {...common} type={f.type === 'number' ? 'number' : 'text'} maxLength={f.maxLength} min={f.min} max={f.max} />;
        })}
        {confirmWord && <TextInput label={`Type ${confirmWord} to confirm`} value={typed} onChange={setTyped} autoComplete="off" />}
        <Notice kind="error">{error}</Notice>
        <div className="row right">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant={danger ? 'danger' : 'primary'} busy={busy} disabled={confirmWord ? typed !== confirmWord : false}>
            {submitLabel}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
