import { UserError } from './errors.js';

export const DIALOG_SELECTOR = 'dialog[open], [role="dialog"], [role="alertdialog"]';

// HTML of the outermost visible dialogs, in document order.
export function openDialogsHtml(page) {
  // Visible first, then outermost: a hidden wrapper must not hide the visible dialog inside it.
  return page.$$eval(DIALOG_SELECTOR, (els) =>
    els
      .filter((e) => e.checkVisibility?.() ?? true)
      .filter((e, _, shown) => !shown.some((o) => o !== e && o.contains(e)))
      .map((e) => e.outerHTML),
  );
}

// The visible dialog with the largest area: the modal, not a banner in a corner.
export async function largestDialog(page) {
  const all = page.locator(DIALOG_SELECTOR);
  const areas = await all.evaluateAll((els) =>
    els.map((e) => {
      if (e.checkVisibility?.() === false) return 0;
      const r = e.getBoundingClientRect();
      return r.width * r.height;
    }),
  );
  const best = areas.indexOf(Math.max(0, ...areas));
  if (best < 0 || !areas[best]) throw new UserError('no dialog open (looked for <dialog> and role=dialog/alertdialog)');
  return all.nth(best);
}
