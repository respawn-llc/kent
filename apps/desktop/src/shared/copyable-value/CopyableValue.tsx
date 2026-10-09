import type { ReactNode } from "react";

import { errorMessage } from "@/api";
import { useAppServices } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { CopyableValueButton, showStatusToast } from "@/ui";

type CopyFeedback = Readonly<{ id: string; title: string }>;

export function CopyableValue({
  accessibleLabel,
  children,
  className,
  clipboardValue,
  copyFailed,
  copySucceeded,
  size = "sm",
}: Readonly<{
  accessibleLabel: string;
  children: ReactNode;
  className?: string;
  clipboardValue: string;
  copyFailed: CopyFeedback;
  copySucceeded: CopyFeedback;
  size?: "sm" | "xs";
}>) {
  const { nativeBridge } = useAppServices();
  return (
    <CopyableValueButton
      accessibleLabel={accessibleLabel}
      {...(className === undefined ? {} : { className })}
      size={size}
      onActivate={() => {
        void writeClipboardText(clipboardValue, nativeBridge)
          .then(() => {
            showStatusToast({
              id: copySucceeded.id,
              title: copySucceeded.title,
              tone: "success",
            });
          })
          .catch((error: unknown) => {
            showStatusToast({
              body: errorMessage(error),
              id: copyFailed.id,
              title: copyFailed.title,
              tone: "danger",
            });
          });
      }}
    >
      {children}
    </CopyableValueButton>
  );
}
