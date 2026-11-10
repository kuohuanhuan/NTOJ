import { useState } from "react";
import { Link, redirect, useNavigate, useRevalidator, useSearchParams } from "react-router";

import { Field } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { act, api } from "~/lib/api";
import { useSession, type Session } from "~/lib/session";

/** Only same-site paths are accepted as a post-login destination. */
export function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") && !next.includes("\\") ? next : "/";
}

export async function clientLoader() {
  const session = await api<Session>("/session");
  if (session.account) throw redirect("/");
  return null;
}

export default function SignIn() {
  const { site } = useSession();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const revalidator = useRevalidator();
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    setBusy(true);
    const ok = await act(() => api("/auth/signin", { body: { email: form.get("email"), password: form.get("password") } }));
    setBusy(false);
    if (ok) {
      await revalidator.revalidate();
      navigate(safeNext(params.get("next")), { replace: true });
    }
  };

  return (
    <Card className="mx-auto mt-10 max-w-sm">
      <CardHeader>
        <CardTitle className="text-xl">Sign in</CardTitle>
      </CardHeader>
      <form onSubmit={submit}>
        <CardContent className="grid gap-4">
          <Field label="Email">
            <Input name="email" type="email" autoComplete="username" required autoFocus />
          </Field>
          <Field label="Password">
            <Input name="password" type="password" autoComplete="current-password" required />
          </Field>
        </CardContent>
        <CardFooter className="mt-5 flex-col items-stretch gap-3">
          <Button type="submit" disabled={busy}>
            Sign in
          </Button>
          {site.allow_signup && (
            <p className="text-muted-foreground text-center text-sm">
              No account?{" "}
              <Link to={"/signup?" + params.toString()} className="text-foreground underline">
                Sign up
              </Link>
            </p>
          )}
        </CardFooter>
      </form>
    </Card>
  );
}
