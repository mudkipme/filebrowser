const archiveExtensions = [
  ".tar.gz",
  ".tar.xz",
  ".zip",
  ".rar",
  ".7z",
  ".tar",
  ".gz",
  ".xz",
];

export function stripArchiveExtension(name: string): string | null {
  const lower = name.toLowerCase();

  for (const ext of archiveExtensions) {
    if (lower.endsWith(ext)) {
      return name.slice(0, -ext.length);
    }
  }

  return null;
}

export function isSupportedArchive(name: string): boolean {
  return stripArchiveExtension(name) !== null;
}
