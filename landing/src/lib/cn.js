/** Join class names, skipping falsy values. */
export const cn = (...parts) => parts.filter(Boolean).join(' ');
