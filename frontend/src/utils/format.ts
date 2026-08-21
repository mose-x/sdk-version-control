// Shared formatting utilities. Centralizes byte-size formatting so
// DetailPanel, SettingsPage, and future consumers stay in sync instead of
// duplicating the logic (and missing edge-case guards).

export const formatBytes = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  // Clamp: values in (0, 1) yield a negative index, and anything beyond GB
  // must stay at the largest unit.
  const idx = Math.max(0, Math.min(i, units.length - 1))
  return (
    (bytes / Math.pow(1024, idx)).toFixed(idx === 0 ? 0 : 1) + ' ' + units[idx]
  )
}
