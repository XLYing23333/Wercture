<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import ImageBoard from "../ImageBoard.vue";
import { ApiError, api, type Picture, type Token, type Warehouse } from "../api";
import { confirmAction } from "../confirm";
import { reasonText } from "../i18n";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const warehouse = ref<Warehouse | null>(null);
const pictures = ref<Picture[]>([]);
const tokens = ref<Token[]>([]);
const all = ref<Warehouse[]>([]);
const tokenName = ref("");
const busy = ref(false);
const keysOpen = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const sentinel = ref<HTMLElement | null>(null);
const nextBefore = ref(0);
const done = ref(false);
const loadingMore = ref(false);
let observer: IntersectionObserver | null = null;

function id() {
  return String(route.params.id);
}

async function loadBase() {
  const [item, warehouses] = await Promise.all([
    api<Warehouse>(`/api/v1/warehouses/${id()}`),
    api<{ warehouses: Warehouse[] }>("/api/v1/warehouses"),
  ]);
  warehouse.value = item;
  all.value = warehouses.warehouses;
}

async function loadTokens() {
  const tokenList = await api<{ tokens: Token[] }>(`/api/v1/warehouses/${id()}/tokens`);
  tokens.value = tokenList.tokens;
}

async function loadMore() {
  if (loadingMore.value || done.value) return;
  loadingMore.value = true;
  try {
    const query = new URLSearchParams({ limit: "24" });
    if (nextBefore.value) query.set("before", String(nextBefore.value));
    const page = await api<{ images: Picture[]; next: number }>(`/api/v1/warehouses/${id()}/images?${query}`);
    const seen = new Set(pictures.value.map((item) => item.id));
    pictures.value.push(...page.images.filter((item) => !seen.has(item.id)));
    nextBefore.value = page.next;
    done.value = page.next === 0;
  } catch (err) {
    fail(err);
    done.value = true;
  } finally {
    loadingMore.value = false;
  }
  await nextTick();
  const root = document.querySelector(".content");
  const box = sentinel.value?.getBoundingClientRect();
  const rootBox = root?.getBoundingClientRect();
  const visible = !!box && (rootBox ? box.top < rootBox.bottom + 240 : box.top < window.innerHeight + 240);
  if (!done.value && visible) void loadMore();
}

async function resetImages() {
  pictures.value = [];
  nextBefore.value = 0;
  done.value = false;
  await loadMore();
}

async function refresh() {
  await loadBase();
  if (keysOpen.value) await loadTokens();
  await resetImages();
}

function fail(err: unknown) {
  ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
}

async function upload(event: Event) {
  const input = event.target as HTMLInputElement;
  const files = Array.from(input.files ?? []);
  input.value = "";
  for (const file of files) {
    const body = new FormData();
    body.append("file", file);
    try {
      await api(`/api/v1/warehouses/${id()}/images`, { method: "POST", body });
    } catch (err) {
      fail(err);
    }
  }
  await refresh();
}

async function openKeys() {
  keysOpen.value = true;
  try {
    await loadTokens();
  } catch (err) {
    fail(err);
  }
}

async function createToken() {
  if (busy.value) return;
  busy.value = true;
  try {
    await api(`/api/v1/warehouses/${id()}/tokens`, { method: "POST", body: JSON.stringify({ name: tokenName.value }) });
    tokenName.value = "";
    await loadTokens();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

async function revoke(tokenId: number) {
  if (!(await confirmAction(t("confirmRevoke")))) return;
  await api(`/api/v1/tokens/${tokenId}`, { method: "DELETE" });
  await loadTokens();
}

async function rename(imageId: number, name: string) {
  try {
    await api(`/api/v1/images/${imageId}`, { method: "PATCH", body: JSON.stringify({ name }) });
    await refresh();
  } catch (err) {
    fail(err);
  }
}

async function copyImages(ids: number[], warehouseId: number) {
  try {
    await api("/api/v1/images/copy", { method: "POST", body: JSON.stringify({ ids, warehouse_id: warehouseId }) });
    await refresh();
  } catch (err) {
    fail(err);
  }
}

async function move(ids: number[], warehouseId: number) {
  try {
    await api("/api/v1/images/move", { method: "POST", body: JSON.stringify({ ids, warehouse_id: warehouseId }) });
    await refresh();
  } catch (err) {
    fail(err);
  }
}

async function trash(ids: number[]) {
  try {
    await api("/api/v1/images/trash", { method: "POST", body: JSON.stringify({ ids }) });
    await refresh();
  } catch (err) {
    fail(err);
  }
}

async function removeWarehouse() {
  if (!(await confirmAction(t("confirmDeleteWarehouse")))) return;
  await api(`/api/v1/warehouses/${id()}`, { method: "DELETE" });
  await router.push("/");
}

function importLink(token: string) {
  const params = new URLSearchParams({
    host: window.location.origin,
    token,
    name: warehouse.value?.name || "wercture",
  });
  return `wercture://import?${params.toString()}`;
}

function shownLink(token: string) {
  return importLink(token).replace("?", "?\u200b").replaceAll("&", "\u200b&");
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  ElMessage.success(t("copied"));
}

function bindScroll() {
  observer?.disconnect();
  const root = document.querySelector(".content");
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) void loadMore();
  }, { root: root instanceof HTMLElement ? root : null, rootMargin: "240px" });
  if (sentinel.value) observer.observe(sentinel.value);
}

watch(sentinel, () => bindScroll());
watch(() => route.params.id, () => {
  keysOpen.value = false;
  void refresh();
});
onMounted(() => void refresh());
onUnmounted(() => observer?.disconnect());
</script>

<template>
  <main class="page" v-if="warehouse">
    <header class="detail-head">
      <h1>{{ warehouse.name }}</h1>
      <p class="hint">{{ warehouse.image_count }} {{ t("images") }}<template v-if="warehouse.owner"> · {{ t("owner") }}: {{ warehouse.owner }}</template></p>
    </header>
    <div class="toolbar action-bar">
      <el-button type="primary" @click="fileInput?.click()">{{ t("upload") }}</el-button>
      <input ref="fileInput" hidden type="file" accept="image/jpeg,image/png,image/gif,image/webp,application/pdf,.pdf,.md,.markdown,text/markdown" multiple @change="upload" />
      <el-button @click="removeWarehouse">{{ t("deleteWarehouse") }}</el-button>
      <el-button @click="openKeys">{{ t("manageKeys") }}</el-button>
    </div>
    <ImageBoard :pictures="pictures" :warehouses="all" :current-id="warehouse.id" mode="warehouse" @rename="rename" @copy="copyImages" @move="move" @trash="trash" />
    <div v-show="!done" ref="sentinel" class="sentinel" />
    <el-dialog v-model="keysOpen" :title="t('manageKeys')" width="min(760px, calc(100vw - 24px))" align-center>
      <p class="hint keys-hint">{{ t("tokenHint") }} {{ t("importHint") }}</p>
      <el-form class="toolbar keys-create" @submit.prevent="createToken">
        <el-input v-model="tokenName" :placeholder="t('tokenName')" maxlength="64" />
        <el-button type="primary" native-type="submit" :loading="busy">{{ t("createToken") }}</el-button>
      </el-form>
      <p v-if="!tokens.length" class="hint">{{ t("noKeys") }}</p>
      <div v-else class="key-list">
        <article v-for="token in tokens" :key="token.id" class="key-card">
          <header class="key-head">
            <strong>{{ token.name }}</strong>
            <el-button link type="danger" @click="revoke(token.id)">{{ t("revoke") }}</el-button>
          </header>
          <div class="key-field">
            <span>{{ t("keyLabel") }}</span>
            <code>{{ token.token }}</code>
            <el-button size="small" @click="copy(token.token)">{{ t("copyKey") }}</el-button>
          </div>
          <div class="key-field">
            <span>{{ t("importURL") }}</span>
            <code>{{ shownLink(token.token) }}</code>
            <el-button size="small" @click="copy(importLink(token.token))">{{ t("copyURL") }}</el-button>
          </div>
        </article>
      </div>
    </el-dialog>
  </main>
</template>
