import { useState, useEffect, useCallback, useRef } from 'react'
import { Button, Input, Tag, Spin, Progress, App, Tooltip, Modal, Dropdown, Result } from 'antd'
import {
  CheckCircleFilled,
  CloseCircleFilled,
  WarningFilled,
  DeleteOutlined,
  DownloadOutlined,
  SearchOutlined,
  ReloadOutlined,
  CloudUploadOutlined,
  CopyOutlined,
  ImportOutlined,
  FolderOpenOutlined,
  FileOutlined,
} from '@ant-design/icons'
import { useTranslation } from 'react-i18next'
import { SdkStatus, VersionInfo, InstallProgress, PackageManagerInfo } from '../../types/sdk'
import { GetRemoteVersions, InstallSdk, GetPackageManagers, InstallPackageManager, UpdatePackageManager, SwitchVersion, SelectLocalFile, SelectLocalDir, ImportLocalSdk, GetSdkDownloadURL, CheckSystemConflicts, UninstallVersion, CancelInstall } from '../../../wailsjs/go/main/App'

interface DetailPanelProps {
  status: SdkStatus | undefined
  installProgress: InstallProgress | null
  onRefresh: () => void
}

const DetailPanel: React.FC<DetailPanelProps> = ({ status, installProgress, onRefresh }) => {
  const [versions, setVersions] = useState<VersionInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState(false)
  const [searchText, setSearchText] = useState('')
  const [installing, setInstalling] = useState(false)
  const [packageManagers, setPackageManagers] = useState<PackageManagerInfo[]>([])
  const [pmLoading, setPmLoading] = useState<string>('')
  const [switching, setSwitching] = useState(false)
  const [importing, setImporting] = useState(false)
  const [conflicts, setConflicts] = useState<string[]>([])
  const { message: msgApi } = App.useApp()
  const { t } = useTranslation()
  const [modal, modalContextHolder] = Modal.useModal()
  const modalRef = useRef<any>(null)

  const formatBytes = (bytes: number): string => {
    if (bytes <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    const idx = Math.min(i, units.length - 1)
    return (bytes / Math.pow(1024, idx)).toFixed(idx === 0 ? 0 : 1) + ' ' + units[idx]
  }

  const translateProgress = (progress: InstallProgress): string => {
    switch (progress.stage) {
      case 'downloading':
        if (progress.totalBytes > 0) {
          const percent = Math.floor(progress.downloadedBytes * 100 / progress.totalBytes)
          return t('progress.downloadingPercent', { percent })
        }
        return t('progress.downloading')
      case 'extracting':
        return t('progress.extracting')
      case 'configuring_path':
        return t('progress.configuring_path')
      case 'done':
        return t('progress.done')
      case 'error':
        return t('progress.error', { error: progress.message })
      default:
        return progress.message
    }
  }

  const fetchVersions = useCallback(async (stale?: () => boolean) => {
    if (!status) return
    setLoading(true)
    setLoadError(false)
    try {
      const result = await GetRemoteVersions(status.sdkType)
      if (stale?.()) return
      setVersions(result || [])
    } catch (e) {
      if (stale?.()) return
      console.error('获取远程版本失败:', e)
      setLoadError(true)
    } finally {
      if (!stale?.()) setLoading(false)
    }
  }, [status, t])

  const fetchPackageManagers = useCallback(async (stale?: () => boolean) => {
    if (!status) return
    try {
      const pms = await GetPackageManagers(status.sdkType)
      if (stale?.()) return
      setPackageManagers(pms || [])
    } catch { if (!stale?.()) setPackageManagers([]) }
  }, [status])

  useEffect(() => {
    let stale = false
    if (status) {
      setVersions([])
      setLoadError(false)
      setSearchText('')
      fetchVersions(() => stale)
      fetchPackageManagers(() => stale)
    }
    return () => { stale = true }
  }, [status, fetchVersions, fetchPackageManagers])

  useEffect(() => {
    if (!status) return
    let stale = false
    setConflicts([])
    CheckSystemConflicts(status.sdkType).then((entries) => {
      if (stale) return
      if (entries && entries.length > 0) {
        setConflicts(entries)
        modal.warning({
          title: t('detail.systemConflictTitle'),
          content: (
            <div>
              <p>{t('detail.systemConflictMsg')}</p>
              <ul style={{ maxHeight: 200, overflow: 'auto', paddingLeft: 20, margin: '8px 0' }}>
                {entries.map((e: string, i: number) => (
                  <li key={i} style={{ fontSize: 12, color: '#666', wordBreak: 'break-all' }}>{e}</li>
                ))}
              </ul>
            </div>
          ),
          okText: t('app.confirm'),
          width: 520,
        })
      }
    }).catch(() => {})
    return () => { stale = true }
  }, [status])

  useEffect(() => {
    if (installProgress && modalRef.current) {
      modalRef.current.update({
        content: (
          <div style={{ textAlign: 'center', padding: '10px 0' }}>
            <Progress
              percent={installProgress.percent}
              status={installProgress.stage === 'error' ? 'exception' : installProgress.stage === 'done' ? 'success' : 'active'}
            />
            <p style={{ margin: '8px 0 0', color: '#888' }}>{translateProgress(installProgress)}</p>
            {installProgress.stage === 'downloading' && installProgress.speedBytesPerSec > 0 && (
              <p style={{ margin: '4px 0 0', fontSize: 12, color: '#aaa' }}>
                {formatBytes(installProgress.speedBytesPerSec)}/s
                {installProgress.totalBytes > 0 && ` · ${formatBytes(installProgress.downloadedBytes)} / ${formatBytes(installProgress.totalBytes)}`}
              </p>
            )}
          </div>
        ),
      })
    }
  }, [installProgress])

  const handleInstall = async (version: string) => {
    if (!status) return
    const ref = modal.confirm({
      title: t('detail.confirmInstallSdk', { sdk: status.displayName, version }),
      content: t('detail.confirmInstallDesc', { version }),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      maskClosable: false,
      onOk: () => {
        const sdkType = status.sdkType
        setInstalling(true)
        InstallSdk(sdkType, version)
          .then(() => {
            msgApi.success(t('detail.installSuccess', { sdk: status.displayName, version }))
            onRefresh()
            fetchPackageManagers()
          })
          .catch((e: any) => {
            msgApi.error(t('detail.installFail', { error: e?.message || e }))
          })
          .finally(() => {
            setInstalling(false)
          })
      },
    })
    modalRef.current = ref
    try {
      await ref
    } finally {
      modalRef.current = null
    }
  }

  const handleReinstall = (version: string) => {
    if (!status) return
    Modal.confirm({
      title: t('detail.reinstallConfirm', { version }),
      content: t('detail.reinstallConfirmDesc'),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: () => handleInstall(version),
    })
  }

  const handleSwitchVersion = (version: string) => {
    if (!status) return
    if (version === status.currentVersion) return
    Modal.confirm({
      title: t('detail.switchConfirm', { sdk: status.displayName, version }),
      content: t('detail.switchConfirmDesc'),
      okText: t('app.confirm'),
      cancelText: t('app.cancel'),
      onOk: async () => {
        setSwitching(true)
        try {
          await SwitchVersion(status.sdkType, version)
          msgApi.success(t('detail.switchSuccess', { version }), 5)
          msgApi.info(t('detail.reopenTerminalHint'), 5)
          await onRefresh()
          fetchPackageManagers()
        } catch (e: any) {
          msgApi.error(t('detail.switchFail', { error: e?.message || e }))
        } finally {
          setSwitching(false)
        }
      },
    })
  }

  const handleUninstallVersion = (version: string) => {
    if (!status) return
    Modal.confirm({
      title: t('detail.uninstallConfirm', { sdk: status.displayName, version }),
      content: t('detail.uninstallConfirmDesc'),
      okText: t('app.confirm'),
      okButtonProps: { danger: true },
      cancelText: t('app.cancel'),
      onOk: async () => {
        try {
          await UninstallVersion(status.sdkType, version)
          msgApi.success(t('detail.uninstallSuccess', { version }))
          onRefresh()
        } catch (e: any) {
          msgApi.error(t('detail.uninstallFail', { error: e?.message || e }))
        }
      },
    })
  }

  const handleCopyDownloadUrl = async (version: string) => {
    if (!status) return
    let url = installProgress?.downloadUrl || ''
    if (!url || installProgress?.version !== version) {
      try {
        url = await GetSdkDownloadURL(status.sdkType, version) as string
      } catch (e: any) {
        msgApi.error(t('detail.copyUrlFail'))
        return
      }
    }
    if (url) {
      try {
        await navigator.clipboard.writeText(url)
        msgApi.success(t('detail.copiedToClipboard'))
      } catch {
        msgApi.error(t('detail.copyUrlFail'))
      }
    }
  }

  const handleImportFile = async () => {
    if (!status) return
    setImporting(true)
    try {
      const filePath = await SelectLocalFile() as string
      if (!filePath) { setImporting(false); return }
      const ref = modal.confirm({
        title: t('detail.importingSdk', { sdk: status.displayName }),
        content: <div style={{ textAlign: 'center', padding: '10px 0' }}><Spin /><p style={{ marginTop: 8, color: '#888' }}>{t('detail.importingDesc')}</p></div>,
        okText: t('app.confirm'),
        cancelText: t('app.cancel'),
        cancelButtonProps: { disabled: true },
        okButtonProps: { loading: true },
        maskClosable: false,
        onOk: async () => {
          try {
            await ImportLocalSdk(status.sdkType, filePath)
            msgApi.success(t('detail.importSuccess'))
            onRefresh()
            fetchPackageManagers()
          } catch (e: any) {
            msgApi.error(t('detail.importFail', { error: e?.message || e }))
            ref.update({ cancelButtonProps: { disabled: false }, okButtonProps: { loading: false } })
            throw e
          }
        },
      })
      modalRef.current = ref
      try {
        await ref
      } finally {
        modalRef.current = null
      }
    } finally {
      setImporting(false)
    }
  }

  const handleImportDir = async () => {
    if (!status) return
    setImporting(true)
    try {
      const dirPath = await SelectLocalDir() as string
      if (!dirPath) { setImporting(false); return }
      const ref = modal.confirm({
        title: t('detail.importingSdk', { sdk: status.displayName }),
        content: <div style={{ textAlign: 'center', padding: '10px 0' }}><Spin /><p style={{ marginTop: 8, color: '#888' }}>{t('detail.importingDesc')}</p></div>,
        okText: t('app.confirm'),
        cancelText: t('app.cancel'),
        cancelButtonProps: { disabled: true },
        okButtonProps: { loading: true },
        maskClosable: false,
        onOk: async () => {
          try {
            await ImportLocalSdk(status.sdkType, dirPath)
            msgApi.success(t('detail.importSuccess'))
            onRefresh()
            fetchPackageManagers()
          } catch (e: any) {
            msgApi.error(t('detail.importFail', { error: e?.message || e }))
            ref.update({ cancelButtonProps: { disabled: false }, okButtonProps: { loading: false } })
            throw e
          }
        },
      })
      modalRef.current = ref
      try {
        await ref
      } finally {
        modalRef.current = null
      }
    } finally {
      setImporting(false)
    }
  }

  const handlePmInstall = async (name: string) => {
    setPmLoading(name)
    try {
      await InstallPackageManager(name)
      msgApi.success(t('detail.pmInstallSuccess', { name }))
      fetchPackageManagers()
    } catch (e: any) {
      msgApi.error(t('detail.pmInstallFail', { name, error: e?.message || e }))
    } finally { setPmLoading('') }
  }

  const handlePmUpdate = async (name: string) => {
    setPmLoading(name)
    try {
      await UpdatePackageManager(name)
      msgApi.success(t('detail.pmUpdateSuccess', { name }))
      fetchPackageManagers()
    } catch (e: any) {
      msgApi.error(t('detail.pmUpdateFail', { name, error: e?.message || e }))
    } finally { setPmLoading('') }
  }

  if (!status) {
    return (
      <div className="detail-panel">
        <div className="empty-state">
          <h3>{t('detail.selectSdk')}</h3>
          <p>{t('detail.selectSdkDesc')}</p>
        </div>
      </div>
    )
  }

  const showConflictModal = () => {
    modal.warning({
      title: t('detail.systemConflictTitle'),
      content: (
        <div>
          <p>{t('detail.systemConflictMsg')}</p>
          <ul style={{ maxHeight: 200, overflow: 'auto', paddingLeft: 20, margin: '8px 0' }}>
            {conflicts.map((e: string, i: number) => (
              <li key={i} style={{ fontSize: 12, color: '#666', wordBreak: 'break-all' }}>{e}</li>
            ))}
          </ul>
        </div>
      ),
      okText: t('app.confirm'),
      width: 520,
    })
  }

  const filteredVersions = versions.filter(v =>
    v.version.includes(searchText) || String(v.major).includes(searchText)
  )

  const installedSet = new Set(status.installedVersions || [])
  const currentVersion = status.currentVersion || ''

  return (
    <div className="detail-panel">
      {modalContextHolder}
      {/* Status Header */}
      <div className="status-header" style={{ position: 'relative' }}>
        {conflicts.length > 0 && (
          <Tooltip title={t('detail.systemConflictTitle')}>
            <WarningFilled
              onClick={showConflictModal}
              style={{
                position: 'absolute',
                top: 12,
                right: 12,
                fontSize: 18,
                color: '#ff4d4f',
                cursor: 'pointer',
              }}
            />
          </Tooltip>
        )}
        <h2>
          {status.displayName}
          <span className={`status-badge ${status.configured ? 'configured' : status.pathConfigured ? 'path-only' : 'not-configured'}`}>
            {status.configured ? (
              <>
                <CheckCircleFilled /> v{status.currentVersion}
              </>
            ) : status.pathConfigured ? (
              <>
                <CheckCircleFilled /> {status.pathVersion ? `v${status.pathVersion}` : ''} ({t('app.inPathOnly')})
              </>
            ) : (
              <>
                <CloseCircleFilled /> {t('app.notConfigured')}
              </>
            )}
          </span>
        </h2>

        {status.installedVersions && status.installedVersions.length > 0 && (
          <div className="installed-versions">
            <span style={{ fontSize: 12, color: '#888', marginRight: 8 }}>{t('detail.installed')}:</span>
            {status.installedVersions.map(v => {
              const isCurrent = v === currentVersion
              return (
                <Tooltip
                  key={v}
                  title={isCurrent ? t('detail.currentVersion') : t('detail.clickToSwitch')}
                >
                  <Tag
                    className={`installed-version-tag ${isCurrent ? 'current-version-tag' : ''}`}
                    style={{ cursor: isCurrent || switching ? 'default' : 'pointer' }}
                    color={isCurrent ? 'green' : undefined}
                    onClick={() => !isCurrent && !switching && handleSwitchVersion(v)}
                  >
                    {isCurrent && <CheckCircleFilled style={{ marginRight: 4, fontSize: 10 }} />}
                    {v}
                    {!isCurrent && (
                      <DeleteOutlined
                        style={{ marginLeft: 6, fontSize: 10, color: '#999' }}
                        onClick={(e) => {
                          e.stopPropagation()
                          handleUninstallVersion(v)
                        }}
                      />
                    )}
                  </Tag>
                </Tooltip>
              )
            })}
          </div>
        )}
      </div>

      {/* Package Managers */}
      {packageManagers.length > 0 && (
        <div className="package-managers-section">
          <h3>{t('detail.packageManagers')}</h3>
          <div className="package-manager-list">
            {packageManagers.map(pm => (
              <div key={pm.name} className="package-manager-item">
                <span className="pm-name">{pm.name}</span>
                {pm.installed ? (
                  <Tooltip title={t('detail.updateToLatest')}>
                    <Tag
                      color="green"
                      className="pm-tag-hover"
                      style={{ cursor: 'pointer' }}
                      onClick={() => handlePmUpdate(pm.name)}
                    >
                      v{pm.version}
                      <CloudUploadOutlined style={{ marginLeft: 4 }} />
                    </Tag>
                  </Tooltip>
                ) : (
                  <Tooltip title={t('detail.clickToInstall')}>
                    <Button
                      size="small"
                      type="primary"
                      icon={<DownloadOutlined />}
                      loading={pmLoading === pm.name}
                      onClick={() => handlePmInstall(pm.name)}
                      style={{ padding: '0 6px' }}
                    />
                  </Tooltip>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Version Section */}
      <div className="version-section">
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
          <h3>{t('detail.availableVersions')}</h3>
          <div style={{ display: 'flex', gap: 8 }}>
            <Dropdown
              menu={{
                items: [
                  { key: 'file', icon: <FileOutlined />, label: t('detail.importFile'), onClick: handleImportFile },
                  { key: 'dir', icon: <FolderOpenOutlined />, label: t('detail.importDir'), onClick: handleImportDir },
                ],
              }}
              trigger={['click']}
            >
              <Button
                icon={<ImportOutlined />}
                size="small"
                loading={importing}
              >
                {t('detail.importConfig')}
              </Button>
            </Dropdown>
            <Button
              icon={<ReloadOutlined />}
              size="small"
              onClick={() => fetchVersions()}
              loading={loading}
            >
              {t('detail.refresh')}
            </Button>
          </div>
        </div>

        <Input
          prefix={<SearchOutlined />}
          placeholder={t('detail.searchVersion')}
          value={searchText}
          onChange={e => setSearchText(e.target.value)}
          style={{ marginBottom: 12 }}
          allowClear
        />

        {loadError ? (
          <Result
            status="error"
            title={t('detail.loadError')}
            style={{ padding: '40px 0' }}
            extra={
              <Button type="primary" icon={<ReloadOutlined />} onClick={() => fetchVersions()}>
                {t('detail.retry')}
              </Button>
            }
          />
        ) : loading ? (
          <div className="loading-container">
            <Spin />
          </div>
        ) : (
          <div className="version-list" style={{ paddingBottom: 20 }}>
            {filteredVersions.map((v, index) => {
              const isInstalled = installedSet.has(v.version)
              const isCurrentConfig = v.version === currentVersion
              return (
                <div
                  key={v.version}
                  className={`version-row ${index === 0 ? 'latest' : ''} ${isInstalled ? 'installed' : ''} ${isCurrentConfig ? 'current-config' : ''}`}
                >
                  <span className="version-number">
                    {v.version}
                    {index === 0 && <Tag color="blue" style={{ marginLeft: 8 }}>{t('detail.latest')}</Tag>}
                    {isInstalled && <Tag color="green" style={{ marginLeft: 4 }}>{t('detail.installed')}</Tag>}
                    {isCurrentConfig && <Tag color="purple" style={{ marginLeft: 4 }}>{t('detail.currentConfig')}</Tag>}
                  </span>
                  {v.isLts && <span className="version-lts-badge">LTS</span>}
                  {v.releaseDate && (
                    <span className="version-date">{v.releaseDate}</span>
                  )}
                  {isInstalled ? (
                    <Tooltip title={t('detail.reinstallHover')}>
                      <Button
                        className="install-btn reinstall-btn"
                        size="small"
                        icon={<DownloadOutlined />}
                        loading={installing && installProgress?.version === v.version}
                        onClick={() => handleReinstall(v.version)}
                        disabled={installing}
                      >
                        <span className="reinstall-text">
                          {t('detail.installed')}
                        </span>
                        <span className="reinstall-hover-text">
                          {t('detail.reinstall')}
                        </span>
                      </Button>
                    </Tooltip>
                  ) : (
                    <Button
                      className="install-btn"
                      type={index === 0 ? 'primary' : 'default'}
                      size="small"
                      icon={<DownloadOutlined />}
                      loading={installing && installProgress?.version === v.version}
                      onClick={() => handleInstall(v.version)}
                      disabled={installing}
                    >
                      {t('detail.install')}
                    </Button>
                  )}
                  <Tooltip title={t('detail.copyDownloadUrl')}>
                    <Button
                      className="copy-url-btn"
                      size="small"
                      type="text"
                      icon={<CopyOutlined />}
                      onClick={() => handleCopyDownloadUrl(v.version)}
                    />
                  </Tooltip>
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Progress Section */}
      {installProgress && (
        <div className="progress-section">
          <h4>
            {t('detail.installing', { sdk: status.displayName, version: installProgress.version })}
          </h4>
          <Progress
            percent={installProgress.percent}
            status={installProgress.stage === 'error' ? 'exception' : installProgress.stage === 'done' ? 'success' : 'active'}
            strokeColor={{
              '0%': '#1677ff',
              '100%': '#52c41a',
            }}
          />
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: 13, color: '#aaa', marginTop: 8 }}>
            <span>{translateProgress(installProgress)}</span>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              {installProgress.stage === 'downloading' && installProgress.downloadedBytes > 0 && (
                <span>
                  {formatBytes(installProgress.downloadedBytes)}
                  {installProgress.totalBytes > 0 ? ` / ${formatBytes(installProgress.totalBytes)}` : ''}
                  {installProgress.speedBytesPerSec > 0 ? `  ${formatBytes(installProgress.speedBytesPerSec)}/s` : ''}
                </span>
              )}
              {installProgress.downloadUrl && (
                <Tooltip title={t('detail.copyDownloadUrl')}>
                  <Button
                    type="text"
                    size="small"
                    icon={<CopyOutlined />}
                    onClick={() => {
                      navigator.clipboard.writeText(installProgress.downloadUrl)
                      msgApi.success(t('detail.copiedToClipboard'))
                    }}
                    style={{ color: '#aaa', padding: '0 4px' }}
                  />
                </Tooltip>
              )}
              {installProgress.stage === 'downloading' && (
                <Tooltip title={t('detail.cancelInstall')}>
                  <Button
                    type="text"
                    size="small"
                    danger
                    icon={<CloseCircleFilled />}
                    onClick={() => CancelInstall(status.sdkType)}
                    style={{ padding: '0 4px' }}
                  />
                </Tooltip>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default DetailPanel
