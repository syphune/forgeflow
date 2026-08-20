import Link from "next/link";
import { translate as t } from "@forgeflow/ui";

type Props = {
  basePath: string;
  title: string;
  eyebrow: string;
  description: string;
  links?: Array<{ href: string; label: string }>;
  backHref?: string;
};

export function SectionPage({ basePath, title, eyebrow, description, links = [], backHref = "/app" }: Props) {
  return (
    <section className="app-v2-page" aria-labelledby="section-heading">
      <div className="app-v2-page-heading"><div><p className="eyebrow">{eyebrow}</p><h2 id="section-heading">{title}</h2><p>{description}</p></div></div>
      <div className="app-v2-section-grid">
        {links.map((link) => <Link className="app-v2-surface-card app-v2-section-card" key={link.href} href={`${basePath}/${link.href}`}><strong>{link.label}</strong><span>{t("section.open")} <span aria-hidden="true">→</span></span></Link>)}
        {!links.length ? <div className="app-v2-empty"><strong>{t("section.ready")}</strong><p>{t("section.description")}</p><Link className="button button-secondary" href={backHref}>{t("section.back")}</Link></div> : null}
      </div>
    </section>
  );
}
