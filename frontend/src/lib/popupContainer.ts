/**
 * antd 浮层（Select 等下拉）的挂载容器。
 *
 * 为什么需要：本应用的滚动容器是 App.vue 里的 `.app-main`（overflow-y: auto），
 * 而 antd 的下拉默认挂到 `document.body`。body 不滚动、`.app-main` 才滚，
 * 于是出现「页面在滚、下拉框停在原位」的脱节（2026-09-21 用户实测）。
 *
 * 做法：把浮层挂到触发元素旁边的流内节点，让它随内容一起滚动。
 * 弹窗内的选择框单独处理——挂到 `.ant-modal-body`，浮层随弹窗内容滚动，
 * 也不会跑出对话框（弹窗是 fixed 遮罩，用 body 默认值虽然能对齐，但弹窗
 * 内容一变高就会脱节）。
 */

/** 供 `:get-popup-container` 使用的容器选择函数。 */
export function popupContainer(trigger: HTMLElement): HTMLElement {
  const modalBody = trigger.closest('.ant-modal-body')
  if (modalBody instanceof HTMLElement) return modalBody
  return trigger.parentElement ?? document.body
}
