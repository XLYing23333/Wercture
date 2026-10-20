<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import ImageBoard from "../ImageBoard.vue";
import { ApiError, api, type Picture, type Warehouse } from "../api";
import { confirmAction } from "../confirm";
import { reasonText } from "../i18n";

const { t } = useI18n();
const pictures = ref<Picture[]>([]);
const warehouses = ref<Warehouse[]>([]);

async function load() {
  const [trash, list] = await Promise.all([
    api<{ images: Picture[] }>("/api/v1/trash"),
    api<{ warehouses: Warehouse[] }>("/api/v1/warehouses"),
  ]);
  pictures.value = trash.images;
  warehouses.value = list.warehouses;
}

function fail(err: unknown) {
  ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
}

async function restore(ids: number[]) {
  try {
    await api("/api/v1/trash/restore", { method: "POST", body: JSON.stringify({ ids }) });
    await load();
  } catch (err) {
    fail(err);
  }
}

async function move(ids: number[], warehouseId: number) {
  try {
    await api("/api/v1/trash/move", { method: "POST", body: JSON.stringify({ ids, warehouse_id: warehouseId }) });
    await load();
  } catch (err) {
    fail(err);
  }
}

async function purge(ids: number[]) {
  try {
    await api("/api/v1/trash/purge", { method: "POST", body: JSON.stringify({ ids }) });
    await load();
  } catch (err) {
    fail(err);
  }
}

async function empty() {
  if (!pictures.value.length || !(await confirmAction(t("confirmEmptyTrash")))) return;
  try {
    await api("/api/v1/trash/empty", { method: "POST" });
    await load();
  } catch (err) {
    fail(err);
  }
}

onMounted(() => void load());
</script>

<template>
  <main class="page">
    <p class="hint">{{ t("trashHint") }}</p>
    <div class="toolbar">
      <el-button color="#d64545" :disabled="pictures.length === 0" @click="empty">{{ t("emptyTrashAction") }}</el-button>
    </div>
    <ImageBoard :pictures="pictures" :warehouses="warehouses" mode="trash" @restore="restore" @move="move" @purge="purge" />
  </main>
</template>
