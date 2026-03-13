type UnarchiveStatus = "running" | "success" | "failed";

type UnarchiveJob = {
  id: number;
  source: string;
  archiveName: string;
  destination: string;
  status: UnarchiveStatus;
  error?: string;
  createdAt: string;
  finishedAt?: string;
};
