// GREYMAN_PROXY is one URL, but Playwright drops any user:pass@ in `server` and only
// honors separate username/password fields. Short form "host:3128" means an HTTP proxy.
export function parseProxy(raw) {
  if (!raw) return undefined;
  let url;
  try {
    url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `http://${raw}`);
  } catch {
    // Not echoed: the value may carry a password.
    throw new Error('GREYMAN_PROXY is not a valid proxy URL');
  }
  const proxy = { server: `${url.protocol}//${url.host}` };
  if (url.username) proxy.username = decodeURIComponent(url.username);
  if (url.password) proxy.password = decodeURIComponent(url.password);
  return proxy;
}
