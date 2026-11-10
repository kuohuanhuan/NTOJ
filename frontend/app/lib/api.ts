import { data, redirect } from "react-router";
import { toast } from "sonner";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public extra: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

type Options = { method?: string; body?: unknown; form?: FormData; signal?: AbortSignal };

/** Calls the JSON API. Throws ApiError for any non-2xx response. */
export async function api<T = any>(path: string, opts: Options = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? (opts.body !== undefined || opts.form ? "POST" : "GET"),
    credentials: "same-origin",
    signal: opts.signal,
    headers: { Accept: "application/json" },
  };
  if (opts.form) {
    init.body = opts.form;
  } else if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  let res: Response;
  try {
    res = await fetch("/api" + path, init);
  } catch {
    throw new ApiError(0, "network", "Cannot reach the server");
  }
  if (res.status === 204) return undefined as T;
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const e = body?.error ?? {};
    throw new ApiError(res.status, e.code ?? "error", e.message ?? `Request failed (${res.status})`, e.extra ?? {});
  }
  return body as T;
}

/**
 * Fetches data inside a route loader. Authentication and contest-lockdown
 * failures become redirects; everything else reaches the error boundary.
 */
export async function load<T = any>(path: string, request: Request): Promise<T> {
  try {
    return await api<T>(path, { signal: request.signal });
  } catch (err) {
    if (err instanceof ApiError) {
      const url = new URL(request.url);
      if (err.status === 401) {
        throw redirect("/signin?next=" + encodeURIComponent(url.pathname + url.search));
      }
      if (err.code === "contest_lockdown") {
        throw redirect(`/contests/${err.extra.contest_id}`);
      }
      throw data({ message: err.message }, { status: err.status || 503 });
    }
    throw err;
  }
}

/** Runs a mutation, reporting the outcome as a toast. Resolves to whether it succeeded. */
export async function act(fn: () => Promise<unknown>, success?: string): Promise<boolean> {
  try {
    await fn();
    if (success) toast.success(success);
    return true;
  } catch (err) {
    toast.error(err instanceof Error ? err.message : "Something went wrong");
    return false;
  }
}

/** Builds a query string, dropping empty values. */
export function qs(params: Record<string, string | number | null | undefined>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== null && v !== undefined && v !== "") sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? "?" + s : "";
}
