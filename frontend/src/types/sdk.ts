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
  pathConfigured: boolean;  // 在 PATH 中但不在 .svc
  pathVersion: string;      // PATH 中检测到的版本号
  currentVersion: string;
  installedVersions: string[];
  installPath: string;
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
