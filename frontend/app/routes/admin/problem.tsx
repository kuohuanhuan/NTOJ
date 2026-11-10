import { useState } from "react";
import { Link, useNavigate, useRevalidator } from "react-router";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { Route } from "./+types/problem";
import { ConfirmButton, Empty, Field, PageHeader } from "~/components/common";
import { Markdown } from "~/components/markdown";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Switch } from "~/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "~/components/ui/tabs";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load } from "~/lib/api";
import { fmtSize, parseIdList } from "~/lib/format";
import { useSession } from "~/lib/session";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load(`/admin/problems/${params.id}`, request);
}

const CHECKERS = {
  diff: "Compare tokens (ignore whitespace)",
  "diff-strict": "Exact match",
  "float-4": "Floating point, error 1e-4",
  "float-6": "Floating point, error 1e-6",
  "float-9": "Floating point, error 1e-9",
  testlib: "Custom testlib checker",
};

function GeneralTab({ p, base, reload }: { p: any; base: string; reload: () => void }) {
  const { site } = useSession();
  const [allowSubmit, setAllowSubmit] = useState<boolean>(p.allow_submit);
  const [languages, setLanguages] = useState<string[]>(p.languages);
  const save = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const body = {
      name: f.get("name"),
      status: f.get("status"),
      checker: f.get("checker"),
      allow_submit: allowSubmit,
      languages,
      tags: String(f.get("tags")).split(",").map((t) => t.trim()).filter(Boolean),
      time_limit_ms: Number(f.get("time")),
      memory_limit_kb: Number(f.get("memory")) * 1024,
      score_precision: Number(f.get("precision")),
    };
    if (await act(() => api(base, { method: "PATCH", body }), "Problem saved")) reload();
  };
  return (
    <form onSubmit={save} className="grid gap-4 sm:grid-cols-2">
      <Field label="Name" className="sm:col-span-2">
        <Input name="name" defaultValue={p.name} maxLength={64} required />
      </Field>
      <Field label="Visibility" hint="Contest: reachable only through contests that include it.">
        <NativeSelect name="status" defaultValue={p.status}>
          <NativeSelectOption value="online">Online (public)</NativeSelectOption>
          <NativeSelectOption value="contest">Contest only</NativeSelectOption>
          <NativeSelectOption value="hidden">Hidden (administrators only)</NativeSelectOption>
        </NativeSelect>
      </Field>
      <Field label="Checker">
        <NativeSelect name="checker" defaultValue={p.checker}>
          {Object.entries(CHECKERS).map(([id, label]) => (
            <NativeSelectOption key={id} value={id}>
              {label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </Field>
      <Field label="Time limit (ms)">
        <Input name="time" type="number" min={1} max={600000} defaultValue={p.time_limit_ms} required />
      </Field>
      <Field label="Memory limit (MiB)">
        <Input name="memory" type="number" min={1} max={16384} defaultValue={Math.round(p.memory_limit_kb / 1024)} required />
      </Field>
      <Field label="Score decimals">
        <Input name="precision" type="number" min={0} max={3} defaultValue={p.score_precision} required />
      </Field>
      <Field label="Tags" hint="Comma separated.">
        <Input name="tags" defaultValue={p.tags.join(", ")} />
      </Field>
      <Field label="Allowed languages" hint="Leave everything unchecked to allow every language." className="sm:col-span-2">
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          {site.languages.map((l) => (
            <Label key={l.id} className="font-normal">
              <input
                type="checkbox"
                checked={languages.includes(l.id)}
                onChange={(e) => setLanguages(e.target.checked ? [...languages, l.id] : languages.filter((x) => x !== l.id))}
              />
              {l.name}
            </Label>
          ))}
        </div>
      </Field>
      <Label className="sm:col-span-2">
        <Switch checked={allowSubmit} onCheckedChange={setAllowSubmit} /> Accept submissions
      </Label>
      <div className="sm:col-span-2">
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}

function StatementTab({ p, base, reload }: { p: any; base: string; reload: () => void }) {
  const [text, setText] = useState<string>(p.statement);
  const save = async () => {
    if (await act(() => api(base, { method: "PATCH", body: { statement: text } }), "Statement saved")) reload();
  };
  return (
    <div className="grid gap-4">
      <p className="text-muted-foreground text-sm">
        Markdown with GitHub tables and LaTeX math (<code>$x^2$</code>). Reference attachments as{" "}
        <code>/api/problems/{p.id}/files/NAME</code>.
      </p>
      <div className="grid gap-4 lg:grid-cols-2">
        <Textarea value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} className="min-h-[28rem] font-mono text-xs md:text-xs" />
        <Card className="max-h-[40rem] overflow-auto">
          <CardContent>
            <Markdown>{text}</Markdown>
          </CardContent>
        </Card>
      </div>
      <div>
        <Button onClick={save}>Save statement</Button>
      </div>
    </div>
  );
}

function Upload({ url, accept, hint, reload }: { url: string; accept?: string; hint: string; reload: () => void }) {
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formEl = e.currentTarget;
    const input = formEl.elements.namedItem("files") as HTMLInputElement;
    if (!input.files?.length) return;
    const form = new FormData();
    for (const file of input.files) form.append("files", file, file.name);
    setBusy(true);
    const ok = await act(() => api(url, { form }), "Uploaded");
    setBusy(false);
    if (ok) {
      formEl.reset();
      reload();
    }
  };
  return (
    <form onSubmit={submit} className="grid gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Input name="files" type="file" multiple accept={accept} required className="w-80" />
        <Button type="submit" variant="outline" disabled={busy}>
          {busy ? "Uploading…" : "Upload"}
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">{hint}</p>
    </form>
  );
}

function TestdataTab({ p, base, reload }: { p: any; base: string; reload: () => void }) {
  return (
    <div className="grid gap-4">
      <Upload url={`${base}/testcases`} accept=".in,.out,.ans" reload={reload} hint="Select pairs named NAME.in and NAME.out (or .ans). Uploading an existing name replaces that file." />
      {p.testcases.length === 0 ? (
        <Empty>No test cases yet.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Name</TableHead>
              <TableHead className="text-right">Input</TableHead>
              <TableHead className="text-right">Output</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.testcases.map((t: any) => (
              <TableRow key={t.id}>
                <TableCell className="tabular-nums">{t.id}</TableCell>
                <TableCell className="font-medium">{t.name}</TableCell>
                {(["in", "out"] as const).map((kind) => (
                  <TableCell key={kind} className="text-right tabular-nums">
                    <a href={`/api${base}/testcases/${t.id}/${kind}`} className="hover:underline">
                      {fmtSize(kind === "in" ? t.input_size : t.output_size)}
                    </a>
                  </TableCell>
                ))}
                <TableCell className="text-right">
                  <ConfirmButton
                    title={`Delete test case "${t.name}"?`}
                    description="It is also removed from every subtask."
                    variant="ghost"
                    onConfirm={async () => {
                      if (await act(() => api(`${base}/testcases/${t.id}`, { method: "DELETE" }), "Deleted")) reload();
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
    </div>
  );
}

function SubtasksTab({ p, base, reload }: { p: any; base: string; reload: () => void }) {
  const idToName = new Map<number, string>(p.testcases.map((t: any) => [t.id, t.name]));
  const nameToId = new Map<string, number>(p.testcases.map((t: any) => [t.name, t.id]));
  const [rows, setRows] = useState<{ score: string; cases: string; depends: string }[]>(
    p.subtasks.map((s: any) => ({
      score: String(s.score),
      cases: s.testcase_ids.map((id: number) => idToName.get(id) ?? id).join(", "),
      depends: s.depends.join(", "),
    })),
  );
  const update = (i: number, patch: object) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const total = rows.reduce((sum, r) => sum + (Number(r.score) || 0), 0);

  const save = async () => {
    const body: { score: number; testcase_ids: number[]; depends: number[] }[] = [];
    for (const [i, r] of rows.entries()) {
      const ids: number[] = [];
      for (const name of r.cases.split(/[\s,]+/).filter(Boolean)) {
        const id = nameToId.get(name);
        if (id === undefined) return void toast.error(`Subtask ${i + 1}: unknown test case "${name}"`);
        ids.push(id);
      }
      const depends = parseIdList(r.depends);
      if (!depends || !Number.isFinite(Number(r.score))) return void toast.error(`Subtask ${i + 1}: invalid score or dependencies`);
      body.push({ score: Number(r.score), testcase_ids: ids, depends });
    }
    if (await act(() => api(`${base}/subtasks`, { method: "PUT", body }), "Subtasks saved")) reload();
  };

  return (
    <div className="grid gap-4">
      <p className="text-muted-foreground text-sm">
        A subtask awards its score only when every listed test case passes. With no subtasks, all test cases form a single 100-point group.
        Changing the configuration does not rescore old submissions; rejudge the problem for that.
      </p>
      {rows.map((r, i) => (
        <Card key={i} className="py-4">
          <CardContent className="grid gap-3 sm:grid-cols-[6rem_minmax(0,1fr)_10rem_auto]">
            <Field label={`#${i + 1} score`}>
              <Input type="number" min={0} step="any" value={r.score} onChange={(e) => update(i, { score: e.target.value })} />
            </Field>
            <Field label="Test cases (names)">
              <Input value={r.cases} onChange={(e) => update(i, { cases: e.target.value })} placeholder="1, 2, 3" />
            </Field>
            <Field label="Depends on">
              <Input value={r.depends} onChange={(e) => update(i, { depends: e.target.value })} placeholder="e.g. 1" />
            </Field>
            <Button variant="ghost" size="icon" className="self-end" onClick={() => setRows(rows.filter((_, j) => j !== i))} aria-label="Remove subtask">
              <Trash2 />
            </Button>
          </CardContent>
        </Card>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" onClick={() => setRows([...rows, { score: "0", cases: "", depends: "" }])}>
          <Plus /> Add subtask
        </Button>
        <Button variant="outline" onClick={() => setRows([{ score: "100", cases: p.testcases.map((t: any) => t.name).join(", "), depends: "" }])}>
          One group with all tests
        </Button>
        <Button onClick={save}>Save subtasks</Button>
        <span className="text-muted-foreground text-sm">Total: {total} points</span>
      </div>
    </div>
  );
}

function FilesTab({ p, base, reload }: { p: any; base: string; reload: () => void }) {
  return (
    <div className="grid gap-4">
      <Upload url={`${base}/files`} reload={reload} hint="Images and PDFs open in the browser; every other type is offered as a download. Anyone who can read the problem can fetch these files." />
      {p.files.length === 0 ? (
        <Empty>No attachments.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.files.map((f: any) => (
              <TableRow key={f.name}>
                <TableCell>
                  <a href={`/api/problems/${p.id}/files/${encodeURIComponent(f.name)}`} target="_blank" rel="noopener noreferrer" className="hover:underline">
                    {f.name}
                  </a>
                </TableCell>
                <TableCell className="text-right tabular-nums">{fmtSize(f.size)}</TableCell>
                <TableCell className="text-right">
                  <ConfirmButton
                    title={`Delete "${f.name}"?`}
                    variant="ghost"
                    onConfirm={async () => {
                      if (await act(() => api(`${base}/files/${encodeURIComponent(f.name)}`, { method: "DELETE" }), "Deleted")) reload();
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
    </div>
  );
}

export default function AdminProblem({ loaderData: p }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const navigate = useNavigate();
  const base = `/admin/problems/${p.id}`;
  const reload = () => void revalidator.revalidate();
  return (
    <>
      <PageHeader title={`#${p.id} ${p.name}`} description={`Status: ${p.status}`}>
        <Button asChild variant="outline" size="sm">
          <Link to={`/problems/${p.id}`}>View</Link>
        </Button>
      </PageHeader>
      <Tabs defaultValue="general">
        <TabsList>
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="statement">Statement</TabsTrigger>
          <TabsTrigger value="testdata">Test data ({p.testcases.length})</TabsTrigger>
          <TabsTrigger value="subtasks">Subtasks ({p.subtasks.length})</TabsTrigger>
          <TabsTrigger value="files">Attachments ({p.files.length})</TabsTrigger>
          <TabsTrigger value="danger">Maintenance</TabsTrigger>
        </TabsList>
        <TabsContent value="general">
          <GeneralTab key={p.updated_at} p={p} base={base} reload={reload} />
        </TabsContent>
        <TabsContent value="statement">
          <StatementTab p={p} base={base} reload={reload} />
        </TabsContent>
        <TabsContent value="testdata">
          <TestdataTab p={p} base={base} reload={reload} />
        </TabsContent>
        <TabsContent value="subtasks">
          <SubtasksTab key={JSON.stringify([p.subtasks, p.testcases.length])} p={p} base={base} reload={reload} />
        </TabsContent>
        <TabsContent value="files">
          <FilesTab p={p} base={base} reload={reload} />
        </TabsContent>
        <TabsContent value="danger" className="grid gap-4">
          {p.contests.length > 0 && (
            <p className="text-sm">
              Used in:{" "}
              {p.contests.map((c: any, i: number) => (
                <span key={c.id}>
                  {i > 0 && ", "}
                  <Link to={`/contests/${c.id}`} className="underline">
                    {c.name}
                  </Link>
                </span>
              ))}
            </p>
          )}
          <Card>
            <CardHeader>
              <CardTitle>Rejudge</CardTitle>
              <CardDescription>Sends every submission of this problem back to the judge queue at low priority.</CardDescription>
            </CardHeader>
            <CardContent>
              <ConfirmButton title="Rejudge all submissions?" description="Scores are recomputed with the current test data and subtasks." variant="outline" onConfirm={() => act(() => api(`${base}/rejudge`, { method: "POST" }), "Queued for rejudge")}>
                Rejudge all submissions
              </ConfirmButton>
            </CardContent>
          </Card>
          <Card className="border-destructive/40">
            <CardHeader>
              <CardTitle>Delete problem</CardTitle>
              <CardDescription>Removes the problem, its test data, attachments and every submission to it.</CardDescription>
            </CardHeader>
            <CardContent>
              <ConfirmButton
                title={`Delete "${p.name}"?`}
                confirmLabel="Delete"
                onConfirm={async () => {
                  if (await act(() => api(base, { method: "DELETE" }), "Problem deleted")) navigate("/admin/problems");
                }}
              >
                Delete problem
              </ConfirmButton>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </>
  );
}
