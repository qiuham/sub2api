import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { list, createNative } = vi.hoisted(() => ({ list: vi.fn(), createNative: vi.fn() }))

vi.mock('@/api/admin', () => ({ adminAPI: { tlsFingerprintProfiles: { list, createNative } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import TLSFingerprintProfilesModal from './TLSFingerprintProfilesModal.vue'

describe('TLSFingerprintProfilesModal Native templates', () => {
  it('creates Claude and Codex profiles through the existing template API and reloads the list', async () => {
    list.mockReset().mockResolvedValue([])
    createNative.mockReset().mockResolvedValue({ id: 7, name: 'verified' })
    const wrapper = mount(TLSFingerprintProfilesModal, {
      props: { show: true },
      global: { stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true, Icon: true
      } }
    })
    await flushPromises()
    const claude = wrapper.findAll('button').find(button => button.text() === 'Native Claude')
    const codex = wrapper.findAll('button').find(button => button.text() === 'Native Codex')
    expect(claude).toBeTruthy()
    expect(codex).toBeTruthy()
    await claude!.trigger('click')
    await flushPromises()
    await codex!.trigger('click')
    await flushPromises()
    expect(createNative.mock.calls.map(call => call[0])).toEqual(['claude', 'codex'])
    expect(list).toHaveBeenCalledTimes(3)
  })

  it('shows the verified family and versions returned by the template API', async () => {
    list.mockReset().mockResolvedValue([{ id: 7, name: 'codex-verified', native_family: 'codex', native_versions: ['0.155.1', '0.156.1'] }])
    const wrapper = mount(TLSFingerprintProfilesModal, {
      props: { show: true },
      global: { stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true, Icon: true
      } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Native Codex · 0.155.1, 0.156.1')
  })
})
