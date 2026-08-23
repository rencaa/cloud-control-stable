<template>
  <div class="task-list">
    <el-card class="search-card">
      <el-row :gutter="16">
        <el-col :span="6">
          <el-input v-model="keyword" placeholder="搜索任务名称" clearable @change="loadData" />
        </el-col>
        <el-col :span="4">
          <el-select v-model="filterStatus" placeholder="任务状态" clearable @change="loadData" style="width:100%">
            <el-option label="全部" :value="-1" />
            <el-option label="已停止" :value="0" />
            <el-option label="运行中" :value="1" />
            <el-option label="已完成" :value="2" />
          </el-select>
        </el-col>
        <el-col :span="14" style="text-align:right">
          <el-button @click="batchControl('start')" :disabled="!selectedIds.length">批量启动</el-button>
          <el-button @click="batchControl('stop')" :disabled="!selectedIds.length">批量停止</el-button>
          <el-button @click="batchControl('reset')" :disabled="!selectedIds.length">批量重置</el-button>
          <el-button type="danger" @click="batchDelete()" :disabled="!selectedIds.length">批量删除</el-button>
          <el-button type="primary" @click="showCreateDialog = true">创建任务</el-button>
        </el-col>
      </el-row>
    </el-card>

    <el-card style="margin-top:16px">
      <el-table :data="tasks" v-loading="loading" @selection-change="handleSelection" stripe>
        <el-table-column type="selection" width="50" />
        <el-table-column prop="name" label="任务名称" min-width="150" />
        <el-table-column label="关联脚本" width="120">
          <template #default="{ row }">{{ row.script?.name || '-' }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : row.status === 2 ? '' : 'info'" size="small">
              {{ row.status === 1 ? '运行中' : row.status === 2 ? '已完成' : '已停止' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="cron_expr" label="定时" width="100" />
        <el-table-column label="时区/补跑" width="150">
          <template #default="{ row }">{{ row.cron_timezone || 'Asia/Shanghai' }} / {{ misfireText(row.misfire_policy) }}</template>
        </el-table-column>
        <el-table-column label="超时" width="90">
          <template #default="{ row }">{{ row.timeout_seconds || 3600 }}秒</template>
        </el-table-column>
        <el-table-column label="下次执行" width="170">
          <template #default="{ row }">{{ formatDate(row.next_run_at) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <el-button link type="success" size="small" @click="handleControl(row, 'start')" :disabled="row.status===1">启动</el-button>
            <el-button link type="warning" size="small" @click="handleControl(row, 'stop')" :disabled="row.status!==1">停止</el-button>
            <el-button link type="primary" size="small" @click="handleControl(row, 'reset')">重置</el-button>
            <el-button link type="primary" size="small" @click="handleRepair(row)">修复</el-button>
            <el-button link size="small" @click="handleViewLogs(row)">日志</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[10,20,50]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>

    <!-- 创建任务对话框 -->
    <el-dialog v-model="showCreateDialog" title="创建任务" width="600px">
      <el-form :model="createForm" label-width="100px">
        <el-form-item label="任务名称" required>
          <el-input v-model="createForm.name" placeholder="请输入任务名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" />
        </el-form-item>
        <el-form-item label="关联脚本" required>
          <el-select v-model="createForm.script_id" placeholder="选择脚本" style="width:100%">
            <el-option v-for="s in scripts" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标设备" required>
          <el-select v-model="createForm.device_ids" multiple placeholder="选择设备" style="width:100%">
            <el-option v-for="d in devices" :key="d.id" :label="`${d.name} (${d.device_id})`" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="任务参数">
          <el-input v-model="createForm.params_str" type="textarea" :rows="3" placeholder='JSON格式' />
        </el-form-item>
          <el-form-item label="执行超时">
            <el-input-number v-model="createForm.timeout_seconds" :min="30" :max="86400" :step="60" />
            <span style="margin-left:8px;color:#909399">秒（30秒～24小时）</span>
          </el-form-item>
          <el-form-item label="定时执行">
            <el-switch v-model="createForm.cron_enabled" />
          </el-form-item>
          <el-form-item v-if="createForm.cron_enabled" label="执行周期">
            <el-radio-group v-model="createForm.cron_expr" size="small">
              <el-radio-button value="@every 60s">每分钟</el-radio-button>
              <el-radio-button value="@every 300s">5分钟</el-radio-button>
              <el-radio-button value="@every 600s">10分钟</el-radio-button>
              <el-radio-button value="@every 1800s">30分钟</el-radio-button>
              <el-radio-button value="@every 3600s">每小时</el-radio-button>
            </el-radio-group>
            <el-input v-model="createForm.cron_expr" placeholder="或自定义: @every 600s / 0 */10 * * * *" size="small" style="margin-top:8px" />
          </el-form-item>
          <el-form-item v-if="createForm.cron_enabled" label="任务时区">
            <el-select v-model="createForm.cron_timezone" style="width:100%">
              <el-option label="中国标准时间" value="Asia/Shanghai" />
              <el-option label="UTC" value="UTC" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="createForm.cron_enabled" label="错过执行">
            <el-select v-model="createForm.misfire_policy" style="width:100%">
              <el-option label="只执行最近一次（推荐）" value="latest" />
              <el-option label="补跑一次" value="run_once" />
              <el-option label="直接跳过" value="skip" />
            </el-select>
          </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="handleCreateTask" :loading="creating">创建</el-button>
      </template>
    </el-dialog>

    <!-- 任务修复对话框 -->
    <el-dialog v-model="showRepairDialog" title="任务修复 - 重新下发异常设备" width="700px">
      <el-input v-model="repairSearch" placeholder="搜索设备" style="margin-bottom:12px" />
      <el-table :data="filteredRepairDevices" ref="repairTable" @selection-change="handleRepairSelection" max-height="400">
        <el-table-column type="selection" width="50" />
        <el-table-column prop="device.name" label="设备名称" />
        <el-table-column prop="device.device_id" label="设备ID" />
        <el-table-column prop="device.ip" label="IP地址" />
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="repairStatusType(row.status)" size="small">{{ repairStatusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="showRepairDialog = false">取消</el-button>
        <el-button type="primary" @click="confirmRepair" :disabled="!repairSelected.length">修复选中设备</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  getTasks, createTask, deleteTask, startTask, stopTask, resetTask,
  batchControlTasks, repairTask, getTaskDevices,
} from '@/api/endpoints'
import { getScripts } from '@/api/endpoints'
import { getDevices } from '@/api/endpoints'

const tasks = ref([])
const scripts = ref([])
const devices = ref([])
const loading = ref(false)
const creating = ref(false)
const page = ref(1)
const size = ref(10)
const total = ref(0)
const keyword = ref('')
const filterStatus = ref(-1)
const selectedIds = ref([])

const showCreateDialog = ref(false)
const emptyCreateForm = () => ({
  name: '', description: '', script_id: null, device_ids: [], params_str: '', cron_expr: '', cron_enabled: false,
  timeout_seconds: 3600, cron_timezone: 'Asia/Shanghai', misfire_policy: 'latest',
})
const createForm = ref(emptyCreateForm())
let refreshTimer = null

const showRepairDialog = ref(false)
const repairDevices = ref([])
const repairSearch = ref('')
const repairSelected = ref([])
const currentTaskId = ref(null)

const filteredRepairDevices = computed(() => {
  if (!repairSearch.value) return repairDevices.value
  const kw = repairSearch.value.toLowerCase()
  return repairDevices.value.filter(d =>
    (d.device?.name || '').toLowerCase().includes(kw) ||
    (d.device?.device_id || '').toLowerCase().includes(kw)
  )
})

async function loadData() {
  loading.value = true
  try {
    const res = await getTasks({ page: page.value, size: size.value, keyword: keyword.value, status: filterStatus.value >= 0 ? filterStatus.value : undefined })
    tasks.value = res.data
    total.value = res.total
  } finally { loading.value = false }
}

function handleSelection(rows) { selectedIds.value = rows.map(r => r.id) }

async function handleControl(row, action) {
  try {
    const api = { start: startTask, stop: stopTask, reset: resetTask }
    await api[action](row.id)
    ElMessage.success({ start: '已启动', stop: '已停止', reset: '已重置' }[action])
    loadData()
  } catch {}
}

async function batchControl(action) {
  try {
    await batchControlTasks(selectedIds.value, action)
    ElMessage.success('批量操作成功')
    loadData()
  } catch {}
}

async function batchDelete() {
  try {
    await ElMessageBox.confirm('确定批量删除选中任务？', '确认', { type: 'warning' })
    for (const id of selectedIds.value) await deleteTask(id)
    ElMessage.success('批量删除成功')
    selectedIds.value = []
    loadData()
  } catch {}
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除任务 "${row.name}"？`, '确认', { type: 'warning' })
    await deleteTask(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch {}
}

async function handleRepair(row) {
  currentTaskId.value = row.id
  repairSearch.value = ''
  repairSelected.value = []
  try {
    const res = await getTaskDevices(row.id)
    repairDevices.value = res.data || []
    showRepairDialog.value = true
  } catch {}
}

function handleRepairSelection(rows) { repairSelected.value = rows.map(r => r.device_id || r.device?.id) }

async function confirmRepair() {
  try {
    await repairTask(currentTaskId.value, repairSelected.value)
    ElMessage.success('修复成功，任务已重新下发')
    showRepairDialog.value = false
    loadData()
  } catch (e) { ElMessage.error(e.message) }
}

async function handleCreateTask() {
  creating.value = true
  try {
    await createTask({
      name: createForm.value.name,
      description: createForm.value.description,
      script_id: createForm.value.script_id,
      device_ids: createForm.value.device_ids,
      params: createForm.value.params_str || null,
	      cron_expr: createForm.value.cron_expr,
	      cron_enabled: createForm.value.cron_enabled,
	      timeout_seconds: createForm.value.timeout_seconds,
	      cron_timezone: createForm.value.cron_timezone,
	      misfire_policy: createForm.value.misfire_policy,
	    })
    ElMessage.success('任务创建成功')
    showCreateDialog.value = false
	    createForm.value = emptyCreateForm()
    loadData()
  } catch (e) { ElMessage.error(e.message) }
  creating.value = false
}

function handleViewLogs(row) {
  window.open(`/task/logs?task_id=${row.id}`, '_blank')
}

function repairStatusType(s) { return { 0: 'info', 1: 'warning', 2: 'success', 3: 'danger', 4: 'danger' }[s] || 'info' }
function repairStatusText(s) { return { 0: '待执行', 1: '执行中', 2: '成功', 3: '失败', 4: '异常' }[s] || '未知' }

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }
function misfireText(value) { return { latest: '最近一次', run_once: '补跑一次', skip: '跳过' }[value] || '最近一次' }

onMounted(async () => {
  loadData()
  try { scripts.value = (await getScripts({ page: 1, size: 100 })).data || [] } catch {}
  try { devices.value = (await getDevices({ page: 1, size: 100 })).data || [] } catch {}
  refreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible' && !loading.value && !showCreateDialog.value && !showRepairDialog.value) loadData()
  }, 3000)
})

onUnmounted(() => { if (refreshTimer) clearInterval(refreshTimer) })
</script>
