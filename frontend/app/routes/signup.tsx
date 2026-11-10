import { useState } from "react";
import { Link, redirect, useNavigate, useRevalidator, useSearchParams } from "react-router";
import { toast } from "sonner";

import { safeNext } from "./signin";
import { Field } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { act, api } from "~/lib/api";
import { useSession, type Session } from "~/lib/session";

export async function clientLoader() {
  const session = await api<Session>("/session");
  if (session.account) throw redirect("/");
  return null;
}

export default function SignUp() {
  const { site } = useSession();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const revalidator = useRevalidator();
  const [busy, setBusy] = useState(false);

  if (!site.allow_signup) {
    return <p className="text-muted-foreground mt-10 text-center">Registration is closed. Ask an administrator for an account.</p>;
  }

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    if (form.get("password") !== form.get("confirm")) {
      toast.error("The passwords do not match");
      return;
    }
    setBusy(true);
    const ok = await act(() =>
      api("/auth/signup", { body: { email: form.get("email"), name: form.get("name"), password: form.get("password") } }),
    );
    setBusy(false);
    if (ok) {
      await revalidator.revalidate();
      navigate(safeNext(params.get("next")), { replace: true });
    }
  };

  return (
    <Card className="mx-auto mt-10 max-w-sm">
      <CardHeader>
        <CardTitle className="text-xl">Create an account</CardTitle>
      </CardHeader>
      <form onSubmit={submit}>
        <CardContent className="grid gap-4">
          <Field label="Email">
            <Input name="email" type="email" autoComplete="username" required autoFocus />
          </Field>
          <Field label="Display name">
            <Input name="name" maxLength={32} autoComplete="nickname" required />
          </Field>
          <Field label="Password" hint="At least 8 characters.">
            <Input name="password" type="password" minLength={8} autoComplete="new-password" required />
          </Field>
          <Field label="Confirm password">
            <Input name="confirm" type="password" minLength={8} autoComplete="new-password" required />
          </Field>
        </CardContent>
        <CardFooter className="mt-5 flex-col items-stretch gap-3">
          <Button type="submit" disabled={busy}>
            Sign up
          </Button>
          <p className="text-muted-foreground text-center text-sm">
            Already registered?{" "}
            <Link to={"/signin?" + params.toString()} className="text-foreground underline">
              Sign in
            </Link>
          </p>
        </CardFooter>
      </form>
    </Card>
  );
}
