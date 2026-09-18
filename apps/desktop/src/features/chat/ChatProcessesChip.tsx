import { useTranslation } from "react-i18next";
import type { ChatSessionTarget } from "@/api";
import { useOwnedSidebarRoots } from "@/app-facade";
import { InteractiveChip, useDelayedAppearance } from "@/ui";

export function ChatProcessesChip(props: Readonly<{ target: ChatSessionTarget; count: number }>) {
  return props.count > 0 ? <ActiveProcessesChip {...props} /> : null;
}

function ActiveProcessesChip({ target, count }: Readonly<{ target: ChatSessionTarget; count: number }>) {
  const { t } = useTranslation();
  const roots = useOwnedSidebarRoots();
  const visible = useDelayedAppearance(3000);
  if (!visible) return null;
  return (
    <InteractiveChip
      size="default"
      onClick={(event) => {
        const origin = event.currentTarget;
        void roots
          .open({ kind: "processes", projectID: target.projectID, sessionID: target.sessionID })
          .lifecycle.then((outcome) => {
            if (outcome === "closed" && origin.isConnected) origin.focus();
          });
      }}
    >
      {t("processes.activeCount", { count })}
    </InteractiveChip>
  );
}
