// moveFocus walks to the next/prev focusable suggestion item within a container
// (roving focus for autocomplete listboxes). dir: 1 = next, -1 = previous.
export function moveFocus(container: HTMLElement | null, dir: 1 | -1) {
  if (!container) return
  const items = Array.from(container.querySelectorAll<HTMLButtonElement>('.user-suggestion-item'))
  if (!items.length) return
  const idx = items.findIndex(el => el === document.activeElement)
  const next = idx === -1 ? (dir === 1 ? 0 : items.length - 1) : (idx + dir + items.length) % items.length
  items[next].focus()
}
