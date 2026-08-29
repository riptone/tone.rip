/**
 * Deadline plumbing, and nothing else.
 *
 * Every "is this endpoint reachable" call in the repo needs the same three
 * lines - an AbortController, a timer, a `clearTimeout` in a `finally` - and
 * three of them had written it out. Callers keep their own try/catch and
 * their own reading of the response; this owns the deadline.
 *
 * It lived in @repo/content, which is a package about the CV and the site
 * copy. It was the most-imported symbol in the repo from the package it had
 * least to do with.
 */

/**
 * Runs `attempt` with a signal that aborts after `timeoutMs`.
 *
 * Split out from `fetchWithTimeout` because not every caller reaches the
 * network through global `fetch`: apps/dashboard talks to apps/api over a
 * Cloudflare service binding, whose `.fetch` is a method on a binding
 * object. That call still deserves a deadline, and a binding is typed by
 * workerd rather than by lib.dom - so the fetch itself stays at the call
 * site, where its own types apply, and only the timeout comes from here.
 */
export async function withTimeout<T>(
  timeoutMs: number,
  attempt: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await attempt(controller.signal);
  } finally {
    clearTimeout(timer);
  }
}

/** `withTimeout` around global `fetch`, which is what most callers want. */
export function fetchWithTimeout(
  input: string | URL,
  timeoutMs: number,
  init?: RequestInit,
): Promise<Response> {
  return withTimeout(timeoutMs, (signal) => fetch(input, { ...init, signal }));
}
