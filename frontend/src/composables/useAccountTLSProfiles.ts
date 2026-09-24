import { computed, ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { TLSFingerprintProfile } from '@/api/admin/tlsFingerprintProfile'

// Shared by create/edit: use the existing profile CRUD and verified-template endpoint.
export function useAccountTLSProfiles(
  platform: () => string | undefined,
  nativeEnabled: () => boolean,
  selectedId: Ref<number | null>
) {
  const { t } = useI18n()
  const tlsFingerprintProfiles = ref<TLSFingerprintProfile[]>([])
  const tlsProfilesLoading = ref(false)
  const tlsProfileCreating = ref(false)
  const tlsProfilesError = ref('')
  const family = computed(() => platform() === 'openai' ? 'codex' : 'claude')
  const selectableTLSProfiles = computed(() => nativeEnabled()
    ? tlsFingerprintProfiles.value.filter(p => p.native_family === family.value)
    : tlsFingerprintProfiles.value)
  const tlsProfileOptions = computed(() => [
    ...(!nativeEnabled() ? [
      { value: null, label: t('admin.accounts.quotaControl.tlsFingerprint.defaultProfile') },
      ...(tlsFingerprintProfiles.value.length ? [{ value: -1, label: t('admin.accounts.quotaControl.tlsFingerprint.randomProfile') }] : [])
    ] : []),
    ...selectableTLSProfiles.value.map(p => ({
      value: p.id,
      label: p.name + (p.native_versions?.length ? ` (${p.native_versions.join(', ')})` : '')
    }))
  ])
  let requestId = 0
  async function loadTLSProfiles() {
    const id = ++requestId
    tlsProfilesLoading.value = true
    tlsProfilesError.value = ''
    try {
      const profiles = await adminAPI.tlsFingerprintProfiles.list()
      if (id === requestId) tlsFingerprintProfiles.value = profiles
    } catch {
      if (id === requestId) {
        tlsFingerprintProfiles.value = []
        tlsProfilesError.value = '模板加载失败，请重试。'
      }
    } finally {
      if (id === requestId) tlsProfilesLoading.value = false
    }
  }
  async function createNativeTLSProfile() {
    if (tlsProfileCreating.value || tlsProfilesLoading.value) return
    const requestedFamily = family.value
    tlsProfileCreating.value = true
    tlsProfilesError.value = ''
    try {
      const profile = await adminAPI.tlsFingerprintProfiles.createNative(requestedFamily)
      tlsFingerprintProfiles.value = [...tlsFingerprintProfiles.value.filter(p => p.id !== profile.id), profile]
      if (family.value === requestedFamily && nativeEnabled()) selectedId.value = profile.id
    } catch {
      tlsProfilesError.value = '模板创建失败，请重试或到指纹模板管理中检查。'
    } finally {
      tlsProfileCreating.value = false
    }
  }
  function selectTLSProfile(value: string | number | boolean | null) {
    selectedId.value = value === null ? null : Number(value)
  }
  return {
    selectableTLSProfiles, tlsProfileOptions,
    tlsProfilesLoading, tlsProfileCreating, tlsProfilesError,
    loadTLSProfiles, createNativeTLSProfile, selectTLSProfile
  }
}
