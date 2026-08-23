import axios from 'axios'
import { handleAuthError } from '@/stores/auth'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 45000,
})

// 请求拦截器
http.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    if (!config.headers['Content-Type'] && !(config.data instanceof FormData)) {
      config.headers['Content-Type'] = 'application/json'
    }
    return config
  },
  (error) => Promise.reject(error)
)

// 响应拦截器
http.interceptors.response.use(
  (response) => {
    if (response.config?.responseType === 'blob') return response
    const data = response.data
    if (data.code !== 200) {
      if (data.code === 401) {
        handleAuthError()
        return Promise.reject(new Error('未授权，请重新登录'))
      }
      return Promise.reject(new Error(data.message || '请求失败'))
    }
    return data
  },
  (error) => {
    let message = '请求失败'
    if (error.response) {
      switch (error.response.status) {
        case 401:
          handleAuthError()
          return Promise.reject(error)
        case 403: message = '拒绝访问'; break
        case 404: message = '请求地址不存在'; break
        case 500: message = '服务器内部错误'; break
        default: message = error.response.data?.message || `请求失败: ${error.response.status}`
      }
    } else if (error.request) {
      message = '网络错误，请检查网络连接'
    }
    return Promise.reject(new Error(message))
  }
)

export default http
