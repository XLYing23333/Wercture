<script setup lang="ts">
import { onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ApiError, api } from "../api";
import { reasonText } from "../i18n";

const router = useRouter();
const { t } = useI18n();
const username = ref("");
const password = ref("");
const error = ref("");
const busy = ref(false);
let request: AbortController | null = null;

async function submit() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  request = new AbortController();
  try {
    await api("/api/v1/auth/login", {
      method: "POST",
      signal: request.signal,
      body: JSON.stringify({ username: username.value, password: password.value }),
    });
    password.value = "";
    await router.push("/");
  } catch (err) {
    if (!request.signal.aborted) error.value = t(reasonText(err instanceof ApiError ? err.code : "invalid"));
  } finally {
    password.value = "";
    busy.value = false;
    request = null;
  }
}
onUnmounted(() => {
  request?.abort();
  password.value = "";
});
</script>

<template>
  <el-form class="login" label-position="top" @submit.prevent="submit">
    <img class="login-mark" src="/logo.png" alt="" width="72" height="72" />
    <h1 class="login-name">Wercture</h1>
    <el-form-item :label="t('username')"><el-input v-model="username" :disabled="busy" autocomplete="username" /></el-form-item>
    <el-form-item :label="t('password')">
      <el-input v-model="password" :disabled="busy" type="password" autocomplete="current-password" show-password />
    </el-form-item>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <el-button type="primary" native-type="submit" :loading="busy" style="width: 100%; margin-top: 12px">{{ t("login") }}</el-button>
  </el-form>
</template>
