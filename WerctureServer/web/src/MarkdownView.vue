<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { renderMarkdown } from "./markdown";

const props = defineProps<{ src: string }>();
const { t } = useI18n();
const html = ref("");
const error = ref("");

async function load() {
  error.value = "";
  html.value = "";
  try {
    const response = await fetch(props.src, { credentials: "same-origin" });
    if (!response.ok) throw new Error(String(response.status));
    html.value = renderMarkdown(await response.text());
  } catch {
    error.value = t("docError");
  }
}

onMounted(() => void load());
watch(() => props.src, () => void load());
</script>

<template>
  <article class="md-sheet">
    <p v-if="error" class="md-error">{{ error }}</p>
    <div v-else class="md-body" v-html="html"></div>
  </article>
</template>
