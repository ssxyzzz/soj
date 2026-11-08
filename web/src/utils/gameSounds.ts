// 游戏音效合成器（v0.41）：WebAudio 振荡器运行时合成——零音频包（≤200KB 红线
// 直接归零），8-bit 风格短音效。默认开，顶栏一键静音（localStorage soj_muted，
// prefs 持久化随设置页二期）。所有播放静默失败（无 AudioContext 环境不炸）。

let ctx: AudioContext | null = null
let muted = localStorage.getItem('soj_muted') === '1'

export function isMuted(): boolean {
  return muted
}

export function setMuted(v: boolean) {
  muted = v
  localStorage.setItem('soj_muted', v ? '1' : '0')
}

function ac(): AudioContext | null {
  if (muted) return null
  try {
    ctx ??= new AudioContext()
    if (ctx.state === 'suspended') void ctx.resume()
    return ctx
  } catch {
    return null
  }
}

/** 单音：type 波形 / freq 频率 / dur 时长 s / delay 起始偏移 s / vol 音量 */
function tone(c: AudioContext, type: OscillatorType, freq: number, dur: number, delay: number, vol: number) {
  const o = c.createOscillator()
  const g = c.createGain()
  o.type = type
  o.frequency.value = freq
  const t0 = c.currentTime + delay
  g.gain.setValueAtTime(0.0001, t0)
  g.gain.exponentialRampToValueAtTime(vol, t0 + 0.01)
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur)
  o.connect(g).connect(c.destination)
  o.start(t0)
  o.stop(t0 + dur + 0.05)
}

/** 音效预设：随机 AC 音（8-bit 上行琶音）、PERFECT、SSR、升级、连击变奏、成就、WA 无声 */

// 通过庆祝家族音量减半（v0.83.1 用户反馈「通过的特效音量有点大」）：playAC/Perfect/SSR/Coin
// 四组 vol 全部 ×0.5（升级 playLevelUp / 成就 playAchievement 非通过链路，不动）

/** 随机 AC 音：连击越高音越亮 */
export function playAC(combo = 1) {
  const c = ac()
  if (!c) return
  const base = 440 * Math.pow(2, Math.min(combo, 10) / 12)
  tone(c, 'square', base, 0.09, 0, 0.06)
  tone(c, 'square', base * 1.25, 0.09, 0.08, 0.06)
  tone(c, 'square', base * 1.5, 0.14, 0.16, 0.06)
}

export function playPerfect() {
  const c = ac()
  if (!c) return
  ;[523, 659, 784, 1047].forEach((f, i) => tone(c, 'triangle', f, 0.16, i * 0.09, 0.07))
}

export function playSSR() {
  const c = ac()
  if (!c) return
  ;[392, 523, 659, 784, 1047, 1319].forEach((f, i) => tone(c, 'sawtooth', f, 0.22, i * 0.11, 0.05))
  tone(c, 'triangle', 2093, 0.5, 0.66, 0.06)
}

export function playLevelUp() {
  const c = ac()
  if (!c) return
  ;[440, 554, 659, 880].forEach((f, i) => tone(c, 'square', f, 0.18, i * 0.12, 0.12))
}

export function playAchievement() {
  const c = ac()
  if (!c) return
  tone(c, 'triangle', 880, 0.12, 0, 0.12)
  tone(c, 'triangle', 1109, 0.12, 0.1, 0.12)
  tone(c, 'triangle', 1319, 0.24, 0.2, 0.12)
}

/** 金币飞入（掉玉结算短音） */
export function playCoin() {
  const c = ac()
  if (!c) return
  tone(c, 'square', 988, 0.07, 0, 0.05)
  tone(c, 'square', 1319, 0.18, 0.06, 0.05)
}

/** WA：不发声（拍板——失败不出声），保留接口占位避免调用侧分支 */
export function playWA() {
  /* 静默 */
}
