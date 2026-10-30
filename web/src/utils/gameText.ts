// 游戏文案字库（v0.41）：欢迎语四级 / 每日一句 / WA 吐槽。前端版本化内置（后台
// 自定义句子二期）。随机由调用方做（每日句用日期 seed——同日全站同句）。

/** 登录欢迎语：按等级分档随机（新锐/中坚/大佬/传说） */
export const WELCOME_TIERS: { min: number; lines: string[] }[] = [
  {
    min: 10,
    lines: [
      '极境大佬降临，今天也来虐题吗？',
      '传说之下，无题可挡。欢迎回来。',
      '极境在此，题库诸君退避。',
    ],
  },
  {
    min: 7,
    lines: [
      '欢迎回来，超强大佬！',
      '大佬驾到，题库抖三抖。',
      '今天也想对大佬说：带带我们。',
    ],
  },
  {
    min: 4,
    lines: [
      '欢迎回来，中坚力量！',
      '中坚大佬上线，稳住我们能赢。',
      '欢迎回来，今天的你也很强。',
    ],
  },
  {
    min: 0,
    lines: [
      '欢迎回来，未来的大佬！',
      '新锐驾到，今天也来变强吧。',
      '欢迎回来，每 AC 一题都在变强。',
    ],
  },
]

/** 首页每日一句（时段彩蛋在 pickDailyQuote 内处理） */
export const DAILY_QUOTES: string[] = [
  '把每一次 WA 都当作谜语，解开会更好玩。',
  '算法不会辜负你，它只是在等你想通的那一刻。',
  '今天的题不难，难的是打开今天的题。',
  '刷题如登山，回头看已是云海。',
  '别怕超时，复杂度是可以驯服的。',
  '每一个 AC，都是与昨天的自己告别。',
  '数据结构选得好，代码写得像诗。',
  '递归的尽头是出口，刷题的尽头是下一题。',
  '十个测试点，总有一个在等你。',
  '思考五分钟，AC 一秒钟。',
  '位运算像魔法，图论像迷宫，而你手握地图。',
  '今天也是适合对拍的一天。',
  '暴力能过，说明出题人今天心情好。',
  '你的下一题，何必是下一题。',
  'Debug 之路，道阻且长，行则将至。',
]

/** WA 吐槽（自嘲式，不伤人） */
export const WA_TAUNTS: string[] = [
  '差一点点，江湖再见。',
  '测试点在嘲笑你（划掉）在等你。',
  'WA：还没到说再见的时候。',
  '这题在给你暗示：换个角度试试。',
  '第 N 个测试点向你扔了一只袜子。',
  '再检查一眼边界？就一眼。',
  '恭喜发现一种错误做法（也是一种进步）。',
  '答案在路上，WA 在脚下。',
]

/** 时间段彩蛋（叠加在每日一句前） */
export function pickDailyQuote(date = new Date()): string {
  const h = date.getHours()
  if (h >= 0 && h < 5) return '夜深了，AC 就是最好的宵夜。'
  if (h >= 5 && h < 8) return '晨光正好，脑子最清醒的一小时。'
  const seed = date.getFullYear() * 10000 + (date.getMonth() + 1) * 100 + date.getDate()
  return DAILY_QUOTES[seed % DAILY_QUOTES.length]
}

export function pickWelcome(level: number): string {
  for (const t of WELCOME_TIERS) {
    if (level >= t.min) return t.lines[Math.floor(Math.random() * t.lines.length)]
  }
  return '欢迎回来'
}

export function pickTaunt(): string {
  return WA_TAUNTS[Math.floor(Math.random() * WA_TAUNTS.length)]
}
