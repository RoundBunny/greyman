// Anti-detection hooks. Philosophy: be a consistent, plausible Linux Chromium user.
// Fix the environment for real where possible (fonts, screen, tz/locale in the image/
// launch config) and spoof only what cannot be fixed. Never spoof UA or headers:
// the browser sends its own, so they stay consistent with the TLS/HTTP2 fingerprint.

export const IGNORE_DEFAULT_ARGS = ['--enable-automation'];

export function launchArgs({ locale } = {}) {
  const args = [
    '--disable-blink-features=AutomationControlled',
    // A permission prompt is browser UI: snap, read and shot cannot show it and nothing can
    // answer it, so the page would wait forever. Deny every prompt at once instead. The
    // permission state still reads "prompt" until a page asks and "denied" after, as after
    // a user's Block. Geolocation alone can be allowed, through CDP (see geolocation.js).
    '--deny-permission-prompts',
    '--window-position=0,0',
    `--window-size=${process.env.GREYMAN_SCREEN_W ?? 1920},${process.env.GREYMAN_SCREEN_H ?? 1080}`,
  ];
  if (locale) args.push(`--lang=${locale}`);
  return args;
}

// Called once per BrowserContext, before any page script runs.
export async function applyToContext(_context) {
  // TODO(1): CDP Runtime.enable leak. Playwright triggers it on every page; detectors
  //   use it to spot automation. Needs a patched driver (patchright or similar).
  // TODO(2): WebGL UNMASKED_VENDOR/RENDERER init script via _context.addInitScript.
  //   Must cover iframes and workers, keep toString() native-looking, and stay consistent
  //   with MAX_TEXTURE_SIZE / extensions. Real renderer is SwiftShader (no GPU in Docker
  //   for Mac), so this is a string-level patch only.
  // TODO(3): navigator.userAgentData.brands is ["Not:A-Brand","Chromium"] with no
  //   "Google Chrome" entry (measured; see docs/DESIGN.md). Codecs are NOT a tell on
  //   Playwright's Chromium 156: H.264/AAC report "probably".
  // TODO(4): No audio devices (0); window outer size is 1919x1079 on a 1920x1080 screen.
  //   Headless mode additionally leaks "HeadlessChrome" in the UA and a 800x600 screen.
  //   Persona stays honestly Linux.
  // TODO(5): Human handoff (noVNC) for CAPTCHAs.
}
