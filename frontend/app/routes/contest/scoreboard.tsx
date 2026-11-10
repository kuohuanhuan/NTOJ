import { useEffect } from "react";
import { Link, useRevalidator } from "react-router";
import { Snowflake } from "lucide-react";

import { problemLabel } from "./problems";
import type { Route } from "./+types/scoreboard";
import { Empty } from "~/components/common";
import { Alert, AlertDescription } from "~/components/ui/alert";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load } from "~/lib/api";
import { useSession } from "~/lib/session";
import { cn } from "~/lib/utils";

interface Cell {
  score: number;
  attempts: number;
  solved: boolean;
  minutes: number;
  frozen: number;
  pending: number;
}

interface Board {
  mode: "ioi" | "acm";
  frozen: boolean;
  problems: { id: number; name: string }[];
  rows: { rank: number; account_id: number; name: string; total: number; solved: number; penalty: number; cells: Record<string, Cell> }[];
}

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<Board>(`/contests/${params.cid}/scoreboard`, request);
}

function CellView({ cell, acm }: { cell?: Cell; acm: boolean }) {
  if (!cell) return <span className="text-muted-foreground">—</span>;
  const waiting = cell.frozen + cell.pending;
  if (acm) {
    const failed = cell.attempts - (cell.solved ? 1 : 0);
    return (
      <div className={cn("leading-tight", cell.solved ? "text-emerald-600 dark:text-emerald-400" : failed ? "text-red-600 dark:text-red-400" : "")}>
        <div className="font-semibold">{cell.solved ? `+${failed || ""}` : failed ? `-${failed}` : waiting ? "?" : "—"}</div>
        <div className="text-muted-foreground text-xs">{cell.solved ? cell.minutes : waiting ? `${waiting} pending` : ""}</div>
      </div>
    );
  }
  return (
    <div className="leading-tight">
      <div className={cn("font-semibold", cell.solved && "text-emerald-600 dark:text-emerald-400")}>{cell.attempts ? cell.score : "—"}</div>
      {waiting > 0 && <div className="text-muted-foreground text-xs">{waiting} pending</div>}
    </div>
  );
}

export default function Scoreboard({ loaderData: board }: Route.ComponentProps) {
  const { account } = useSession();
  const revalidator = useRevalidator();
  const acm = board.mode === "acm";

  useEffect(() => {
    const timer = setInterval(() => {
      if (document.visibilityState === "visible" && revalidator.state === "idle") revalidator.revalidate();
    }, 30000);
    return () => clearInterval(timer);
  }, [revalidator]);

  return (
    <>
      {board.frozen && (
        <Alert className="mb-4">
          <Snowflake />
          <AlertDescription>The scoreboard is frozen: recent submissions are counted as pending.</AlertDescription>
        </Alert>
      )}
      {board.rows.length === 0 ? (
        <Empty>No contestants yet.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12">#</TableHead>
              <TableHead>Contestant</TableHead>
              <TableHead className="text-right">{acm ? "Solved" : "Total"}</TableHead>
              {acm && <TableHead className="text-right">Penalty</TableHead>}
              {board.problems.map((p, i) => (
                <TableHead key={p.id} className="text-center" title={p.name}>
                  <Link to={`../problems/${p.id}`} relative="path" className="hover:underline">
                    {problemLabel(i)}
                  </Link>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {board.rows.map((r) => (
              <TableRow key={r.account_id} className={cn(r.account_id === account?.id && "bg-muted/60")}>
                <TableCell className="tabular-nums">{r.rank}</TableCell>
                <TableCell className="max-w-56 truncate font-medium">{r.name}</TableCell>
                <TableCell className="text-right font-semibold tabular-nums">{acm ? r.solved : r.total}</TableCell>
                {acm && <TableCell className="text-right tabular-nums">{r.penalty}</TableCell>}
                {board.problems.map((p) => (
                  <TableCell key={p.id} className="text-center tabular-nums">
                    <CellView cell={r.cells[p.id]} acm={acm} />
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  );
}
