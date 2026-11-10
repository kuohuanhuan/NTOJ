import { useState } from "react";
import { useNavigate, useRevalidator } from "react-router";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";

import type { Route } from "./+types/manage";
import { ConfirmButton, Empty, Field, Pagination } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "~/components/ui/tabs";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load, qs } from "~/lib/api";
import { fmtTime, fromLocalInput, toLocalInput } from "~/lib/format";
import { useSession } from "~/lib/session";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  const page = new URL(request.url).searchParams.get("page");
  const [manage, logs] = await Promise.all([
    load(`/contests/${params.cid}/manage`, request),
    load(`/contests/${params.cid}/manage/logs` + qs({ page }), request),
  ]);
  return { ...manage, logs };
}

function Toggle({ label, hint, checked, onChange }: { label: string; hint?: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex items-start gap-3">
      <Switch checked={checked} onCheckedChange={onChange} className="mt-0.5" />
      <div className="grid gap-0.5">
        <Label>{label}</Label>
        {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
      </div>
    </div>
  );
}

function SettingsTab({ contest, save }: { contest: any; save: (body: object, message: string) => Promise<boolean> }) {
  const { site } = useSession();
  const [c, setC] = useState({ ...contest, start_at: toLocalInput(contest.start_at), end_at: toLocalInput(contest.end_at), reg_end_at: toLocalInput(contest.reg_end_at) });
  const set = (patch: object) => setC((prev: any) => ({ ...prev, ...patch }));
  const num = (key: string) => (e: React.ChangeEvent<HTMLInputElement>) => set({ [key]: Number(e.target.value) });
  const toggleLanguage = (id: string, on: boolean) => set({ languages: on ? [...c.languages, id] : c.languages.filter((l: string) => l !== id) });

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const { id, creator_id, desc_before, desc_during, desc_after, ...settings } = c;
    save({ ...settings, start_at: fromLocalInput(c.start_at), end_at: fromLocalInput(c.end_at), reg_end_at: fromLocalInput(c.reg_end_at) }, "Settings saved");
  };

  return (
    <form onSubmit={submit} className="grid gap-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Field label="Name" className="sm:col-span-2 lg:col-span-3">
          <Input value={c.name} onChange={(e) => set({ name: e.target.value })} maxLength={64} required />
        </Field>
        <Field label="Format">
          <NativeSelect value={c.mode} onChange={(e) => set({ mode: e.target.value })}>
            <NativeSelectOption value="ioi">IOI (partial scores)</NativeSelectOption>
            <NativeSelectOption value="acm">ICPC (solved count and penalty)</NativeSelectOption>
          </NativeSelect>
        </Field>
        <Field label="Opens">
          <Input type="datetime-local" value={c.start_at} onChange={(e) => set({ start_at: e.target.value })} required />
        </Field>
        <Field label="Closes">
          <Input type="datetime-local" value={c.end_at} onChange={(e) => set({ end_at: e.target.value })} required />
        </Field>
        <Field label="Timing" hint="Flexible: each contestant starts their own timer inside the window.">
          <NativeSelect value={c.time_mode} onChange={(e) => set({ time_mode: e.target.value })}>
            <NativeSelectOption value="fixed">Fixed window for everyone</NativeSelectOption>
            <NativeSelectOption value="flexible">Flexible start</NativeSelectOption>
          </NativeSelect>
        </Field>
        {c.time_mode === "flexible" && (
          <Field label="Duration per contestant (minutes)">
            <Input type="number" min={1} value={Math.round(c.duration_sec / 60)} onChange={(e) => set({ duration_sec: Number(e.target.value) * 60 })} required />
          </Field>
        )}
        <Field label="Registration">
          <NativeSelect value={c.reg_mode} onChange={(e) => set({ reg_mode: e.target.value })}>
            <NativeSelectOption value="invited">By invitation only</NativeSelectOption>
            <NativeSelectOption value="free">Anyone may register</NativeSelectOption>
            <NativeSelectOption value="approval">Registration needs approval</NativeSelectOption>
          </NativeSelect>
        </Field>
        <Field label="Registration closes">
          <Input type="datetime-local" value={c.reg_end_at} onChange={(e) => set({ reg_end_at: e.target.value })} required />
        </Field>
        <Field label="Seconds between submissions">
          <Input type="number" min={0} max={86400} value={c.cooldown_sec} onChange={num("cooldown_sec")} required />
        </Field>
        <Field label="Scoreboard freeze (minutes before the end)">
          <Input type="number" min={0} value={c.freeze_minutes} onChange={num("freeze_minutes")} required />
        </Field>
        {c.mode === "acm" && (
          <Field label="Penalty per rejected run (minutes)">
            <Input type="number" min={0} value={c.penalty_minutes} onChange={num("penalty_minutes")} required />
          </Field>
        )}
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Toggle label="Public scoreboard" hint="Otherwise only contestants can see it, and submissions stay private afterwards." checked={c.public_scoreboard} onChange={(v) => set({ public_scoreboard: v })} />
        <Toggle label="Hide managers from the scoreboard" checked={c.hide_admin} onChange={(v) => set({ hide_admin: v })} />
        <Toggle label="Keep the scoreboard frozen after the end" hint="Turn off to reveal the final standings." checked={c.keep_frozen} onChange={(v) => set({ keep_frozen: v })} />
        <Toggle label="Allow other pages during the contest" hint="When off, contestants cannot browse the public problem set or submissions while their contest is running." checked={c.allow_other_pages} onChange={(v) => set({ allow_other_pages: v })} />
      </div>
      <Field label="Allowed languages" hint="Leave everything unchecked to allow every language.">
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          {site.languages.map((l) => (
            <Label key={l.id} className="font-normal">
              <input type="checkbox" checked={c.languages.includes(l.id)} onChange={(e) => toggleLanguage(l.id, e.target.checked)} />
              {l.name}
            </Label>
          ))}
        </div>
      </Field>
      <div>
        <Button type="submit">Save settings</Button>
      </div>
    </form>
  );
}

function DescriptionsTab({ contest, save }: { contest: any; save: (body: object, message: string) => Promise<boolean> }) {
  const submit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    save({ desc_before: f.get("before"), desc_during: f.get("during"), desc_after: f.get("after") }, "Descriptions saved");
  };
  return (
    <form onSubmit={submit} className="grid gap-4">
      <p className="text-muted-foreground text-sm">Markdown. Each text is shown on the overview page during the matching phase.</p>
      <Field label="Before the contest">
        <Textarea name="before" defaultValue={contest.desc_before} className="min-h-32 font-mono text-xs md:text-xs" />
      </Field>
      <Field label="During the contest">
        <Textarea name="during" defaultValue={contest.desc_during} className="min-h-32 font-mono text-xs md:text-xs" />
      </Field>
      <Field label="After the contest">
        <Textarea name="after" defaultValue={contest.desc_after} className="min-h-32 font-mono text-xs md:text-xs" />
      </Field>
      <div>
        <Button type="submit">Save descriptions</Button>
      </div>
    </form>
  );
}

function ProblemsTab({ base, initial, reload }: { base: string; initial: any[]; reload: () => void }) {
  const [rows, setRows] = useState(initial);
  const [adding, setAdding] = useState("");
  const update = (i: number, patch: object) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const move = (i: number, d: number) => {
    const next = [...rows];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    setRows(next);
  };
  const add = (e: React.FormEvent) => {
    e.preventDefault();
    const id = Number(adding);
    if (!Number.isInteger(id) || id <= 0 || rows.some((r) => r.problem_id === id)) return;
    setRows([...rows, { problem_id: id, name: "(new)", score_type: "best", result_style: "full" }]);
    setAdding("");
  };
  const save = async () => {
    const body = rows.map(({ problem_id, score_type, result_style }) => ({ problem_id, score_type, result_style }));
    if (await act(() => api(`${base}/manage/problems`, { method: "PUT", body }), "Problems saved")) reload();
  };
  const rejudge = async (problem_id: number | null) => {
    await act(() => api(`${base}/manage/rejudge`, { body: { problem_id } }), "Queued for rejudge");
  };

  return (
    <div className="grid gap-4">
      {rows.length === 0 ? (
        <Empty>No problems yet.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Order</TableHead>
              <TableHead>Problem</TableHead>
              <TableHead>Scoring</TableHead>
              <TableHead>Results shown while running</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r, i) => (
              <TableRow key={r.problem_id}>
                <TableCell>
                  <Button variant="ghost" size="icon" disabled={i === 0} onClick={() => move(i, -1)} aria-label="Move up">
                    <ArrowUp />
                  </Button>
                  <Button variant="ghost" size="icon" disabled={i === rows.length - 1} onClick={() => move(i, 1)} aria-label="Move down">
                    <ArrowDown />
                  </Button>
                </TableCell>
                <TableCell>
                  #{r.problem_id} {r.name} {r.status && <span className="text-muted-foreground">({r.status})</span>}
                </TableCell>
                <TableCell>
                  <NativeSelect value={r.score_type} onChange={(e) => update(i, { score_type: e.target.value })}>
                    <NativeSelectOption value="best">Best submission</NativeSelectOption>
                    <NativeSelectOption value="subtask_union">Best of each subtask</NativeSelectOption>
                  </NativeSelect>
                </TableCell>
                <TableCell>
                  <NativeSelect value={r.result_style} onChange={(e) => update(i, { result_style: e.target.value })}>
                    <NativeSelectOption value="full">Everything</NativeSelectOption>
                    <NativeSelectOption value="subtask">Subtasks only</NativeSelectOption>
                    <NativeSelectOption value="total">Total only</NativeSelectOption>
                  </NativeSelect>
                </TableCell>
                <TableCell className="text-right">
                  <Button variant="ghost" size="sm" onClick={() => rejudge(r.problem_id)}>
                    Rejudge
                  </Button>
                  <Button variant="ghost" size="icon" onClick={() => setRows(rows.filter((_, j) => j !== i))} aria-label="Remove">
                    <Trash2 />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <form onSubmit={add} className="flex gap-2">
          <Input value={adding} onChange={(e) => setAdding(e.target.value)} placeholder="Problem ID" inputMode="numeric" className="w-32" />
          <Button type="submit" variant="outline">
            Add
          </Button>
        </form>
        <Button onClick={save}>Save problems</Button>
        <ConfirmButton title="Rejudge every submission of this contest?" description="Results are recomputed; the scoreboard changes while the queue drains." variant="outline" onConfirm={() => rejudge(null)}>
          Rejudge all
        </ConfirmButton>
      </div>
      <p className="text-muted-foreground text-xs">
        Problems marked hidden are never shown to contestants. Use the "contest" status for problems that should only be reachable through a contest.
      </p>
    </div>
  );
}

function MembersTab({ base, users, flexible, canManageAdmins, reload }: { base: string; users: any[]; flexible: boolean; canManageAdmins: boolean; reload: () => void }) {
  const [adding, setAdding] = useState("");
  const [status, setStatus] = useState("approved");
  const setUser = async (id: number, s: string) => {
    if (await act(() => api(`${base}/manage/users/${id}`, { method: "PUT", body: { status: s } }), "Saved")) reload();
  };
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    const ids = adding.split(/[\s,]+/).filter(Boolean).map(Number);
    if (ids.length === 0 || ids.some((n) => !Number.isInteger(n) || n <= 0)) return;
    for (const id of ids) {
      if (!(await act(() => api(`${base}/manage/users/${id}`, { method: "PUT", body: { status } })))) break;
    }
    setAdding("");
    reload();
  };
  const pending = users.filter((u) => u.status === "requested").length;

  return (
    <div className="grid gap-4">
      <form onSubmit={add} className="flex flex-wrap gap-2">
        <Input value={adding} onChange={(e) => setAdding(e.target.value)} placeholder="Account IDs, e.g. 12, 15, 20" className="w-64" />
        <NativeSelect value={status} onChange={(e) => setStatus(e.target.value)}>
          <NativeSelectOption value="approved">as contestants</NativeSelectOption>
          {canManageAdmins && <NativeSelectOption value="admin">as managers</NativeSelectOption>}
        </NativeSelect>
        <Button type="submit" variant="outline">
          Add
        </Button>
      </form>
      {pending > 0 && <p className="text-sm">{pending} registration(s) waiting for approval.</p>}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Status</TableHead>
            {flexible && <TableHead>Session</TableHead>}
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((u) => {
            const locked = u.status === "admin" && !canManageAdmins;
            return (
              <TableRow key={u.account_id}>
                <TableCell className="tabular-nums">{u.account_id}</TableCell>
                <TableCell className="max-w-56 truncate">{u.name}</TableCell>
                <TableCell>
                  <NativeSelect value={u.status} disabled={locked} onChange={(e) => setUser(u.account_id, e.target.value)}>
                    {u.status === "requested" && <NativeSelectOption value="requested">Requested</NativeSelectOption>}
                    <NativeSelectOption value="approved">Contestant</NativeSelectOption>
                    <NativeSelectOption value="rejected">Rejected</NativeSelectOption>
                    {(canManageAdmins || u.status === "admin") && <NativeSelectOption value="admin">Manager</NativeSelectOption>}
                  </NativeSelect>
                </TableCell>
                {flexible && (
                  <TableCell className="text-muted-foreground text-xs">
                    {u.session_start ? `${fmtTime(u.session_start)} – ${fmtTime(u.session_end)}` : "not started"}
                  </TableCell>
                )}
                <TableCell className="text-right">
                  {flexible && u.session_start && (
                    <ConfirmButton
                      title={`Reset ${u.name}'s session?`}
                      description="They will be able to start the contest again with a full timer."
                      variant="ghost"
                      onConfirm={async () => {
                        if (await act(() => api(`${base}/manage/users/${u.account_id}/reset-session`, { method: "POST" }), "Session reset")) reload();
                      }}
                    >
                      Reset session
                    </ConfirmButton>
                  )}
                  {!locked && (
                    <ConfirmButton
                      title={`Remove ${u.name} from the contest?`}
                      variant="ghost"
                      onConfirm={async () => {
                        if (await act(() => api(`${base}/manage/users/${u.account_id}`, { method: "DELETE" }), "Removed")) reload();
                      }}
                    >
                      Remove
                    </ConfirmButton>
                  )}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export default function ManageContest({ loaderData, params }: Route.ComponentProps) {
  const { contest, problems, users, logs, can_manage_admins } = loaderData;
  const { account } = useSession();
  const revalidator = useRevalidator();
  const navigate = useNavigate();
  const base = `/contests/${params.cid}`;
  const reload = () => void revalidator.revalidate();
  const save = async (body: object, message: string) => {
    const ok = await act(() => api(base, { method: "PATCH", body }), message);
    if (ok) reload();
    return ok;
  };

  return (
    <Tabs defaultValue="settings">
      <TabsList>
        <TabsTrigger value="settings">Settings</TabsTrigger>
        <TabsTrigger value="descriptions">Descriptions</TabsTrigger>
        <TabsTrigger value="problems">Problems</TabsTrigger>
        <TabsTrigger value="members">Members</TabsTrigger>
        <TabsTrigger value="log">Log</TabsTrigger>
      </TabsList>
      <TabsContent value="settings" className="grid gap-6">
        <SettingsTab contest={contest} save={save} />
        {account?.role === "admin" && (
          <Card className="border-destructive/40">
            <CardHeader>
              <CardTitle>Delete contest</CardTitle>
              <CardDescription>Removes the contest with all its submissions, questions and announcements.</CardDescription>
            </CardHeader>
            <CardContent>
              <ConfirmButton
                title={`Delete "${contest.name}"?`}
                confirmLabel="Delete"
                onConfirm={async () => {
                  if (await act(() => api(base, { method: "DELETE" }), "Contest deleted")) navigate("/contests");
                }}
              >
                Delete contest
              </ConfirmButton>
            </CardContent>
          </Card>
        )}
      </TabsContent>
      <TabsContent value="descriptions">
        <DescriptionsTab contest={contest} save={save} />
      </TabsContent>
      <TabsContent value="problems">
        <ProblemsTab key={JSON.stringify(problems)} base={base} initial={problems} reload={reload} />
      </TabsContent>
      <TabsContent value="members">
        <MembersTab base={base} users={users} flexible={contest.time_mode === "flexible"} canManageAdmins={can_manage_admins} reload={reload} />
      </TabsContent>
      <TabsContent value="log">
        <LogTable logs={logs} />
      </TabsContent>
    </Tabs>
  );
}

export function LogTable({ logs }: { logs: any }) {
  if (logs.items.length === 0) return <Empty>Nothing logged.</Empty>;
  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>
            <TableHead>Type</TableHead>
            <TableHead>Operator</TableHead>
            <TableHead>IP</TableHead>
            <TableHead>Message</TableHead>
            <TableHead>Details</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {logs.items.map((l: any) => (
            <TableRow key={l.id}>
              <TableCell className="text-muted-foreground">{fmtTime(l.created_at)}</TableCell>
              <TableCell className="font-mono text-xs">{l.type}</TableCell>
              <TableCell>{l.operator_name ? `${l.operator_name} (#${l.operator_id})` : "—"}</TableCell>
              <TableCell className="font-mono text-xs">{l.operator_ip}</TableCell>
              <TableCell>{l.message}</TableCell>
              <TableCell className="text-muted-foreground max-w-96 truncate font-mono text-xs" title={JSON.stringify(l.params)}>
                {Object.keys(l.params).length ? JSON.stringify(l.params) : ""}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pagination total={logs.total} page={logs.page} perPage={logs.per_page} />
    </>
  );
}
