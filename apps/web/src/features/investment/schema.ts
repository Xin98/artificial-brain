import { isRFC3339 } from "../validation";
export interface Schema {
  $ref?: string;
  type?: string;
  anyOf?: Schema[];
  properties?: Record<string, Schema>;
  required?: string[];
  additionalProperties?: boolean;
  items?: Schema;
  pattern?: string;
  format?: string;
  minItems?: number;
  maxItems?: number;
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  enum?: unknown[];
}
export function matches(
  value: unknown,
  s: Schema,
  all: Record<string, Schema>,
): boolean {
  if (s.$ref)
    return matches(
      value,
      all[s.$ref.split("/").pop()!] ?? { type: "never" },
      all,
    );
  if (s.anyOf) return s.anyOf.some((x) => matches(value, x, all));
  if (s.enum && !s.enum.includes(value)) return false;
  switch (s.type) {
    case "null":
      return value === null;
    case "string":
      return (
        typeof value === "string" &&
        (!s.pattern || new RegExp(s.pattern).test(value)) &&
        (!s.format || s.format !== "date-time" || isRFC3339(value)) &&
        (s.minLength === undefined || value.length >= s.minLength) &&
        (s.maxLength === undefined || value.length <= s.maxLength)
      );
    case "number":
    case "integer":
      return (
        typeof value === "number" &&
        Number.isFinite(value) &&
        (s.type !== "integer" || Number.isInteger(value)) &&
        (s.minimum === undefined || value >= s.minimum) &&
        (s.maximum === undefined || value <= s.maximum)
      );
    case "boolean":
      return typeof value === "boolean";
    case "array":
      return (
        Array.isArray(value) &&
        (s.minItems === undefined || value.length >= s.minItems) &&
        (s.maxItems === undefined || value.length <= s.maxItems) &&
        value.every((x) => matches(x, s.items!, all))
      );
    case "object":
      if (!value || typeof value !== "object" || Array.isArray(value))
        return false;
      const r = value as Record<string, unknown>;
      const p = s.properties ?? {};
      return (
        (s.required ?? []).every((k) => Object.hasOwn(r, k)) &&
        Object.keys(r).every((k) =>
          p[k] ? matches(r[k], p[k], all) : s.additionalProperties !== false,
        )
      );
    default:
      return false;
  }
}
