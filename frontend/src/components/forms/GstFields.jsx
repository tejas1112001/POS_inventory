import { SelectField, TextField } from '@/components/kit/Form';
import { GSTIN_RULE, useGstStates } from '@/lib/gst';

// StateField is a GST state select (M3). It is drawn once the list has
// loaded, so the form's saved value is applied to a select that has its
// options (a select registered before them would lose the value).
export function StateField({ name = 'stateCode', label = 'State', hint }) {
  const states = useGstStates();
  if (!states) {
    return <p className="text-sm text-muted-foreground">Loading states…</p>;
  }
  const options = [{ value: '', label: 'Not set' }, ...states.map((s) => ({ value: s.code, label: `${s.name} (${s.code})` }))];
  return <SelectField name={name} label={label} hint={hint} options={options} />;
}

// GstinField is a GSTIN input; the server checks the check character and
// that it belongs to the state (a GSTIN with no state sets it).
export function GstinField({ name = 'gstin', label = 'GSTIN', hint = 'Optional. 15 characters, such as 27AAPFU0939F1ZV. The state is taken from it.' }) {
  return <TextField name={name} label={label} hint={hint} maxLength={15} autoComplete="off" spellCheck={false} style={{ textTransform: 'uppercase' }} rules={GSTIN_RULE} />;
}
