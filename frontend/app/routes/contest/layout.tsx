import { useEffect, useState } from "react";
import { NavLink, Outlet, useOutletContext, useRevalidator } from "react-router";

import type { Route } from "./+types/layout";
import { Badge } from "~/components/ui/badge";
import { load } from "~/lib/api";
import { fmtDuration } from "~/lib/format";
import { cn } from "~/lib/utils";

export interface ContestAccess {
  phase: "before" | "running" | "ended";
  global: "before" | "running" | "ended";
  status: string;
  activated: boolean;
  start: string;
  end: string;
  is_admin: boolean;
  is_member: boolean;
  is_participant: boolean;
  can_view_problems: boolean;
  can_submit: boolean;
  can_view_scoreboard: boolean;
  can_ask: boolean;
  can_start: boolean;
  can_register: boolean;
  can_withdraw: boolean;
}

export interface ContestInfo {
  contest: {
    id: number;
    name: string;
    mode: "ioi" | "acm";
    start_at: string;
    end_at: string;
    time_mode: "fixed" | "flexible";
    duration_sec: number;
    reg_mode: "invited" | "free" | "approval";
    reg_end_at: string;
    languages: string[];
    public_scoreboard: boolean;
    cooldown_sec: number;
    freeze_minutes: number;
    penalty_minutes: number;
  };
  description: string;
  access: ContestAccess;
  participants: number;
  unread: number;
  now: string;
}

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<ContestInfo>(`/contests/${params.cid}`, request);
}

// Access depends on the clock, so refresh it on every navigation inside the contest.
export function shouldRevalidate() {
  return true;
}

export function meta({ loaderData: data }: Route.MetaArgs) {
  return [{ title: data?.contest.name ?? "Contest" }];
}

export function useContest() {
  return useOutletContext<ContestInfo>();
}

/** Counts down to the viewer's next phase change, corrected for clock skew. */
function Countdown({ info }: { info: ContestInfo }) {
  const revalidator = useRevalidator();
  const [skew] = useState(() => new Date(info.now).getTime() - Date.now());
  const [now, setNow] = useState(() => Date.now() + skew);
  const { access } = info;
  const target = access.phase === "before" ? new Date(access.start).getTime() : new Date(access.end).getTime();

  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now() + skew), 1000);
    return () => clearInterval(timer);
  }, [skew]);
  const remaining = (target - now) / 1000;
  const live = access.phase !== "ended" && access.activated;
  useEffect(() => {
    if (live && remaining <= 0 && revalidator.state === "idle") revalidator.revalidate();
  }, [live, remaining, revalidator]);

  if (!access.activated) return <Badge variant="secondary">Not started by you</Badge>;
  if (access.phase === "ended") return <Badge variant="outline">Ended</Badge>;
  return (
    <span className="flex items-center gap-2 text-sm">
      <Badge variant={access.phase === "running" ? "default" : "secondary"}>{access.phase === "running" ? "Running" : "Upcoming"}</Badge>
      <span className="text-muted-foreground">{access.phase === "running" ? "ends in" : "starts in"}</span>
      <span className="font-mono tabular-nums">{fmtDuration(remaining)}</span>
    </span>
  );
}

export default function ContestLayout({ loaderData: info }: Route.ComponentProps) {
  const { access, contest } = info;
  const tabs = [
    { to: ".", label: "Overview", end: true, show: true },
    { to: "problems", label: "Problems", show: access.can_view_problems },
    { to: "submissions", label: "Submissions", show: access.is_member || access.phase === "ended" },
    { to: "scoreboard", label: "Scoreboard", show: access.can_view_scoreboard },
    { to: "qa", label: "Q & A", show: true, badge: info.unread },
    { to: "manage", label: "Manage", show: access.is_admin },
  ];
  return (
    <>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h1 className="min-w-0 truncate text-2xl font-semibold tracking-tight">{contest.name}</h1>
        <Countdown info={info} key={access.phase + access.start} />
      </div>
      <nav className="mb-6 flex gap-1 overflow-x-auto border-b">
        {tabs
          .filter((t) => t.show)
          .map((t) => (
            <NavLink
              key={t.to}
              to={t.to}
              end={t.end}
              className={({ isActive }) =>
                cn(
                  "-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors",
                  isActive ? "border-foreground font-medium" : "text-muted-foreground hover:text-foreground border-transparent",
                )
              }
            >
              {t.label}
              {!!t.badge && <Badge variant="destructive">{t.badge}</Badge>}
            </NavLink>
          ))}
      </nav>
      <Outlet context={info} />
    </>
  );
}
