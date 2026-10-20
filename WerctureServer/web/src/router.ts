import { createRouter, createWebHistory } from "vue-router";
import { api, type User } from "./api";
import { currentUser } from "./session";

const Home = () => import("./views/Home.vue");
const Warehouse = () => import("./views/Warehouse.vue");
const Users = () => import("./views/Users.vue");
const Trash = () => import("./views/Trash.vue");
const Settings = () => import("./views/Settings.vue");
const Login = () => import("./views/Login.vue");
const Setup = () => import("./views/Setup.vue");
const Join = () => import("./views/Join.vue");

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: Login },
    { path: "/setup", component: Setup },
    { path: "/init:token([a-f0-9]{64})", component: Setup },
    { path: "/join:token([a-f0-9]{64})", component: Join },
    { path: "/", component: Home },
    { path: "/warehouses/:id", component: Warehouse },
    { path: "/users", component: Users, meta: { admin: true } },
    { path: "/trash", component: Trash, meta: { admin: true } },
    { path: "/settings", component: Settings },
  ],
});

router.beforeEach(async (to) => {
  const setupToken = to.path.startsWith("/init") && typeof to.params.token === "string" ? to.params.token : "";
  const state = await api<{ initialized: boolean; authorized: boolean }>("/api/v1/setup", {
    headers: setupToken ? { "X-Wercture-Setup": setupToken } : {},
  });
  if (!state.initialized) {
    currentUser.value = null;
    if (to.path === "/setup") return true;
    if (setupToken && state.authorized) return true;
    return "/setup";
  }
  if (to.path === "/setup" || to.path.startsWith("/init")) return "/login";
  if (to.path.startsWith("/join")) return true;
  if (to.path === "/login") {
    currentUser.value = null;
    return true;
  }
  try {
    const session = await api<User>("/api/v1/auth/session");
    currentUser.value = session;
    if (to.meta.admin && session.role !== "admin") return "/";
    return true;
  } catch {
    currentUser.value = null;
    return "/login";
  }
});
