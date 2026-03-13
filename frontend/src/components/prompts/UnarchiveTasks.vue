<template>
  <div v-if="jobs.length > 0" class="unarchive-tasks">
    <div class="card floating">
      <div class="card-title" @click="unarchiveStore.toggle">
        <div class="title-copy">
          <i class="material-icons">folder_zip</i>
          <span>
            {{
              t("unarchive.title", {
                count: jobs.length,
                running: unarchiveStore.runningCount,
              })
            }}
          </span>
        </div>
        <button
          class="action"
          type="button"
          @click.stop="unarchiveStore.toggle"
        >
          <i class="material-icons">
            {{ unarchiveStore.expanded ? "expand_more" : "chevron_left" }}
          </i>
        </button>
      </div>

      <div v-if="unarchiveStore.expanded" class="card-content">
        <div
          v-for="job in jobs"
          :key="job.id"
          class="task"
          :data-status="job.status"
        >
          <div class="task-main">
            <div class="task-heading">
              <i class="material-icons">{{ statusIcon(job.status) }}</i>
              <strong>{{ job.archiveName }}</strong>
            </div>
            <div class="task-detail">
              {{
                t("unarchive.destination", {
                  destination: folderName(job.destination),
                })
              }}
            </div>
            <div v-if="job.status === 'failed' && job.error" class="task-error">
              {{ job.error }}
            </div>
          </div>
          <button
            v-if="job.status !== 'running'"
            class="action"
            type="button"
            :aria-label="t('buttons.clear')"
            :title="t('buttons.clear')"
            @click="dismiss(job.id)"
          >
            <i class="material-icons">close</i>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useUnarchiveStore } from "@/stores/unarchive";
import { storeToRefs } from "pinia";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";

const unarchiveStore = useUnarchiveStore();
const { jobs } = storeToRefs(unarchiveStore);
const { t } = useI18n();

onMounted(() => {
  unarchiveStore.refresh().catch(() => undefined);
});

const statusIcon = (status: UnarchiveStatus) => {
  switch (status) {
    case "running":
      return "autorenew";
    case "success":
      return "done";
    case "failed":
      return "error";
  }
};

const folderName = (destination: string) => {
  const parts = destination.split("/").filter(Boolean);
  return parts[parts.length - 1] || destination;
};

const dismiss = async (id: number) => {
  await unarchiveStore.dismiss(id);
};
</script>
