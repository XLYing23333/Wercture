export class ApiError extends Error {
  code: string;
  constructor(code: string) {
    super(code);
    this.code = code;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(path, { ...init, headers, credentials: "same-origin" });
  const text = await response.text();
  const body = text ? JSON.parse(text) : {};
  if (!response.ok) throw new ApiError(typeof body.error === "string" ? body.error : "invalid");
  return body as T;
}

export type User = { id: number; username: string; role: "admin" | "user"; status?: "pending" | "active" | "disabled"; invite?: string };
export type Settings = { locale: "en" | "zh-CN"; theme: "system" | "light" | "dark" };
export type Warehouse = {
  id: number;
  name: string;
  owner_id: number;
  owner: string;
  created_at: string;
  image_count: number;
  previews?: string[];
};
export type Token = { id: number; warehouse_id: number; name: string; token: string; created_at: string };
export type Picture = {
  id: number;
  public_id: string;
  name: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  warehouse_id: number | null;
  warehouse_name?: string;
  can_restore: boolean;
  uploaded_at: string;
  deleted_at?: string;
  path: string;
  url: string;
  render_path?: string;
  render_url?: string;
  markdown: string;
  html: string;
};

export function pageLink(picture: Picture) {
  return `${window.location.origin}${picture.path}`;
}

export function pageRender(picture: Picture) {
  const path = picture.render_path || `/i/${picture.public_id}.html`;
  return `${window.location.origin}${path}`;
}

export function pageMarkdown(picture: Picture) {
  const name = picture.name.replaceAll("]", "");
  if (picture.mime === "text/markdown") return `[${name}](${pageRender(picture)})`;
  return `![${name}](${pageLink(picture)})`;
}

export function pageHTML(picture: Picture) {
  if (picture.mime === "text/markdown") {
    return `<a href="${pageRender(picture)}">${escapeHTML(picture.name)}</a>`;
  }
  const alt = picture.name.replaceAll('"', "'");
  return `<img src="${pageLink(picture)}" alt="${alt}">`;
}

function escapeHTML(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}
