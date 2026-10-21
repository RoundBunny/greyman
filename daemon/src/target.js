import { UserError, firstLine } from './errors.js';
import { INTERACTIVE } from './snapshot.js';

const REF_RE = /^[a-z0-9]+$/i;
// Roles getByRole can re-check by name: the ones snapshots give refs to.
const CHECKABLE = new Set([...INTERACTIVE, 'img']);
const SHOWN_MATCHES = 8;

export function locate(page, ref) {
  if (!REF_RE.test(ref ?? '')) throw new UserError('ref required (e.g. e12); run snap');
  return page.getByRef(ref);
}

const short = (s) => (s.length > 60 ? `${s.slice(0, 60)}...` : s);
const moved = (tab) => (tab.snapUrl !== tab.page.url() ? ' (the page has changed since the last snapshot)' : '');

// A ref is bound to a DOM element, not to what it was when snapped. A single-page app
// can reuse that element for something else, so a click would land on the wrong thing
// without any error. Check that the element still has the role and name it had.
export async function resolveRef(tab, ref) {
  const loc = locate(tab.page, ref);
  const info = tab.refs?.get(ref);
  if (!info) {
    const why = !tab.refs
      ? tab.snapUrl
        ? 'the page was loaded again since the last snapshot'
        : 'no snapshot of this tab yet'
      : `ref ${ref} is not in the last snapshot${moved(tab)}`;
    throw new UserError(`${why}; run snap`);
  }
  if (info.frame || !info.name || !CHECKABLE.has(info.role)) return loc;
  const same = loc.and(tab.page.getByRole(info.role, { name: info.name }));
  let found;
  try {
    found = await same.count();
  } catch (err) {
    // A ref from a document that has since been replaced names a frame that no longer exists.
    if (/Invalid frame|aria-ref/.test(err?.message ?? '')) {
      throw new UserError(`ref ${ref} is from an earlier page${moved(tab)}; run snap`);
    }
    throw err;
  }
  if (found === 0) {
    throw new UserError(`ref ${ref} was ${info.role} "${short(info.name)}" in the last snapshot but is gone or changed${moved(tab)}; run snap`);
  }
  return loc;
}

// Finds an element by role and accessible name without a snapshot. Zero or several
// matches are errors that say what was found, never a silent first pick.
export async function resolveName(page, { name, role, exact, nth }) {
  const roles = role ? [role] : [...INTERACTIVE];
  const all = roles.map((r) => page.getByRole(r, { name, exact: Boolean(exact) })).reduce((a, b) => a.or(b));
  let n;
  try {
    n = await all.count();
  } catch (err) {
    throw new UserError(`bad --role "${role}": ${firstLine(err?.message)}`);
  }
  const what = `${role ?? 'interactive element'} named "${name}"`;
  if (n === 0) throw new UserError(`no ${what}; run snap`);
  if (nth) {
    if (!Number.isInteger(nth) || nth < 1 || nth > n) throw new UserError(`--nth must be 1 to ${n}`);
    return all.nth(nth - 1);
  }
  if (n > 1) {
    const rows = await all.evaluateAll(
      (els, limit) =>
        els.slice(0, limit).map((el) => ({
          role: el.getAttribute('role') || el.tagName.toLowerCase(),
          text: (el.getAttribute('aria-label') || el.innerText || el.value || '').trim().replace(/\s+/g, ' ').slice(0, 60),
        })),
      SHOWN_MATCHES,
    );
    const list = rows.map((r, i) => `${i + 1}. ${r.role} "${r.text}"`).join('; ');
    throw new UserError(`${n} elements match ${what}: ${list}${n > SHOWN_MATCHES ? '; ...' : ''}. Add --role, --exact or --nth N`);
  }
  return all;
}
