/// <reference types="vite/client" />

declare global {
  interface Window {
    go: {
      main: {
        App: {
          GetAllSdkStatus: () => Promise<any>;
          GetSdkStatus: (sdkType: string) => Promise<any>;
          GetRemoteVersions: (sdkType: string) => Promise<any>;
          InstallSdk: (sdkType: string, version: string) => Promise<void>;
          GetInstallDir: (sdkType: string) => Promise<string>;
          GetPathEntries: () => Promise<any[]>;
          ImportSdk: (externalPath: string, sdkType: string) => Promise<void>;
          GetSettings: () => Promise<{
            theme: string;
            language: string;
            proxy: {
              enabled: boolean;
              mode: string;
              url: string;
              protocol: string;
            };
            endpoints: Record<string, string>;
            installPath: string;
            githubMirror: string;
            downloadThreads: number;
          }>;
          SaveSettings: (settings: {
            theme: string;
            language: string;
            proxy: {
              enabled: boolean;
              mode: string;
              url: string;
              protocol: string;
            };
            endpoints?: Record<string, string>;
            installPath?: string;
            githubMirror?: string;
            downloadThreads: number;
          }) => Promise<void>;
          GetAppInfo: () => Promise<{
            version: string;
            buildDate: string;
            goVersion: string;
            license: string;
            repoUrl: string;
          }>;
          GetPackageManagers: (sdkType: string) => Promise<any[]>;
          DetectPathVersion: (sdkType: string) => Promise<string>;
          InstallPackageManager: (name: string) => Promise<void>;
          UpdatePackageManager: (name: string) => Promise<void>;
        };
      };
    };
    runtime: {
      EventsOn: (eventName: string, callback: (...data: any[]) => void) => () => void;
      EventsOff: (eventName: string) => void;
    };
  }
}

export {}
