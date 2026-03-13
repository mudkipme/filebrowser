import { defineStore } from "pinia";
import { files as api } from "@/api";
import { useFileStore } from "@/stores/file";
import { removeLastDir } from "@/utils/url";
import { computed, ref } from "vue";

export const useUnarchiveStore = defineStore("unarchive", () => {
  const jobs = ref<UnarchiveJob[]>([]);
  const expanded = ref(true);
  let pollTimer: number | null = null;

  const runningCount = computed(
    () => jobs.value.filter((job) => job.status === "running").length
  );

  const start = async (source: string) => {
    const task = await api.unarchive(source);
    upsert(task);
    expanded.value = true;
    syncPolling();
  };

  const refresh = async () => {
    const previousJobs = new Map(jobs.value.map((job) => [job.id, job]));
    jobs.value = await api.listUnarchiveTasks();

    const fileStore = useFileStore();
    const currentDir = fileStore.req?.path ?? null;
    let preselect: string | null = null;
    let shouldReload = false;

    for (const job of jobs.value) {
      const previousStatus = previousJobs.get(job.id)?.status;
      if (previousStatus !== "running") continue;

      if (job.status === "success") {
        shouldReload = fileStore.isFiles || shouldReload;
        if (fileStore.isListing && currentDir === parentPath(job.destination)) {
          preselect = job.destination;
        }
      }
    }

    if (preselect) {
      fileStore.preselect = preselect;
    }
    if (shouldReload) {
      fileStore.reload = true;
    }

    syncPolling();
  };

  const dismiss = async (id: number) => {
    await api.deleteUnarchiveTask(id);
    jobs.value = jobs.value.filter((job) => job.id !== id);
    syncPolling();
  };

  const toggle = () => {
    expanded.value = !expanded.value;
  };

  const upsert = (job: UnarchiveJob) => {
    jobs.value = [
      job,
      ...jobs.value.filter((existingJob) => existingJob.id !== job.id),
    ];
  };

  const syncPolling = () => {
    if (jobs.value.some((job) => job.status === "running")) {
      if (pollTimer !== null) return;
      pollTimer = window.setInterval(() => {
        refresh().catch(() => undefined);
      }, 2000);
      return;
    }

    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  };

  const parentPath = (filePath: string) => {
    return removeLastDir(filePath) || "/";
  };

  return {
    jobs,
    expanded,
    runningCount,
    start,
    refresh,
    dismiss,
    toggle,
  };
});
