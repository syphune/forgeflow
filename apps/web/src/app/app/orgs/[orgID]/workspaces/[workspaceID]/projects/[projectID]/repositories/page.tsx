import { RepositoryPage } from "@/features/repositories/repository-page";

type Props = { params: Promise<{ orgID: string; workspaceID: string; projectID: string }> };

export default async function RepositoriesRoute({ params }: Props) {
  const { orgID, workspaceID, projectID } = await params;
  const basePath = `/app/orgs/${orgID}/workspaces/${workspaceID}/projects/${projectID}`;
  return <RepositoryPage projectID={projectID} basePath={basePath} />;
}
