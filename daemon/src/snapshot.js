import vm from 'node:vm';
import { UserError } from './errors.js';
import { truncate } from './text.js';

// Roles an agent can act on. Only these (and anything marked cursor=pointer) keep refs.
export const INTERACTIVE = new Set([
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
// Dialogs sit at the end of the DOM, where truncation cuts them first, so they print first.
const DIALOG = new Set(['dialog', 'alertdialog']);
export const DEFAULT_MAX_TEXT = 200;
// Option rows without refs (a closed <select>) shown before the rest collapse.
const OPTIONS_SHOWN = 5;
// Structurally identical siblings in a row before the repeats collapse.
const REPEAT_MIN = 3;
// What a ref looks like: e12, or f5e5 inside a frame.
const REF_RE = /^(f\d+)?e\d+$/;

// role "name" [attr]* [ref=eN] (: inline text)?   — trailing ":" without text means children follow
const LINE_RE = /^(\/?[\w-]+)(?: ("(?:[^"\\]|\\.)*"))?((?: \[[^\]]*\])*)(:)?(?: (.*))?$/;

// Playwright writes a whole line in YAML quotes when a name needs escaping (any ": " in
// it, as in "Sale: 50% off"): 'button "Sale: 50% off" [ref=e4]' with inner ' doubled, or
// in double quotes when it holds escapes. Unwrap it, or the node loses its role and ref.
function unquoteKey(body) {
  const single = /^'((?:[^']|'')*)'(.*)$/s.exec(body);
  if (single) return single[1].replace(/''/g, "'") + single[2];
  const double = /^("(?:[^"\\]|\\.)*")(.*)$/s.exec(body);
  if (double) {
    try {
      return JSON.parse(double[1]) + double[2];
    } catch {}
  }
  return body;
}

// A text value is quoted the same way when it holds ": " or a leading symbol.
function unquoteScalar(s) {
  if (s.length > 1 && s[0] === "'" && s.at(-1) === "'") return s.slice(1, -1).replace(/''/g, "'");
  if (s.length > 1 && s[0] === '"' && s.at(-1) === '"') {
    try {
      const v = JSON.parse(s);
      if (typeof v === 'string') return v;
    } catch {}
  }
  return s;
}

function parse(text) {
  const root = { children: [] };
  const stack = [{ depth: -1, node: root }];
  for (const raw of text.split('\n')) {
    const m = /^( *)- (.*)$/.exec(raw);
    if (!m) continue;
    const depth = m[1].length / 2;
    const body = unquoteKey(m[2]);
    const p = LINE_RE.exec(body);
    const attrs = p ? [...p[3].matchAll(/\[([^\]]*)\]/g)].map((a) => a[1]) : [];
    const node = p
      ? {
          role: p[1],
          name: p[2] ?? '',
          ref: attrs.find((a) => a.startsWith('ref='))?.slice(4) ?? '',
          attrs: attrs.filter((a) => !a.startsWith('ref=')),
          text: unquoteScalar(p[5] ?? ''),
          children: [],
        }
      : { role: 'raw', name: '', ref: '', attrs: [], text: body, children: [] };
    while (stack.at(-1).depth >= depth) stack.pop();
    stack.at(-1).node.children.push(node);
    stack.push({ depth, node });
  }
  return root;
}

// ref -> {role, name, frame} for every ref in the raw tree. `frame` marks refs inside an
// iframe, which live in another frame's ref table.
function collectRefs(node, refs = new Map(), frame = false) {
  for (const c of node.children) {
    if (c.ref) {
      let name = c.name;
      try {
        name = c.name ? JSON.parse(c.name) : '';
      } catch {}
      refs.set(c.ref, { role: c.role, name, frame });
    }
    collectRefs(c, refs, frame || c.role === 'iframe');
  }
  return refs;
}

// A rendered node: `line` is what prints, `plain` is the same without the ref (so two
// nodes that differ only in refs compare equal), `kids` the rendered children.
function leaf(line) {
  return { role: 'note', ref: '', attrs: [], line, plain: line, kids: [] };
}

function render(node, clip) {
  // Fold property children (/url, /placeholder) into this line.
  const extras = [];
  const kids = [];
  for (const c of node.children) {
    if (FOLDED[c.role] && c.text) extras.push(`${FOLDED[c.role]} ${clip(c.text)}`);
    else kids.push(c);
  }

  const clickable = node.attrs.includes('cursor=pointer');
  // Named images get refs so they can be saved or shot.
  const keepRef = INTERACTIVE.has(node.role) || clickable || (node.role === 'img' && node.name);
  const ref = keepRef ? node.ref : '';
  const attrs = node.attrs.filter((a) => !NOISE_ATTRS.has(a));

  // Separators like "|" or "(" are layout noise, not content.
  if (node.role === 'text' && !/[\p{L}\p{N}]/u.test(node.text)) return [];

  const bare = !node.name && !ref && !attrs.length && !extras.length;
  if (bare && FLATTEN.has(node.role)) {
    const out = node.text ? [leaf(clip(node.text))] : [];
    for (const c of kids) out.push(...render(c, clip));
    return out;
  }

  const body = kids.flatMap((c) => render(c, clip));
  if (bare && !node.text && !body.length && DROP_IF_BARE.has(node.role)) return [];

  const line = (withRef) => {
    const head = [node.role === 'raw' ? '' : node.role, node.name];
    if (attrs.length) head.push(attrs.map((a) => `[${a}]`).join(' '));
    if (withRef && ref) head.push(`[${ref}]`);
    let s = head.filter(Boolean).join(' ');
    if (extras.length) s += ` ${extras.join(' ')}`;
    if (node.text) s += `${s ? ': ' : ''}${clip(node.text)}`;
    return s;
  };
  return [{ role: node.role, ref, attrs, line: line(true), plain: line(false), kids: body }];
}

// Moves the outermost dialogs out of the tree into `found`. A wrapper that held nothing
// else is dropped with them.
function takeDialogs(list, found) {
  const out = [];
  for (const n of list) {
    if (DIALOG.has(n.role)) {
      found.push(n);
      continue;
    }
    if (n.kids.length) {
      n.kids = takeDialogs(n.kids, found);
      if (!n.kids.length && n.line === n.role) continue;
    }
    out.push(n);
  }
  return out;
}

// Gives every node a structural signature (its line without the ref, plus its children's)
// and the refs under it. Taken from the whole tree before anything collapses, so equal
// siblings compare equal whatever is inside them and no ref is lost from a note.
function annotate(list) {
  for (const n of list) {
    annotate(n.kids);
    n.sig = `${n.plain}{${n.kids.map((k) => k.sig).join(',')}}`;
    n.allRefs = [...(n.ref ? [n.ref] : []), ...n.kids.flatMap((k) => k.allRefs)];
  }
}

// Collapses what an agent doesn't need twice: option rows nobody can click (a closed
// <select>) and runs of identical siblings. Refs of collapsed siblings stay listed.
// Needs annotate() first, and runs after any filtering so a filter still sees every ref.
function dedupe(list) {
  const out = [];
  for (let i = 0; i < list.length; ) {
    const n = list[i];
    n.kids = dedupe(n.kids);

    if (n.role === 'option' && !n.ref) {
      let j = i;
      while (j < list.length && list[j].role === 'option' && !list[j].ref) j++;
      const run = list.slice(i, j);
      if (run.length > OPTIONS_SHOWN + 1) {
        const shown = run.filter((o, k) => k < OPTIONS_SHOWN || o.attrs.includes('selected'));
        out.push(...shown, leaf(`(+${run.length - shown.length} more options)`));
        i = j;
        continue;
      }
    }

    let j = i + 1;
    while (j < list.length && list[j].sig === n.sig) j++;
    if (j - i >= REPEAT_MIN) {
      const rest = list.slice(i + 1, j);
      const refs = rest.flatMap((r) => r.allRefs);
      out.push(n, leaf(`(+${rest.length} similar${refs.length ? `: ${refs.join(' ')}` : ''})`));
      i = j;
      continue;
    }
    out.push(n);
    i++;
  }
  return out;
}

// A pattern comes from the agent and runs on the daemon's only thread: one with nested
// quantifiers can pin it for hours and take every command down with it. Run the filter
// under a time limit.
const PATTERN_LIMIT_MS = 1000;
function guarded(fn) {
  try {
    return vm.runInNewContext('fn()', { fn }, { timeout: PATTERN_LIMIT_MS });
  } catch (err) {
    if (err?.code === 'ERR_SCRIPT_EXECUTION_TIMEOUT') {
      throw new UserError(`pattern took over ${PATTERN_LIMIT_MS} ms (catastrophic backtracking?); simplify --grep/--within`);
    }
    throw err;
  }
}

function regex(spec, what) {
  try {
    return new RegExp(spec, 'i');
  } catch (err) {
    throw new UserError(`invalid ${what} pattern: ${err.message}`);
  }
}

// Outermost nodes matching `pred`, subtrees intact.
function find(list, pred) {
  return list.flatMap((n) => (pred(n) ? [n] : find(n.kids, pred)));
}

// Keeps nodes that match plus their ancestors, so a match still shows where it lives.
function select(list, pred) {
  const out = [];
  for (const n of list) {
    const kids = select(n.kids, pred);
    if (pred(n) || kids.length) out.push({ ...n, kids });
  }
  return out;
}

function emit(list, depth, out) {
  for (const n of list) {
    out.push(`${'  '.repeat(depth)}- ${n.line}`);
    emit(n.kids, depth + 1, out);
  }
}

// Compacts an AI-mode aria snapshot. Pure so it can be tested against fixtures.
// opts: maxText (clip any one text), modal (dialogs only), within (a ref, or a pattern
// matched against lines: only those subtrees), role (comma list) and grep (pattern):
// matches plus their ancestors. Returns the text and every ref in the raw tree.
export function compactSnapshot(text, opts = {}) {
  const { maxText = DEFAULT_MAX_TEXT, modal = false, within, role, grep } = opts;
  const clip = (s) => (s.length > maxText ? `${s.slice(0, maxText)}...` : s);
  const root = parse(text);

  const dialogs = [];
  const rest = takeDialogs(root.children.flatMap((c) => render(c, clip)), dialogs);
  let list = modal ? dialogs : [...dialogs, ...rest];

  if (within) {
    // A ref that is gone is an error; as a pattern, "e12" would also match e120-e129.
    if (REF_RE.test(within)) {
      list = find(list, (n) => n.ref === within);
      if (!list.length) throw new UserError(`--within ${within}: no such ref in this snapshot${modal ? ' (or it is outside the dialogs)' : ''}; run snap`);
    } else {
      const re = regex(within, '--within');
      list = guarded(() => find(list, (n) => re.test(n.line)));
    }
  }
  if (role || grep) {
    const roles = role ? new Set(role.split(',').map((r) => r.trim())) : null;
    const re = grep ? regex(grep, '--grep') : null;
    list = guarded(() => select(list, (n) => (!roles || roles.has(n.role)) && (!re || re.test(n.line))));
  }
  annotate(list);
  list = dedupe(list);

  const out = [];
  emit(list, 0, out);
  if (!out.length && (modal || within || role || grep)) {
    out.push(modal && !within && !role && !grep ? '(no dialog open)' : '(no matches)');
  }
  return { text: out.join('\n'), refs: collectRefs(root) };
}

export const compact = (text, opts) => compactSnapshot(text, opts).text;

export async function snapshot(page, maxChars, opts = {}) {
  const tree = await page.ariaSnapshot({ mode: 'ai' });
  const title = await page.title();
  const { text, refs } = compactSnapshot(tree, opts);
  return { text: truncate(`# ${title} <${page.url()}>\n${text}`, maxChars), refs };
}
