// An error the caller caused or can act on; reported as HTTP 400 with its message.
export class UserError extends Error {}

// Playwright errors end with a multi-line call log; the first line is the useful part.
export const firstLine = (s) => String(s).split('\n', 1)[0];
