// Helpers for multi-step forms whose steps all stay on the page (hidden
// when not current), so a submit collects every field.

// checkWizard runs the browser's field checks. On the first invalid field
// it opens that field's step (data-step on the step container), shows the
// browser's message and returns false.
export function checkWizard(form, goToStep) {
  if (form.checkValidity()) return true;
  const bad = form.querySelector(':invalid:not(fieldset):not(form)');
  const step = bad?.closest('[data-step]')?.getAttribute('data-step');
  if (step !== null && step !== undefined) goToStep(Number(step));
  setTimeout(() => {
    bad?.focus();
    bad?.reportValidity?.();
  }, 50);
  return false;
}

// formValues reads a form into an object, without empty file inputs (the
// server expects a picture as a data URL string, never a File).
export function formValues(form) {
  const data = {};
  for (const [k, v] of new FormData(form).entries()) {
    if (typeof v === 'string') data[k] = v;
  }
  return data;
}

// stepClass is the class of a step container: hidden unless current.
export const stepClass = (current, step) => (current === step ? undefined : 'hidden');
