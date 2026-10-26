// A queue that runs functions one at a time, in call order: run(fn) returns fn's result.
// A failure goes to its own caller and does not stop what is queued after it.
// run.idle() settles once everything queued so far has run.
export function serial() {
  let tail = Promise.resolve();
  const run = (fn) => {
    const result = tail.then(fn);
    tail = result.catch(() => {});
    return result;
  };
  run.idle = () => tail;
  return run;
}
