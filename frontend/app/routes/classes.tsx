import { useState } from "react";
import { Link, useRevalidator } from "react-router";
import { Star } from "lucide-react";
import { toast } from "sonner";

import type { Route } from "./+types/classes";
import { ConfirmButton, Empty, Field, PageHeader } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "~/components/ui/card";
import { Checkbox } from "~/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load } from "~/lib/api";
import { parseIdList } from "~/lib/format";
import { useSession } from "~/lib/session";
import { cn } from "~/lib/utils";

interface ProblemClass {
  id: number;
  name: string;
  description: string;
  owner_id: number | null;
  owner_name: string | null;
  public: boolean;
  problem_count: number;
  problem_ids: number[];
  collected: boolean;
}

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<ProblemClass[]>("/classes", request);
}

function ClassDialog({ value, onClose }: { value: Partial<ProblemClass> & { official?: boolean }; onClose: (saved: boolean) => void }) {
  const [isPublic, setPublic] = useState(value.public ?? true);
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const ids = parseIdList(String(f.get("problems")));
    if (!ids || ids.length === 0) return void toast.error("List problem IDs such as: 1, 2, 5-9");
    const body = { name: f.get("name"), description: f.get("description"), problem_ids: ids, public: isPublic, official: !!value.official };
    const ok = await act(
      () => (value.id ? api(`/classes/${value.id}`, { method: "PUT", body }) : api("/classes", { body })),
      "Class saved",
    );
    if (ok) onClose(true);
  };
  return (
    <Dialog open onOpenChange={() => onClose(false)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{value.id ? "Edit class" : value.official ? "New official class" : "New class"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4">
          <Field label="Name">
            <Input name="name" defaultValue={value.name} maxLength={50} required />
          </Field>
          <Field label="Description">
            <Textarea name="description" defaultValue={value.description} maxLength={2048} />
          </Field>
          <Field label="Problems" hint="Problem IDs separated by commas; ranges such as 5-9 are allowed.">
            <Input name="problems" defaultValue={value.problem_ids?.join(", ")} required />
          </Field>
          <Label>
            <Checkbox checked={isPublic} onCheckedChange={(v) => setPublic(v === true)} /> Visible to everyone
          </Label>
          <DialogFooter>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export default function Classes({ loaderData: classes }: Route.ComponentProps) {
  const { account } = useSession();
  const revalidator = useRevalidator();
  const [editing, setEditing] = useState<(Partial<ProblemClass> & { official?: boolean }) | null>(null);
  const isAdmin = account?.role === "admin";
  const canEdit = (c: ProblemClass) => !!account && (c.owner_id === account.id || (isAdmin && c.owner_id === null));

  const toggle = async (c: ProblemClass) => {
    await act(() => api(`/classes/${c.id}/collect`, { method: c.collected ? "DELETE" : "PUT" }));
    revalidator.revalidate();
  };
  const sorted = [...classes].sort((a, b) => Number(b.collected) - Number(a.collected));

  return (
    <>
      <PageHeader title="Problem classes" description="Curated problem lists. Star the ones you want to keep at the top.">
        {isAdmin && (
          <Button variant="outline" onClick={() => setEditing({ official: true })}>
            New official class
          </Button>
        )}
        {account && <Button onClick={() => setEditing({})}>New class</Button>}
      </PageHeader>
      {classes.length === 0 && <Empty>No classes yet.</Empty>}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {sorted.map((c) => (
          <Card key={c.id}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Link to={`/problems?class_id=${c.id}`} className="truncate hover:underline">
                  {c.name}
                </Link>
                {account && (
                  <button onClick={() => toggle(c)} aria-label={c.collected ? "Remove star" : "Star"} className="ml-auto shrink-0">
                    <Star className={cn("size-4", c.collected ? "fill-yellow-400 text-yellow-500" : "text-muted-foreground")} />
                  </button>
                )}
              </CardTitle>
              <CardDescription className="flex flex-wrap items-center gap-2">
                {c.owner_id === null ? <Badge variant="secondary">Official</Badge> : <span>by {c.owner_name}</span>}
                {!c.public && <Badge variant="outline">Private</Badge>}
                <span>{c.problem_count} problems</span>
              </CardDescription>
            </CardHeader>
            {c.description && <CardContent className="text-muted-foreground text-sm break-words whitespace-pre-wrap">{c.description}</CardContent>}
            {canEdit(c) && (
              <CardFooter className="mt-auto">
                <Button variant="outline" size="sm" onClick={() => setEditing(c)}>
                  Edit
                </Button>
                <ConfirmButton
                  title={`Delete "${c.name}"?`}
                  onConfirm={async () => {
                    await act(() => api(`/classes/${c.id}`, { method: "DELETE" }), "Class deleted");
                    revalidator.revalidate();
                  }}
                >
                  Delete
                </ConfirmButton>
              </CardFooter>
            )}
          </Card>
        ))}
      </div>
      {editing && (
        <ClassDialog
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
