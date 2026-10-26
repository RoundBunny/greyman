// Cuts at the last line break before `max` so a node is never half-printed, unless a
// single line is longer than half the budget.
export function truncate(text, max) {
  if (text.length <= max) return text;
  let cut = text.lastIndexOf('\n', max);
  if (cut < max / 2) cut = max;
  return text.slice(0, cut);
}
