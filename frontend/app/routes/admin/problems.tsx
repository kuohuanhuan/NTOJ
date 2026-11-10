import { useState } from "react";
import { Link, useNavigate } from "react-router";

import type { Route } from "./+types/problems";
import { PageHeader, Pagination } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load, qs } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load("/admin/problems" + qs({ page: new URL(request.url).searchParams.get("page") }), request);
}

export default function AdminProblems({ loaderData }: Route.ComponentProps) {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    let id = 0;
    if (await act(async () => void (id = (await api("/admin/problems", { body: { name } })).id))) navigate(`/admin/problems/${id}`);
  };
  return (
    <>
      <PageHeader title="Problems" description="New problems start hidden.">
        <form onSubmit={create} className="flex gap-2">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="New problem name" maxLength={64} required className="w-56" />
          <Button type="submit">Create</Button>
        </form>
      </PageHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">Tests</TableHead>
            <TableHead className="text-right">Submissions</TableHead>
            <TableHead>Updated</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loaderData.items.map((p: any) => (
            <TableRow key={p.id}>
              <TableCell className="tabular-nums">{p.id}</TableCell>
              <TableCell className="max-w-80 truncate font-medium">
                <Link to={String(p.id)} className="hover:underline">
                  {p.name}
                </Link>
              </TableCell>
              <TableCell>
                <Badge variant={p.status === "online" ? "default" : p.status === "contest" ? "secondary" : "outline"}>{p.status}</Badge>
                {!p.allow_submit && (
                  <Badge variant="outline" className="ml-1">
                    closed
                  </Badge>
                )}
              </TableCell>
              <TableCell className="text-right tabular-nums">{p.testcase_count}</TableCell>
              <TableCell className="text-right tabular-nums">{p.submissions}</TableCell>
              <TableCell className="text-muted-foreground">{fmtTime(p.updated_at)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pagination total={loaderData.total} page={loaderData.page} perPage={loaderData.per_page} />
    </>
  );
}
