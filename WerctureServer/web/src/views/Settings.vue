<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import { ApiError, api, type Settings, type User } from "../api";
import { reasonText } from "../i18n";
import { applyTheme } from "../theme";

const router = useRouter();
const { t, locale } = useI18n();
const me = ref<User | null>(null);
const settings = ref<Settings | null>(null);
const current = ref("");
const next = ref("");
const again = ref("");
const busy = ref(false);

async function load() {
  const [session, value] = await Promise.all([api<User>("/api/v1/auth/session"), api<Settings>("/api/v1/settings")]);
  me.value = session;
  settings.value = value;
  locale.value = value.locale;
  applyTheme(value.theme);
}

function fail(err: unknown) {
  ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
}

async function saveAppearance() {
  if (!settings.value) return;
  try {
    const saved = await api<Settings>("/api/v1/settings", { method: "PUT", body: JSON.stringify(settings.value) });
    settings.value = saved;
    locale.value = saved.locale;
    applyTheme(saved.theme);
    ElMessage.success(t("saved"));
  } catch (err) {
    fail(err);
    await load();
  }
}

async function changePassword() {
  if (busy.value) return;
  if (next.value !== again.value) {
    ElMessage.error(t("invalid"));
    return;
  }
  busy.value = true;
  try {
    await api("/api/v1/auth/password", {
      method: "POST",
      body: JSON.stringify({ current: current.value, password: next.value, confirm: again.value }),
    });
    await router.push("/login");
  } catch (err) {
    fail(err);
  } finally {
    current.value = "";
    next.value = "";
    again.value = "";
    busy.value = false;
  }
}

onMounted(() => void load());
onUnmounted(() => {
  current.value = "";
  next.value = "";
  again.value = "";
});
</script>

<template>
  <main class="page" v-if="settings">
    <div class="settings-grid">
      <section v-if="me?.role === 'admin'" class="form-block">
        <h3>{{ t("settings") }}</h3>
        <div class="option-row">
          <span>{{ t("language") }}</span>
          <el-radio-group v-model="settings.locale" @change="saveAppearance">
            <el-radio-button value="zh-CN">中文</el-radio-button>
            <el-radio-button value="en">English</el-radio-button>
          </el-radio-group>
        </div>
        <div class="option-row">
          <span>{{ t("theme") }}</span>
          <el-radio-group v-model="settings.theme" @change="saveAppearance">
            <el-radio-button value="system">{{ t("system") }}</el-radio-button>
            <el-radio-button value="light">{{ t("light") }}</el-radio-button>
            <el-radio-button value="dark">{{ t("dark") }}</el-radio-button>
          </el-radio-group>
        </div>
      </section>
      <section class="form-block">
        <h3>{{ t("password") }}</h3>
        <p v-if="me" class="account-name">{{ t("username") }}：<strong>{{ me.username }}</strong></p>
        <el-form label-position="top" @submit.prevent="changePassword">
          <el-form-item :label="t('currentPassword')"><el-input v-model="current" type="password" show-password autocomplete="current-password" /></el-form-item>
          <el-form-item :label="t('newPassword')"><el-input v-model="next" type="password" show-password autocomplete="new-password" /></el-form-item>
          <el-form-item :label="t('repeatPassword')"><el-input v-model="again" type="password" show-password autocomplete="new-password" /></el-form-item>
          <el-button type="primary" native-type="submit" :loading="busy">{{ t("save") }}</el-button>
        </el-form>
      </section>
    </div>
  </main>
</template>
