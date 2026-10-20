<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import * as pdfjs from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

const props = defineProps<{ src: string }>();
const { t } = useI18n();
const host = ref<HTMLElement | null>(null);
const error = ref("");
let generation = 0;

async function load() {
  const current = ++generation;
  error.value = "";
  if (host.value) host.value.replaceChildren();
  try {
    const doc = await pdfjs.getDocument({ url: props.src, withCredentials: true }).promise;
    if (current !== generation || !host.value) return;
    for (let number = 1; number <= doc.numPages; number += 1) {
      if (current !== generation || !host.value) return;
      const page = await doc.getPage(number);
      const viewport = page.getViewport({ scale: 1.35 });
      const canvas = document.createElement("canvas");
      canvas.width = viewport.width;
      canvas.height = viewport.height;
      host.value.append(canvas);
      const context = canvas.getContext("2d");
      if (!context) throw new Error("canvas");
      await page.render({ canvasContext: context, canvas, viewport }).promise;
    }
  } catch {
    if (current === generation) error.value = t("docError");
  }
}

onMounted(() => void load());
watch(() => props.src, () => void load());
</script>

<template>
  <div class="pdf-sheet">
    <p v-if="error" class="md-error">{{ error }}</p>
    <div ref="host" class="pdf-pages"></div>
  </div>
</template>
