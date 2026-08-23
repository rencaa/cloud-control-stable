<template>
  <div class="device-list">
    <!-- 搜索和操作栏 -->
    <el-card class="search-card">
      <el-row :gutter="16">
        <el-col :span="6">
          <el-input v-model="searchKeyword" placeholder="搜索设备名称/ID/IP" clearable @clear="reloadFromFirstPage" @keyup.enter="reloadFromFirstPage">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="filterStatus" placeholder="设备状态" clearable @change="reloadFromFirstPage" style="width:100%">
            <el-option label="全部" :value="-1" />
            <el-option label="离线" :value="0" />
            <el-option label="在线" :value="1" />
            <el-option label="忙碌" :value="2" />
            <el-option label="执行中" :value="3" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="filterGroup" placeholder="设备分组" clearable @change="reloadFromFirstPage" style="width:100%">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-col>
        <el-col :span="10" style="text-align: right">
          <el-button @click="handleBatchAddGroup" :disabled="!selectedIds.length">批量添加分组</el-button>
          <el-button type="warning" @click="showBatchDlg = true" :disabled="!selectedIds.length">批量操作</el-button>
          <el-button @click="handleBatchReset" :disabled="!selectedIds.length">批量重置</el-button>
          <el-button type="danger" @click="handleBatchDelete" :disabled="!selectedIds.length">批量删除</el-button>
          <el-button type="primary" @click="showAddDialog = true">添加设备</el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 设备表格 -->
    <el-card style="margin-top: 16px">
      <el-table :data="devices" v-loading="loading" row-key="id" @selection-change="handleSelectionChange" stripe>
        <el-table-column type="selection" width="50" :reserve-selection="true" />
        <el-table-column label="设备名称" min-width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="router.push({name:'DeviceDetail',params:{id:row.device_id}})">{{ row.name }}</el-button>
          </template>
        </el-table-column>
        <el-table-column prop="device_id" label="设备ID" width="140" />
        <el-table-column prop="model" label="型号" width="100" />
        <el-table-column prop="os_version" label="系统版本" width="100" />
        <el-table-column label="客户端/协议/队列" width="170">
          <template #default="{ row }">
            <el-tag :type="agentOutdated(row) ? 'warning' : 'success'" size="small">
              {{ row.agent_version || '旧版' }} / P{{ row.protocol_version || 1 }} / Q{{ row.agent_outbox_depth || 0 }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP地址" width="130" />
        <el-table-column label="位置" width="120">
          <template #default="{ row }">{{ row.province || '' }}{{ row.city || '' }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="deviceStatusType(row)" size="small">{{ deviceStatusText(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="电量" width="65">
          <template #default="{ row }">{{ row.battery ? row.battery + '%' : '-' }}</template>
        </el-table-column>
        <el-table-column label="在线" width="60">
          <template #default="{ row }">
            <span :style="{color: row.online ? '#67C23A' : '#909399', fontWeight:'bold'}">{{ row.online ? '●' : '○' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="group" label="分组" width="120">
          <template #default="{ row }">{{ row.group?.name || '-' }}</template>
        </el-table-column>
        <el-table-column label="最后心跳" width="160">
          <template #default="{ row }">{{ formatDate(row.last_heartbeat) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="340" fixed="right">
          <template #default="{ row }">
            <el-button link type="success" size="small" @click="handleCommand(row, 'run_local')" :disabled="!row.online">启动</el-button>
            <el-button link type="danger" size="small" @click="handleCommand(row, 'stop_local')" :disabled="!row.online">停止</el-button>
            <el-button link type="warning" size="small" @click="handleCommand(row, 'screenshot')" :disabled="!row.online">截图</el-button>
            <el-button link size="small" @click="handleViewScreenshots(row)">📸</el-button>
            <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
            <el-button link type="primary" size="small" @click="handleViewLogs(row)">日志</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @change="loadData"
        style="margin-top: 16px; justify-content: flex-end"
      />
    </el-card>

    <!-- 添加/编辑设备对话框 -->
    <el-dialog v-model="showAddDialog" :title="editingDevice ? '编辑设备' : '添加设备'" width="500px">
      <el-form ref="deviceFormRef" :model="deviceForm" :rules="deviceRules" label-width="100px">
        <el-form-item label="设备名称" prop="name">
          <el-input v-model="deviceForm.name" placeholder="请输入设备名称" />
        </el-form-item>
        <el-form-item label="设备ID" prop="device_id">
          <el-input v-model="deviceForm.device_id" placeholder="自动生成或手动输入" :disabled="!!editingDevice" />
        </el-form-item>
        <el-form-item label="设备型号" prop="model">
          <el-input v-model="deviceForm.model" placeholder="如: Xiaomi 13" />
        </el-form-item>
        <el-form-item label="系统版本" prop="os_version">
          <el-input v-model="deviceForm.os_version" placeholder="如: Android 13" />
        </el-form-item>
        <el-form-item label="IP地址">
          <el-input v-model="deviceForm.ip" placeholder="设备IP地址" />
        </el-form-item>
        <el-form-item label="所属分组">
          <el-select v-model="deviceForm.group_id" placeholder="选择分组" clearable style="width:100%">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="设备参数">
          <el-input v-model="deviceForm.params_str" type="textarea" :rows="3" placeholder='JSON格式, 如: {"key":"value"}' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSaveDevice" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 批量添加分组对话框 -->
    <el-dialog v-model="showBatchGroupDialog" title="批量添加分组" width="400px">
      <el-select v-model="batchGroupId" placeholder="选择分组" style="width:100%">
        <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
      </el-select>
      <template #footer>
        <el-button @click="showBatchGroupDialog = false">取消</el-button>
        <el-button type="primary" @click="confirmBatchAddGroup">确定</el-button>
      </template>
    </el-dialog>

    <!-- 批量操作 -->
    <el-dialog v-model="showBatchDlg" title="批量操作" width="500px">
      <div style="margin-bottom:8px;color:#666">已选 {{ selectedIds.length }} 台设备</div>
      <div style="margin-bottom:12px">
        <el-input
          v-model="liveRoomTarget"
          clearable
          placeholder="直播间ID或抖音分享链接（进入直播间时填写）"
        />
      </div>
      <el-row :gutter="12">
        <el-col :span="12" v-for="act in batchActions" :key="act.cmd" style="margin-bottom:12px">
          <el-card shadow="hover" :body-style="{padding:'16px',cursor:'pointer',textAlign:'center'}"
                   @click="handleBatchCmd(act)" :style="{borderColor:act.color||'#ddd'}">
            <div style="font-size:22px;margin-bottom:4px">{{ act.icon }}</div>
            <div style="font-size:13px;font-weight:bold">{{ act.label }}</div>
            <div style="font-size:11px;color:#999;margin-top:4px">{{ act.desc }}</div>
          </el-card>
        </el-col>
      </el-row>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getDevicesRealtime, createDevice, updateDevice, deleteDevice,
  batchDeleteDevices, batchResetDevices, batchAddGroup,
  getDeviceGroups, pushCommandToDevice, pushCommandToDevices,
} from '@/api/endpoints'

const router = useRouter()

const devices = ref([])
const groups = ref([])
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const size = ref(10)
const total = ref(0)
const searchKeyword = ref('')
const filterStatus = ref(-1)
const filterGroup = ref('')
const selectedIds = ref([])
const selectedDeviceIds = ref([])
let deviceRefreshTimer = null
let deviceRequestInFlight = false

const showAddDialog = ref(false)
const editingDevice = ref(null)
const deviceFormRef = ref(null)
const deviceForm = ref({
  name: '', device_id: '', model: '', os_version: '', ip: '', group_id: null, params_str: '',
})
const deviceRules = {
  name: [{ required: true, message: '请输入设备名称', trigger: 'blur' }],
}

const showBatchGroupDialog = ref(false)
const batchGroupId = ref(null)
const showBatchDlg = ref(false)
const liveRoomTarget = ref('')

const batchActions = [
  { cmd: 'run_local', icon: '▶', label: '批量启动', desc: '启动抖音业务', color: '#67C23A' },
  { cmd: 'stop_local', icon: '⏹', label: '批量停止', desc: '停止抖音业务', color: '#F56C6C' },
  { cmd: 'screenshot', icon: '📷', label: '批量截图', desc: '截取设备屏幕', color: '#E6A23C' },
  { cmd: 'home', icon: '🏠', label: '返回桌面', desc: '按 Home 键', color: '#909399' },
  { cmd: 'read_contacts', icon: '👤', label: '读取通讯录', desc: '拉取设备通讯录', color: '#5856D6' },
  { cmd: 'read_sms', icon: '💬', label: '读取短信', desc: '拉取最近20条短信', color: '#FF6B35' },
  { cmd: 'sms_monitor', icon: '📲', label: '短信监听', desc: '开启/关闭实时监听', color: '#30D158' },
  { cmd: 'screen_stream_start', icon: '📺', label: '开始推流', desc: '开启屏幕推流', color: '#0066FF' },
  { cmd: 'screen_stream_stop', icon: '⏸', label: '停止推流', desc: '关闭屏幕推流', color: '#909399' },
  { cmd: 'enter_live_room', icon: '🎥', label: '进入直播间', desc: '输入ID或分享链接', color: '#8B5CF6', needsRoom: true },
  { cmd: 'back', icon: '🚪', label: '退出直播间', desc: '返回抖音上一级', color: '#64748B' },
]

async function handleBatchCmd(act) {
  const target = liveRoomTarget.value.trim()
  if (act.needsRoom && !target) {
    ElMessage.warning('请先输入直播间ID或分享链接')
    return
  }

  try {
    await ElMessageBox.confirm(`确定对 ${selectedIds.value.length} 台设备执行「${act.label}」？`, '确认', { type: 'warning' })
    const res = await pushCommandToDevices({
      device_ids: selectedDeviceIds.value,
      command: act.cmd,
      ...(act.needsRoom ? { params: { live_room_id: target } } : {}),
    })
    const result = res.data || {}
    ElMessage.success(`${act.label} 已发送到 ${result.sent || 0}/${selectedIds.value.length} 台设备`)
    showBatchDlg.value = false
  } catch {}
}

async function loadData(silent = false) {
  if (deviceRequestInFlight) return
  deviceRequestInFlight = true
  if (silent !== true) loading.value = true
  try {
    const params = {
      page: page.value,
      size: size.value,
      keyword: searchKeyword.value || undefined,
      status: filterStatus.value >= 0 ? filterStatus.value : undefined,
      group_id: filterGroup.value || undefined,
    }
    const res = await getDevicesRealtime(params)
    devices.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    if (silent !== true) ElMessage.error('加载设备列表失败')
  } finally {
    if (silent !== true) loading.value = false
    deviceRequestInFlight = false
  }
}

function reloadFromFirstPage() {
  page.value = 1
  loadData()
}

async function loadGroups() {
  try {
    const res = await getDeviceGroups()
    groups.value = res.data || []
  } catch {}
}

function handleSelectionChange(rows) {
  selectedIds.value = rows.map(r => r.id)
  selectedDeviceIds.value = rows.map(r => r.device_id)
}

function handleEdit(row) {
  editingDevice.value = row
  deviceForm.value = {
    name: row.name,
    device_id: row.device_id,
    model: row.model,
    os_version: row.os_version,
    ip: row.ip,
    group_id: row.group_id || null,
    params_str: row.device_params ? JSON.stringify(typeof row.device_params === 'string' ? JSON.parse(row.device_params) : row.device_params, null, 2) : '',
  }
  showAddDialog.value = true
}

async function handleSaveDevice() {
  const valid = await deviceFormRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const data = {
      name: deviceForm.value.name,
      device_id: deviceForm.value.device_id,
      model: deviceForm.value.model,
      os_version: deviceForm.value.os_version,
      ip: deviceForm.value.ip,
      group_id: deviceForm.value.group_id,
      device_params: deviceForm.value.params_str || null,
    }

    if (editingDevice.value) {
      await updateDevice(editingDevice.value.id, data)
      ElMessage.success('设备更新成功')
		} else {
			const result = await createDevice(data)
			ElMessage.success('设备添加成功')
			if (result.data?.device_token) {
				await ElMessageBox.alert(`设备凭证（只显示这一次）：\n\n${result.data.device_token}\n\n零接触自动注册模式下手机会自动保存；如需手动接入，再填入脚本的 deviceToken/DEVICE_TOKEN。`, '保存设备凭证', { confirmButtonText: '我已保存', type: 'warning' })
			}
    }
    showAddDialog.value = false
    editingDevice.value = null
    loadData()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
  saving.value = false
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定要删除设备 "${row.name}" 吗？`, '确认删除', { type: 'warning' })
    await deleteDevice(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

async function handleReset(row) {
  try {
    await batchResetDevices([row.id])
    ElMessage.success('重置成功')
    loadData()
  } catch {}
}

async function handleBatchDelete() {
  try {
    await ElMessageBox.confirm(`确定要删除选中的 ${selectedIds.value.length} 台设备吗？`, '批量删除', { type: 'warning' })
    await batchDeleteDevices(selectedIds.value)
    ElMessage.success('批量删除成功')
    selectedIds.value = []
    selectedDeviceIds.value = []
    loadData()
  } catch {}
}

async function handleBatchReset() {
  try {
    await batchResetDevices(selectedIds.value)
    ElMessage.success('批量重置成功')
    loadData()
  } catch {}
}

function handleBatchAddGroup() {
  showBatchGroupDialog.value = true
  batchGroupId.value = null
}

async function confirmBatchAddGroup() {
  if (!batchGroupId.value) return
  try {
    await batchAddGroup({ device_ids: selectedIds.value, group_id: batchGroupId.value })
    ElMessage.success('分组设置成功')
    showBatchGroupDialog.value = false
    loadData()
  } catch {}
}

async function handleBatchBuiltin() {
  try {
    await batchBuiltinTask(selectedIds.value)
    ElMessage.success('内置任务设置成功')
    loadData()
  } catch {}
}

function handleViewLogs(row) {
  // 保持在桌面端主窗口内，避免 Electron/WebView 将 window.open 交给外部程序。
  router.push({ name: 'DeviceLogs', query: { device_id: String(row.id) } })
}

function statusType(status) {
  const map = { 0: 'info', 1: 'success', 2: 'warning', 3: '' }
  return map[status] || 'info'
}

function statusText(status) {
  const map = { 0: '离线', 1: '在线', 2: '忙碌', 3: '执行中' }
  return map[status] || '未知'
}

function scriptStatusText(ss) {
  const map = { idle: '空闲', running: '执行中', paused: '暂停' }
  return map[ss] || ss
}

function deviceStatusText(row) {
  if (!row.online || row.status === 0) return '离线'
  return row.script_status ? scriptStatusText(row.script_status) : statusText(row.status)
}

function deviceStatusType(row) {
  if (!row.online || row.status === 0) return 'info'
  return statusType(row.status)
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}
function agentOutdated(row) { return Number(row.protocol_version || 1) < 2 }

async function handleCommand(row, command) {
  const labels = { run_local: '启动业务', stop_local: '停止业务', screenshot: '截图', home: '回桌面', back: '返回' }
  try {
    await ElMessageBox.confirm(`确定要对 "${row.name}" 执行 ${labels[command] || command} 吗？`, '确认操作', { type: 'warning' })
    await pushCommandToDevice({ device_id: row.device_id, command })
    ElMessage.success('命令已发送')
    setTimeout(() => loadData(true), 500)
  } catch {}
}

function handleViewScreenshots(row) {
  router.push({ name: 'DeviceScreenshots', query: { device_id: row.device_id, device_name: row.name } })
}

onMounted(() => {
  loadGroups()
  loadData()
  deviceRefreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible') loadData(true)
  }, 3000)
})

onBeforeUnmount(() => {
  if (deviceRefreshTimer) clearInterval(deviceRefreshTimer)
})
</script>
