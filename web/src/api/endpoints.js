import http from './index'

// 认证
export const loginApi = (data) => http.post('/auth/login', data)
export const logoutApi = () => http.post('/auth/logout')
export const refreshTokenApi = (token) => http.post('/auth/refresh', {}, { headers: { Authorization: `Bearer ${token}` } })
export const getUserInfoApi = () => http.get('/user/info')
export const changePasswordApi = (data) => http.post('/user/change-password', data)
export const updateProfileApi = (data) => http.put('/user/profile', data)

// 仪表盘
export const getDashboardStats = () => http.get('/dashboard/stats')
export const getDashboardRealtime = () => http.get('/dashboard/realtime')

// 设备管理
export const getDevices = (params) => http.get('/devices', { params })
export const createDevice = (data) => http.post('/devices', data)
export const updateDevice = (id, data) => http.put(`/devices/${id}`, data)
export const deleteDevice = (id) => http.delete(`/devices/${id}`)
export const batchDeleteDevices = (ids) => http.delete('/devices/batch', { data: { ids } })
export const batchResetDevices = (ids) => http.post('/devices/batch/reset', { ids })
export const batchAddGroup = (data) => http.post('/devices/batch/add-group', data)
export const batchBuiltinTask = (ids) => http.post('/devices/batch/builtin-task', { ids })
export const updateDeviceParams = (id, data) => http.put(`/devices/${id}/params`, data)
export const getDeviceLogs = (id, params) => http.get(`/devices/${id}/logs`, { params })

// 设备分组
export const getDeviceGroups = () => http.get('/device-groups')
export const createDeviceGroup = (data) => http.post('/device-groups', data)
export const updateDeviceGroup = (id, data) => http.put(`/device-groups/${id}`, data)
export const deleteDeviceGroup = (id) => http.delete(`/device-groups/${id}`)
export const resetGroupDevices = (id) => http.post(`/device-groups/${id}/reset`)
export const getGroupDevices = (id) => http.get(`/device-groups/${id}/devices`)

// 脚本管理
export const getScripts = (params) => http.get('/scripts', { params })
export const createScript = (data) => http.post('/scripts', data)
export const updateScript = (id, data) => http.put(`/scripts/${id}`, data)
export const deleteScript = (id) => http.delete(`/scripts/${id}`)
export const getScriptContent = (id) => http.get(`/scripts/${id}/content`)
export const shareScript = (id, toUserIds) => http.post(`/scripts/${id}/share`, { to_user_ids: toUserIds })
export const getScriptShares = () => http.get('/script-shares')

// 任务管理
export const getTasks = (params) => http.get('/tasks', { params })
export const createTask = (data) => http.post('/tasks', data)
export const updateTask = (id, data) => http.put(`/tasks/${id}`, data)
export const deleteTask = (id) => http.delete(`/tasks/${id}`)
export const startTask = (id) => http.post(`/tasks/${id}/start`)
export const stopTask = (id) => http.post(`/tasks/${id}/stop`)
export const resetTask = (id) => http.post(`/tasks/${id}/reset`)
export const batchControlTasks = (ids, action) => http.post('/tasks/batch/control', { ids, action })
export const repairTask = (id, deviceIds) => http.post(`/tasks/${id}/repair`, { device_ids: deviceIds })
export const getTaskDevices = (id) => http.get(`/tasks/${id}/devices`)
export const getTaskLogs = (id, params) => http.get(`/tasks/${id}/logs`, { params })
export const shareTask = (id, toUserIds) => http.post(`/tasks/${id}/share`, { to_user_ids: toUserIds })
export const getTaskShares = () => http.get('/task-shares')

// 资源管理
export const getResources = (params) => http.get('/resources', { params })
export const uploadResource = (formData) => http.post('/resources/upload', formData, { timeout: 600000, headers: { 'Content-Type': 'multipart/form-data' } })
export const deleteResource = (id) => http.delete(`/resources/${id}`)
export const replaceResource = (id, formData) => http.put(`/resources/${id}/replace`, formData, { timeout: 600000, headers: { 'Content-Type': 'multipart/form-data' } })
export const shareResource = (id, toUserIds) => http.post(`/resources/${id}/share`, { to_user_ids: toUserIds })
export const getResourceShares = () => http.get('/resource-shares')

// 参数模板
export const getTemplates = (params) => http.get('/templates', { params })
export const createTemplate = (data) => http.post('/templates', data)
export const updateTemplate = (id, data) => http.put(`/templates/${id}`, data)
export const deleteTemplate = (id) => http.delete(`/templates/${id}`)

// 数据管理
export const getDataTemplates = () => http.get('/data/templates')
export const createDataTemplate = (data) => http.post('/data/templates', data)
export const updateDataTemplate = (id, data) => http.put(`/data/templates/${id}`, data)
export const deleteDataTemplate = (id) => http.delete(`/data/templates/${id}`)
export const getDataRecords = (params) => http.get('/data/records', { params })
export const createDataRecord = (data) => http.post('/data/records', data)
export const updateDataRecord = (id, data) => http.put(`/data/records/${id}`, data)
export const deleteDataRecord = (id) => http.delete(`/data/records/${id}`)
export const getDataPermissions = (params) => http.get('/data/permissions', { params })
export const setDataPermission = (data) => http.post('/data/permissions', data)
export const getDataLogs = (params) => http.get('/data/logs', { params })

// 系统管理
export const getUsers = (params) => http.get('/users', { params })
export const getAdmins = () => http.get('/users/admins')
export const createUser = (data) => http.post('/users', data)
export const updateUser = (id, data) => http.put(`/users/${id}`, data)
export const deleteUser = (id) => http.delete(`/users/${id}`)
export const batchDeleteUsers = (ids) => http.delete('/users/batch', { data: { ids } })
export const assignUserRoles = (id, roleCodes) => http.post(`/users/${id}/roles`, { role_codes: roleCodes })
export const getRoles = () => http.get('/roles')
export const createRole = (data) => http.post('/roles', data)
export const updateRole = (id, data) => http.put(`/roles/${id}`, data)
export const deleteRole = (id) => http.delete(`/roles/${id}`)
export const getPermissions = () => http.get('/permissions')
export const getSystemLogs = (params) => http.get('/system/logs', { params })
export const getSystemConfig = () => http.get('/system/config')
export const updateSystemConfig = (data) => http.put('/system/config', data)

// WebSocket
export const pushTaskToDevice = (data) => http.post('/ws/push-task', data)
export const pushCommandToDevice = (data) => http.post('/ws/push-command', data)
export const pushCommandToDevices = (data) => http.post('/ws/push-command-batch', data)
export const getOnlineDevices = () => http.get('/ws/online-devices')
export const getDevicesRealtime = (params) => http.get('/ws/devices-realtime', { params })
export const getScreenshots = (params) => http.get('/ws/screenshots', { params })
export const getScreenFrames = (params) => http.get('/ws/screen-frames', { params })
