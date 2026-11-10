import { useState } from "react";
import { useRevalidator } from "react-router";
import { toast } from "sonner";

import type { Route } from "./+types/settings";
import { ConfirmButton, Field, PageHeader } from "~/components/common";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load } from "~/lib/api";
import { fmtTime } from "~/lib/format";
import { useSession } from "~/lib/session";

interface SessionRow {
  id: number;
  ip: string;
  user_agent: string;
  created_at: string;
  last_seen_at: string;
  current: boolean;
}

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<SessionRow[]>("/account/sessions", request);
}

export default function Settings({ loaderData: sessions }: Route.ComponentProps) {
  const { account } = useSession();
  const revalidator = useRevalidator();
  const [busy, setBusy] = useState(false);
  if (!account) return null;

  const saveProfile = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    setBusy(true);
    await act(
      () =>
        api("/account", {
          method: "PATCH",
          body: { name: f.get("name"), motto: f.get("motto"), photo_url: f.get("photo_url"), cover_url: f.get("cover_url") },
        }),
      "Profile saved",
    );
    setBusy(false);
    revalidator.revalidate();
  };

  const changePassword = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    if (f.get("new") !== f.get("confirm")) return void toast.error("The new passwords do not match");
    if (await act(() => api("/account/password", { body: { old: f.get("old"), new: f.get("new") } }), "Password changed; other devices were signed out")) {
      form.reset();
      revalidator.revalidate();
    }
  };

  const revoke = async (id?: number) => {
    await act(() => api("/account/sessions" + (id ? `/${id}` : ""), { method: "DELETE" }), "Signed out");
    revalidator.revalidate();
  };

  return (
    <>
      <PageHeader title="Settings" />
      <div className="grid gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Profile</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={saveProfile} className="grid max-w-xl gap-4">
              <Field label="Display name">
                <Input name="name" defaultValue={account.name} maxLength={32} required />
              </Field>
              <Field label="Motto">
                <Input name="motto" defaultValue={account.motto} maxLength={100} />
              </Field>
              <Field label="Photo URL" hint="Must be an https:// link.">
                <Input name="photo_url" type="url" pattern="https://.*" defaultValue={account.photo_url} />
              </Field>
              <Field label="Cover URL" hint="Must be an https:// link.">
                <Input name="cover_url" type="url" pattern="https://.*" defaultValue={account.cover_url} />
              </Field>
              <div>
                <Button type="submit" disabled={busy}>
                  Save
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Password</CardTitle>
            <CardDescription>Changing your password signs out every other device.</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={changePassword} className="grid max-w-xl gap-4">
              <Field label="Current password">
                <Input name="old" type="password" autoComplete="current-password" required />
              </Field>
              <Field label="New password" hint="At least 8 characters.">
                <Input name="new" type="password" minLength={8} autoComplete="new-password" required />
              </Field>
              <Field label="Confirm new password">
                <Input name="confirm" type="password" minLength={8} autoComplete="new-password" required />
              </Field>
              <div>
                <Button type="submit">Change password</Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex-row items-start justify-between gap-4">
            <div className="grid gap-1.5">
              <CardTitle>Signed-in devices</CardTitle>
              <CardDescription>Sign out sessions you do not recognise.</CardDescription>
            </div>
            <ConfirmButton title="Sign out all other devices?" description="Only this browser stays signed in." onConfirm={() => revoke()} variant="outline">
              Sign out others
            </ConfirmButton>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>IP address</TableHead>
                  <TableHead>Browser</TableHead>
                  <TableHead>Signed in</TableHead>
                  <TableHead>Last active</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-mono text-xs">{s.ip}</TableCell>
                    <TableCell className="max-w-72 truncate" title={s.user_agent}>
                      {s.user_agent || "Unknown"}
                    </TableCell>
                    <TableCell>{fmtTime(s.created_at)}</TableCell>
                    <TableCell>{fmtTime(s.last_seen_at)}</TableCell>
                    <TableCell className="text-right">
                      {s.current ? (
                        <Badge variant="secondary">This device</Badge>
                      ) : (
                        <Button variant="outline" size="sm" onClick={() => revoke(s.id)}>
                          Sign out
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </>
  );
}
