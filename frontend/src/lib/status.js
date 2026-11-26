// statusLabel: a status in sentence case for display ("In Stock" from the
// server reads "In stock"), like every other label in the app.
export const statusLabel = (value) => (typeof value === 'string' && value ? value.charAt(0) + value.slice(1).toLowerCase() : value);
