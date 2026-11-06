<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { mediaTime, seekPosition } from "./media-controls";

const props = defineProps<{ src: string; mime: string; name: string; poster: string }>();
const { t } = useI18n();
const frame = ref<HTMLElement | null>(null);
const player = ref<HTMLMediaElement | null>(null);
const failed = ref(false);
const playing = ref(false);
const waiting = ref(false);
const duration = ref(0);
const current = ref(0);
const seeking = ref(false);
const draft = ref(0);
const volume = ref(100);
const muted = ref(false);
const fullscreen = ref(false);
const video = computed(() => props.mime.startsWith("video/"));
const position = computed(() => seeking.value ? draft.value : current.value);
const fullscreenAvailable = typeof document !== "undefined" && document.fullscreenEnabled;

const icons = {
  play: "M8 5v14l11-7z",
  pause: "M6 5h4v14H6zm8 0h4v14h-4z",
  volume: "M3 9v6h4l5 4V5L7 9zm13-3-1.4 1.4a5 5 0 0 1 0 9.2L16 18a7 7 0 0 0 0-12zm-2 3-1.4 1.4a2 2 0 0 1 0 3.2L14 15a4 4 0 0 0 0-6z",
  mute: "M3 9v6h4l5 4V5L7 9zm12-1 3 3 3-3 1 1-3 3 3 3-1 1-3-3-3 3-1-1 3-3-3-3z",
  fullscreen: "M3 3h7v2H5v5H3zm11 0h7v7h-2V5h-5zM3 14h2v5h5v2H3zm16 0h2v7h-7v-2h5z",
  exit: "M8 3h2v7H3V8h5zm6 0h2v5h5v2h-7zM3 14h7v7H8v-5H3zm11 0h7v2h-5v5h-2z",
};

function syncMetadata() {
  const media = player.value;
  if (!media) return;
  duration.value = Number.isFinite(media.duration) ? media.duration : 0;
  current.value = Number.isFinite(media.currentTime) ? media.currentTime : 0;
}

function syncVolume() {
  if (!player.value) return;
  volume.value = Math.round(player.value.volume * 100);
  muted.value = player.value.muted;
}

async function togglePlayback() {
  if (!player.value || failed.value) return;
  if (!player.value.paused) { player.value.pause(); return; }
  try { await player.value.play(); }
  catch (error) {
    if (!(error instanceof DOMException && error.name === "AbortError")) failed.value = true;
  }
}

function previewSeek(value: number | number[]) {
  seeking.value = true;
  draft.value = seekPosition(Number(value), duration.value);
}

function seek(value: number | number[]) {
  const target = seekPosition(Number(value), duration.value);
  if (player.value && duration.value > 0) player.value.currentTime = target;
  current.value = target;
  seeking.value = false;
}

function setVolume(value: number | number[]) {
  if (!player.value) return;
  player.value.volume = Math.min(100, Math.max(0, Number(value))) / 100;
  player.value.muted = Number(value) === 0;
  syncVolume();
}

function toggleMute() {
  if (!player.value) return;
  player.value.muted = !player.value.muted;
  syncVolume();
}

async function toggleFullscreen() {
  if (!frame.value) return;
  if (document.fullscreenElement === frame.value) await document.exitFullscreen();
  else await frame.value.requestFullscreen();
}

function syncFullscreen() { fullscreen.value = document.fullscreenElement === frame.value; }
document.addEventListener("fullscreenchange", syncFullscreen);

watch(() => props.src, () => {
  failed.value = false;
  playing.value = false;
  waiting.value = false;
  seeking.value = false;
  current.value = duration.value = 0;
});
onBeforeUnmount(() => {
  document.removeEventListener("fullscreenchange", syncFullscreen);
  if (!player.value) return;
  player.value.pause();
  player.value.removeAttribute("src");
  player.value.load();
});
</script>

<template>
  <section ref="frame" class="media-view" :class="{ 'audio-view': !video }" :aria-label="name" @click.stop>
    <h2>{{ name }}</h2>
    <div class="media-stage">
      <video v-if="video" :key="src" ref="player" :src="src" :poster="poster" :controls="false" playsinline disablepictureinpicture preload="metadata"
        @click="togglePlayback" @loadedmetadata="syncMetadata" @durationchange="syncMetadata" @timeupdate="syncMetadata"
        @play="playing = true" @pause="playing = false" @ended="playing = false" @waiting="waiting = true" @playing="waiting = false"
        @volumechange="syncVolume" @error="failed = true; playing = false" />
      <template v-else>
        <img class="audio-art" :src="poster" alt="" />
        <audio :key="src" ref="player" :src="src" :controls="false" preload="metadata"
          @loadedmetadata="syncMetadata" @durationchange="syncMetadata" @timeupdate="syncMetadata"
          @play="playing = true" @pause="playing = false" @ended="playing = false" @waiting="waiting = true" @playing="waiting = false"
          @volumechange="syncVolume" @error="failed = true; playing = false" />
      </template>
      <span v-if="waiting && playing && !failed" class="media-loading" role="status">{{ t("mediaLoading") }}</span>
    </div>
    <div class="media-controls">
      <el-slider class="media-seek" :model-value="position" :min="0" :max="duration || 1" :step="0.1" :disabled="!duration || failed" :show-tooltip="false" :aria-label="t('mediaProgress')" @input="previewSeek" @change="seek" />
      <div class="media-control-row">
        <el-button class="media-icon-button" :aria-label="t(playing ? 'mediaPause' : 'mediaPlay')" :title="t(playing ? 'mediaPause' : 'mediaPlay')" :disabled="failed" @click="togglePlayback">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path :d="playing ? icons.pause : icons.play" /></svg>
        </el-button>
        <span class="media-time">{{ mediaTime(position) }} / {{ mediaTime(duration) }}</span>
        <div class="media-volume">
          <el-button class="media-icon-button" :aria-label="t(muted ? 'mediaUnmute' : 'mediaMute')" :title="t(muted ? 'mediaUnmute' : 'mediaMute')" @click="toggleMute">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path :d="muted || volume === 0 ? icons.mute : icons.volume" /></svg>
          </el-button>
          <el-slider :model-value="muted ? 0 : volume" :min="0" :max="100" :show-tooltip="false" :aria-label="t('mediaVolume')" @input="setVolume" />
        </div>
        <el-button v-if="video && fullscreenAvailable" class="media-icon-button" :aria-label="t(fullscreen ? 'mediaExitFullscreen' : 'mediaFullscreen')" :title="t(fullscreen ? 'mediaExitFullscreen' : 'mediaFullscreen')" @click="toggleFullscreen">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path :d="fullscreen ? icons.exit : icons.fullscreen" /></svg>
        </el-button>
      </div>
    </div>
    <p v-if="failed" class="media-error" role="alert">{{ t("mediaPlaybackFailed") }}</p>
    <el-button class="media-download" tag="a" :href="src" :download="name">{{ t("downloadFile") }}</el-button>
  </section>
</template>
