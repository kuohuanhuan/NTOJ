import { useState } from "react";
import { Link, useNavigate } from "react-router";

import type { Route } from "./+types/contests";
import { Empty, PageHeader } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load } from "~/lib/api";
import { fmtDuration, fmtTime } from "~/lib/format";
import { useSession } from "~/lib/session";

interface ContestRow {
  id: number;
  name: string;
  mode: string;
  start_at: string;
  end_at: string;
  time_mode: string;
  duration_sec: number;
  reg_mode: string;
  my_status: string | null;
  participants: number;
}

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<ContestRow[]>("/contests", request);
}

export function contestPhase(c: { start_at: string; end_at: string }, now = Date.now()) {
  if (now < new Date(c.start_at).getTime()) return "Upcoming";
  return now < new Date(c.end_at).getTime() ? "Running" : "Ended";
}

export default function Contests({ loaderData }: Route.ComponentProps) {
  const { account } = useSession();
  const navigate = useNavigate();
  const [name, setName] = useState("");

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    let id = 0;
    if (await act(async () => void (id = (await api("/contests", { body: { name } })).id))) navigate(`/contests/${id}/manage`);
  };

  return (
    <>
      <PageHeader title="Contests">
        {account?.role === "admin" && (
          <form onSubmit={create} className="flex gap-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="New contest name" maxLength={64} required className="w-56" />
            <Button type="submit">Create</Button>
          </form>
        )}
      </PageHeader>
      {loaderData.length === 0 ? (
        <Empty>No contests yet.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Start</TableHead>
              <TableHead>Length</TableHead>
              <TableHead>Format</TableHead>
              <TableHead className="text-right">Contestants</TableHead>
              <TableHead>You</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loaderData.map((c) => {
              const phase = contestPhase(c);
              const length = c.time_mode === "flexible" ? c.duration_sec : (new Date(c.end_at).getTime() - new Date(c.start_at).getTime()) / 1000;
              return (
                <TableRow key={c.id}>
                  <TableCell className="max-w-72 truncate font-medium">
                    <Link to={`/contests/${c.id}`} className="hover:underline">
                      {c.name}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant={phase === "Running" ? "default" : phase === "Upcoming" ? "secondary" : "outline"}>{phase}</Badge>
                  </TableCell>
                  <TableCell>{fmtTime(c.start_at)}</TableCell>
                  <TableCell className="tabular-nums">
                    {fmtDuration(length)}
                    {c.time_mode === "flexible" && <span className="text-muted-foreground"> (flexible)</span>}
                  </TableCell>
                  <TableCell className="uppercase">{c.mode}</TableCell>
                  <TableCell className="text-right tabular-nums">{c.participants}</TableCell>
                  <TableCell className="text-muted-foreground capitalize">{c.my_status ?? "—"}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </>
  );
}
