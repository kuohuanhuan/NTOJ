import { Form, useSearchParams } from "react-router";

import { LogTable } from "../contest/manage";
import type { Route } from "./+types/logs";
import { PageHeader } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { load, qs } from "~/lib/api";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  const sp = new URL(request.url).searchParams;
  return load("/admin/logs" + qs({ type: sp.get("type"), page: sp.get("page") }), request);
}

export default function AdminLogs({ loaderData }: Route.ComponentProps) {
  const [params] = useSearchParams();
  return (
    <>
      <PageHeader title="Audit log">
        <Form method="get" className="flex gap-2">
          <Input name="type" placeholder="Type prefix, e.g. signin or manage." defaultValue={params.get("type") ?? ""} className="w-64" />
          <Button type="submit" variant="secondary">
            Filter
          </Button>
        </Form>
      </PageHeader>
      <LogTable logs={loaderData} />
    </>
  );
}
