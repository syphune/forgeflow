import { BacklogPage } from "@/features/backlog/backlog-page";

type Props = {
  params: Promise<{ orgID: string; workspaceID: string; projectID: string }>;
  searchParams: Promise<{ create?: string }>;
};

export default async function BacklogRoute({ params, searchParams }: Props) {
  const { projectID, orgID, workspaceID } = await params;
  const query = await searchParams;
  const basePath = `/app/orgs/${orgID}/workspaces/${workspaceID}/projects/${projectID}`;
  return <BacklogPage projectID={projectID} basePath={basePath} createOnMount={query.create === "1"} />;
}
