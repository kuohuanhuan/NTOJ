import {
  isRouteErrorResponse,
  Link,
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
  type ShouldRevalidateFunctionArgs,
} from "react-router";

import type { Route } from "./+types/root";
import "./app.css";
import { Button } from "~/components/ui/button";
import { Toaster } from "~/components/ui/sonner";
import { api } from "~/lib/api";
import type { Session } from "~/lib/session";

export function applyTheme() {
  const stored = localStorage.getItem("theme");
  const dark = stored ? stored === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.classList.toggle("dark", dark);
}

export async function clientLoader(): Promise<Session> {
  applyTheme();
  return api<Session>("/session");
}

// The session changes only through explicit actions, which revalidate.
export function shouldRevalidate({ currentUrl, nextUrl, defaultShouldRevalidate }: ShouldRevalidateFunctionArgs) {
  return currentUrl.href === nextUrl.href ? defaultShouldRevalidate : false;
}

export function meta({ loaderData: data }: Route.MetaArgs) {
  return [{ title: data?.site.title ?? "NTOJ" }];
}

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export function HydrateFallback() {
  return <div className="text-muted-foreground flex h-dvh items-center justify-center text-sm">Loading…</div>;
}

export default function App() {
  return (
    <>
      <Outlet />
      <Toaster position="top-center" richColors />
    </>
  );
}

export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  let title = "Something went wrong";
  let details = "An unexpected error occurred.";
  if (isRouteErrorResponse(error)) {
    title = String(error.status);
    details = error.data?.message ?? error.statusText;
  } else if (import.meta.env.DEV && error instanceof Error) {
    details = error.message;
  }
  return (
    <main className="mx-auto flex h-dvh max-w-md flex-col items-center justify-center gap-3 p-4 text-center">
      <h1 className="text-3xl font-semibold">{title}</h1>
      <p className="text-muted-foreground">{details}</p>
      <Button asChild variant="outline">
        <Link to="/">Back to home</Link>
      </Button>
    </main>
  );
}
