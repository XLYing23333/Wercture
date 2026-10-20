<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import { ApiError, api, type User, type Warehouse } from "../api";
import { reasonText } from "../i18n";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const items = ref<Warehouse[]>([]);
const ownerID = computed(() => Number(route.query.owner) || 0);
const shown = computed(() => (ownerID.value ? items.value.filter((item) => item.owner_id === ownerID.value) : items.value));
const ownerName = computed(() => shown.value[0]?.owner || items.value.find((item) => item.owner_id === ownerID.value)?.owner || "");
const me = ref<User | null>(null);
const ready = ref(false);
const name = ref("");
const open = ref(false);
const busy = ref(false);

async function load() {
  try {
    const [list, session] = await Promise.all([
      api<{ warehouses: Warehouse[] }>("/api/v1/warehouses"),
      api<User>("/api/v1/auth/session"),
    ]);
    items.value = list.warehouses;
    me.value = session;
  } catch (err) {
    ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
  } finally {
    ready.value = true;
  }
}

async function create() {
  if (busy.value) return;
  busy.value = true;
  try {
    await api("/api/v1/warehouses", { method: "POST", body: JSON.stringify({ name: name.value }) });
    name.value = "";
    open.value = false;
    await load();
  } catch (err) {
    ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
  } finally {
    busy.value = false;
  }
}

watch(() => route.query.owner, () => void load());
onMounted(() => void load());
</script>

<template>
  <main class="page" v-loading="!ready">
    <div class="toolbar">
      <el-button type="primary" @click="open = true">{{ t("createWarehouse") }}</el-button>
    </div>
    <p v-if="ready && ownerID" class="hint filter-line">
      {{ t("owner") }}: {{ ownerName || ownerID }}
      <el-button link type="primary" @click="router.push('/')">{{ t("warehouses") }}</el-button>
    </p>
    <el-empty v-if="ready && shown.length === 0" :description="t('emptyWarehouses')" />
    <div v-else-if="ready" class="cards">
      <router-link v-for="item in shown" :key="item.id" class="card" :to="'/warehouses/' + item.id">
        <span class="stack" aria-hidden="true">
          <img v-for="(src, index) in (item.previews || []).slice(0, 3)" :key="src" :src="src" alt="" :style="{ zIndex: 3 - index }" />
        </span>
        <span class="card-copy">
          <strong>{{ item.name }}</strong>
          <span>{{ item.image_count }} {{ t("images") }}</span>
          <span v-if="me?.role === 'admin'">{{ t("owner") }}: {{ item.owner }}</span>
        </span>
      </router-link>
    </div>
    <el-dialog v-model="open" :title="t('createWarehouse')" width="min(420px, calc(100vw - 24px))">
      <el-form label-position="top" @submit.prevent="create">
        <el-form-item :label="t('warehouseName')"><el-input v-model="name" maxlength="64" /></el-form-item>
      </el-form>
      <template #footer>
        <div class="form-actions">
          <el-button @click="open = false">{{ t("cancel") }}</el-button>
          <el-button type="primary" :loading="busy" @click="create">{{ t("save") }}</el-button>
        </div>
      </template>
    </el-dialog>
  </main>
</template>
