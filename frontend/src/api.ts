// Typed client for the idea-collect backend. All calls are same-origin and send the
// session cookie (credentials: 'include').

export interface AuthInfo {
  project_name: string;
  display_name: string;
}

export interface SubmitResult {
  issue_url: string;
  issue_number: number;
}

async function handle<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = `Something went wrong (${res.status}).`;
    try {
      const data = await res.json();
      if (data && typeof data.error === 'string') message = data.error;
    } catch {
      // non-JSON error body; keep the default message
    }
    throw new Error(message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handle<T>(res);
}

export const api = {
  /** Exchange an invite code for a session. */
  auth: (code: string) => postJSON<AuthInfo>('/api/auth', { auth_code: code }),

  /** Return the current session, or null if not signed in. */
  async session(): Promise<AuthInfo | null> {
    const res = await fetch('/api/session', { credentials: 'include' });
    if (res.status === 401) return null;
    return handle<AuthInfo>(res);
  },

  /** Submit a new idea. */
  submit: (title: string, body: string) =>
    postJSON<SubmitResult>('/api/submissions', { title, body }),

  /** End the session. */
  logout: () => postJSON<void>('/api/logout', {}),
};
