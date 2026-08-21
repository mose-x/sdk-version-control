// Shared SDK metadata for the frontend. Centralizes the SDK color palette and
// category grouping so HomePage, Sidebar, and PathModal stay in sync. Keep this
// the single source of truth — do not re-declare these maps locally.
//
// Colors are brand/official hex values for each SDK's identity color. The
// `22` alpha suffix is appended at call sites (e.g. Sidebar chip backgrounds)
// rather than baked in here, so consumers can pick the opacity they need.

export const sdkColors: Record<string, string> = {
  nodejs: '#339933',
  jdk: '#f89820',
  go: '#00ADD8',
  python: '#3776AB',
  rust: '#CE422B',
  ruby: '#CC342D',
  dotnet: '#512BD4',
  php: '#777BB4',
  perl: '#39457E',
  maven: '#C71A36',
  gradle: '#02303A',
  flutter: '#02569B',
  android: '#3DDC84',
  dart: '#0175C2',
}

export interface SdkCategory {
  key: string
  sdkTypes: string[]
}

export const SDK_CATEGORIES: SdkCategory[] = [
  {
    key: 'runtime',
    sdkTypes: [
      'nodejs',
      'jdk',
      'go',
      'python',
      'rust',
      'ruby',
      'dotnet',
      'php',
      'perl',
    ],
  },
  { key: 'build', sdkTypes: ['maven', 'gradle'] },
  { key: 'mobile', sdkTypes: ['flutter', 'android', 'dart'] },
]
