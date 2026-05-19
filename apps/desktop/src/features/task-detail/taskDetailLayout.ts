import { taskDetailContentMaxWidthPx, taskDetailDialogOuterMaxWidthPx } from "@builder/desktop-native-bridge";

const px = (value: number): string => `${value}px`;

export const taskDetailContentMaxWidth = px(taskDetailContentMaxWidthPx);
export const taskDetailDialogOuterMaxWidth = px(taskDetailDialogOuterMaxWidthPx);
