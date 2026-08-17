import { fixupConfigRules } from "@eslint/compat";
import nextTypeScript from "eslint-config-next/typescript";
import nextVitals from "eslint-config-next/core-web-vitals";

export default [
  ...fixupConfigRules([...nextVitals, ...nextTypeScript]),
  {
    ignores: [".next/**", "node_modules/**", "eslint.config.mjs"],
  },
];
