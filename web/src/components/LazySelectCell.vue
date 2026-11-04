<script setup lang="ts">
// 惰性单元格选择器（v0.62 挂载冻结根治批）：表格行内编辑专用——默认纯文本展示态，
// 点击该格才挂载 el-select（EP 选择器脚手架 ElTooltip/ElPopper 同屏最多 1 份），
// 单选选中即提交退出；多选失焦提交退出；Esc 退出。病理背景：行内常驻 el-select 在
// 100 行规模实测挂载冻结 ~940ms（每实例 ~2.5ms，与选项数无关），展示态/编辑态解耦后
// 同表纯文本仅 ~70ms（详见 plan §2.1）。
import { computed, nextTick, ref } from 'vue'

export interface LazyOpt {
  id: number
  label: string
}

const props = withDefaults(
  defineProps<{
    modelValue: number | number[]
    options: LazyOpt[]
    multiple?: boolean
    empty?: string // 展示态空值占位
    readonly?: boolean // 只读态：点击不进入编辑（如已应用行），灰显
  }>(),
  { multiple: false, empty: '—', readonly: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: number | number[]): void }>()

const editing = ref(false)
const selectRef = ref()

function idText(id: number): string {
  return props.options.find((o) => o.id === id)?.label ?? `#${id}`
}
const text = computed(() =>
  props.multiple
    ? (props.modelValue as number[]).map(idText).join('、')
    : ((props.modelValue as number) ? idText(props.modelValue as number) : ''),
)

function startEdit() {
  if (props.readonly) return
  editing.value = true
  // automatic-dropdown：聚焦即展开下拉（单选免二次点击；多选 filterable 直接可输入过滤）
  void nextTick(() => selectRef.value?.focus?.())
}
function stopEdit() {
  editing.value = false
}
function onSingleChange(v?: number) {
  emit('update:modelValue', v ?? 0)
  editing.value = false
}
</script>

<template>
  <el-select
    v-if="editing"
    ref="selectRef"
    :model-value="modelValue"
    :multiple="multiple"
    :filterable="multiple"
    clearable
    automatic-dropdown
    :persistent="false"
    size="small"
    :placeholder="empty"
    class="lazy-select"
    @update:model-value="
      multiple
        ? emit('update:modelValue', ($event ?? []) as number[])
        : onSingleChange($event as number | undefined)
    "
    @blur="stopEdit"
    @keydown.esc.capture="stopEdit"
  >
    <el-option v-for="o in options" :key="o.id" :value="o.id" :label="o.label" />
  </el-select>
  <span
    v-else
    class="lazy-cell"
    :class="{ empty: !text, ro: readonly }"
    :title="text || undefined"
    @click="startEdit"
  >{{ text || empty }}</span>
</template>

<style scoped>
/* 展示态：可点击的编辑占位——hover 点状下划线提示可改，点击热区比文字大一圈 */
.lazy-cell {
  display: inline-block;
  min-height: 22px;
  line-height: 22px;
  padding: 0 var(--soj-sp-6);
  margin: 0 calc(-1 * var(--soj-sp-6));
  border-radius: 4px;
  cursor: pointer;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.lazy-cell:hover {
  color: var(--soj-blue);
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}

.lazy-cell.empty {
  color: var(--el-text-color-placeholder);
}

/* 只读态（v0.63）：不可编辑的行内值——无下划线提示、不可点 */
.lazy-cell.ro {
  cursor: default;
}

.lazy-cell.ro:hover {
  color: inherit;
  text-decoration: none;
}

/* 编辑态占满列宽，避免与文本态宽度跳变 */
.lazy-select {
  width: 100%;
}
</style>
