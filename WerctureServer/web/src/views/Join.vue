<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ApiError, api } from "../api";
import { reasonText } from "../i18n";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const username = ref("");
const password = ref("");
const confirm = ref("");
const busy = ref(false);
const error = ref("");
const ready = ref(false);
let request: AbortController | null = null;

function token() {
  return String(route.params.token || "");
}

onMounted(async () => {
  try {
    const state = await api<{ username: string }>("/api/v1/join", { headers: { "X-Wercture-Join": token() } });
    username.value = state.username;
    ready.value = true;
  } catch {
    ready.value = false;
  }
});

async function submit() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  request = new AbortController();
  try {
    await api("/api/v1/join", {
      method: "POST",
      signal: request.signal,
      headers: { "X-Wercture-Join": token() },
      body: JSON.stringify({ password: password.value, confirm: confirm.value }),
    });
    await router.replace("/login");
  } catch (err) {
    if (!request.signal.aborted) error.value = t(reasonText(err instanceof ApiError ? err.code : "invalid"));
  } finally {
    password.value = "";
    confirm.value = "";
    busy.value = false;
    request = null;
  }
}

onUnmounted(() => {
  request?.abort();
  password.value = "";
  confirm.value = "";
});
</script>

<template>
  <el-form class="login" label-position="top" @submit.prevent="submit">
    <img class="login-mark" src="/logo.png" alt="" width="72" height="72" />
    <h1>{{ t("joinTitle") }}</h1>
    <template v-if="ready">
      <p>{{ t("joinHint") }}</p>
      <el-form-item :label="t('username')"><el-input :model-value="username" disabled /></el-form-item>
      <el-form-item :label="t('password')"><el-input v-model="password" :disabled="busy" type="password" autocomplete="new-password" show-password /></el-form-item>
      <el-form-item :label="t('repeatPassword')"><el-input v-model="confirm" :disabled="busy" type="password" autocomplete="new-password" show-password /></el-form-item>
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <el-button type="primary" native-type="submit" :loading="busy" style="width: 100%">{{ t("save") }}</el-button>
    </template>
    <p v-else>{{ t("joinInvalid") }}</p>
  </el-form>
</template>
