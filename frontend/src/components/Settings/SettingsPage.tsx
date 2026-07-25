import { useState, useEffect } from 'react'
import { Radio, Divider, Button, App, Tabs, Descriptions, Switch, Input, Modal, Progress } from 'antd'
import {
  SettingOutlined,
  BgColorsOutlined,
  GlobalOutlined,
  SyncOutlined,
  InfoCircleOutlined,
  FileProtectOutlined,
  GithubOutlined,
  WifiOutlined,
  LinkOutlined,
  FolderOutlined,
  DeleteOutlined,
  FileTextOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { GetSettings, SaveSettings, GetAppInfo, GetDefaultEndpoints, GetEndpoints, SaveEndpoints, GetDefaultInstallPath, GetInstallPath, MigrateInstallPath, CheckUpdate, DownloadUpdate, ApplyUpdate, GetTmpCacheSize, CleanTmpCache, CheckProxy, GetLogFiles, GetLogContent, CleanLogs, GetLogDir, DeleteLogFile } from '../../../wailsjs/go/main/App'
import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime/runtime'

interface AppSettings {
  theme: string
  language: string
  proxy: {
    enabled: boolean
    mode: string
    url: string
    protocol: string
  }
  githubMirror: string
  downloadThreads: number
}

interface AppInfo {
  version: string
  buildDate: string
  goVersion: string
  license: string
  repoUrl: string
}

interface EndpointInfo {
  sdkType: string
  displayName: string
  defaultEndpoint: string
}

interface UpdateInfo {
  hasUpdate: boolean
  latestVersion: string
  changelog: string
  downloadUrl: string
  filename: string
}

interface SettingsPageProps {
  onBack: () => void
  onThemeChange: (theme: string) => void
  onLanguageChange: (lang: string) => void
}

const SettingsPage: React.FC<SettingsPageProps> = ({ onThemeChange, onLanguageChange }) => {
  const { t, i18n } = useTranslation()
  const { message: msgApi } = App.useApp()
  const [settings, setSettings] = useState<AppSettings>({ theme: 'dark', language: 'zh', proxy: { enabled: false, mode: 'system', url: '', protocol: 'http' }, githubMirror: '', downloadThreads: 4 })
  const [appInfo, setAppInfo] = useState<AppInfo | null>(null)
  const [checking, setChecking] = useState(false)
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null)
  const [updateModalOpen, setUpdateModalOpen] = useState(false)
  const [downloadProgress, setDownloadProgress] = useState<{ percent: number; message: string; stage: string } | null>(null)
  const [downloading, setDownloading] = useState(false)
  const [downloadDone, setDownloadDone] = useState(false)
  const [defaultEndpoints, setDefaultEndpoints] = useState<EndpointInfo[]>([])
  const [customEndpoints, setCustomEndpoints] = useState<Record<string, string>>({})
  const [draftEndpoints, setDraftEndpoints] = useState<Record<string, string>>({})
  const [installPath, setInstallPath] = useState('')
  const [defaultInstallPath, setDefaultInstallPath] = useState('')
  const [installPathDraft, setInstallPathDraft] = useState('')
  const [migrating, setMigrating] = useState(false)
  const [tmpCacheSize, setTmpCacheSize] = useState(0)
  const [cleaning, setCleaning] = useState(false)
  const [checkingProxy, setCheckingProxy] = useState<Record<string, boolean>>({})
  const [logFiles, setLogFiles] = useState<any[]>([])
  const [logModalOpen, setLogModalOpen] = useState(false)
  const [currentLogFile, setCurrentLogFile] = useState('')
  const [logContent, setLogContent] = useState('')
  const [loadingLogs, setLoadingLogs] = useState(false)
  const [cleaningLogs, setCleaningLogs] = useState(false)
  const [logDir, setLogDir] = useState('')

  useEffect(() => {
    const off = EventsOn('update:progress', (progress: any) => {
      setDownloadProgress({ percent: progress.percent, message: progress.message, stage: progress.stage })
      if (progress.stage === 'done') {
        setDownloading(false)
        setDownloadDone(true)
      }
    })
    return () => { off() }
  }, [])

  useEffect(() => {
    GetSettings().then(s => setSettings(s))
    GetAppInfo().then(info => setAppInfo(info))
    GetDefaultEndpoints().then(de => setDefaultEndpoints(de || []))
    GetDefaultInstallPath().then(p => {
      setDefaultInstallPath(p)
      setInstallPathDraft(p)
    })
    GetInstallPath().then(p => {
      setInstallPath(p)
      setInstallPathDraft(p)
    })
    GetEndpoints().then(ce => {
      const endpoints = ce || {}
      setCustomEndpoints(endpoints)
      setDraftEndpoints({ ...endpoints })
    })
    loadTmpCacheSize()
    loadLogFiles()
    GetLogDir().then((d: string) => setLogDir(d || ''))
  }, [])

  const loadTmpCacheSize = () => {
    GetTmpCacheSize().then(size => setTmpCacheSize(size || 0))
  }

  const loadLogFiles = () => {
    setLoadingLogs(true)
    GetLogFiles().then((files: any[]) => {
      setLogFiles(files || [])
    }).finally(() => {
      setLoadingLogs(false)
    })
  }

  const handleViewLog = async (filename: string) => {
    setCurrentLogFile(filename)
    setLogContent('')
    setLogModalOpen(true)
    try {
      const content = await GetLogContent(filename)
      setLogContent(content || '')
    } catch (e: any) {
      msgApi.error(e?.message || e)
    }
  }

  const handleCleanLogs = () => {
    Modal.confirm({
      title: t('logs.cleanConfirm'),
      content: t('logs.cleanConfirmDesc'),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      okButtonProps: { danger: true },
      onOk: async () => {
        setCleaningLogs(true)
        try {
          await CleanLogs()
          msgApi.success(t('logs.cleanSuccess'))
          loadLogFiles()
        } catch (e: any) {
          msgApi.error(t('logs.cleanFail', { error: e?.message || e }))
        } finally {
          setCleaningLogs(false)
        }
      }
    })
  }

  const handleDeleteLog = (filename: string) => {
    Modal.confirm({
      title: t('logs.deleteConfirm'),
      content: t('logs.deleteConfirmDesc', { filename }),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await DeleteLogFile(filename)
          msgApi.success(t('logs.deleteSuccess'))
          loadLogFiles()
        } catch (e: any) {
          msgApi.error(t('logs.deleteFail', { error: e?.message || e }))
        }
      }
    })
  }

  const handleCheckProxy = async (target: string, label: string) => {
    setCheckingProxy(prev => ({ ...prev, [target]: true }))
    try {
      await CheckProxy(target)
      msgApi.success(t('settings.proxyCheckSuccess', { target: label }))
    } catch (e: any) {
      msgApi.error(t('settings.proxyCheckFail', { target: label, error: e?.message || e }))
    } finally {
      setCheckingProxy(prev => ({ ...prev, [target]: false }))
    }
  }

  const handleCleanTmpCache = async () => {
    setCleaning(true)
    try {
      await CleanTmpCache()
      msgApi.success(t('settings.cleanSuccess'))
      loadTmpCacheSize()
    } catch (e: any) {
      msgApi.error(e?.message || e)
    } finally {
      setCleaning(false)
    }
  }

  const formatBytes = (bytes: number): string => {
    if (bytes <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
  }

  const handleThemeChange = (theme: string) => {
    const newSettings = { ...settings, theme } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      onThemeChange(theme)
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleLanguageChange = (lang: string) => {
    const newSettings = { ...settings, language: lang } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      i18n.changeLanguage(lang)
      onLanguageChange(lang)
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleProxyToggle = (enabled: boolean) => {
    const newSettings = { ...settings, proxy: { ...settings.proxy, enabled } } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleProxyModeChange = (mode: string) => {
    const newSettings = { ...settings, proxy: { ...settings.proxy, mode } } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleProxyUrlChange = (url: string) => {
    const newSettings = { ...settings, proxy: { ...settings.proxy, url } } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleProxyProtocolChange = (protocol: string) => {
    const newSettings = { ...settings, proxy: { ...settings.proxy, protocol } } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleGithubMirrorChange = (url: string) => {
    const newSettings = { ...settings, githubMirror: url } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleDownloadThreadsChange = (threads: number) => {
    const newSettings = { ...settings, downloadThreads: threads } as any
    setSettings(newSettings)
    SaveSettings(newSettings).then(() => {
      msgApi.success(t('settings.settingsSaved'))
    })
  }

  const handleCheckUpdate = async () => {
    setChecking(true)
    setUpdateInfo(null)
    try {
      const info = await CheckUpdate()
      if (info.hasUpdate) {
        setUpdateInfo(info)
        setUpdateModalOpen(true)
      } else {
        msgApi.success(t('about.upToDate'))
      }
    } catch (e: any) {
      msgApi.success(t('about.upToDate'))
    } finally {
      setChecking(false)
    }
  }

  // Install path
  const hasInstallPathChange = () => {
    return installPathDraft.trim() !== installPath.trim()
  }

  const handleSaveInstallPath = () => {
    const newPath = installPathDraft.trim()
    if (!newPath) return
    Modal.confirm({
      title: t('settings.installPathConfirm'),
      content: t('settings.installPathConfirmDesc', { path: newPath }),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: async () => {
        setMigrating(true)
        try {
          await MigrateInstallPath(newPath)
          setInstallPath(newPath)
          msgApi.success(t('settings.installPathSuccess'))
        } catch (e: any) {
          msgApi.error(t('settings.installPathFail', { error: e?.message || e }))
        } finally {
          setMigrating(false)
        }
      }
    })
  }

  const handleResetInstallPath = () => {
    Modal.confirm({
      title: t('settings.installPathResetConfirm'),
      content: t('settings.installPathResetConfirmDesc', { path: defaultInstallPath }),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: async () => {
        setMigrating(true)
        try {
          await MigrateInstallPath(defaultInstallPath)
          setInstallPath(defaultInstallPath)
          setInstallPathDraft(defaultInstallPath)
          msgApi.success(t('settings.installPathSuccess'))
        } catch (e: any) {
          msgApi.error(t('settings.installPathFail', { error: e?.message || e }))
        } finally {
          setMigrating(false)
        }
      }
    })
  }

  const handleEndpointChange = (sdkType: string, value: string) => {
    setDraftEndpoints(prev => ({ ...prev, [sdkType]: value }))
  }

  const hasEndpointChanges = () => {
    return JSON.stringify(draftEndpoints) !== JSON.stringify(customEndpoints)
  }

  const handleSaveEndpoints = () => {
    Modal.confirm({
      title: t('endpoint.confirmSave'),
      content: t('endpoint.confirmSaveDesc'),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: () => {
        // Clean up empty values
        const cleaned: Record<string, string> = {}
        for (const [k, v] of Object.entries(draftEndpoints)) {
          if (v.trim()) cleaned[k] = v.trim()
        }
        SaveEndpoints(cleaned).then(() => {
          setCustomEndpoints(cleaned)
          setDraftEndpoints({ ...cleaned })
          msgApi.success(t('settings.settingsSaved'))
        })
      }
    })
  }

  const handleResetOneEndpoint = (sdkType: string, displayName: string) => {
    Modal.confirm({
      title: t('endpoint.confirmResetOne', { sdk: displayName }),
      content: t('endpoint.confirmResetOneDesc', { sdk: displayName }),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: () => {
        const newDraft = { ...draftEndpoints }
        delete newDraft[sdkType]
        setDraftEndpoints(newDraft)
        // Save immediately
        const cleaned: Record<string, string> = {}
        for (const [k, v] of Object.entries(newDraft)) {
          if (v.trim()) cleaned[k] = v.trim()
        }
        SaveEndpoints(cleaned).then(() => {
          setCustomEndpoints(cleaned)
          setDraftEndpoints({ ...cleaned })
          msgApi.success(t('settings.settingsSaved'))
        })
      }
    })
  }

  const tabItems = [
    {
      key: 'settings',
      label: (
        <span>
          <SettingOutlined />
          {t('settings.title')}
        </span>
      ),
      children: (
        <div className="settings-content">
          {/* Theme */}
          <div className="settings-section">
            <div className="settings-label">
              <BgColorsOutlined style={{ marginRight: 8 }} />
              {t('settings.theme')}
            </div>
            <Radio.Group
              value={settings.theme}
              onChange={e => handleThemeChange(e.target.value)}
              optionType="button"
              buttonStyle="solid"
            >
              <Radio.Button value="system">{t('settings.themeSystem')}</Radio.Button>
              <Radio.Button value="dark">{t('settings.themeDark')}</Radio.Button>
              <Radio.Button value="light">{t('settings.themeLight')}</Radio.Button>
            </Radio.Group>
          </div>

          <Divider />

          {/* Language */}
          <div className="settings-section">
            <div className="settings-label">
              <GlobalOutlined style={{ marginRight: 8 }} />
              {t('settings.language')}
            </div>
            <Radio.Group
              value={settings.language}
              onChange={e => handleLanguageChange(e.target.value)}
              optionType="button"
              buttonStyle="solid"
            >
              <Radio.Button value="zh">Chinese</Radio.Button>
              <Radio.Button value="en">English</Radio.Button>
            </Radio.Group>
          </div>

          <Divider />

          {/* Proxy */}
          <div className="settings-section">
            <div className="settings-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <span>
                <WifiOutlined style={{ marginRight: 8 }} />
                {t('settings.proxy')}
              </span>
              <Switch
                checked={settings.proxy?.enabled}
                onChange={handleProxyToggle}
                size="small"
              />
            </div>
            {settings.proxy?.enabled && (
              <div style={{ marginTop: 12 }}>
                <Radio.Group
                  value={settings.proxy.mode}
                  onChange={e => handleProxyModeChange(e.target.value)}
                  optionType="button"
                  buttonStyle="solid"
                  style={{ marginBottom: 12 }}
                >
                  <Radio.Button value="system">{t('settings.proxySystem')}</Radio.Button>
                  <Radio.Button value="custom">{t('settings.proxyCustom')}</Radio.Button>
                </Radio.Group>
                {settings.proxy.mode === 'custom' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    <Radio.Group
                      value={settings.proxy.protocol || 'http'}
                      onChange={e => handleProxyProtocolChange(e.target.value)}
                      optionType="button"
                      buttonStyle="solid"
                      size="small"
                    >
                      <Radio.Button value="http">HTTP</Radio.Button>
                      <Radio.Button value="socks5">SOCKS5</Radio.Button>
                    </Radio.Group>
                    <Input
                      placeholder={settings.proxy.protocol === 'socks5' ? '127.0.0.1:1080' : '127.0.0.1:7890'}
                      value={settings.proxy.url}
                      onChange={e => handleProxyUrlChange(e.target.value)}
                      onBlur={e => handleProxyUrlChange(e.target.value.trim())}
                      style={{ maxWidth: 400 }}
                    />
                  </div>
                )}
                <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                  <Button
                    size="small"
                    loading={checkingProxy['https://www.baidu.com']}
                    onClick={() => handleCheckProxy('https://www.baidu.com', t('settings.proxyCheckBaidu'))}
                  >
                    {t('settings.proxyCheckBaidu')}
                  </Button>
                  <Button
                    size="small"
                    loading={checkingProxy['https://www.google.com']}
                    onClick={() => handleCheckProxy('https://www.google.com', 'Google')}
                  >
                    Google
                  </Button>
                </div>
              </div>
            )}
          </div>

          <Divider />

          {/* GitHub Mirror */}
          <div className="settings-section">
            <div className="settings-label">
              <GithubOutlined style={{ marginRight: 8 }} />
              {t('settings.githubMirror')}
            </div>
            <div style={{ paddingLeft: 0 }}>
              <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                <Input
                  value={settings.githubMirror || ''}
                  onChange={e => {
                    const val = e.target.value
                    setSettings({ ...settings, githubMirror: val } as any)
                  }}
                  placeholder="https://ghfast.top"
                  style={{ flex: 1 }}
                />
                <Button
                  type="primary"
                  onClick={() => {
                    const trimmed = (settings.githubMirror || '').trim()
                    handleGithubMirrorChange(trimmed)
                  }}
                >
                  {t('app.confirm')}
                </Button>
              </div>
              <div style={{ fontSize: 12, color: '#888' }}>
                {t('settings.githubMirrorDesc')}
              </div>
            </div>
          </div>

          <Divider />

          {/* Download Threads */}
          <div className="settings-section">
            <div className="settings-label">
              <SyncOutlined style={{ marginRight: 8 }} />
              {t('settings.downloadThreads')}
            </div>
            <Radio.Group
              value={settings.downloadThreads || 4}
              onChange={e => handleDownloadThreadsChange(e.target.value)}
              optionType="button"
              buttonStyle="solid"
            >
              <Radio.Button value={1}>1</Radio.Button>
              <Radio.Button value={2}>2</Radio.Button>
              <Radio.Button value={4}>4</Radio.Button>
              <Radio.Button value={8}>8</Radio.Button>
            </Radio.Group>
            <div style={{ fontSize: 12, color: '#888', marginTop: 8 }}>
              {t('settings.downloadThreadsDesc')}
            </div>
          </div>

          <Divider />

          {/* Install Path */}
          <div className="settings-section">
            <div className="settings-label">
              <FolderOutlined style={{ marginRight: 8 }} />
              {t('settings.installPath')}
            </div>
            <div style={{ paddingLeft: 0 }}>
              <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                <Input
                  value={installPathDraft}
                  onChange={e => setInstallPathDraft(e.target.value)}
                  placeholder={defaultInstallPath}
                  style={{ flex: 1 }}
                />
                <Button
                  type="primary"
                  onClick={handleSaveInstallPath}
                  disabled={!hasInstallPathChange()}
                  loading={migrating}
                >
                  {t('app.confirm')}
                </Button>
                {installPath !== defaultInstallPath && (
                  <Button
                    onClick={handleResetInstallPath}
                    loading={migrating}
                  >
                    {t('settings.installPathReset')}
                  </Button>
                )}
              </div>
              <div style={{ fontSize: 12, color: '#888' }}>
                {t('settings.installPathDefault')}: {defaultInstallPath}
              </div>
            </div>
          </div>

          <Divider />

          {/* Storage Management */}
          <div className="settings-section">
            <div className="settings-label">
              <DeleteOutlined style={{ marginRight: 8 }} />
              {t('settings.storageManagement')}
            </div>
            <div style={{ paddingLeft: 0 }}>
              {/* Tmp cache */}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                <span style={{ fontSize: 13, color: '#aaa' }}>
                  {t('settings.tmpCache')}: {formatBytes(tmpCacheSize)}
                </span>
                <Button
                  danger
                  disabled={tmpCacheSize === 0}
                  loading={cleaning}
                  onClick={handleCleanTmpCache}
                >
                  {t('settings.clean')}
                </Button>
              </div>
            </div>
          </div>
        </div>
      ),
    },
    {
      key: 'endpoint',
      label: (
        <span>
          <LinkOutlined />
          {t('endpoint.title')}
        </span>
      ),
      children: (
        <div className="settings-content">
          <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: 13, color: '#888' }}>{t('endpoint.description')}</span>
            <Button
              type="primary"
              size="small"
              onClick={handleSaveEndpoints}
              disabled={!hasEndpointChanges()}
            >
              {t('endpoint.save')}
            </Button>
          </div>
          {[
            { key: 'runtime', sdkTypes: ['nodejs', 'jdk', 'go', 'python', 'rust', 'ruby', 'dotnet', 'php', 'perl'] },
            { key: 'build', sdkTypes: ['maven', 'gradle'] },
            { key: 'mobile', sdkTypes: ['flutter', 'android', 'dart'] },
          ].map(cat => {
            const catEndpoints = defaultEndpoints.filter(ep => cat.sdkTypes.includes(ep.sdkType))
            if (catEndpoints.length === 0) return null
            return (
              <div key={cat.key} style={{ marginBottom: 16 }}>
                <div style={{ fontSize: 12, color: '#666', fontWeight: 600, marginBottom: 8, textTransform: 'uppercase', letterSpacing: 1 }}>
                  {t(`categories.${cat.key}`)}
                </div>
                {catEndpoints.map(ep => {
                  const hasCustom = !!(draftEndpoints[ep.sdkType] && draftEndpoints[ep.sdkType].trim())
                  return (
                    <div key={ep.sdkType} style={{ marginBottom: 12, paddingLeft: 16 }}>
                      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 4 }}>
                        <span style={{ fontWeight: 500, minWidth: 100 }}>{ep.displayName}</span>
                        <span style={{ fontSize: 12, color: '#888', marginLeft: 8 }}>{ep.defaultEndpoint}</span>
                        {hasCustom && (
                          <Button
                            type="link"
                            size="small"
                            danger
                            style={{ marginLeft: 8, padding: '0 4px', fontSize: 12 }}
                            onClick={() => handleResetOneEndpoint(ep.sdkType, ep.displayName)}
                          >
                            {t('endpoint.resetDefault')}
                          </Button>
                        )}
                      </div>
                      <Input
                        size="small"
                        placeholder={ep.defaultEndpoint}
                        value={draftEndpoints[ep.sdkType] || ''}
                        onChange={e => handleEndpointChange(ep.sdkType, e.target.value)}
                        allowClear
                        style={{ maxWidth: 500 }}
                      />
                    </div>
                  )
                })}
              </div>
            )
          })}
        </div>
      ),
    },
    {
      key: 'logs',
      label: (
        <span>
          <FileTextOutlined />
          {t('logs.title')}
        </span>
      ),
      children: (
        <div className="settings-content">
          <div className="settings-section">
            <div className="settings-label" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span>
                <FileTextOutlined style={{ marginRight: 8 }} />
                {t('logs.logFiles')}
              </span>
              <div style={{ display: 'flex', gap: 8 }}>
                <Button
                  size="small"
                  icon={<ReloadOutlined />}
                  onClick={loadLogFiles}
                  loading={loadingLogs}
                >
                  {t('logs.refresh')}
                </Button>
                <Button
                  size="small"
                  danger
                  onClick={handleCleanLogs}
                  loading={cleaningLogs}
                  disabled={logFiles.length === 0}
                >
                  {t('logs.clean')}
                </Button>
              </div>
            </div>
            <div style={{ marginTop: 12, fontSize: 12, color: 'var(--ant-color-text-secondary)', marginBottom: 12 }}>
              {t('logs.logDir')}: {logDir}
            </div>
            {loadingLogs ? (
              <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--ant-color-text-secondary)' }}>Loading...</div>
            ) : logFiles.length === 0 ? (
              <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--ant-color-text-secondary)' }}>
                {t('logs.noLogs')}
              </div>
            ) : (
              <div style={{ border: '1px solid var(--ant-color-border)', borderRadius: 8, overflow: 'hidden' }}>
                <div style={{ display: 'flex', padding: '8px 12px', background: 'var(--ant-color-bg-layout)', fontSize: 12, fontWeight: 600, color: 'var(--ant-color-text-secondary)' }}>
                  <div style={{ flex: 2 }}>Filename</div>
                  <div style={{ flex: 1, textAlign: 'right' }}>{t('logs.size')}</div>
                  <div style={{ flex: 2, textAlign: 'right', paddingRight: 8 }}>{t('logs.modified')}</div>
                  <div style={{ width: 120, textAlign: 'right' }}>{t('logs.actions')}</div>
                </div>
                {logFiles.map((file: any, idx: number) => (
                  <div key={idx} style={{ display: 'flex', padding: '8px 12px', borderTop: idx > 0 ? '1px solid var(--ant-color-border)' : 'none', alignItems: 'center', fontSize: 13, color: 'var(--ant-color-text)' }}>
                    <div style={{ flex: 2, fontFamily: 'monospace' }}>{file.name}</div>
                    <div style={{ flex: 1, textAlign: 'right', color: 'var(--ant-color-text-secondary)' }}>{formatBytes(file.size)}</div>
                    <div style={{ flex: 2, textAlign: 'right', paddingRight: 8, color: 'var(--ant-color-text-secondary)' }}>{file.modTime}</div>
                    <div style={{ width: 120, textAlign: 'right', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                      <Button type="link" size="small" onClick={() => handleViewLog(file.name)}>
                        {t('logs.view')}
                      </Button>
                      <Button type="link" size="small" danger onClick={() => handleDeleteLog(file.name)}>
                        {t('logs.delete')}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      ),
    },
    {
      key: 'about',
      label: (
        <span>
          <InfoCircleOutlined />
          {t('about.title')}
        </span>
      ),
      children: (
        <div className="settings-content">
          {appInfo && (
            <>
              {/* Version Info */}
              <div className="settings-section">
                <div className="settings-label">
                  <InfoCircleOutlined style={{ marginRight: 8 }} />
                  {t('about.version')}
                </div>
                <Descriptions
                  column={1}
                  size="small"
                  bordered
                  style={{ maxWidth: 400 }}
                >
                  <Descriptions.Item label={t('about.version')}>
                    v{appInfo.version}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('about.buildDate')}>
                    {appInfo.buildDate}
                  </Descriptions.Item>
                  <Descriptions.Item label={t('about.goVersion')}>
                    {appInfo.goVersion}
                  </Descriptions.Item>
                </Descriptions>
              </div>

              <Divider />

              {/* Check for Updates */}
              <div className="settings-section">
                <div className="settings-label">
                  <SyncOutlined style={{ marginRight: 8 }} />
                  {t('about.checkUpdate')}
                </div>
                <Button
                  icon={<SyncOutlined spin={checking} />}
                  onClick={handleCheckUpdate}
                  loading={checking}
                >
                  {t('about.checkUpdateBtn')}
                </Button>
              </div>

              <Divider />

              {/* License */}
              <div className="settings-section">
                <div className="settings-label">
                  <FileProtectOutlined style={{ marginRight: 8 }} />
                  {t('about.license')}
                </div>
                <span style={{ color: '#aaa' }}>{appInfo.license}</span>
              </div>

              <Divider />

              {/* Repo */}
              <div className="settings-section">
                <div className="settings-label">
                  <GithubOutlined style={{ marginRight: 8 }} />
                  {t('about.repo')}
                </div>
                <a
                  href="#"
                  onClick={e => {
                    e.preventDefault()
                  }}
                  style={{ color: '#1677ff' }}
                >
                  {appInfo.repoUrl}
                </a>
              </div>
            </>
          )}
        </div>
      ),
    },
  ]

  return (
    <div className="settings-page">
      <div className="settings-header">
        <Tabs items={tabItems} style={{ flex: 1 }} />
      </div>

      <Modal
        title={downloadDone ? t('about.updateReady') : t('about.newVersion', { version: updateInfo?.latestVersion || '' })}
        open={updateModalOpen}
        onCancel={() => !downloading && setUpdateModalOpen(false)}
        closable={!downloading}
        maskClosable={!downloading}
        footer={
          downloadDone ? [
            <Button key="cancel" onClick={() => setUpdateModalOpen(false)}>
              {t('about.updateLater')}
            </Button>,
            <Button
              key="restart"
              type="primary"
              onClick={async () => {
                try {
                  await ApplyUpdate()
                } catch (e: any) {
                  msgApi.error(t('about.applyUpdateFail', { error: e?.message || e }))
                }
              }}
            >
              {t('about.restartNow')}
            </Button>,
          ] : downloading ? [
            <Button key="cancel" disabled>{t('about.downloading')}</Button>,
          ] : [
            <Button key="cancel" onClick={() => setUpdateModalOpen(false)}>
              {t('app.cancel')}
            </Button>,
            <Button
              key="download"
              type="primary"
              onClick={async () => {
                if (!updateInfo?.downloadUrl) {
                  BrowserOpenURL(updateInfo?.downloadUrl || '')
                  setUpdateModalOpen(false)
                  return
                }
                setDownloading(true)
                setDownloadProgress(null)
                setDownloadDone(false)
                try {
                  await DownloadUpdate(updateInfo.downloadUrl)
                } catch (e: any) {
                  setDownloading(false)
                  msgApi.error(t('about.downloadFail', { error: e?.message || e }))
                }
              }}
            >
              {updateInfo?.downloadUrl ? t('about.downloadAndInstall') : t('about.goDownload')}
            </Button>,
          ]
        }
      >
        <div style={{ marginBottom: 8, fontSize: 13, color: '#888' }}>
          {t('about.currentVersion', { version: appInfo?.version || '' })} → v{updateInfo?.latestVersion}
        </div>

        {downloading && downloadProgress && (
          <div style={{ marginBottom: 12 }}>
            <Progress percent={downloadProgress.percent} status="active" />
            <div style={{ fontSize: 12, color: '#888', marginTop: 4 }}>
              {downloadProgress.stage === 'done'
                ? t('progress.updateDone')
                : downloadProgress.stage === 'downloading'
                  ? t('progress.updateDownloading')
                  : downloadProgress.message}
            </div>
          </div>
        )}

        {downloadDone && (
          <div style={{ padding: '12px 16px', background: '#52c41a22', borderRadius: 8, marginBottom: 12, color: '#52c41a', fontSize: 13 }}>
            {t('about.updateReadyDesc')}
          </div>
        )}

        {updateInfo?.changelog && !downloading && (
          <div
            style={{
              background: 'var(--ant-color-bg-layout, #1a1a1a)',
              padding: '12px 16px',
              borderRadius: 8,
              fontSize: 13,
              lineHeight: 1.8,
              whiteSpace: 'pre-wrap',
              maxHeight: 300,
              overflowY: 'auto',
            }}
          >
            {updateInfo.changelog}
          </div>
        )}
      </Modal>

      <Modal
        title={t('logs.viewLog', { name: currentLogFile })}
        open={logModalOpen}
        onCancel={() => setLogModalOpen(false)}
        footer={[
          <Button key="close" onClick={() => setLogModalOpen(false)}>
            {t('logs.close')}
          </Button>,
        ]}
        width={800}
      >
        <div
          style={{
            background: '#000',
            color: '#0f0',
            padding: '12px 16px',
            borderRadius: 8,
            fontSize: 12,
            fontFamily: 'monospace',
            whiteSpace: 'pre-wrap',
            maxHeight: 500,
            overflowY: 'auto',
            lineHeight: 1.6,
          }}
        >
          {logContent || 'No content'}
        </div>
      </Modal>
    </div>
  )
}

export default SettingsPage
