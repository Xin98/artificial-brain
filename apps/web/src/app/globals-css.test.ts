import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, it } from "vitest";

// This gate keeps globals.css in sync with the class names used by the UI.
// ITER-0002 shipped features whose classNames had no CSS rules, leaving the
// workbench unstyled; tests and builds stayed green because nothing checked
// the styling layer. Every static className token must have a matching
// selector in globals.css, and every dynamic prefix (e.g. `status-` in
// `status-${status}`) must match at least one selector.

const appDir = path.dirname(fileURLToPath(import.meta.url));
const srcRoot = path.resolve(appDir, "..");
const cssPath = path.resolve(appDir, "globals.css");

function collectTsxFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectTsxFiles(full));
      continue;
    }
    if (entry.name.endsWith(".tsx") && !entry.name.includes(".test.")) {
      files.push(full);
    }
  }
  return files;
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

interface Usage {
  classes: Map<string, Set<string>>;
  prefixes: Map<string, Set<string>>;
}

function record(map: Map<string, Set<string>>, name: string, file: string) {
  const files = map.get(name) ?? new Set<string>();
  files.add(file);
  map.set(name, files);
}

// extractUsage collects class tokens from static className="..." attributes
// and from template-literal className={`...`} attributes. In a template,
// literal segments between ${...} expressions yield complete classes, except
// the segment edges that touch an expression, which are dynamic prefixes.
function extractUsage(source: string, file: string, usage: Usage): void {
  for (const match of source.matchAll(/className="([^"]+)"/g)) {
    for (const name of match[1].split(/\s+/)) {
      if (name !== "") {
        record(usage.classes, name, file);
      }
    }
  }
  for (const match of source.matchAll(/className=\{`([\s\S]+?)`\}/g)) {
    const segments = match[1].split(/\$\{[^}]*\}/);
    segments.forEach((segment, index) => {
      const tokens = segment.split(/\s+/).filter((token) => token !== "");
      tokens.forEach((token, tokenIndex) => {
        const touchesExpression =
          (index > 0 && tokenIndex === 0) ||
          (index < segments.length - 1 && tokenIndex === tokens.length - 1);
        if (touchesExpression) {
          record(usage.prefixes, token, file);
        } else {
          record(usage.classes, token, file);
        }
      });
    });
  }
}

function describeUsage(map: Map<string, Set<string>>): string[] {
  return [...map.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(
      ([name, files]) => `${name} (used in ${[...files].sort().join(", ")})`,
    );
}

it("defines a globals.css rule for every className used by components", () => {
  const css = readFileSync(cssPath, "utf8");
  const usage: Usage = { classes: new Map(), prefixes: new Map() };

  for (const file of collectTsxFiles(srcRoot)) {
    extractUsage(
      readFileSync(file, "utf8"),
      path.relative(srcRoot, file),
      usage,
    );
  }

  const missingClasses = [...usage.classes.keys()].filter(
    (name) => !new RegExp(`\\.${escapeRegExp(name)}(?![\\w-])`).test(css),
  );
  const missingPrefixes = [...usage.prefixes.keys()].filter(
    (prefix) => !new RegExp(`\\.${escapeRegExp(prefix)}[\\w-]+`).test(css),
  );

  expect(
    describeUsage(
      new Map(missingClasses.map((name) => [name, usage.classes.get(name)!])),
    ),
  ).toEqual([]);
  expect(
    describeUsage(
      new Map(missingPrefixes.map((name) => [name, usage.prefixes.get(name)!])),
    ),
  ).toEqual([]);
});

it("keeps globals.css present and non-empty", () => {
  expect(existsSync(cssPath)).toBe(true);
  expect(readFileSync(cssPath, "utf8").trim().length).toBeGreaterThan(0);
});
