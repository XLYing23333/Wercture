<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { ElMessage } from "element-plus";
import { ApiError, api, type User } from "../api";
import { reasonText } from "../i18n";

const router = useRouter();
const { t } = useI18n();
const users = ref<User[]>([]);
const me = ref<User | null>(null);
const open = ref(false);
const username = ref("");
const createdURL = ref("");
const busy = ref(false);
const resetFor = ref<User | null>(null);
const removeFor = ref<User | null>(null);
const removeMode = ref<"trash" | "purge">("trash");
const next = ref("");
const again = ref("");

async function load() {
  const [list, session] = await Promise.all([api<{ users: User[] }>("/api/v1/users"), api<User>("/api/v1/auth/session")]);
  users.value = list.users;
  me.value = session;
}

function fail(err: unknown) {
  ElMessage.error(t(reasonText(err instanceof ApiError ? err.code : "invalid")));
}

function inviteURL(token: string) {
  return `${window.location.origin}/join${token}`;
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  ElMessage.success(t("copied"));
}

function closeCreate() {
  open.value = false;
  createdURL.value = "";
  username.value = "";
}

async function create() {
  if (busy.value || createdURL.value) return;
  busy.value = true;
  try {
    const user = await api<User>("/api/v1/users", {
      method: "POST",
      body: JSON.stringify({ username: username.value }),
    });
    createdURL.value = user.invite ? inviteURL(user.invite) : "";
    username.value = "";
    await load();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

async function reset() {
  if (!resetFor.value || busy.value) return;
  busy.value = true;
  try {
    await api(`/api/v1/users/${resetFor.value.id}/password`, {
      method: "POST",
      body: JSON.stringify({ password: next.value, confirm: again.value }),
    });
    resetFor.value = null;
    next.value = "";
    again.value = "";
    ElMessage.success(t("saved"));
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
    next.value = "";
    again.value = "";
  }
}

async function setEnabled(user: User, enabled: boolean) {
  try {
    await api(`/api/v1/users/${user.id}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) });
    await load();
  } catch (err) {
    fail(err);
  }
}

function remove(user: User) {
  removeMode.value = "trash";
  removeFor.value = user;
}

async function confirmRemove() {
  if (!removeFor.value || busy.value) return;
  busy.value = true;
  try {
    await api(`/api/v1/users/${removeFor.value.id}?mode=${removeMode.value}`, { method: "DELETE" });
    removeFor.value = null;
    await load();
  } catch (err) {
    fail(err);
  } finally {
    busy.value = false;
  }
}

function statusLabel(status?: string) {
  if (status === "pending") return t("statusPending");
  if (status === "disabled") return t("statusDisabled");
  return t("statusActive");
}

onMounted(() => void load());
</script>

<template>
  <main class="page">
    <div class="toolbar">
      <el-button type="primary" @click="open = true">{{ t("createUser") }}</el-button>
    </div>
    <el-table :data="users" class="centered-table">
      <el-table-column prop="username" :label="t('username')" min-width="120" align="center" />
      <el-table-column :label="t('status')" min-width="100" align="center">
        <template #default="{ row }">
          <el-tag :type="row.status === 'pending' ? 'warning' : row.status === 'disabled' ? 'info' : 'success'" effect="plain">{{ statusLabel(row.status) }}</el-tag>
          <el-button v-if="row.status === 'pending' && row.invite" link @click="copy(inviteURL(row.invite))">{{ t("copyActivation") }}</el-button>
        </template>
      </el-table-column>
      <el-table-column :label="t('role')" min-width="100" align="center">
        <template #default="{ row }">{{ row.role === "admin" ? t("roleAdmin") : t("roleUser") }}</template>
      </el-table-column>
      <el-table-column :label="t('actions')" min-width="520" align="center">
        <template #default="{ row }">
          <span class="row-actions">
            <el-button color="#1f6feb" @click="router.push({ path: '/', query: { owner: String(row.id) } })">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16v12H4z" /><path d="M4 7l4-3h8l4 3" /></svg>
              {{ t("viewWarehouses") }}
            </el-button>
            <template v-if="row.id !== me?.id">
              <el-button color="#c48a12" @click="resetFor = row">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v8" /><path d="M7 8a6 6 0 1 0 6-4" /></svg>
                {{ t("resetPassword") }}
              </el-button>
              <el-button v-if="row.status === 'disabled'" color="#1f8a4c" @click="setEnabled(row, true)">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5l11 7-11 7z" /></svg>
                {{ t("startUser") }}
              </el-button>
              <el-button v-else color="#5b6b80" @click="setEnabled(row, false)">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5h3v14H8zM13 5h3v14h-3z" /></svg>
                {{ t("stopUser") }}
              </el-button>
              <el-button color="#d64545" @click="remove(row)">
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 8h14l-1 12H6z" /><path d="M9 8V5h6v3" /></svg>
                {{ t("deleteUser") }}
              </el-button>
            </template>
          </span>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('emptyUsers')" />
      </template>
    </el-table>
    <el-dialog :model-value="open" :title="t('createUser')" width="min(520px, calc(100vw - 24px))" @close="closeCreate">
      <el-form v-if="!createdURL" class="login invite-form" label-position="top" @submit.prevent="create">
        <img class="login-mark" src="/logo.png" alt="" width="72" height="72" />
        <p>{{ t("createUserHint") }}</p>
        <el-form-item :label="t('username')"><el-input v-model="username" maxlength="64" autocomplete="off" /></el-form-item>
        <el-button type="primary" native-type="submit" :loading="busy" style="width: 100%">{{ t("createUser") }}</el-button>
      </el-form>
      <div v-else>
        <p class="hint">{{ t("createUserHint") }}</p>
        <p class="hint">{{ t("activationLink") }}</p>
        <code class="invite-url">{{ createdURL }}</code>
        <el-button type="primary" @click="copy(createdURL)">{{ t("copyActivation") }}</el-button>
      </div>
    </el-dialog>
    <el-dialog :model-value="removeFor !== null" :title="t('deleteUser')" width="min(520px, calc(100vw - 24px))" :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" @close="removeFor = null">
      <p v-if="removeFor">{{ removeFor.username }}</p>
      <el-radio-group v-model="removeMode" :disabled="busy">
        <el-radio value="trash">{{ t("moveToTrash") }}</el-radio>
        <el-radio value="purge">{{ t("deleteNow") }}</el-radio>
      </el-radio-group>
      <p class="hint">{{ t(removeMode === "purge" ? "confirmDeleteUserPurge" : "confirmDeleteUser") }}</p>
      <template #footer>
        <div class="form-actions">
          <el-button :disabled="busy" @click="removeFor = null">{{ t("cancel") }}</el-button>
          <el-button type="danger" :loading="busy" @click="confirmRemove">{{ t("confirm") }}</el-button>
        </div>
      </template>
    </el-dialog>
    <el-dialog :model-value="resetFor !== null" :title="t('resetPassword')" width="min(420px, calc(100vw - 24px))" @close="resetFor = null">
      <el-form label-position="top" @submit.prevent="reset">
        <el-form-item :label="t('newPassword')"><el-input v-model="next" type="password" show-password /></el-form-item>
        <el-form-item :label="t('repeatPassword')"><el-input v-model="again" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <div class="form-actions">
          <el-button @click="resetFor = null">{{ t("cancel") }}</el-button>
          <el-button type="primary" :loading="busy" @click="reset">{{ t("save") }}</el-button>
        </div>
      </template>
    </el-dialog>
  </main>
</template>
