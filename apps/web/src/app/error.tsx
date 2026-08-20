"use client";

import Link from "next/link";
import { translate as t } from "@forgeflow/ui";

export default function Error({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className="route-state" role="alert">
      <div className="route-state-mark is-error" aria-hidden="true">!</div>
      <p className="eyebrow">{t("error.eyebrow")}</p>
      <h1>{t("error.title")}</h1>
      <p className="route-state-copy">{t("error.description")}</p>
      <div className="route-state-actions">
        <button className="button button-primary" type="button" onClick={() => reset()}>Thử lại <span aria-hidden="true">↗</span></button>
        <Link className="text-link" href="/">{t("error.home")} <span aria-hidden="true">↗</span></Link>
      </div>
    </main>
  );
}
