import { useState } from "react";
import { useRevalidator } from "react-router";

import type { Route } from "./+types/bulletins";
import type { Bulletin } from "../home";
import { ConfirmButton, Empty, Field, PageHeader } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<Bulletin[]>("/bulletins", request);
}

function BulletinDialog({ value, onClose }: { value: Partial<Bulletin>; onClose: (saved: boolean) => void }) {
  const [pinned, setPinned] = useState(value.pinned ?? false);
  const save = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const body = { title: f.get("title"), content: f.get("content"), color: f.get("color"), pinned };
    const ok = await act(
      () => (value.id ? api(`/admin/bulletins/${value.id}`, { method: "PUT", body }) : api("/admin/bulletins", { body })),
      "Bulletin saved",
    );
    if (ok) onClose(true);
  };
  return (
    <Dialog open onOpenChange={() => onClose(false)}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{value.id ? "Edit bulletin" : "New bulletin"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="grid gap-4">
          <Field label="Title">
            <Input name="title" defaultValue={value.title} maxLength={100} required />
          </Field>
          <Field label="Content" hint="Markdown.">
            <Textarea name="content" defaultValue={value.content} required className="min-h-48 font-mono text-xs md:text-xs" />
          </Field>
          <div className="flex flex-wrap items-end gap-6">
            <Field label="Accent">
              <NativeSelect name="color" defaultValue={value.color ?? ""}>
                <NativeSelectOption value="">None</NativeSelectOption>
                {["red", "orange", "yellow", "green", "blue", "purple"].map((c) => (
                  <NativeSelectOption key={c} value={c}>
                    {c}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
            <Label className="pb-2">
              <Switch checked={pinned} onCheckedChange={setPinned} /> Pinned
            </Label>
          </div>
          <DialogFooter>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function AdminBulletins({ loaderData }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const [editing, setEditing] = useState<Partial<Bulletin> | null>(null);
  return (
    <>
      <PageHeader title="Bulletins">
        <Button onClick={() => setEditing({})}>New bulletin</Button>
      </PageHeader>
      {loaderData.length === 0 ? (
        <Empty>No bulletins.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Title</TableHead>
              <TableHead>Published</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loaderData.map((b) => (
              <TableRow key={b.id}>
                <TableCell className="max-w-96 truncate font-medium">
                  {b.title}
                  {b.pinned && (
                    <Badge variant="secondary" className="ml-2">
                      Pinned
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground">{fmtTime(b.created_at)}</TableCell>
                <TableCell className="text-right">
                  <Button variant="outline" size="sm" onClick={() => setEditing(b)} className="mr-2">
                    Edit
                  </Button>
                  <ConfirmButton
                    title={`Delete "${b.title}"?`}
                    onConfirm={async () => {
                      if (await act(() => api(`/admin/bulletins/${b.id}`, { method: "DELETE" }), "Deleted")) revalidator.revalidate();
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
        <BulletinDialog
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
