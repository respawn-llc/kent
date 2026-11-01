import type { ChangeEvent } from "react";
import { useForm, type FieldErrors, type RegisterOptions, type UseFormRegisterReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { useTextFieldSubmitShortcut } from "@/app-facade";
import { Button, Dialog, TextInput } from "@/ui";

const errorMessageSchema = z.string();

export type ProjectDraft = Readonly<{
  name: string;
  key: string;
  workspaceRoot: string;
}>;

export type ProjectCreateDialogProps = Readonly<{
  draft: ProjectDraft;
  onResult: (result: ProjectCreateResult) => void;
}>;

type ProjectCreateResult = Readonly<{ kind: "cancel" }> | Readonly<{ kind: "confirm"; draft: ProjectDraft }>;

export function ProjectCreateDialog({ draft, onResult }: ProjectCreateDialogProps) {
  const { t } = useTranslation();
  const form = useForm<ProjectDraft>({
    criteriaMode: "all",
    defaultValues: draft,
    mode: "onChange",
  });
  const nameField = form.register("name", projectNameRules(t));
  const keyField = form.register("key", projectKeyRules(t));
  const formShortcut = useTextFieldSubmitShortcut({
    available: true,
    kind: "form",
  });

  return (
    <Dialog
      className="w-[min(560px,calc(100vw-32px))]"
      closeLabel={t("app.close")}
      onClose={() => {
        onResult({ kind: "cancel" });
      }}
      open
      title={t("home.createProject")}
    >
      <form
        className="grid gap-[var(--space-3)]"
        onKeyDown={formShortcut}
        onSubmit={(event) =>
          void form.handleSubmit((draft) => {
            onResult({ kind: "confirm", draft });
          })(event)
        }
      >
        <TextInput
          error={fieldErrorMessages(form.formState.errors, "name")}
          label={t("home.projectName")}
          {...nameField}
        />
        <TextInput
          error={fieldErrorMessages(form.formState.errors, "key")}
          label={t("home.projectKey")}
          {...keyField}
          onChange={(event) => {
            handleUppercaseProjectKeyChange(event, keyField);
          }}
        />
        <TextInput label={t("home.workspaceRoot")} readOnly {...form.register("workspaceRoot")} />
        <Button type="submit" variant="primary">
          {t("home.createProject")}
        </Button>
      </form>
    </Dialog>
  );
}

function projectNameRules(t: ReturnType<typeof useTranslation>["t"]): RegisterOptions<ProjectDraft, "name"> {
  return {
    validate: {
      length: (value) => {
        const length = value.trim().length;
        return length >= 1 && length <= 80 ? true : t("form.projectNameLength");
      },
      whitespace: (value) => (value === value.trim() ? true : t("form.noEdgeWhitespace")),
      singleLine: (value) => (hasLineBreak(value) ? t("form.singleLine") : true),
    },
  };
}

function projectKeyRules(t: ReturnType<typeof useTranslation>["t"]): RegisterOptions<ProjectDraft, "key"> {
  return {
    validate: {
      length: (value) => (value.length >= 2 && value.length <= 8 ? true : t("form.projectKeyLength")),
      whitespace: (value) => (hasWhitespace(value) ? t("form.noWhitespace") : true),
      firstSymbol: (value) =>
        isAsciiUppercaseLetter(value.at(0) ?? "") ? true : t("form.projectKeyStartsWithLetter"),
      permittedSymbols: (value) =>
        hasOnlyAsciiUppercaseLettersAndDigits(value) ? true : t("form.projectKeySymbols"),
    },
  };
}

function handleUppercaseProjectKeyChange(
  event: ChangeEvent<HTMLInputElement>,
  keyField: UseFormRegisterReturn<"key">,
): void {
  event.target.value = event.target.value.toUpperCase();
  void keyField.onChange(event);
}

function fieldErrorMessages(
  fieldErrors: FieldErrors<ProjectDraft>,
  field: keyof ProjectDraft,
): readonly string[] | undefined {
  const error = fieldErrors[field];
  if (error === undefined) {
    return undefined;
  }
  const typedMessages = error.types === undefined ? [] : Object.values(error.types).flatMap(errorMessageItem);
  if (typedMessages.length > 0) {
    return typedMessages;
  }
  return error.message === undefined ? undefined : [error.message];
}

function hasLineBreak(value: string): boolean {
  for (const char of value) {
    if (char === "\n" || char === "\r") {
      return true;
    }
  }
  return false;
}

function hasWhitespace(value: string): boolean {
  for (const char of value) {
    if (char.trim().length === 0) {
      return true;
    }
  }
  return false;
}

function hasOnlyAsciiUppercaseLettersAndDigits(value: string): boolean {
  for (const char of value) {
    if (!isAsciiUppercaseLetter(char) && !isAsciiDigit(char)) {
      return false;
    }
  }
  return true;
}

function isAsciiUppercaseLetter(value: string): boolean {
  if (value.length !== 1) {
    return false;
  }
  const code = value.charCodeAt(0);
  return code >= 65 && code <= 90;
}

function isAsciiDigit(value: string): boolean {
  if (value.length !== 1) {
    return false;
  }
  const code = value.charCodeAt(0);
  return code >= 48 && code <= 57;
}

function errorMessageItem(value: unknown): readonly string[] {
  const parsed = errorMessageSchema.safeParse(value);
  return parsed.success && parsed.data.length > 0 ? [parsed.data] : [];
}
