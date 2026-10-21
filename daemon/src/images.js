import { UserError } from './errors.js';

// Runs in the page, on the element a ref points at (an <img>, a wrapper around one, or a
// CSS background). Returns where its pixels come from.
export function imageSources(el) {
  const out = { src: '', srcsets: [], bg: '' };
  const img = el instanceof HTMLImageElement ? el : el.querySelector('img');
  if (img) {
    out.src = img.currentSrc || img.src;
    if (img.srcset) out.srcsets.push(img.srcset);
    if (img.parentElement instanceof HTMLPictureElement) {
      for (const s of img.parentElement.querySelectorAll('source')) {
        if (s.srcset && (!s.media || matchMedia(s.media).matches)) out.srcsets.push(s.srcset);
      }
    }
  }
  const m = /url\(["']?(.*?)["']?\)/.exec(getComputedStyle(el).backgroundImage);
  if (m) out.bg = m[1];
  return out;
}

// Candidates of one srcset attribute. A URL is a run of non-space characters (it may
// contain commas); a trailing comma ends a candidate with no descriptor.
export function parseSrcset(s) {
  const out = [];
  let i = 0;
  while (i < s.length) {
    while (i < s.length && /[\s,]/.test(s[i])) i++;
    let j = i;
    while (j < s.length && !/\s/.test(s[j])) j++;
    let url = s.slice(i, j);
    i = j;
    let desc = '';
    if (url.endsWith(',')) {
      url = url.replace(/,+$/, '');
    } else {
      let k = s.indexOf(',', i);
      if (k < 0) k = s.length;
      desc = s.slice(i, k).trim();
      i = k + 1;
    }
    if (url) out.push({ url, w: Number(/^([\d.]+)w$/.exec(desc)?.[1] ?? 0), x: Number(/^([\d.]+)x$/.exec(desc)?.[1] ?? 0) });
  }
  return out;
}

// The highest-resolution source: widest `w` descriptor, else highest `x` density
// (an `x` ranks by x * 1000 so the two kinds stay comparable), else the plain src.
export function bestImageUrl({ src, srcsets, bg }, base) {
  const all = srcsets.flatMap(parseSrcset);
  const score = (c) => c.w || c.x * 1000;
  let best = null;
  for (const c of all) if (!best || score(c) > score(best)) best = c;
  const url = best && score(best) > 0 ? best.url : src || best?.url || bg;
  if (!url) return '';
  try {
    return new URL(url, base).href;
  } catch {
    return '';
  }
}

// {body, type} from a data: URL, which needs no request.
export function decodeDataUrl(url) {
  const m = /^data:([^,;]*)((?:;[^,;]*)*),(.*)$/s.exec(url);
  if (!m) throw new UserError('malformed data: URL');
  const base64 = m[2].split(';').includes('base64');
  let body;
  if (base64) {
    if (!m[3] || !/^[A-Za-z0-9+/_-]*={0,2}$/.test(m[3].replace(/\s/g, ''))) throw new UserError('malformed base64 in data: URL');
    body = Buffer.from(m[3], 'base64');
  } else {
    try {
      body = Buffer.from(decodeURIComponent(m[3]), 'utf8');
    } catch {
      throw new UserError('malformed data: URL');
    }
  }
  return { type: m[1] || 'text/plain', body };
}

// What to print for a data: URL instead of megabytes of it.
export const describeDataUrl = (url) => `data: URL (${/^data:([^,;]*)/.exec(url)?.[1] || 'text/plain'}, ${url.length} chars)`;
