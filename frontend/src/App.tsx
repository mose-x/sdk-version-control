import { useState, useEffect, useCallback, useMemo } from 'react'
import { ConfigProvider, theme, App as AntApp, Spin } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import { useTranslation } from 'react-i18next'
import Sidebar from './components/Sidebar/Sidebar'
import DetailPanel from './components/Panel/DetailPanel'
import HomePage from './components/HomePage/HomePage'
import PathModal from './components/PathModal/PathModal'
import SettingsPage from './components/Settings/SettingsPage'
import { SdkStatus, SdkType, InstallProgress } from './types/sdk'
import { GetAllSdkStatus, GetSettings } from '../wailsjs/go/main/App'
import { EventsOn, WindowSetLightTheme, WindowSetDarkTheme } from '../wailsjs/runtime/runtime'
import './i18n'
import './App.css'
import logoImg from './assets/logo.png'

function App() {
  const { i18n, t } = useTranslation()
  const [sdkStatuses, setSdkStatuses] = useState<SdkStatus[]>([])
  const [selectedSdk, setSelectedSdk] = useState<SdkType | null>(null)
  const [installProgressMap, setInstallProgressMap] = useState<Record<string, InstallProgress>>({})

  const downloadingSdks = useMemo(() => {
    const set = new Set<string>()
    for (const [key, p] of Object.entries(installProgressMap)) {
      if (p.stage !== 'done' && p.stage !== 'error') set.add(key)
    }
    return set
  }, [installProgressMap])
  const [themeMode, setThemeMode] = useState<string>('system')
  const [language, setLanguage] = useState<string>('zh')
  const [showSettings, setShowSettings] = useState(false)
  const [showPathModal, setShowPathModal] = useState(false)
  const [initialLoading, setInitialLoading] = useState(true)

  // Load settings on mount
  useEffect(() => {
    GetSettings().then(s => {
      if (s) {
        setThemeMode(s.theme || 'dark')
        const lang = s.language || 'zh'
        setLanguage(lang)
        i18n.changeLanguage(lang)
      }
    }).catch(() => {})
  }, [])

  const refreshStatuses = useCallback(async () => {
    try {
      const statuses = await GetAllSdkStatus()
      setSdkStatuses(statuses || [])
    } catch (e) {
      console.error('获取SDK状态失败:', e)
    }
  }, [])

  useEffect(() => {
    refreshStatuses().finally(() => setInitialLoading(false))
  }, [refreshStatuses])

  useEffect(() => {
    const off = EventsOn('install:progress', (progress: InstallProgress) => {
      setInstallProgressMap(prev => ({ ...prev, [progress.sdkType]: progress }))
      if (progress.stage === 'done' || progress.stage === 'error') {
        setTimeout(() => {
          refreshStatuses()
          setInstallProgressMap(prev => {
            const next = { ...prev }
            delete next[progress.sdkType]
            return next
          })
        }, 2000)
      }
    })
    return () => { off() }
  }, [refreshStatuses])

  // System theme detection
  const systemDark = useMemo(() => {
    if (typeof window !== 'undefined' && window.matchMedia) {
      return window.matchMedia('(prefers-color-scheme: dark)').matches
    }
    return true
  }, [])

  const isDark = themeMode === 'system' ? systemDark : themeMode === 'dark'

  // Sync window title bar theme with app theme
  useEffect(() => {
    if (isDark) {
      WindowSetDarkTheme()
    } else {
      WindowSetLightTheme()
    }
  }, [isDark])

  const antLocale = language === 'zh' ? zhCN : enUS

  const currentStatus = selectedSdk ? sdkStatuses.find(s => s.sdkType === selectedSdk) : undefined

  const handleSelectSdk = (sdk: SdkType) => {
    setSelectedSdk(sdk)
    setShowSettings(false)
  }

  return (
    <ConfigProvider
      locale={antLocale}
      theme={{
        algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: '#1677ff',
        },
      }}
    >
      <AntApp>
        {initialLoading ? (
          <div className={`app-container ${isDark ? 'dark' : 'light'}`} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100vh', gap: 16 }}>
            <img src={logoImg} alt="logo" style={{ width: 120, height: 120 }} />
            <Spin size="large" />
            <div style={{ fontSize: 15, color: isDark ? '#aaa' : '#666' }}>
              {t('sidebar.loadingSdk')}
            </div>
          </div>
        ) : (
        <div className={`app-container ${isDark ? 'dark' : 'light'}`}>
          <Sidebar
            statuses={sdkStatuses}
            selectedSdk={showSettings ? null : selectedSdk}
            downloadingSdks={downloadingSdks}
            onSelect={handleSelectSdk}
            onGoHome={() => { setSelectedSdk(null); setShowSettings(false) }}
            onOpenSettings={() => setShowSettings(true)}
            onRefresh={refreshStatuses}
          />
          {showSettings ? (
            <SettingsPage
              onBack={() => setShowSettings(false)}
              onThemeChange={setThemeMode}
              onLanguageChange={setLanguage}
            />
          ) : selectedSdk ? (
            <DetailPanel
              status={currentStatus}
              installProgress={currentStatus ? installProgressMap[currentStatus.sdkType] || null : null}
              onRefresh={refreshStatuses}
            />
          ) : (
            <HomePage
              statuses={sdkStatuses}
              onSelect={handleSelectSdk}
              onOpenPathInfo={() => setShowPathModal(true)}
            />
          )}
          <PathModal
            open={showPathModal}
            onClose={() => setShowPathModal(false)}
            onRefresh={refreshStatuses}
          />
        </div>
        )}
      </AntApp>
    </ConfigProvider>
  )
}

export default App
