import { Link } from "react-router";

import type { Route } from "./+types/board";
import { Empty, PageHeader, StateText, UserLink } from "~/components/common";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load(`/boards/${params.id}`, request);
}

export default function Board({ loaderData: b }: Route.ComponentProps) {
  return (
    <>
      <PageHeader title={b.name} description={`${fmtTime(b.start_at)} – ${fmtTime(b.end_at)}`} />
      {b.rows.length === 0 ? (
        <Empty>No users on this board.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-12">#</TableHead>
              <TableHead>User</TableHead>
              <TableHead className="text-right">Total</TableHead>
              {b.problems.map((p: any) => (
                <TableHead key={p.id} className="text-center" title={p.name}>
                  <Link to={`/problems/${p.id}`} className="hover:underline">
                    {p.id}
                  </Link>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {b.rows.map((r: any, i: number) => (
              <TableRow key={r.account_id}>
                <TableCell className="tabular-nums">{i + 1}</TableCell>
                <TableCell className="font-medium">
                  <UserLink id={r.account_id} name={r.name} />
                </TableCell>
                <TableCell className="text-right font-semibold tabular-nums">{r.total}</TableCell>
                {b.problems.map((p: any) => {
                  const cell = r.cells[p.id];
                  return (
                    <TableCell key={p.id} className="text-center tabular-nums">
                      {cell ? (
                        <>
                          <StateText state={cell.state} /> <span className="text-muted-foreground">{cell.score}</span>
                        </>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  );
}
