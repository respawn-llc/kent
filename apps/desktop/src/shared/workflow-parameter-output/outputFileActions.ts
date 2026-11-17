import type { TFunction } from "i18next";

import { errorMessage } from "@/api";
import type { AppServices } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { showStatusToast } from "@/ui";
import { outputFileTarget, type WorkflowOutputRoot } from "./outputFileTarget";

export async function openOutputFile(
  path: string,
  root: WorkflowOutputRoot,
  nativeBridge: AppServices["nativeBridge"],
  t: TFunction,
): Promise<void> {
  const resolved = outputFileTarget(path, root);
  if (nativeBridge.capabilities.files.open && resolved.kind === "resolvable") {
    try {
      await nativeBridge.files.openDefault(resolved.target);
      return;
    } catch (cause) {
      showStatusToast({
        id: "workflow-output-file-open-failed",
        title: t("app.fileOpenFailed"),
        body: errorMessage(cause),
        tone: "danger",
      });
    }
  }
  try {
    await writeClipboardText(path, nativeBridge);
    showStatusToast({ id: "workflow-output-path-copied", title: t("app.copied"), tone: "success" });
  } catch (cause) {
    showStatusToast({
      id: "workflow-output-path-copy-failed",
      title: t("app.filePathCopyFailed"),
      body: errorMessage(cause),
      tone: "danger",
    });
  }
}
