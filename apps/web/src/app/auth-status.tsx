"use client";

import { useEffect, useState } from "react";
import { translate as t } from "@forgeflow/ui";

type AuthState = "checking" | "signed-in" | "signed-out";

export function AuthStatus({ signInURL, meURL }: { signInURL: string; meURL: string }) {
  const [state, setState] = useState<AuthState>("checking");

  useEffect(() => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      controller.abort();
      setState("signed-out");
    }, 5000);
    fetch(meURL, { headers: { Accept: "application/json" }, credentials: "include", signal: controller.signal })
      .then((response) => setState(response.ok ? "signed-in" : "signed-out"))
      .catch(() => {
        if (!controller.signal.aborted) setState("signed-out");
      })
      .finally(() => window.clearTimeout(timeout));
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [meURL]);

  if (state === "checking") return <span className="topbar-login" aria-live="polite">{t("auth.checking-short")}</span>;
  if (state === "signed-in") return <a className="topbar-login" href="#workspace">{t("auth.signed-in-short")} <span aria-hidden="true">↘</span></a>;
  return <a className="topbar-login" href={signInURL}>{t("auth.sign-in-short")} <span aria-hidden="true">↗</span></a>;
}
