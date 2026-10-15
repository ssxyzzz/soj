import { ref } from 'vue'
import { defineStore } from 'pinia'

// 登录态持久化在 localStorage（soj_token / soj_nickname / soj_role）
export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('soj_token') ?? '')
  const nickname = ref(localStorage.getItem('soj_nickname') ?? '')
  const role = ref(localStorage.getItem('soj_role') ?? '')

  function setLogin(newToken: string, newNickname: string, newRole: string) {
    token.value = newToken
    nickname.value = newNickname
    role.value = newRole
    localStorage.setItem('soj_token', newToken)
    localStorage.setItem('soj_nickname', newNickname)
    localStorage.setItem('soj_role', newRole)
  }

  function logout() {
    token.value = ''
    nickname.value = ''
    role.value = ''
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_nickname')
    localStorage.removeItem('soj_role')
  }

  return { token, nickname, role, setLogin, logout }
})
