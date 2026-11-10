import { Link } from "react-router";

import type { Route } from "./+types/problems";
import { Empty, StateText } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load } from "~/lib/api";

export const problemLabel = (index: number) => (index < 26 ? String.fromCharCode(65 + index) : `P${index + 1}`);

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<any[]>(`/contests/${params.cid}/problems`, request);
}

export default function ContestProblems({ loaderData }: Route.ComponentProps) {
  if (loaderData.length === 0) return <Empty>This contest has no problems yet.</Empty>;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-12" />
          <TableHead>Problem</TableHead>
          <TableHead className="text-right">Your result</TableHead>
          <TableHead className="text-right">Your score</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loaderData.map((p, i) => (
          <TableRow key={p.id}>
            <TableCell className="font-semibold">{problemLabel(i)}</TableCell>
            <TableCell className="font-medium">
              <Link to={String(p.id)} className="hover:underline">
                {p.name}
              </Link>
              {p.status === "hidden" && (
                <Badge variant="outline" className="ml-2">
                  hidden from contestants
                </Badge>
              )}
            </TableCell>
            <TableCell className="text-right">
              <StateText state={p.my_state} />
            </TableCell>
            <TableCell className="text-right tabular-nums">{p.my_score ?? "—"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
