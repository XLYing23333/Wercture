import { ElMessageBox } from "element-plus";
import { i18n } from "./i18n";

export async function confirmAction(message: string) {
  const t = i18n.global.t;
  try {
    await ElMessageBox.confirm(message, t("confirm"), {
      confirmButtonText: t("confirm"),
      cancelButtonText: t("cancel"),
      type: "warning",
    });
    return true;
  } catch {
    return false;
  }
}
