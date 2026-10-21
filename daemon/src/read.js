import { Readability } from '@mozilla/readability';
import { parseHTML } from 'linkedom';
import TurndownService from 'turndown';
import { truncate } from './text.js';

const turndown = new TurndownService({ headingStyle: 'atx', codeBlockStyle: 'fenced', bulletListMarker: '-' });
turndown.remove(['script', 'style', 'noscript']);

const READ_HINT = 'raise --max';

// Main content of the current page as markdown. Falls back to the whole body when
// Readability finds no article (app UIs, listings).
export async function readPage(page, maxChars) {
  const url = page.url();
  const { document } = parseHTML(await page.content());
  // Readability resolves relative links against these; linkedom doesn't set them.
  for (const prop of ['baseURI', 'documentURI']) {
    Object.defineProperty(document, prop, { value: url, configurable: true });
  }

  const article = new Readability(document).parse();
  let title = article?.title;
  let html = article?.content;
  if (!html) {
    const { document: raw } = parseHTML(await page.content());
    title ||= raw.title;
    html = raw.body?.innerHTML ?? '';
  }
  const md = turndown.turndown(html).replace(/\n{3,}/g, '\n\n').trim();
  return truncate(`# ${title || url}\n\n${md}`, maxChars, READ_HINT);
}

// Markdown for given elements' HTML (no Readability: the caller already chose the part).
export function readHtml(parts, url, maxChars) {
  const md = parts
    .map((html) => turndown.turndown(html))
    .join('\n\n---\n\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
  return truncate(`# ${url}\n\n${md}`, maxChars, READ_HINT);
}
