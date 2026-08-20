import { WorkItemModal } from "@/features/work-items/work-item-modal";
import { WorkItemSurface } from "@/features/work-items/work-item-surface";

type Props = {
  params: Promise<{ orgID: string; workspaceID: string; projectID: string; itemID: string }>;
};

export default async function WorkItemModalRoute({ params }: Props) {
  const { orgID, workspaceID, projectID, itemID } = await params;
  const basePath = `/app/orgs/${orgID}/workspaces/${workspaceID}/projects/${projectID}`;
  return <WorkItemModal restoreItemID={itemID}><WorkItemSurface projectID={projectID} itemID={itemID} basePath={basePath} modal /></WorkItemModal>;
}
