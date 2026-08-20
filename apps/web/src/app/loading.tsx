import { translate as t } from "@forgeflow/ui";

export default function Loading() {
  return (
    <main className="route-state" aria-busy="true" aria-live="polite">
      <div className="route-state-mark" aria-hidden="true"><span /></div>
      <p className="eyebrow">{t("app.preparing")}</p>
      <div className="route-skeleton route-skeleton-title" />
      <div className="route-skeleton route-skeleton-copy" />
    </main>
  );
}
