<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import { pageHTML, pageLink, pageMarkdown, pageRender, type Picture, type Warehouse } from "./api";
import { confirmAction } from "./confirm";
import MarkdownView from "./MarkdownView.vue";
import PdfView from "./PdfView.vue";
import { selectionLimit, selectPicture } from "./selection";

const props = defineProps<{
  pictures: Picture[];
  warehouses: Warehouse[];
  mode: "warehouse" | "trash";
  currentId?: number;
}>();
const emit = defineEmits<{
  rename: [id: number, name: string];
  copy: [ids: number[], warehouseId: number];
  move: [ids: number[], warehouseId: number];
  trash: [ids: number[]];
  restore: [ids: number[]];
  purge: [ids: number[]];
}>();

const { t } = useI18n();
const selected = ref<number[]>([]);
const menu = ref<{ x: number; y: number; picture: Picture } | null>(null);
const preview = ref<Picture | null>(null);
const scale = ref(1);
const renameOpen = ref(false);
const renameValue = ref("");
const renameId = ref(0);
const moveOpen = ref(false);
const placeMode = ref<"copy" | "move">("move");
const moveIds = ref<number[]>([]);
const moveTarget = ref<number | undefined>();
const targets = computed(() =>
  props.warehouses.filter((item) => placeMode.value === "copy" || item.id !== props.currentId),
);

type GalleryView = "small" | "medium" | "auto";
type GallerySort = "none" | "time" | "size";
const savedView = localStorage.getItem("wercture.gallery");
const view = ref<GalleryView>(savedView === "small" || savedView === "auto" ? savedView : "medium");
const savedSort = localStorage.getItem("wercture.sort");
const sort = ref<GallerySort>(savedSort === "time" || savedSort === "size" ? savedSort : "none");
watch(view, (value) => localStorage.setItem("wercture.gallery", value));
watch(sort, (value) => localStorage.setItem("wercture.sort", value));

const shown = computed(() => {
  const list = [...props.pictures];
  if (sort.value === "time") list.sort((a, b) => b.uploaded_at.localeCompare(a.uploaded_at) || b.id - a.id);
  if (sort.value === "size") list.sort((a, b) => b.size - a.size || b.id - a.id);
  return list;
});

function cycleView() {
  view.value = view.value === "small" ? "medium" : view.value === "medium" ? "auto" : "small";
}

function cycleSort() {
  sort.value = sort.value === "none" ? "time" : sort.value === "time" ? "size" : "none";
}

function day(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value.slice(0, 10);
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const dateNum = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${dateNum}`;
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}

const sortRange = computed(() => {
  if (!shown.value.length) return "";
  if (sort.value === "time") {
    const days = shown.value.map((item) => day(item.uploaded_at)).sort();
    return days[0] === days[days.length - 1] ? days[0] : `${days[0]} – ${days[days.length - 1]}`;
  }
  if (sort.value === "size") {
    const sizes = shown.value.map((item) => item.size);
    const min = Math.min(...sizes);
    const max = Math.max(...sizes);
    return min === max ? formatSize(min) : `${formatSize(min)} – ${formatSize(max)}`;
  }
  return "";
});

function kindLabel(mime: string) {
  switch (mime) {
    case "image/jpeg":
      return "jpg";
    case "image/png":
      return "png";
    case "image/gif":
      return "gif";
    case "image/webp":
      return "webp";
    case "application/pdf":
      return "pdf";
    case "text/markdown":
      return "md";
    default:
      return "";
  }
}

function isDoc(picture: Picture) {
  return picture.mime === "application/pdf" || picture.mime === "text/markdown";
}

function pictureMeta(picture: Picture) {
  if (sort.value === "time") return day(picture.uploaded_at);
  if (sort.value === "size") return formatSize(picture.size);
  return "";
}

const board = ref<HTMLElement | null>(null);
const boardWidth = ref(0);
let observer: ResizeObserver | null = null;
watch(board, (el) => {
  observer?.disconnect();
  observer = null;
  if (!el) return;
  observer = new ResizeObserver(() => {
    boardWidth.value = el.clientWidth;
  });
  observer.observe(el);
  boardWidth.value = el.clientWidth;
});

type Cell = { picture: Picture; width: number; height: number };
const rows = computed(() => {
  const width = Math.max(160, boardWidth.value);
  const gap = 12;
  const target = 168;
  const out: Cell[][] = [];
  let row: { picture: Picture; ratio: number }[] = [];
  let used = 0;
  function flush(stretch: boolean) {
    const ratios = row.reduce((sum, item) => sum + item.ratio, 0);
    const gaps = gap * Math.max(0, row.length - 1);
    let height = target;
    if (stretch && ratios > 0) {
      height = Math.min(target * 1.8, Math.max(72, (width - gaps) / ratios));
    }
    out.push(row.map((item) => ({ picture: item.picture, width: height * item.ratio, height })));
    row = [];
    used = 0;
  }
  for (const picture of shown.value) {
    const raw = picture.width > 0 && picture.height > 0 ? picture.width / picture.height : isDoc(picture) ? 0.75 : 1;
    const ratio = Math.min(3.2, Math.max(0.35, raw));
    const next = target * ratio;
    if (row.length > 0 && used + gap + next > width) flush(true);
    row.push({ picture, ratio });
    used += (row.length > 1 ? gap : 0) + next;
  }
  if (row.length) flush(used > width * 0.72);
  return out;
});

const gallery = ref<HTMLElement | null>(null);
let suppressClick = false;
let dragMove: ((ev: PointerEvent) => void) | null = null;
let dragUp: (() => void) | null = null;

const batch = computed(() => selected.value.length > 0);
const canRestoreSelection = computed(() => props.pictures.filter((item) => selected.value.includes(item.id) && item.can_restore).length > 0);

function src(picture: Picture) {
  return props.mode === "trash" ? `/api/v1/images/${picture.id}/raw` : picture.path;
}

function thumb(picture: Picture) {
  if (!isDoc(picture)) return src(picture);
  return props.mode === "trash" ? `/api/v1/images/${picture.id}/preview` : `/i/${picture.public_id}.preview.png`;
}

function toggle(id: number, checked: boolean) {
  selected.value = selectPicture(selected.value, id, checked);
}

function openPreview(picture: Picture) {
  if (suppressClick) {
    suppressClick = false;
    return;
  }
  preview.value = picture;
  scale.value = 1;
}

function tilesUnder(x1: number, y1: number, x2: number, y2: number) {
  const tiles = gallery.value?.querySelectorAll<HTMLElement>(".tile");
  if (!tiles) return [];
  const dist = Math.hypot(x2 - x1, y2 - y1);
  const steps = Math.max(1, Math.ceil(dist / 8));
  const ids: number[] = [];
  for (let step = 0; step <= steps; step += 1) {
    const x = x1 + ((x2 - x1) * step) / steps;
    const y = y1 + ((y2 - y1) * step) / steps;
    tiles.forEach((tile) => {
      const rect = tile.getBoundingClientRect();
      if (x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom) ids.push(Number(tile.dataset.id));
    });
  }
  return ids;
}

function startDrag(event: PointerEvent) {
  if (event.button !== 0) return;
  const target = event.target as HTMLElement;
  if (target.closest(".tick, button, a, input")) return;
  const originX = event.clientX;
  const originY = event.clientY;
  const startTile = target.closest<HTMLElement>(".tile");
  const startId = Number(startTile?.dataset.id || 0);
  const removing = startId > 0 && selected.value.includes(startId);
  const seen = new Set<number>();
  let moved = false;
  let lastX = originX;
  let lastY = originY;
  const apply = (ids: number[]) => {
    for (const id of ids) {
      if (!id || seen.has(id)) continue;
      seen.add(id);
      if (removing) selected.value = selected.value.filter((item) => item !== id);
      else toggle(id, true);
    }
  };
  const move = (ev: PointerEvent) => {
    if (!moved && Math.hypot(ev.clientX - originX, ev.clientY - originY) < 8) return;
    if (!moved) apply(tilesUnder(originX, originY, ev.clientX, ev.clientY));
    moved = true;
    apply(tilesUnder(lastX, lastY, ev.clientX, ev.clientY));
    lastX = ev.clientX;
    lastY = ev.clientY;
  };
  const up = () => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    dragMove = null;
    dragUp = null;
    if (moved) suppressClick = true;
  };
  dragMove = move;
  dragUp = up;
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
}

async function confirmTrash(ids: number[]) {
  const ok = await confirmAction(ids.length > 1 ? t("confirmTrashMany", { count: ids.length }) : t("confirmTrash"));
  if (!ok) return;
  emit("trash", ids);
  selected.value = selected.value.filter((id) => !ids.includes(id));
  closeMenu();
}

async function confirmPurge(ids: number[]) {
  const ok = await confirmAction(ids.length > 1 ? t("confirmPurgeMany", { count: ids.length }) : t("confirmPurge"));
  if (!ok) return;
  emit("purge", ids);
  selected.value = selected.value.filter((id) => !ids.includes(id));
  closeMenu();
}

function openMenu(event: MouseEvent, picture: Picture) {
  const width = 220;
  const height = props.mode === "warehouse" ? 300 : 150;
  const x = Math.min(Math.max(8, event.clientX), window.innerWidth - width - 8);
  const y = Math.min(Math.max(8, event.clientY), window.innerHeight - height - 8);
  menu.value = { x, y, picture };
}

function closeMenu() {
  menu.value = null;
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  ElMessage.success(t("copied"));
  closeMenu();
}

function startRename(picture: Picture) {
  renameId.value = picture.id;
  renameValue.value = picture.name;
  renameOpen.value = true;
  closeMenu();
}

function startPlace(ids: number[], mode: "copy" | "move") {
  placeMode.value = mode;
  moveIds.value = ids;
  moveTarget.value = targets.value[0]?.id;
  moveOpen.value = true;
  closeMenu();
}

function confirmPlace() {
  if (!moveTarget.value) return;
  if (placeMode.value === "copy") emit("copy", moveIds.value, moveTarget.value);
  else emit("move", moveIds.value, moveTarget.value);
  moveOpen.value = false;
  selected.value = [];
}

function onWheel(event: WheelEvent) {
  if (!preview.value || isDoc(preview.value)) return;
  event.preventDefault();
  const next = scale.value + (event.deltaY < 0 ? 0.15 : -0.15);
  scale.value = Math.min(6, Math.max(0.2, next));
}

function onKey(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  preview.value = null;
  menu.value = null;
  selected.value = [];
}

window.addEventListener("keydown", onKey);
onUnmounted(() => {
  window.removeEventListener("keydown", onKey);
  if (dragMove) window.removeEventListener("pointermove", dragMove);
  if (dragUp) window.removeEventListener("pointerup", dragUp);
  observer?.disconnect();
});
</script>

<template>
  <Teleport v-if="mode === 'warehouse'" defer to=".action-bar">
  <div class="view-switch">
    <button type="button" class="view-btn" :title="t(view === 'small' ? 'viewSmall' : view === 'medium' ? 'viewMedium' : 'viewAuto')" :aria-label="t(view === 'small' ? 'viewSmall' : view === 'medium' ? 'viewMedium' : 'viewAuto')" @click="cycleView">
      <svg v-if="view === 'small'" viewBox="0 0 16 16" aria-hidden="true"><path d="M1 2a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1zM1 7a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1zM1 12a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1zm5 0a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1h-2a1 1 0 0 1-1-1z"/></svg>
      <svg v-else-if="view === 'medium'" viewBox="0 0 16 16" aria-hidden="true"><path d="M1 2.5A1.5 1.5 0 0 1 2.5 1h3A1.5 1.5 0 0 1 7 2.5v3A1.5 1.5 0 0 1 5.5 7h-3A1.5 1.5 0 0 1 1 5.5zm8 0A1.5 1.5 0 0 1 10.5 1h3A1.5 1.5 0 0 1 15 2.5v3A1.5 1.5 0 0 1 13.5 7h-3A1.5 1.5 0 0 1 9 5.5zm-8 8A1.5 1.5 0 0 1 2.5 9h3A1.5 1.5 0 0 1 7 10.5v3A1.5 1.5 0 0 1 5.5 15h-3A1.5 1.5 0 0 1 1 13.5zm8 0A1.5 1.5 0 0 1 10.5 9h3a1.5 1.5 0 0 1 1.5 1.5v3a1.5 1.5 0 0 1-1.5 1.5h-3A1.5 1.5 0 0 1 9 13.5z"/></svg>
      <svg v-else viewBox="0 0 16 16" aria-hidden="true"><path d="M0 3.5A1.5 1.5 0 0 1 1.5 2h13A1.5 1.5 0 0 1 16 3.5v9a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 0 12.5zM1.5 3a.5.5 0 0 0-.5.5v9a.5.5 0 0 0 .5.5h13a.5.5 0 0 0 .5-.5v-9a.5.5 0 0 0-.5-.5z"/><path d="M2 4.5a.5.5 0 0 1 .5-.5h3a.5.5 0 0 1 0 1H3v2.5a.5.5 0 0 1-1 0zm12 7a.5.5 0 0 1-.5.5h-3a.5.5 0 0 1 0-1H13V8.5a.5.5 0 0 1 1 0z"/></svg>
    </button>
    <button type="button" :title="t(sort === 'time' ? 'sortTime' : sort === 'size' ? 'sortSize' : 'sortNone')" @click="cycleSort">
      <span>{{ t(sort === "time" ? "sortTime" : sort === "size" ? "sortSize" : "sortNone") }}</span>
      <span v-if="sortRange" class="sort-range">{{ sortRange }}</span>
    </button>
  </div>
  </Teleport>
  <el-empty v-if="pictures.length === 0" :description="mode === 'trash' ? t('emptyTrash') : t('emptyImages')" />
  <div v-else ref="gallery" class="gallery" @pointerdown="startDrag" @click="closeMenu">
    <div v-if="mode !== 'warehouse' || view !== 'auto'" class="grid" :class="mode === 'warehouse' ? `view-${view}` : ''">
      <article v-for="picture in shown" :key="picture.id" class="tile" :class="{ picked: selected.includes(picture.id) }" :data-id="picture.id">
        <label class="tick"><input type="checkbox" :disabled="selected.length >= selectionLimit && !selected.includes(picture.id)" :checked="selected.includes(picture.id)" @change="toggle(picture.id, ($event.target as HTMLInputElement).checked)" /></label>
        <img :class="{ 'doc-thumb': isDoc(picture) }" :src="thumb(picture)" :alt="picture.name" draggable="false" loading="lazy" decoding="async" @click="openPreview(picture)" @contextmenu.prevent="openMenu($event, picture)" />
        <p :title="picture.name">
          <span class="tile-name">{{ picture.name }}</span>
          <span class="tile-foot">
            <span v-if="kindLabel(picture.mime)" class="tile-kind">{{ kindLabel(picture.mime) }}</span>
            <span v-if="pictureMeta(picture)" class="tile-meta">{{ pictureMeta(picture) }}</span>
          </span>
        </p>
      </article>
    </div>
    <div v-else ref="board" class="justified">
      <div v-for="(row, index) in rows" :key="index" class="just-row">
        <article v-for="cell in row" :key="cell.picture.id" class="tile" :class="{ picked: selected.includes(cell.picture.id) }" :data-id="cell.picture.id" :style="{ width: cell.width + 'px' }">
          <label class="tick"><input type="checkbox" :disabled="selected.length >= selectionLimit && !selected.includes(cell.picture.id)" :checked="selected.includes(cell.picture.id)" @change="toggle(cell.picture.id, ($event.target as HTMLInputElement).checked)" /></label>
          <img :class="{ 'doc-thumb': isDoc(cell.picture) }" :src="thumb(cell.picture)" :alt="cell.picture.name" draggable="false" :style="{ height: cell.height + 'px' }" loading="lazy" decoding="async" @click="openPreview(cell.picture)" @contextmenu.prevent="openMenu($event, cell.picture)" />
          <p :title="cell.picture.name">
            <span class="tile-name">{{ cell.picture.name }}</span>
            <span class="tile-foot">
              <span v-if="kindLabel(cell.picture.mime)" class="tile-kind">{{ kindLabel(cell.picture.mime) }}</span>
              <span v-if="pictureMeta(cell.picture)" class="tile-meta">{{ pictureMeta(cell.picture) }}</span>
            </span>
          </p>
        </article>
      </div>
    </div>
  </div>

  <div v-if="menu" class="menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" @click.stop>
    <template v-if="mode === 'warehouse'">
      <button type="button" class="tone-link" @click="copy(pageLink(menu.picture))">{{ t("copyLink") }}</button>
      <button v-if="menu.picture.mime === 'text/markdown'" type="button" class="tone-link" @click="copy(pageRender(menu.picture))">{{ t("copyRender") }}</button>
      <button type="button" class="tone-md" @click="copy(pageMarkdown(menu.picture))">{{ t("copyMarkdown") }}</button>
      <button type="button" class="tone-html" @click="copy(pageHTML(menu.picture))">{{ t("copyHTML") }}</button>
      <button type="button" class="tone-rename" @click="startRename(menu.picture)">{{ t("rename") }}</button>
      <button type="button" class="tone-copy" @click="startPlace([menu.picture.id], 'copy')">{{ t("copyToWarehouse") }}</button>
      <button type="button" class="tone-move" @click="startPlace([menu.picture.id], 'move')">{{ t("moveToWarehouse") }}</button>
      <button type="button" class="tone-delete" @click="confirmTrash([menu.picture.id])">{{ t("delete") }}</button>
    </template>
    <template v-else>
      <button v-if="menu.picture.can_restore" type="button" class="tone-copy" @click="emit('restore', [menu.picture.id]); closeMenu()">{{ t("restore") }}</button>
      <button type="button" class="tone-move" @click="startPlace([menu.picture.id], 'move')">{{ t("moveToWarehouse") }}</button>
      <button type="button" class="tone-delete" @click="confirmPurge([menu.picture.id])">{{ t("purge") }}</button>
    </template>
  </div>

  <div v-if="batch" class="batch">
    <span class="batch-side">
      <strong>{{ t("selectedCount", { count: selected.length }) }} / {{ selectionLimit }}</strong>
      <el-button @click="selected = []">{{ t("clearSelection") }}</el-button>
    </span>
    <span class="batch-actions">
      <template v-if="mode === 'warehouse'">
        <el-button color="#1f8a4c" @click="startPlace(selected, 'copy')">{{ t("batchCopy") }}</el-button>
        <el-button color="#d9822b" @click="startPlace(selected, 'move')">{{ t("batchMove") }}</el-button>
        <el-button color="#d64545" @click="confirmTrash([...selected])">{{ t("batchDelete") }}</el-button>
      </template>
      <template v-else>
        <el-button v-if="canRestoreSelection" color="#1f8a4c" @click="emit('restore', pictures.filter((item) => selected.includes(item.id) && item.can_restore).map((item) => item.id)); selected = []">{{ t("batchRestore") }}</el-button>
        <el-button color="#d9822b" @click="startPlace(selected, 'move')">{{ t("batchMove") }}</el-button>
        <el-button color="#d64545" @click="confirmPurge([...selected])">{{ t("batchPurge") }}</el-button>
      </template>
    </span>
  </div>

  <div v-if="preview" class="lightbox" :class="{ doc: isDoc(preview) }" @click.self="preview = null" @wheel="onWheel">
    <el-button class="close" @click="preview = null">{{ t("close") }}</el-button>
    <PdfView v-if="preview.mime === 'application/pdf'" :src="src(preview)" />
    <iframe v-else-if="preview.mime === 'text/markdown' && mode === 'warehouse'" class="md-frame" :src="preview.path" title=""></iframe>
    <MarkdownView v-else-if="preview.mime === 'text/markdown'" :src="src(preview)" />
    <img v-else :src="src(preview)" :alt="preview.name" :style="{ transform: `scale(${scale})` }" />
  </div>

  <el-dialog v-model="renameOpen" :title="t('rename')" width="min(420px, calc(100vw - 24px))">
    <el-form label-position="top" @submit.prevent="emit('rename', renameId, renameValue); renameOpen = false">
      <el-form-item :label="t('displayName')"><el-input v-model="renameValue" maxlength="255" /></el-form-item>
    </el-form>
    <template #footer>
      <div class="form-actions">
        <el-button @click="renameOpen = false">{{ t("cancel") }}</el-button>
        <el-button type="primary" @click="emit('rename', renameId, renameValue); renameOpen = false">{{ t("save") }}</el-button>
      </div>
    </template>
  </el-dialog>

  <el-dialog v-model="moveOpen" :title="placeMode === 'copy' ? t('copyToWarehouse') : t('moveToWarehouse')" width="min(420px, calc(100vw - 24px))">
    <el-form label-position="top">
      <el-form-item :label="t('targetWarehouse')">
        <el-select v-model="moveTarget" style="width: 100%">
          <el-option v-for="item in targets" :key="item.id" :label="item.owner ? `${item.name} · ${item.owner}` : item.name" :value="item.id">
            <span class="wh-name">{{ item.name }}</span>
            <span v-if="item.owner" class="wh-owner">{{ item.owner }}</span>
          </el-option>
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <div class="form-actions">
        <el-button @click="moveOpen = false">{{ t("cancel") }}</el-button>
        <el-button type="primary" :disabled="!moveTarget" @click="confirmPlace">{{ t("save") }}</el-button>
      </div>
    </template>
  </el-dialog>
</template>
