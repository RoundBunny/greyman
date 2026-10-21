// Cuts at the last line break before `max` so a node is never half-printed, unless a
// single line is longer than half the budget. `hint` says how to get the rest.
export function truncate(text, max, hint = '') {
  if (text.length <= max) return text;
  let cut = text.lastIndexOf('\n', max);
  if (cut < max / 2) cut = max;
  return `${text.slice(0, cut)}\n... [truncated ${text.length - cut} chars${hint ? `; ${hint}` : ''}]`;
}
