import { OrganizationSettingsPage } from "@/features/settings/tenant-settings-page";

export default async function OrganizationSettingsRoute({ params }: { params: Promise<{ orgID: string; section: string }> }) {
  const { orgID, section } = await params;
  return <OrganizationSettingsPage organizationID={orgID} section={section} />;
}
