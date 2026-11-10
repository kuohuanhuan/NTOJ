import { useState } from "react";
import { Link, useRevalidator } from "react-router";
import { toast } from "sonner";

import type { Route } from "./+types/boards";
import { ConfirmButton, Empty, Field, PageHeader } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load } from "~/lib/api";
import { fmtTime, fromLocalInput, parseIdList, toLocalInput } from "~/lib/format";

interface Board {
  id: number;
  name: string;
  visible: boolean;
  start_at: string;
  end_at: string;
  problem_ids: number[];
  account_ids: number[];
}

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<Board[]>("/admin/boards", request);
}

function BoardDialog({ value, onClose }: { value: Partial<Board>; onClose: (saved: boolean) => void }) {
  const [visible, setVisible] = useState(value.visible ?? true);
  const save = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const problems = parseIdList(String(f.get("problems")));
    const accounts = parseIdList(String(f.get("accounts")));
    if (!problems || !accounts) return void toast.error("Use ID lists such as: 1, 2, 5-9");
    const body = {
      name: f.get("name"),
      visible,
      start_at: fromLocalInput(String(f.get("start"))),
      end_at: fromLocalInput(String(f.get("end"))),
      problem_ids: problems,
      account_ids: accounts,
    };
    const ok = await act(() => (value.id ? api(`/admin/boards/${value.id}`, { method: "PUT", body }) : api("/admin/boards", { body })), "Board saved");
    if (ok) onClose(true);
  };
  return (
    <Dialog open onOpenChange={() => onClose(false)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{value.id ? "Edit board" : "New board"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="grid gap-4">
          <Field label="Name">
            <Input name="name" defaultValue={value.name} maxLength={64} required />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Count submissions from">
              <Input name="start" type="datetime-local" defaultValue={toLocalInput(value.start_at ?? new Date())} required />
            </Field>
            <Field label="Until">
              <Input name="end" type="datetime-local" defaultValue={toLocalInput(value.end_at ?? new Date(Date.now() + 7 * 86400000))} required />
            </Field>
          </div>
          <Field label="Problem IDs" hint="Comma separated; ranges such as 5-9 are allowed.">
            <Input name="problems" defaultValue={value.problem_ids?.join(", ")} />
          </Field>
          <Field label="Account IDs">
            <Input name="accounts" defaultValue={value.account_ids?.join(", ")} />
          </Field>
          <Label>
            <Switch checked={visible} onCheckedChange={setVisible} /> Visible to everyone
          </Label>
          <DialogFooter>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function AdminBoards({ loaderData }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const [editing, setEditing] = useState<Partial<Board> | null>(null);
  return (
    <>
      <PageHeader title="Boards">
        <Button onClick={() => setEditing({})}>New board</Button>
      </PageHeader>
      {loaderData.length === 0 ? (
        <Empty>No boards.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Window</TableHead>
              <TableHead className="text-right">Problems</TableHead>
              <TableHead className="text-right">Accounts</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loaderData.map((b) => (
              <TableRow key={b.id}>
                <TableCell className="font-medium">
                  <Link to={`/boards/${b.id}`} className="hover:underline">
                    {b.name}
                  </Link>
                  {!b.visible && (
                    <Badge variant="outline" className="ml-2">
                      Hidden
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {fmtTime(b.start_at)} – {fmtTime(b.end_at)}
                </TableCell>
                <TableCell className="text-right tabular-nums">{b.problem_ids.length}</TableCell>
                <TableCell className="text-right tabular-nums">{b.account_ids.length}</TableCell>
                <TableCell className="text-right">
                  <Button variant="outline" size="sm" onClick={() => setEditing(b)} className="mr-2">
                    Edit
                  </Button>
                  <ConfirmButton
                    title={`Delete "${b.name}"?`}
                    onConfirm={async () => {
                      if (await act(() => api(`/admin/boards/${b.id}`, { method: "DELETE" }), "Deleted")) revalidator.revalidate();
                    }}
                  >
                    Delete
                  </ConfirmButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {editing && (
        <BoardDialog
          value={editing}
          onClose={(saved) => {
            setEditing(null);
            if (saved) revalidator.revalidate();
          }}
        />
      )}
    </>
  );
}
