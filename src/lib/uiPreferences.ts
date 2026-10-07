import type { Family, Target } from "../types";
const key = "uni-switch-ui-target-v1";
export function loadTarget(): { family: Family; claudeTarget: Target } {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "null");
    return {
      family: value?.family === "claude" ? "claude" : "codex",
      claudeTarget:
        value?.claudeTarget === "claude_cli" ? "claude_cli" : "claude_desktop",
    };
  } catch {
    return { family: "codex", claudeTarget: "claude_desktop" };
  }
}
export function rememberTarget(family: Family, claudeTarget: Target) {
  try {
    localStorage.setItem(key, JSON.stringify({ family, claudeTarget }));
  } catch {
    /* Storage availability cannot block switching. */
  }
}

export function loadListPreferences(): {
  pinned: string[];
  recent: Record<string, number>;
  autoSync: boolean;
} {
  try {
    const value = JSON.parse(
      localStorage.getItem("uni-switch-list-v1") || "null",
    );
    return {
      pinned: Array.isArray(value?.pinned)
        ? value.pinned.filter((v: unknown) => typeof v === "string")
        : [],
      recent:
        value?.recent && typeof value.recent === "object" ? value.recent : {},
      autoSync: value?.autoSync === true,
    };
  } catch {
    return { pinned: [], recent: {}, autoSync: false };
  }
}
export function rememberListPreferences(
  value: ReturnType<typeof loadListPreferences>,
) {
  try {
    localStorage.setItem("uni-switch-list-v1", JSON.stringify(value));
  } catch {
    /* Preferences never block application. */
  }
}
