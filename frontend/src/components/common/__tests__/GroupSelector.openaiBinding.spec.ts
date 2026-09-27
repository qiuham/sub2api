import { readFileSync } from 'node:fs'
import { defineComponent, reactive } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupSelector from '../GroupSelector.vue'

vi.mock('@/stores', () => ({ useAuthStore: () => ({ isSimpleMode: false }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'OpenAI group', platform: 'openai', status: 'active' },
  { id: 2, name: 'Anthropic group', platform: 'anthropic', status: 'active' },
  { id: 3, name: 'Gemini group', platform: 'gemini', status: 'active' }
]

describe.each(['CreateAccountModal', 'EditAccountModal'])('%s group binding', (page) => {
  // Exercise the actual page binding rather than passing a camelCase prop directly.
  const source = readFileSync(`${process.cwd()}/src/components/account/${page}.vue`, 'utf8')
  const template = source.match(/<GroupSelector\b[\s\S]*?\/>/)?.[0]
  if (!template) throw new Error('GroupSelector missing from account page')

  function render(type: string) {
    const form = reactive({ platform: 'openai', type, group_ids: [1, 2] })
    const wrapper = mount(defineComponent({
      components: { GroupSelector },
      template,
      setup: () => ({ form, account: form, groups, selectableGroups: groups, mixedScheduling: false })
    }), {
      global: { stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true } }
    })
    return { wrapper, form }
  }

  it('shows selected Anthropic groups for OpenAI OAuth and updates the form selection', async () => {
    const { wrapper, form } = render('oauth')
    expect(wrapper.text()).toContain('Anthropic group')
    expect(wrapper.text()).toContain('OpenAI group')
    expect(wrapper.text()).not.toContain('Gemini group')
    const input = wrapper.get('input[value="2"]')
    expect((input.element as HTMLInputElement).checked).toBe(true)
    await input.setValue(false)
    expect(form.group_ids).toEqual([1])
    await input.setValue(true)
    expect(form.group_ids).toEqual([1, 2])
  })

  it('keeps the existing platform filter for API key accounts', () => {
    const { wrapper } = render('apikey')
    expect(wrapper.text()).toContain('OpenAI group')
    expect(wrapper.text()).not.toContain('Anthropic group')
  })
})
