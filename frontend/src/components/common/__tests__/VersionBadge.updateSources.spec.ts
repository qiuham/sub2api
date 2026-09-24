import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  auth: { isAdmin: true },
  app: {
    versionLoading: false, currentVersion: '0.2.8-N.3', latestVersion: '0.2.8-N.4',
    hasUpdate: false, releaseInfo: { html_url: 'https://github.com/qiuham/sub2api/releases/tag/v0.2.8-N.4' },
    upstreamLatestVersion: '0.2.9', hasUpstreamUpdate: true,
    upstreamReleaseInfo: { html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.9' },
    buildType: 'release', fetchVersion: vi.fn(), clearVersionCache: vi.fn()
  },
  performUpdate: vi.fn(), restartService: vi.fn(), getRollbackVersions: vi.fn(), rollback: vi.fn()
}))
vi.mock('@/stores', () => ({ useAuthStore: () => mocks.auth, useAppStore: () => mocks.app }))
vi.mock('@/api/admin/system', () => ({
  performUpdate: mocks.performUpdate, restartService: mocks.restartService,
  getRollbackVersions: mocks.getRollbackVersions, rollback: mocks.rollback
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import VersionBadge from '../VersionBadge.vue'

let wrapper: VueWrapper
async function openBadge() {
  wrapper = mount(VersionBadge, { props: { version: '0.2.8-N.3' }, global: { stubs: { Icon: true } } })
  await flushPromises()
  await wrapper.get('button').trigger('click')
}
async function click(text: string) {
  const button = wrapper.findAll('button').find(b => b.text().includes(text))
  expect(button, `button ${text}`).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.isAdmin = true
  mocks.app.hasUpdate = false
  mocks.app.buildType = 'release'
  mocks.app.fetchVersion.mockResolvedValue(null)
  mocks.getRollbackVersions.mockResolvedValue({ versions: [{ version: '0.2.8-N.2', published_at: '', html_url: '' }] })
  mocks.rollback.mockResolvedValue({ need_restart: true })
  mocks.performUpdate.mockResolvedValue({ need_restart: true })
})
afterEach(() => wrapper?.unmount())

describe('版本来源与回滚交互', () => {
  it('上游只有合并提示，不触发自己的更新', async () => {
    await openBadge()
    expect(wrapper.text()).toContain('上游 Wei-Shaw/sub2api：v0.2.9')
    expect(wrapper.text()).toContain('有新版本待手动合并')
    expect(wrapper.findAll('a').map(a => a.attributes('href'))).toContain(mocks.app.upstreamReleaseInfo.html_url)
    expect(wrapper.findAll('button').some(b => b.text().includes('version.updateNow'))).toBe(false)
    expect(mocks.performUpdate).not.toHaveBeenCalled()
  })

  it('自己的更新按钮调用更新 API，成功后不自动重启', async () => {
    mocks.app.hasUpdate = true
    await openBadge()
    expect(wrapper.text()).toContain('v0.2.8-N.4')
    await click('version.updateNow')
    expect(mocks.performUpdate).toHaveBeenCalledOnce()
    expect(mocks.app.clearVersionCache).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('version.restartRequired')
    expect(mocks.restartService).not.toHaveBeenCalled()
  })

  it('回滚列表来自 API，选择后只提交所选版本', async () => {
    await openBadge()
    await click('version.rollback')
    expect(mocks.getRollbackVersions).toHaveBeenCalledOnce()
    await click('v0.2.8-N.2')
    expect(wrapper.text()).toContain('qiuham/sub2api/v0.2.8-N.2/deploy/install.sh')
    await click('version.rollbackConfirm')
    expect(mocks.rollback).toHaveBeenCalledWith('0.2.8-N.2')
    expect(mocks.performUpdate).not.toHaveBeenCalled()
    expect(mocks.restartService).not.toHaveBeenCalled()
  })

  it('回滚列表读取失败时显示错误而不是假装没有版本', async () => {
    mocks.getRollbackVersions.mockRejectedValueOnce(new Error('fixture release list unavailable'))
    await openBadge()
    await click('version.rollback')
    expect(wrapper.text()).toContain('fixture release list unavailable')
    expect(mocks.rollback).not.toHaveBeenCalled()
  })

  it('非管理员不显示更新和回滚入口', async () => {
    mocks.auth.isAdmin = false
    wrapper = mount(VersionBadge, { props: { version: '0.2.8-N.3' } })
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(mocks.app.fetchVersion).not.toHaveBeenCalled()
    expect(mocks.getRollbackVersions).not.toHaveBeenCalled()
  })
})
