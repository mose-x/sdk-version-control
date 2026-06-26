export type SdkType = 
  | 'nodejs' | 'jdk' | 'go' | 'python' | 'rust' | 'ruby' | 'dotnet' | 'php' | 'perl'
  | 'maven' | 'gradle'
  | 'flutter' | 'android' | 'dart';

export interface PackageManagerInfo {
  name: string;
  version: string;
  installed: boolean;
  parentSdk: string;
}

export interface SdkStatus {
  sdkType: string;
  displayName: string;
  configured: boolean;
  pathConfigured: boolean;  // In PATH but not in .svc
  pathVersion: string;      // Version detected in PATH
  currentVersion: string;
  installedVersions: string[];
  installPath: string;
  needsSwitch: boolean;     // true if currentVersion is not in installedVersions
}

export interface VersionInfo {
  version: string;
  major: number;
  downloadUrl: string;
  fileName: string;
  isLts: boolean;
  releaseDate: string;
}

export interface InstallProgress {
  sdkType: string;
  version: string;
  stage: 'downloading' | 'extracting' | 'configuring_path' | 'verifying' | 'done' | 'error';
  percent: number;
  message: string;
  downloadedBytes: number;
  totalBytes: number;
  speedBytesPerSec: number;
  downloadUrl: string;
}

export interface ProxySettings {
  enabled: boolean;
  mode: 'system' | 'custom';
  url: string;
}
