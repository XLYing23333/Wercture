import { ref } from "vue";
import type { User } from "./api";

// Set by the router before the first paint, so a hard refresh does not
// draw the signed-in shell as a guest.
export const currentUser = ref<User | null>(null);
