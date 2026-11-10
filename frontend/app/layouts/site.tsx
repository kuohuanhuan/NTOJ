import { useEffect, useState } from "react";
import { isRouteErrorResponse, Link, NavLink, Outlet, useNavigate, useRevalidator, useRouteError } from "react-router";
import { LogOut, Menu, MessageCircleQuestion, Moon, Settings, Shield, Sun, User } from "lucide-react";

import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { act, api } from "~/lib/api";
import { useSession } from "~/lib/session";
import { cn } from "~/lib/utils";

const NAV = [
  { to: "/problems", label: "Problems" },
  { to: "/submissions", label: "Submissions" },
  { to: "/contests", label: "Contests" },
  { to: "/rank", label: "Ranking" },
  { to: "/classes", label: "Classes" },
  { to: "/boards", label: "Boards" },
];

function ThemeToggle() {
  const [dark, setDark] = useState(false);
  useEffect(() => setDark(document.documentElement.classList.contains("dark")), []);
  const toggle = () => {
    const next = !dark;
    localStorage.setItem("theme", next ? "dark" : "light");
    document.documentElement.classList.toggle("dark", next);
    setDark(next);
  };
  return (
    <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
      {dark ? <Sun /> : <Moon />}
    </Button>
  );
}

function Header() {
  const { site, account, lockdown } = useSession();
  const navigate = useNavigate();
  const revalidator = useRevalidator();
  const [open, setOpen] = useState(false);
  // Contestants in a restricted contest only get the contest itself.
  const nav = lockdown ? [{ to: `/contests/${lockdown}`, label: "Contest" }] : NAV;

  const signOut = async () => {
    if (await act(() => api("/auth/signout", { method: "POST" }))) {
      await revalidator.revalidate();
      navigate("/");
    }
  };

  return (
    <header className="bg-background/90 sticky top-0 z-40 border-b backdrop-blur">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-2 px-4">
        <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setOpen(!open)} aria-label="Menu">
          <Menu />
        </Button>
        <Link to="/" className="mr-4 font-semibold tracking-tight">
          {site.title}
        </Link>
        <nav
          className={cn(
            "bg-background absolute top-14 right-0 left-0 flex-col border-b p-2 md:static md:flex md:flex-row md:border-0 md:p-0",
            open ? "flex" : "hidden",
          )}
        >
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              onClick={() => setOpen(false)}
              className={({ isActive }) =>
                cn(
                  "hover:text-foreground rounded-md px-3 py-1.5 text-sm transition-colors",
                  isActive ? "text-foreground font-medium" : "text-muted-foreground",
                )
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1">
          <ThemeToggle />
          {account ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" className="max-w-40">
                  <span className="truncate">{account.name}</span>
                  {account.unread_replies > 0 && <Badge variant="destructive">{account.unread_replies}</Badge>}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel className="text-muted-foreground max-w-56 truncate text-xs font-normal">
                  {account.email}
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                {!lockdown && (
                  <DropdownMenuItem asChild>
                    <Link to={`/users/${account.id}`}>
                      <User /> Profile
                    </Link>
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem asChild>
                  <Link to="/settings">
                    <Settings /> Settings
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link to="/questions">
                    <MessageCircleQuestion /> Ask the admins
                  </Link>
                </DropdownMenuItem>
                {account.role === "admin" && (
                  <DropdownMenuItem asChild>
                    <Link to="/admin">
                      <Shield /> Administration
                    </Link>
                  </DropdownMenuItem>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={signOut}>
                  <LogOut /> Sign out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : (
            <Button asChild size="sm">
              <Link to="/signin">Sign in</Link>
            </Button>
          )}
        </div>
      </div>
    </header>
  );
}

export default function SiteLayout() {
  return (
    <div className="flex min-h-dvh flex-col">
      <Header />
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}

// Errors from pages render inside the site chrome so navigation keeps working.
export function ErrorBoundary() {
  const error = useRouteError();
  let title = "Something went wrong";
  let details = "An unexpected error occurred.";
  if (isRouteErrorResponse(error)) {
    title = error.status === 404 ? "Not found" : error.status === 403 ? "Not available" : `Error ${error.status}`;
    details = error.data?.message ?? error.statusText;
  } else if (import.meta.env.DEV && error instanceof Error) {
    details = error.message;
  }
  return (
    <div className="flex min-h-dvh flex-col">
      <Header />
      <main className="mx-auto flex w-full max-w-md flex-1 flex-col items-center justify-center gap-3 p-4 text-center">
        <h1 className="text-2xl font-semibold">{title}</h1>
        <p className="text-muted-foreground">{details}</p>
        <Button asChild variant="outline">
          <Link to="/">Back to home</Link>
        </Button>
      </main>
    </div>
  );
}
