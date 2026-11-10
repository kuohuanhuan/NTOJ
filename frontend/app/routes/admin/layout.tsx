import { NavLink, Outlet, redirect } from "react-router";

import type { Route } from "./+types/layout";
import { api } from "~/lib/api";
import type { Session } from "~/lib/session";
import { cn } from "~/lib/utils";

// The API enforces permissions; this only keeps non-admins off empty pages.
export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  const session = await api<Session>("/session");
  if (!session.account) throw redirect("/signin?next=" + encodeURIComponent(new URL(request.url).pathname));
  if (session.account.role !== "admin") throw redirect("/");
  return null;
}

const NAV = [
  { to: "/admin", label: "Dashboard", end: true },
  { to: "/admin/problems", label: "Problems" },
  { to: "/admin/accounts", label: "Accounts" },
  { to: "/admin/bulletins", label: "Bulletins" },
  { to: "/admin/boards", label: "Boards" },
  { to: "/admin/questions", label: "Questions" },
  { to: "/admin/logs", label: "Audit log" },
];

export default function AdminLayout() {
  return (
    <div className="grid gap-6 md:grid-cols-[11rem_minmax(0,1fr)]">
      <nav className="flex gap-1 overflow-x-auto md:flex-col">
        {NAV.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.end}
            className={({ isActive }) =>
              cn("rounded-md px-3 py-1.5 text-sm whitespace-nowrap", isActive ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground")
            }
          >
            {n.label}
          </NavLink>
        ))}
      </nav>
      <div className="min-w-0">
        <Outlet />
      </div>
    </div>
  );
}
