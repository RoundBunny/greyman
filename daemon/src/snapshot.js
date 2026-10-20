import { truncate } from './text.js';

// Roles an agent can act on. Only these (and anything marked cursor=pointer) keep refs.
const INTERACTIVE = new Set([
  'link', 'button', 'textbox', 'searchbox', 'combobox', 'checkbox', 'radio', 'switch',
  'slider', 'spinbutton', 'option', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'tab', 'treeitem',
]);
// Nameless wrappers that add nothing: their children are hoisted into the parent.
const FLATTEN = new Set(['generic', 'rowgroup', 'cell', 'gridcell']);
// Dropped when they end up with no name, text, ref or surviving children.
const DROP_IF_BARE = new Set(['row', 'table', 'list', 'listitem', 'paragraph', 'img', 'separator']);
// Attributes that carry no information once the ref decision is made.
const NOISE_ATTRS = new Set(['active', 'cursor=pointer']);
// Child property lines folded into the parent line.
const FOLDED = { '/url': '->', '/placeholder': 'placeholder' };
export const DEFAULT_MAX_TEXT = 200;

// role "name" [attr]* [ref=eN] (: inline text)?   — trailing ":" without text means children follow
const LINE_RE = /^(\/?[\w-]+)(?: ("(?:[^"\\]|\\.)*"))?((?: \[[^\]]*\])*)(:)?(?: (.*))?$/;

function parse(text) {
  const root = { children: [] };
  const stack = [{ depth: -1, node: root }];
  for (const raw of text.split('\n')) {
    const m = /^( *)- (.*)$/.exec(raw);
    if (!m) continue;
    const depth = m[1].length / 2;
    const body = m[2];
    const p = LINE_RE.exec(body);
    const attrs = p ? [...p[3].matchAll(/\[([^\]]*)\]/g)].map((a) => a[1]) : [];
    const node = p
      ? {
          role: p[1],
          name: p[2] ?? '',
          ref: attrs.find((a) => a.startsWith('ref='))?.slice(4) ?? '',
          attrs: attrs.filter((a) => !a.startsWith('ref=')),
          text: p[5] ?? '',
          children: [],
        }
      : { role: 'raw', name: '', ref: '', attrs: [], text: body, children: [] };
    while (stack.at(-1).depth >= depth) stack.pop();
    stack.at(-1).node.children.push(node);
    stack.push({ depth, node });
  }
  return root;
}

function render(node, depth, out, clip) {
  // Fold property children (/url, /placeholder) into this line.
  const extras = [];
  const kids = [];
  for (const c of node.children) {
    if (FOLDED[c.role] && c.text) extras.push(`${FOLDED[c.role]} ${clip(c.text)}`);
    else kids.push(c);
  }

  const clickable = node.attrs.includes('cursor=pointer');
  const ref = INTERACTIVE.has(node.role) || clickable ? node.ref : '';
  const attrs = node.attrs.filter((a) => !NOISE_ATTRS.has(a));

  // Separators like "|" or "(" are layout noise, not content.
  if (node.role === 'text' && !/[\p{L}\p{N}]/u.test(node.text)) return;

  const bare = !node.name && !ref && !attrs.length && !extras.length;
  if (bare && FLATTEN.has(node.role)) {
    if (node.text) out.push(`${'  '.repeat(depth)}- ${clip(node.text)}`);
    for (const c of kids) render(c, depth, out, clip);
    return;
  }

  const body = [];
  for (const c of kids) render(c, depth + 1, body, clip);
  if (bare && !node.text && !body.length && DROP_IF_BARE.has(node.role)) return;

  const head = [node.role === 'raw' ? '' : node.role, node.name];
  if (attrs.length) head.push(attrs.map((a) => `[${a}]`).join(' '));
  if (ref) head.push(`[${ref}]`);
  let line = head.filter(Boolean).join(' ');
  if (extras.length) line += ` ${extras.join(' ')}`;
  if (node.text) line += `${line ? ': ' : ''}${clip(node.text)}`;
  out.push(`${'  '.repeat(depth)}- ${line}`, ...body);
}

// Compact an AI-mode aria snapshot, clipping any one text to maxText characters.
// Pure so it can be tested against fixtures.
export function compact(text, maxText = DEFAULT_MAX_TEXT) {
  const clip = (s) => (s.length > maxText ? `${s.slice(0, maxText)}...` : s);
  const out = [];
  for (const c of parse(text).children) render(c, 0, out, clip);
  return out.join('\n');
}

export async function snapshot(page, maxChars, maxText) {
  const tree = await page.ariaSnapshot({ mode: 'ai' });
  const title = await page.title();
  return truncate(`# ${title} <${page.url()}>\n${compact(tree, maxText)}`, maxChars);
}
