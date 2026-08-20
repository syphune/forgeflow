import { PlanningPage } from "@/features/planning/planning-page";

type Props = { params: Promise<{ orgID: string; workspaceID: string; projectID: string }> };

export default async function PlanningRoute({ params }: Props) {
  const { orgID, workspaceID, projectID } = await params;
  const basePath = `/app/orgs/${orgID}/workspaces/${workspaceID}/projects/${projectID}`;
  return <PlanningPage projectID={projectID} basePath={basePath} />;
}
