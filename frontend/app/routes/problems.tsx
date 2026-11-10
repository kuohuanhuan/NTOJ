import { Form, Link, useSearchParams } from "react-router";

import type { Route } from "./+types/problems";
import { Empty, PageHeader, Pagination, StateText } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load, qs } from "~/lib/api";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  const sp = new URL(request.url).searchParams;
  const query = qs({ q: sp.get("q"), tag: sp.get("tag"), class_id: sp.get("class_id"), page: sp.get("page") });
  const [problems, tags, classes] = await Promise.all([
    load("/problems" + query, request),
    load<{ tag: string; count: number }[]>("/tags", request),
    load<{ id: number; name: string }[]>("/classes", request),
  ]);
  return { problems, tags, classes };
}

export default function Problems({ loaderData }: Route.ComponentProps) {
  const { problems, tags, classes } = loaderData;
  const [params] = useSearchParams();
  return (
    <>
      <PageHeader title="Problems" description={`${problems.total} problems`} />
      <Form method="get" className="mb-4 flex flex-wrap gap-2">
        <Input name="q" placeholder="Search by name or ID" defaultValue={params.get("q") ?? ""} className="w-56" />
        <NativeSelect name="tag" defaultValue={params.get("tag") ?? ""} aria-label="Tag">
          <NativeSelectOption value="">All tags</NativeSelectOption>
          {tags.map((t) => (
            <NativeSelectOption key={t.tag} value={t.tag}>
              {t.tag} ({t.count})
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect name="class_id" defaultValue={params.get("class_id") ?? ""} aria-label="Class" className="max-w-56">
          <NativeSelectOption value="">All classes</NativeSelectOption>
          {classes.map((c) => (
            <NativeSelectOption key={c.id} value={c.id}>
              {c.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Button type="submit" variant="secondary">
          Search
        </Button>
      </Form>
      {problems.items.length === 0 ? (
        <Empty>No problems match.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">#</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Tags</TableHead>
              <TableHead className="text-right">Solved / Tried</TableHead>
              <TableHead className="text-right">You</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {problems.items.map((p: any) => (
              <TableRow key={p.id}>
                <TableCell className="text-muted-foreground tabular-nums">{p.id}</TableCell>
                <TableCell className="max-w-80 truncate font-medium">
                  <Link to={`/problems/${p.id}`} className="hover:underline">
                    {p.name}
                  </Link>
                  {p.status !== "online" && (
                    <Badge variant="outline" className="ml-2">
                      {p.status}
                    </Badge>
                  )}
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {p.tags.map((t: string) => (
                      <Link key={t} to={"?tag=" + encodeURIComponent(t)}>
                        <Badge variant="secondary">{t}</Badge>
                      </Link>
                    ))}
                  </div>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {p.solved_users} / {p.tried_users}
                </TableCell>
                <TableCell className="text-right">
                  <StateText state={p.my_state} />
                  {p.my_state != null && p.my_state !== 1 && <span className="text-muted-foreground ml-1 tabular-nums">{p.my_score}</span>}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Pagination total={problems.total} page={problems.page} perPage={problems.per_page} />
    </>
  );
}
