<script setup lang="ts">
// 安全密码控件：密文展示、不泄露明文，回车可直接提交。基于 a-input-password。
const props = withDefaults(
  defineProps<{
    modelValue: string
    placeholder?: string
    disabled?: boolean
    id?: string
    autocomplete?: string
  }>(),
  {
    placeholder: '输入密码',
    disabled: false,
    autocomplete: 'current-password',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  enter: []
}>()

function onInput(v: string | number): void {
  emit('update:modelValue', String(v))
}
</script>

<template>
  <a-input-password
    :id="props.id"
    :value="props.modelValue"
    :placeholder="props.placeholder"
    :disabled="props.disabled"
    :autocomplete="props.autocomplete"
    @update:value="onInput"
    @press-enter="emit('enter')"
  />
</template>
