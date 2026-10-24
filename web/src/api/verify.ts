import { del, post } from './index'

// 邮箱验证码与自助找回（v0.16）。独立于 api/index.ts：验证码是一族自包含接口，
// 避免与在途改动共享文件；类型就地内联。
export interface SiteVerifyInfo {
  register_verify_mode: 'none' | 'email'
  password_reset_enabled: boolean
}

export const verifyApi = {
  /** 发送验证码（register/reset 公开；bind 需登录态由后端校验） */
  sendCode(payload: {
    purpose: 'register' | 'reset' | 'bind'
    email: string
    old_mode?: 'password' | 'code'
    old_password?: string
    old_code?: string
  }) {
    return post<{ sent: boolean; message: string }>('/auth/email/code', payload)
  },
  /** 忘记密码：验证码 + 新密码重置（account = 用户名或邮箱） */
  resetPassword(account: string, code: string, password: string) {
    return post<{ reset: boolean }>('/auth/password/reset', { account, code, password })
  },
  /** 绑定/换绑邮箱（登录态） */
  bindEmail(email: string, code: string) {
    return post<{ bound: boolean; email: string }>('/auth/email/bind', { email, code })
  },
  /** root 解绑用户邮箱（管理端用户编辑） */
  adminUnbindEmail(uid: number) {
    return del<{ unbound: boolean }>(`/admin/users/${uid}/email`)
  },
}
