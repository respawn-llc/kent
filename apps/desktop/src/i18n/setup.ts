import i18next, { type i18n } from "i18next";
import { initReactI18next } from "react-i18next";

import { englishResources } from "./en";

export const appI18n: i18n = i18next.createInstance();

export async function initializeI18n(): Promise<void> {
  if (appI18n.isInitialized) {
    return;
  }

  await appI18n.use(initReactI18next).init({
    lng: "en",
    fallbackLng: "en",
    resources: {
      en: englishResources,
    },
    interpolation: {
      escapeValue: false,
    },
    react: {
      useSuspense: false,
    },
  });
}
