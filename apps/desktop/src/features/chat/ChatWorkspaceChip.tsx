import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Folder } from "lucide-react";
import { errorMessage, type WorkspaceCatalogRow } from "@/api";
import { useAppServices, workspaceCatalogInfiniteQueryOptions } from "@/app-facade";
import { projectWorkspaceSelectorProjection } from "@/shared/workspaces";
import {
  Button,
  ErrorState,
  InteractiveChip,
  LoadingState,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  VirtualizedInfiniteList,
  directionalBoundary,
} from "@/ui";

export function ChatWorkspaceChip({
  projectID,
  selected,
  pending,
  loading,
  select,
}: Readonly<{
  projectID: string;
  selected: WorkspaceCatalogRow;
  pending: boolean;
  loading: boolean;
  select(row: WorkspaceCatalogRow): void;
}>) {
  const [open, setOpen] = useState(false);
  const { api } = useAppServices();
  const { t } = useTranslation();
  const catalog = useInfiniteQuery({
    ...workspaceCatalogInfiniteQueryOptions(api, projectID),
    enabled: open,
    retry: false,
  });
  const rows = projectWorkspaceSelectorProjection({
    catalogPages: catalog.data?.pages ?? [],
    initiatingRow: undefined,
    selectedSnapshot: selected,
    catalogExhausted: catalog.data !== undefined && !catalog.hasNextPage,
  }).rows;
  const boundary = (direction: "next" | "previous") =>
    directionalBoundary({
      failed: direction === "next" ? catalog.isFetchNextPageError : catalog.isFetchPreviousPageError,
      loading: direction === "next" ? catalog.isFetchingNextPage : catalog.isFetchingPreviousPage,
      message: errorMessage(catalog.error),
      loadingLabel: t("states.loading"),
      retryLabel: t("app.retry"),
      onRetry: () => {
        void (direction === "next" ? catalog.fetchNextPage() : catalog.fetchPreviousPage());
      },
    });
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="min-w-0">
              <PopoverTrigger asChild>
                <InteractiveChip disabled={pending}>
                  {pending || loading ? <Spinner size="sm" /> : <Folder size={14} />}
                  <span className="truncate">{selected.name}</span>
                </InteractiveChip>
              </PopoverTrigger>
            </span>
          </TooltipTrigger>
          <TooltipContent>{pending ? t("chat.workspacePending") : t("chat.workspace")}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent
        align="start"
        className="w-80 max-w-[var(--radix-popover-content-available-width)] max-h-[var(--radix-popover-content-available-height)] grid-rows-[minmax(0,1fr)] overflow-hidden"
      >
        {catalog.isPending ? (
          <LoadingState title={t("states.loading")} />
        ) : catalog.isError && catalog.data === undefined ? (
          <ErrorState
            title={t("states.error")}
            body={errorMessage(catalog.error)}
            onRetry={() => {
              void catalog.refetch();
            }}
            retryLabel={t("app.retry")}
          />
        ) : (
          <VirtualizedInfiniteList
            items={rows}
            getItemKey={(row) => row.id}
            renderItem={(row) => (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span>
                      <Button
                        variant={row.id === selected.id ? "primary-outline" : "ghost"}
                        disabled={pending}
                        className="w-full justify-start"
                        onClick={() => {
                          select(row);
                          setOpen(false);
                        }}
                      >
                        <span className="truncate">{row.name}</span>
                      </Button>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{pending ? t("chat.workspacePending") : row.rootPath}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
            estimateSize={() => 40}
            className="h-64 min-h-0 overflow-y-auto"
            loadingLabel={t("states.loading")}
            hasNextPage={catalog.hasNextPage}
            isFetchingNextPage={catalog.isFetchingNextPage}
            onLoadMore={() => {
              void catalog.fetchNextPage();
            }}
            nextBoundary={boundary("next")}
            hasPreviousPage={catalog.hasPreviousPage}
            isFetchingPreviousPage={catalog.isFetchingPreviousPage}
            onLoadPrevious={() => {
              void catalog.fetchPreviousPage();
            }}
            previousBoundary={boundary("previous")}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}
