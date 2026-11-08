import i18n from "i18next"
import LanguageDetector from "i18next-browser-languagedetector"
import { initReactI18next } from "react-i18next"

import en from "./locales/en.json"
import pl from "./locales/pl.json"

export const languages = [
  { code: "pl", label: "Polski" },
  { code: "en", label: "English" },
] as const

export type LanguageCode = (typeof languages)[number]["code"]

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: { en: { translation: en }, pl: { translation: pl } },
    fallbackLng: "en",
    supportedLngs: languages.map((l) => l.code),
    nonExplicitSupportedLngs: true,
    interpolation: { escapeValue: false },
    detection: {
      order: ["localStorage", "navigator"],
      lookupLocalStorage: "homepanel-lang",
      caches: ["localStorage"],
    },
  })

i18n.on("languageChanged", (lng) => {
  document.documentElement.lang = lng
})

export default i18n
