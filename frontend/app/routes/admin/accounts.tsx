import { useState } from "react";
import { Form, useRevalidator, useSearchParams } from "react-router";

import type { Route } from "./+types/accounts";
import { Field, PageHeader, Pagination } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load, qs } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  const sp = new URL(request.url).searchParams;
  return load("/admin/accounts" + qs({ q: sp.get("q"), page: sp.get("page") }), request);
}

function EditDialog({ acct, onClose }: { acct: any; onClose: (saved: boolean) => void }) {
  const [disabled, setDisabled] = useState<boolean>(acct.disabled);
  const save = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const body = { name: f.get("name"), role: f.get("role"), allowed_ip: f.get("allowed_ip"), disabled };
    if (!(await act(() => api(`/admin/accounts/${acct.id}`, { method: "PATCH", body }), "Account saved"))) return;
    const password = String(f.get("password"));
    if (password && !(await act(() => api(`/admin/accounts/${acct.id}/password`, { body: { password } }), "Password reset"))) return;
    onClose(true);
  };
  return (
    <Dialog open onOpenChange={() => onClose(false)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            Account #{acct.id} · {acct.email}
          </DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="grid gap-4">
          <Field label="Display name">
            <Input name="name" defaultValue={acct.name} maxLength={32} required />
          </Field>
          <Field label="Role">
            <NativeSelect name="role" defaultValue={acct.role}>
              <NativeSelectOption value="user">User</NativeSelectOption>
              <NativeSelectOption value="admin">Administrator</NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field label="Allowed IP" hint="When set, the account only works from this address. Leave empty for no restriction.">
            <Input name="allowed_ip" defaultValue={acct.allowed_ip} placeholder="e.g. 192.0.2.10" />
          </Field>
          <Field label="New password" hint="Leave empty to keep the current one. Resetting signs the account out everywhere.">
            <Input name="password" type="password" minLength={8} autoComplete="new-password" />
          </Field>
          <Label>
            <Switch checked={disabled} onCheckedChange={setDisabled} /> Disabled (cannot sign in)
          </Label>
          <DialogFooter>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function Accounts({ loaderData }: Route.ComponentProps) {
  const [params] = useSearchParams();
  const revalidator = useRevalidator();
  const [editing, setEditing] = useState<any>(null);
  return (
    <>
      <PageHeader title="Accounts" description={`${loaderData.total} accounts`}>
        <Form method="get" className="flex gap-2">
          <Input name="q" placeholder="Name, email or ID" defaultValue={params.get("q") ?? ""} className="w-56" />
          <Button type="submit" variant="secondary">
            Search
          </Button>
        </Form>
      </PageHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Email</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Last IP</TableHead>
            <TableHead>Allowed IP</TableHead>
            <TableHead>Created</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {loaderData.items.map((a: any) => (
            <TableRow key={a.id}>
              <TableCell className="tabular-nums">{a.id}</TableCell>
              <TableCell className="max-w-48 truncate font-medium">{a.name}</TableCell>
              <TableCell className="max-w-56 truncate">{a.email}</TableCell>
              <TableCell>
                {a.role === "admin" ? <Badge>Admin</Badge> : <Badge variant="outline">User</Badge>}
                {a.disabled && (
                  <Badge variant="destructive" className="ml-1">
                    Disabled
                  </Badge>
                )}
              </TableCell>
              <TableCell className="font-mono text-xs">{a.last_ip}</TableCell>
              <TableCell className="font-mono text-xs">{a.allowed_ip}</TableCell>
              <TableCell className="text-muted-foreground">{fmtTime(a.created_at)}</TableCell>
              <TableCell className="text-right">
                <Button variant="outline" size="sm" onClick={() => setEditing(a)}>
                  Edit
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pagination total={loaderData.total} page={loaderData.page} perPage={loaderData.per_page} />
      {editing && (
        <EditDialog
          acct={editing}
          onClose={(saved) => {
            setEditing(null);
            if (saved) revalidator.revalidate();
          }}
        />
      )}
    </>
  );
}
