import { ref } from 'vue'
import { defineStore } from 'pinia'

// 登录态持久化在 localStorage（soj_token / soj_nickname）
export const useUserStore = defineStore('user', () => {
  const token = ref(localStorage.getItem('soj_token') ?? '')
  const nickname = ref(localStorage.getItem('soj_nickname') ?? '')

  function setLogin(newToken: string, newNickname: string) {
    token.value = newToken
    nickname.value = newNickname
    localStorage.setItem('soj_token', newToken)
    localStorage.setItem('soj_nickname', newNickname)
  }

  function logout() {
    token.value = ''
    nickname.value = ''
    localStorage.removeItem('soj_token')
    localStorage.removeItem('soj_nickname')
  }

  return { token, nickname, setLogin, logout }
})
