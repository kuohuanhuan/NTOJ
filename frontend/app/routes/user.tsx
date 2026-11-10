import { Link } from "react-router";

import type { Route } from "./+types/user";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { load } from "~/lib/api";
import { fmtTime } from "~/lib/format";
import { useSession } from "~/lib/session";
import { cn } from "~/lib/utils";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load(`/accounts/${params.id}`, request);
}

export function meta({ loaderData: data }: Route.MetaArgs) {
  return [{ title: data?.name ?? "User" }];
}

/** Only https images are ever stored; anything else is ignored defensively. */
const safeImage = (url: string) => (url.startsWith("https://") ? url : undefined);

export default function User({ loaderData: u }: Route.ComponentProps) {
  const { account } = useSession();
  const cover = safeImage(u.cover_url);
  const photo = safeImage(u.photo_url);
  return (
    <div className="grid gap-6">
      <Card className="overflow-hidden pt-0">
        <div className="bg-muted h-32">
          {cover && <img src={cover} alt="" className="size-full object-cover" referrerPolicy="no-referrer" />}
        </div>
        <CardContent className="flex flex-wrap items-end gap-4">
          <div className="bg-background -mt-12 size-20 shrink-0 overflow-hidden rounded-full border-4">
            {photo ? (
              <img src={photo} alt="" className="size-full object-cover" referrerPolicy="no-referrer" />
            ) : (
              <div className="bg-muted flex size-full items-center justify-center text-2xl font-semibold">{u.name.slice(0, 1).toUpperCase()}</div>
            )}
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="flex items-center gap-2 truncate text-xl font-semibold">
              {u.name}
              {u.role === "admin" && <Badge variant="secondary">Admin</Badge>}
            </h1>
            <p className="text-muted-foreground text-sm break-words">{u.motto || `Joined ${fmtTime(u.created_at)}`}</p>
          </div>
          <div className="flex gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to={`/submissions?account_id=${u.id}`}>Submissions</Link>
            </Button>
            {account?.id === u.id && (
              <Button asChild variant="outline" size="sm">
                <Link to="/settings">Edit profile</Link>
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Stat label="Score" value={u.rate} />
        <Stat label="Solved" value={`${u.ac_problems} / ${u.total_problems}`} />
        <Stat label="Submissions" value={u.submissions} />
        <Stat label="Accepted" value={u.ac_submissions} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Problems</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-1.5">
          {u.problems.map((p: { id: number; state: number | null; score: number | null }) => (
            <Link
              key={p.id}
              to={`/problems/${p.id}`}
              title={p.score != null ? `Score ${p.score}` : "Not attempted"}
              className={cn(
                "rounded border px-2 py-0.5 text-xs tabular-nums hover:opacity-80",
                p.state === 1 && "border-emerald-500/40 bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
                p.state != null && p.state !== 1 && "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300",
                p.state == null && "text-muted-foreground",
              )}
            >
              {p.id}
            </Link>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <Card className="py-4">
      <CardContent>
        <div className="text-muted-foreground text-xs">{label}</div>
        <div className="text-xl font-semibold tabular-nums">{value}</div>
      </CardContent>
    </Card>
  );
}
