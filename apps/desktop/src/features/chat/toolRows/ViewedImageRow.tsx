import { Image } from "lucide-react";
import { useState } from "react";
import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage, type ChatSessionTarget } from "@/api";
import { queryKeys, useAppServices } from "@/app-facade";
import {
  Button,
  useReducedMotion,
  motionDurationFromCSSVar,
  Shimmer,
  showStatusToast,
  Spinner,
  TranscriptDisclosure,
} from "@/ui";
import {
  createViewedImagePreviewModel,
  useViewedImagePreview,
  type ViewedImagePreviewInput,
} from "./ViewedImagePreviewModel";
import type { ToolPresentation } from "./toolPresentation";
import type { TranscriptToolSlotItem } from "./toolSlotTypes";
import { StaticToolRow } from "./StaticToolRow";

type ImagePresentation = Extract<ToolPresentation, { kind: "view-image" }>;
type RowContext = Readonly<{ target: ChatSessionTarget; effectiveWorkdir: string | null }>;

export function ViewedImageRow({
  presentation,
  item,
  target,
  effectiveWorkdir,
}: RowContext & Readonly<{ presentation: ImagePresentation; item: TranscriptToolSlotItem }>) {
  const { t } = useTranslation();
  const icon = presentation.running ? <Spinner size="sm" /> : <Image className="size-4" strokeWidth={1.8} />;
  if (presentation.outcome === "pdf") {
    return <ViewedPDFRow presentation={presentation} target={target} effectiveWorkdir={effectiveWorkdir} />;
  }
  return (
    <TranscriptDisclosure
      body={
        presentation.outcome === "raster" && item.kind === "committed" ? (
          <PresentImagePreview
            target={target}
            effectiveWorkdir={effectiveWorkdir}
            path={presentation.path}
            locator={item.row.Locator}
          />
        ) : (
          <div className="transcript-tool-sections">
            {presentation.body.map((section) => (
              <pre className="transcript-tool-plain-text" key={section.id}>
                {section.content}
              </pre>
            ))}
          </div>
        )
      }
      collapseLabel={t("chat.toolRows.collapse")}
      defaultExpanded={false}
      expandLabel={t("chat.toolRows.expand")}
      icon={icon}
      iconTone={presentation.iconTone}
      summary={presentation.compact}
      textTone="secondary"
    />
  );
}

type PreviewContext = RowContext & Pick<ViewedImagePreviewInput, "path" | "locator">;

function PresentImagePreview(props: PreviewContext) {
  const present = useIsPresent();
  const key = JSON.stringify([
    props.target.sessionID,
    props.locator.event_sequence,
    props.locator.row_ordinal,
  ]);
  return present ? <ImagePreview key={key} {...props} /> : null;
}

function ImagePreview(props: PreviewContext) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  // An expansion owns its acquisition; later Main View hydration must not restart it.
  const [model] = useState(() =>
    createViewedImagePreviewModel({
      services,
      client,
      target: props.target,
      path: props.path,
      locator: props.locator,
    }),
  );
  const preview = useViewedImagePreview(model);
  const reducedMotion = useReducedMotion();
  if (preview.state.kind === "source-error") {
    return <span>{t("chat.toolRows.imageReadFailed", { error: errorMessage(preview.state.error) })}</span>;
  }
  if (preview.state.kind === "image-error") {
    return <span>{t("chat.toolRows.imageReadFailed", { error: t("chat.toolRows.imageLoadFailed") })}</span>;
  }
  const loaded = preview.state.kind === "loaded";
  const imageURL = preview.state.kind === "resolving" ? null : preview.state.url;
  const scale = loaded ? Math.max(1, 150 / Math.max(preview.state.width, preview.state.height)) : 1;
  const transition = {
    duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-reveal", 280) / 1000,
  };
  return (
    <motion.div
      className="relative max-w-full"
      initial={false}
      animate={
        !loaded
          ? { width: 300, aspectRatio: 2 }
          : {
              width: preview.state.width * scale,
              aspectRatio: preview.state.width / preview.state.height,
            }
      }
      transition={transition}
    >
      <AnimatePresence initial={false}>
        {!loaded ? (
          <motion.div
            key="loading"
            className="absolute inset-0"
            exit={{ opacity: 0 }}
            transition={transition}
          >
            <Shimmer className="block size-full rounded-[var(--radius-s)]" />
          </motion.div>
        ) : null}
      </AnimatePresence>
      {imageURL === null ? null : (
        <motion.img
          src={imageURL}
          alt=""
          onLoad={(event) => {
            preview.loaded({
              width: event.currentTarget.naturalWidth,
              height: event.currentTarget.naturalHeight,
            });
          }}
          onError={() => {
            void preview.failed(undefined).catch((cause: unknown) => {
              reportError(cause);
            });
          }}
          initial={{ opacity: 0 }}
          animate={{ opacity: loaded ? 1 : 0 }}
          transition={transition}
          className="transcript-viewed-image absolute inset-0 size-full"
        />
      )}
    </motion.div>
  );
}

function ViewedPDFRow({
  presentation,
  target,
  effectiveWorkdir,
}: RowContext & Readonly<{ presentation: ImagePresentation }>) {
  const { nativeBridge, logger } = useAppServices();
  const { t } = useTranslation();
  const fileTarget = { path: presentation.path, basePath: effectiveWorkdir ?? undefined };
  const availability = useQuery({
    queryKey: queryKeys.viewedPDF(target.sessionID, { path: presentation.path, basePath: effectiveWorkdir }),
    queryFn: async () => {
      try {
        return await nativeBridge.files.fileAvailable(fileTarget);
      } catch (cause) {
        await logger.append("warn", "Viewed PDF availability failed.", { error: errorMessage(cause) });
        return false;
      }
    },
    enabled: nativeBridge.capabilities.files.stat && nativeBridge.capabilities.files.open,
    networkMode: "always",
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    gcTime: 0,
  });
  return (
    <StaticToolRow
      compact={presentation.compact}
      icon={<Image className="size-4" strokeWidth={1.8} />}
      iconTone={presentation.iconTone}
      actions={
        nativeBridge.capabilities.files.open && availability.data === true ? (
          <Button
            variant="ghost"
            onClick={() => {
              void nativeBridge.files.openDefault(fileTarget).catch((cause: unknown) => {
                showStatusToast({
                  id: "viewed-pdf-open-failed",
                  title: t("chat.toolRows.pdfOpenFailed"),
                  body: errorMessage(cause),
                  tone: "danger",
                });
              });
            }}
          >
            {t("chat.toolRows.openPDF")}
          </Button>
        ) : undefined
      }
    />
  );
}
