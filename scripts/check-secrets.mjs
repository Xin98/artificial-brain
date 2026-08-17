import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync, readlinkSync } from "node:fs";
import path from "node:path";

const privateKeyPattern = new RegExp(
  "-----BEGIN [A-Z0-9 ]*PRIVATE" + " KEY-----",
);
const liveTokenPattern = new RegExp(
  "(?:gh" +
    "p_|github_" +
    "pat_|sk_" +
    "live_|rk_" +
    "live_|sk-" +
    "proj-|AKIA[0-9A-Z]{16}|xox[baprs]-)[A-Za-z0-9_=-]+",
);
const postgresURLPattern = /postgres(?:ql)?:\/\/[^/@\s]+:[^/@\s]+@[^\s]*/giu;
const interpolatedUserinfoPattern = /^\$\{[^}]+\}:\$\{[^}]+\}$/u;
const placeholders = new Set([
  "secret",
  "top-secret",
  "password",
  "example",
  "placeholder",
  "test-fixture",
  "...",
]);

function fail(operation) {
  process.stderr.write(`check-secrets: ${operation} failed\n`);
  process.exit(2);
}

function git(operation, args, options = {}) {
  const result = spawnSync("git", args, {
    cwd: options.cwd,
    input: options.input,
    encoding: null,
    maxBuffer: 256 * 1024 * 1024,
  });
  if (result.error || result.status !== 0 || result.signal !== null) {
    fail(operation);
  }
  return result.stdout;
}

function repositoryRoot() {
  const output = git("rev-parse", ["rev-parse", "--show-toplevel"]);
  if (output.length === 0 || output.at(-1) !== 0x0a) {
    fail("rev-parse");
  }
  return output.subarray(0, -1).toString("utf8");
}

function indexEntries(root) {
  const output = git("ls-files", ["ls-files", "--stage", "-z"], {
    cwd: root,
  });
  const entries = [];
  for (const record of splitNUL(output)) {
    const tab = record.indexOf(0x09);
    if (tab < 0) {
      fail("ls-files");
    }
    const metadata = record.subarray(0, tab).toString("ascii").split(" ");
    if (
      metadata.length !== 3 ||
      !/^[0-7]{6}$/u.test(metadata[0]) ||
      !/^[0-9a-f]+$/u.test(metadata[1]) ||
      !/^[0-3]$/u.test(metadata[2])
    ) {
      fail("ls-files");
    }
    entries.push({
      mode: metadata[0],
      oid: metadata[1],
      stage: metadata[2],
      path: record.subarray(tab + 1),
    });
  }
  return entries;
}

function splitNUL(buffer) {
  const records = [];
  let start = 0;
  for (let offset = 0; offset < buffer.length; offset += 1) {
    if (buffer[offset] !== 0) {
      continue;
    }
    records.push(buffer.subarray(start, offset));
    start = offset + 1;
  }
  if (start !== buffer.length) {
    fail("ls-files");
  }
  return records.filter((record) => record.length > 0);
}

function indexBlobs(root, entries) {
  if (entries.length === 0) {
    return [];
  }
  const input = Buffer.from(`${entries.map(({ oid }) => oid).join("\n")}\n`);
  const output = git("cat-file", ["cat-file", "--batch"], {
    cwd: root,
    input,
  });

  const blobs = [];
  let offset = 0;
  for (const entry of entries) {
    const headerEnd = output.indexOf(0x0a, offset);
    if (headerEnd < 0) {
      fail("cat-file");
    }
    const header = output
      .subarray(offset, headerEnd)
      .toString("ascii")
      .split(" ");
    if (
      header.length !== 3 ||
      header[0] !== entry.oid ||
      header[1] !== "blob" ||
      !/^\d+$/u.test(header[2])
    ) {
      fail("cat-file");
    }
    const size = Number(header[2]);
    const contentStart = headerEnd + 1;
    const contentEnd = contentStart + size;
    if (
      !Number.isSafeInteger(size) ||
      contentEnd >= output.length ||
      output[contentEnd] !== 0x0a
    ) {
      fail("cat-file");
    }
    blobs.push({
      path: entry.path,
      content: output.subarray(contentStart, contentEnd),
    });
    offset = contentEnd + 1;
  }
  if (offset !== output.length) {
    fail("cat-file");
  }
  return blobs;
}

function worktreeBlobs(root, entries) {
  const blobs = [];
  const seen = new Set();
  for (const entry of entries) {
    const key = entry.path.toString("hex");
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);

    const absolutePath = Buffer.concat([
      Buffer.from(root),
      Buffer.from(path.sep),
      entry.path,
    ]);
    let metadata;
    try {
      metadata = lstatSync(absolutePath);
    } catch (error) {
      if (error?.code === "ENOENT") {
        continue;
      }
      fail("tracked content read");
    }

    let content;
    try {
      if (metadata.isSymbolicLink()) {
        content = readlinkSync(absolutePath, { encoding: "buffer" });
      } else if (metadata.isFile()) {
        content = readFileSync(absolutePath);
      } else {
        fail("tracked content read");
      }
    } catch {
      fail("tracked content read");
    }
    blobs.push({ path: entry.path, content });
  }
  return blobs;
}

function escapedPath(buffer) {
  return JSON.stringify(buffer.toString("utf8")).slice(1, -1);
}

function scanBlob(blob, diagnostics) {
  const filename = escapedPath(blob.path);
  const lines = blob.content.toString("utf8").split("\n");
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const lineNumber = index + 1;
    if (privateKeyPattern.test(line)) {
      diagnostics.add(`${filename}:${lineNumber} private-key header`);
    }
    if (liveTokenPattern.test(line)) {
      diagnostics.add(`${filename}:${lineNumber} live-token prefix`);
    }

    postgresURLPattern.lastIndex = 0;
    for (const match of line.matchAll(postgresURLPattern)) {
      const userinfo = match[0]
        .replace(/^postgres(?:ql)?:\/\//iu, "")
        .replace(/@.*$/u, "");
      const separator = userinfo.indexOf(":");
      const password = separator < 0 ? "" : userinfo.slice(separator + 1);
      if (
        !interpolatedUserinfoPattern.test(userinfo) &&
        !placeholders.has(password.toLowerCase())
      ) {
        diagnostics.add(
          `${filename}:${lineNumber} credential-bearing PostgreSQL URL`,
        );
      }
    }
  }
}

const root = repositoryRoot();
const entries = indexEntries(root);
const diagnostics = new Set();
for (const blob of indexBlobs(root, entries)) {
  scanBlob(blob, diagnostics);
}
for (const blob of worktreeBlobs(root, entries)) {
  scanBlob(blob, diagnostics);
}

if (diagnostics.size > 0) {
  process.stderr.write(`${[...diagnostics].sort().join("\n")}\n`);
  process.exit(1);
}
