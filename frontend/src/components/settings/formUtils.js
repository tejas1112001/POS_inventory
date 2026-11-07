// Helpers shared by the settings sections.

export const fieldCls =
  'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60';

// errorNotice turns an ApiError into a notice and field messages.
export function errorNotice(err) {
  if (err?.code === 'FEATURE_NOT_IN_PLAN') {
    return { kind: 'error', text: 'Your plan does not include this. Upgrade the plan on the Billing page.' };
  }
  if (err?.code === 'TENANT_READ_ONLY') {
    return { kind: 'error', text: 'The shop is read-only until the subscription is paid. See the Billing page.' };
  }
  return { kind: 'error', text: err?.fields ? 'Please correct the marked fields.' : err?.message || 'Saving failed.' };
}
