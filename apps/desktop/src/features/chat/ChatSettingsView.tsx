import { useId, useState } from "react";
import { LayoutGroup, motion } from "motion/react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";

import { type ChatSettingsMutation } from "@/api";
import { useChatRuntimePresentation } from "@/app-facade";
import {
  Button,
  Dialog,
  compactDialogWidth,
  motionDurationFromCSSVar,
  Popover,
  PopoverContent,
  SegmentedControl,
} from "@/ui";

import {
  activateChatSetting,
  settingsDisabledReason,
  settingsPresentation,
} from "./chatSettingsPresentation";
import { ChatAutoCompactionSwitch } from "./ChatAutoCompactionSwitch";
import { ThinkingSelector } from "./ThinkingSelector";
import { ChatSettingsChip } from "./ChatSettingsChip";
import { CustomThinkingEditor } from "./CustomThinkingEditor";
import { SettingsRow, SettingsSwitch } from "./ChatSettingsRows";
import { ChatSettingsAgentRow } from "./ChatSettingsAgentRow";
import type { ChatSettingsNavigation, ReadyChatSettings } from "./useChatSettings";
import { ChatSettingsSessionFacts } from "./ChatSettingsSessionFacts";
import { ComposerIcon } from "./ComposerIcon";
import { useAgentChooserLayout } from "./useAgentChooserLayout";

export type ChatSettingsViewProps =
  | Readonly<{ feature: Extract<ReadyChatSettings, { kind: "ready-new-chat" }> }>
  | Readonly<{
      feature: Extract<ReadyChatSettings, { kind: "ready-session" }>;
      navigation: ChatSettingsNavigation;
    }>;

export function ChatSettingsView(props: ChatSettingsViewProps) {
  const { feature } = props;
  const { t } = useTranslation();
  const [view, setView] = useState<"closed" | "overview" | "agents">("closed");
  const [agentSelection, setAgentSelection] = useState<string | null>(null);
  const transitionName = useId();
  const { overviewRef, scrollRef, selectedRef, maxHeight, overviewOpacity, capture } = useAgentChooserLayout(
    view === "agents",
  );
  const settings = settingsPresentation(feature);
  const reason = (
    editability: Parameters<typeof settingsDisabledReason>[1],
    policy?: Parameters<typeof settingsDisabledReason>[2],
  ) => settingsDisabledReason(t, editability, policy);
  function activate(operation: ChatSettingsMutation) {
    activateChatSetting(feature, operation);
  }
  const supervisor = settings.supervisor;
  const agentReason = reason(settings.agentEditability);
  const selectedAgent = {
    ...settings.selectedAgent,
    thinking:
      settings.thinking.kind === "unsupported" ? settings.selectedAgent.thinking : settings.thinking.value,
  };
  const agents =
    feature.kind === "ready-new-chat"
      ? feature.catalog.choices.map((choice) => choice.agent)
      : feature.settings.agentChoices;
  function showAgents() {
    if (agentReason !== undefined) return;
    capture();
    flushSync(() => {
      setView((current) => (current === "closed" ? current : "agents"));
    });
  }
  function selectAgent(role: string) {
    if (agentReason !== undefined) return;
    if (feature.kind === "ready-session" && role !== settings.selectedAgent.role) {
      setView("closed");
      setAgentSelection(role);
      return;
    }
    flushSync(() => {
      activate({ kind: "agent", role });
      setView((current) => (current === "closed" ? current : "overview"));
    });
  }
  const summary = {
    role: settings.selectedAgent.role,
    model: settings.selectedAgent.model,
    thinking: settings.thinking.kind === "unsupported" ? null : settings.thinking.value,
    fast: settings.fast.kind === "supported" && settings.fast.value,
  };
  return (
    <>
      <AgentSwitchDialog
        feature={feature}
        agentSelection={agentSelection}
        close={() => {
          setAgentSelection(null);
        }}
      />
      <LayoutGroup id={transitionName}>
        <Popover
          open={view !== "closed"}
          onOpenChange={(open) => {
            setView(open ? "overview" : "closed");
          }}
        >
          <ChatSettingsChip {...summary} />
          <PopoverContent
            align="start"
            className="w-96 max-w-[var(--radix-popover-content-available-width)] max-h-[var(--radix-popover-content-available-height)] grid-cols-[minmax(0,1fr)] grid-rows-[minmax(0,1fr)] gap-0 overflow-hidden p-[var(--space-1)]"
          >
            <motion.div
              layoutScroll
              ref={scrollRef}
              className="relative min-h-0 min-w-0 overflow-y-auto text-sm"
              style={{ maxHeight }}
            >
              {view === "agents" ? (
                <div>
                  {agents.map((agent) => (
                    <ChatSettingsAgentRow
                      agent={agent.role === selectedAgent.role ? selectedAgent : agent}
                      key={agent.role}
                      onActivate={() => {
                        selectAgent(agent.role);
                      }}
                      reason={agentReason}
                      selected={agent.role === selectedAgent.role}
                      transitionName={transitionName}
                      contentRef={agent.role === selectedAgent.role ? selectedRef : undefined}
                    />
                  ))}
                </div>
              ) : null}
              {/* Retain uncommitted input when returning through the already-selected Agent. */}
              <div ref={overviewRef} hidden={view === "agents"}>
                {view !== "agents" ? (
                  <ChatSettingsAgentRow
                    agent={selectedAgent}
                    onActivate={showAgents}
                    reason={agentReason}
                    selected
                    contentRef={selectedRef}
                    transitionName={transitionName}
                  />
                ) : null}
                <motion.div
                  animate={{ opacity: overviewOpacity }}
                  transition={{ duration: motionDurationFromCSSVar("--motion-morph", 350) / 1000 }}
                >
                  {settings.fast.kind === "supported" ? (
                    <SettingsSwitch
                      checked={settings.fast.value}
                      trailingIcon={<ComposerIcon kind="fast" />}
                      label={t("chatSettings.fast")}
                      onChange={(enabled) => {
                        activate({ kind: "fast", enabled });
                      }}
                      reason={reason(settings.fast.editability)}
                    />
                  ) : null}
                  <SettingsSwitch
                    checked={settings.questions.enabled}
                    label={t("chatSettings.questions")}
                    onChange={(enabled) => {
                      activate({ kind: "questions", enabled });
                    }}
                    reason={reason(settings.questions.editability)}
                  />
                  <ChatAutoCompactionSwitch feature={feature} />
                  <SettingsRow
                    onActivate={() => {
                      activate({
                        kind: "supervisor",
                        value:
                          supervisor.value === "off"
                            ? supervisor.baseline === "off"
                              ? "edits"
                              : supervisor.baseline
                            : "off",
                      });
                    }}
                    reason={reason(supervisor.editability)}
                  >
                    <span className="min-w-0 flex-1 truncate">{t("chatSettings.supervisor")}</span>
                    <div
                      onClick={(event) => {
                        event.stopPropagation();
                      }}
                    >
                      <SegmentedControl
                        size="compact"
                        ariaLabel={t("chatSettings.supervisor")}
                        disabled={reason(supervisor.editability) !== undefined}
                        onValueActivate={(value) => {
                          if (value === supervisor.value) activate({ kind: "supervisor", value });
                        }}
                        onValueChange={(value) => {
                          activate({ kind: "supervisor", value });
                        }}
                        options={[
                          { value: "all", label: t("chatSettings.supervisorAlways") },
                          { value: "edits", label: t("chatSettings.supervisorEdits") },
                          { value: "off", label: t("chatSettings.supervisorOff") },
                        ]}
                        value={supervisor.value}
                      />
                    </div>
                  </SettingsRow>
                  {settings.thinking.kind === "enumerated" && settings.thinking.values.length > 1 ? (
                    <SettingsRow reason={reason(settings.thinking.editability)}>
                      <div className="min-w-0 flex-1">
                        <ThinkingSelector
                          onCommit={(value) => {
                            activate({ kind: "thinking", value });
                          }}
                          thinking={settings.thinking}
                        />
                      </div>
                    </SettingsRow>
                  ) : null}
                  {settings.thinking.kind === "custom" ? (
                    <CustomThinkingEditor
                      reason={reason(settings.thinking.editability)}
                      feature={feature}
                      key={settings.selectedAgent.role}
                      thinking={settings.thinking}
                    />
                  ) : null}
                  {"navigation" in props ? (
                    <ChatSettingsSessionFacts
                      facts={props.feature.session}
                      navigation={props.navigation}
                      closeAndNavigate={(action) => {
                        flushSync(() => {
                          setView("closed");
                        });
                        action();
                      }}
                    />
                  ) : null}
                </motion.div>
              </div>
            </motion.div>
          </PopoverContent>
        </Popover>
      </LayoutGroup>
    </>
  );
}

function AgentSwitchDialog({
  feature,
  agentSelection,
  close,
}: Readonly<{
  feature: ReadyChatSettings;
  agentSelection: string | null;
  close(): void;
}>) {
  const { t } = useTranslation();
  const { activity } = useChatRuntimePresentation();
  const running = activity?.activeStep != null;
  if (feature.kind !== "ready-session") return null;
  return (
    <Dialog
      open={agentSelection !== null}
      title={t("chatSettings.switchAgent")}
      closeLabel={t("chatSettings.cancelSwitch")}
      onClose={close}
      closeDisabled={feature.agentPending}
      width={compactDialogWidth}
    >
      <div className="grid gap-[var(--space-3)]">
        <p>{t("chatSettings.switchAgentWarning", { agent: agentSelection })}</p>
        <div className="flex justify-end gap-[var(--space-2)]">
          <Button disabled={feature.agentPending} onClick={close}>
            {t("chatSettings.cancelSwitch")}
          </Button>
          <Button
            variant="primary"
            loading={feature.agentPending}
            disabled={feature.agentPending}
            onClick={() => {
              if (agentSelection === null) return;
              void feature
                .activate({ kind: "agent", role: agentSelection })
                .then((result) => {
                  if (result.result.kind === "applied") close();
                })
                .catch(() => {
                  /* Mutation owner presents failure and retains this choice. */
                });
            }}
          >
            {t(running ? "chatSettings.stopAndSwitchAgent" : "chatSettings.switchAgent")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
