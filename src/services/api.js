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

const adminHeaders = (token) => ({ Authorization: `Bearer ${token}` })
export const fetchAdminWords = async (token, params) => (await api.get('/admin/words', { params, headers: adminHeaders(token) })).data
export const fetchAdminWhitelist = async (token) => (await api.get('/admin/whitelist', { headers: adminHeaders(token) })).data
export const createAdminWhitelist = async (token, payload) => (await api.post('/admin/whitelist', payload, { headers: adminHeaders(token) })).data
export const deleteAdminWhitelist = async (token, payload) => (await api.delete('/admin/whitelist', { data: payload, headers: adminHeaders(token) })).data
export const createAdminWord = async (token, payload) => (await api.post('/admin/words', payload, { headers: adminHeaders(token) })).data
export const deleteAdminWord = async (token, payload) => (await api.delete('/admin/words', { data: payload, headers: adminHeaders(token) })).data
export const previewAdminImport = async (token, payload) => (await api.post('/admin/words/import/preview', payload, { headers: adminHeaders(token) })).data
export const applyAdminImport = async (token, payload) => (await api.post('/admin/words/import/apply', payload, { headers: adminHeaders(token) })).data
export const fetchWordlistVersions = async (token) => (await api.get('/admin/wordlists/versions', { headers: adminHeaders(token) })).data
export const rollbackWordlist = async (token, version) => (await api.post(`/admin/wordlists/rollback/${encodeURIComponent(version)}`, {}, { headers: adminHeaders(token) })).data
export const fetchAuditEntries = async (token) => (await api.get('/admin/audit', { headers: adminHeaders(token) })).data
export const fetchPolicies = async () => (await api.get('/policies')).data
export const fetchPlatformPolicies = async (token) => (await api.get('/platform/policies', { headers: adminHeaders(token) })).data
export const savePlatformPolicy = async (token, policy) => (await api.put(`/platform/policies/${encodeURIComponent(policy.id)}`, policy, { headers: adminHeaders(token) })).data
export const createBatchTask = async (token, payload) => (await api.post('/platform/tasks', payload, { headers: adminHeaders(token) })).data
export const fetchBatchTasks = async (token) => (await api.get('/platform/tasks', { headers: adminHeaders(token) })).data
export const cancelBatchTask = async (token, id) => (await api.post(`/platform/tasks/${encodeURIComponent(id)}/cancel`, {}, { headers: adminHeaders(token) })).data
export const retryBatchTask = async (token, id) => (await api.post(`/platform/tasks/${encodeURIComponent(id)}/retry`, {}, { headers: adminHeaders(token) })).data
export const deleteBatchTask = async (token, id) => api.delete(`/platform/tasks/${encodeURIComponent(id)}`, { headers: adminHeaders(token) })
export const fetchTaskStorage = async (token) => (await api.get('/platform/storage', { headers: adminHeaders(token) })).data
export const cleanupBatchTasks = async (token) => (await api.post('/platform/tasks/cleanup', {}, { headers: adminHeaders(token) })).data
export const runPolicyEvaluation = async (token, payload) => (await api.post('/platform/evaluations', payload, { headers: adminHeaders(token) })).data
export const createReviewTask = async (token,payload)=>(await api.post('/platform/reviews',payload,{headers:adminHeaders(token)})).data
export const fetchReviewTasks = async (token)=>(await api.get('/platform/reviews',{headers:adminHeaders(token)})).data
export const claimReviewTask = async (token,id,reviewer)=>(await api.post(`/platform/reviews/${id}/claim`,{reviewer},{headers:adminHeaders(token)})).data
export const releaseReviewTask = async (token,id,reviewer)=>(await api.post(`/platform/reviews/${id}/release`,{reviewer},{headers:adminHeaders(token)})).data
export const resolveReviewTask = async (token,id,payload)=>(await api.post(`/platform/reviews/${id}/resolve`,payload,{headers:adminHeaders(token)})).data
export const fetchFeedbackCandidates = async (token)=>(await api.get('/platform/feedback-candidates',{headers:adminHeaders(token)})).data
export const fetchReviewStats = async (token)=>(await api.get('/platform/review-stats',{headers:adminHeaders(token)})).data
export const applyFeedbackCandidate = async (token,id)=>(await api.post(`/platform/feedback-candidates/${encodeURIComponent(id)}/apply`,{},{headers:adminHeaders(token)})).data
export const dismissFeedbackCandidate = async (token,id)=>(await api.post(`/platform/feedback-candidates/${encodeURIComponent(id)}/dismiss`,{},{headers:adminHeaders(token)})).data
export const downloadBatchResults = async (token, id, format = 'csv') => (await api.get(`/platform/tasks/${encodeURIComponent(id)}/results`, { params: { format }, headers: adminHeaders(token), responseType: 'blob' })).data

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
