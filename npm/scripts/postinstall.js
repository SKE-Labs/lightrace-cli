#!/usr/bin/env node

/**
 * Downloads the pre-built Lightrace CLI binary from GitHub Releases.
 * Adapted from the Supabase CLI npm package pattern.
 */
"use strict";

import { createHash } from "crypto";
import fs from "fs";
import fetch from "node-fetch";
import { Agent } from "https";
import { HttpsProxyAgent } from "https-proxy-agent";
import path from "path";
import { extract } from "tar";
import zlib from "zlib";

const ARCH_MAPPING = {
  x64: "amd64",
  arm64: "arm64",
};

const PLATFORM_MAPPING = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const arch = ARCH_MAPPING[process.arch];
const platform = PLATFORM_MAPPING[process.platform];

const readPackageJson = async () => {
  const contents = await fs.promises.readFile("package.json");
  return JSON.parse(contents);
};

const getRepo = (pkg) => {
  const url = typeof pkg.repository === "string" ? pkg.repository : pkg.repository.url;
  const match = url.match(/github\.com\/([^/]+\/[^/.]+)/);
  if (!match) throw new Error(`Cannot parse repository URL: ${url}`);
  return match[1];
};

const getDownloadUrl = (pkg) => {
  const version = pkg.version;
  const repo = getRepo(pkg);
  const ext = platform === "windows" ? "zip" : "tar.gz";
  return `https://github.com/${repo}/releases/download/v${version}/lightrace_${version}_${platform}_${arch}.${ext}`;
};

const getChecksumUrl = (pkg) => {
  const version = pkg.version;
  const repo = getRepo(pkg);
  return `https://github.com/${repo}/releases/download/v${version}/checksums.txt`;
};

const fetchChecksums = async (pkg, agent) => {
  const url = getChecksumUrl(pkg);
  console.info("Downloading", url);
  const response = await fetch(url, { agent });
  if (!response.ok) {
    console.warn("Could not fetch checksum file:", response.status);
    return {};
  }
  const text = await response.text();
  const checksums = {};
  for (const line of text.split("\n")) {
    const [hash, filename] = line.trim().split(/\s+/);
    if (hash && filename) checksums[filename] = hash;
  }
  return checksums;
};

async function main() {
  if (!arch || !platform) {
    throw new Error(`Unsupported platform: ${process.platform} ${process.arch}`);
  }

  const pkg = await readPackageJson();
  if (platform === "windows") {
    pkg.bin[Object.keys(pkg.bin)[0]] += ".exe";
  }

  const binPath = pkg.bin[Object.keys(pkg.bin)[0]];
  const binDir = path.dirname(binPath);
  await fs.promises.mkdir(binDir, { recursive: true });

  const proxyUrl =
    process.env.npm_config_https_proxy ||
    process.env.npm_config_http_proxy ||
    process.env.npm_config_proxy;
  const agent = proxyUrl
    ? new HttpsProxyAgent(proxyUrl, { keepAlive: true })
    : new Agent({ keepAlive: true });

  const checksums = await fetchChecksums(pkg, agent);

  const url = getDownloadUrl(pkg);
  console.info("Downloading", url);
  const resp = await fetch(url, { agent });
  if (!resp.ok) {
    throw new Error(`Failed to download: ${resp.status} ${resp.statusText}`);
  }

  const hash = createHash("sha256");
  const binName = path.basename(binPath);

  const ungz = zlib.createGunzip();
  const untar = extract({ cwd: binDir }, [binName]);

  resp.body
    .on("data", (chunk) => hash.update(chunk))
    .pipe(ungz);

  ungz
    .on("end", () => {
      const ext = platform === "windows" ? "zip" : "tar.gz";
      const filename = `lightrace_${pkg.version}_${platform}_${arch}.${ext}`;
      const expected = checksums[filename];
      if (expected) {
        const actual = hash.digest("hex");
        if (actual !== expected) {
          throw new Error("Checksum mismatch. Downloaded data might be corrupted.");
        }
        console.info("Checksum verified.");
      }
    })
    .pipe(untar);

  await new Promise((resolve, reject) => {
    untar.on("error", reject);
    untar.on("end", resolve);
  });

  console.info("Installed Lightrace CLI successfully");
}

await main();
