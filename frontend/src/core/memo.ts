// core/memo.ts —— 极简单实例缓存的 memoize 工具。
//
// 用于 selectors：pick(state) 输入引用相等 → 跳过重算返回上次结果。
// 因为整个应用只有 1 个 Pinia store 实例，单实例缓存就够；多实例场景另议。

import type { AppState } from './state'

export function memo<I, O>(
  pick: (s: AppState) => I,
  compute: (input: I) => O,
): (s: AppState) => O {
  let lastInput: I
  let lastOutput: O
  let initialized = false
  return (s: AppState) => {
    const input = pick(s)
    if (initialized && input === lastInput) {
      return lastOutput
    }
    lastOutput = compute(input)
    lastInput = input
    initialized = true
    return lastOutput
  }
}
