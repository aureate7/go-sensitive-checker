import axios from 'axios'

export const api = axios.create({
  baseURL: '/api',
  timeout: 35_000,
  headers: { 'Content-Type': 'application/json' },
})

export const fetchServiceStatus = async () => (await api.get('/status')).data
export const fetchCategories = async () => (await api.get('/categories')).data
export const fetchStatistics = async () => (await api.get('/statistics')).data
export const detectText = async (payload, signal) =>
  (await api.post('/detect', payload, { signal })).data

export const describeAPIError = (error) => {
  if (axios.isCancel(error)) return { message: '检测已取消', code: 'REQUEST_CANCELLED' }
  const payload = error?.response?.data?.error
  if (payload && typeof payload === 'object') {
    return {
      message: payload.message || '请求失败',
      code: payload.code || 'API_ERROR',
      requestId: payload.request_id || error?.response?.headers?.['x-request-id'],
    }
  }
  return {
    message: error?.code === 'ECONNABORTED' ? '请求超时，请缩短文本后重试' : '无法连接检测服务',
    code: error?.code || 'NETWORK_ERROR',
    requestId: error?.response?.headers?.['x-request-id'],
  }
}
