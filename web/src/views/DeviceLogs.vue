<template>
  <div>
    <el-card>
      <div style="display:flex;gap:12px;margin-bottom:16px">
        <el-select v-model="logDeviceId" placeholder="选择设备" filterable clearable @change="handleDeviceChange" style="width:240px">
          <el-option v-for="d in devices" :key="d.id" :label="`${d.name} (${d.device_id})`" :value="d.id" />
        </el-select>
        <el-select v-model="logType" placeholder="日志类型" clearable @change="handleFilterChange" style="width:120px">
          <el-option label="全部" value="" />
          <el-option label="调试" value="debug" />
          <el-option label="信息" value="info" />
          <el-option label="警告" value="warn" />
          <el-option label="错误" value="error" />
        </el-select>
        <el-button @click="loadData(false)" :disabled="!logDeviceId">刷新</el-button>
        <span style="color:#909399;line-height:32px">每3秒自动刷新</span>
      </div>
      <el-table :data="logs" v-loading="loading" stripe>
        <el-table-column prop="log_type" label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.log_type === 'error' ? 'danger' : row.log_type === 'warn' ? 'warning' : 'info'" size="small">{{ row.log_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="消息内容" min-width="300">
          <template #default="{ row }"><div class="log-message">{{ row.message }}</div></template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
        </el-table-column>
      </el-table>
      <el-pagination
        v-model:current-page="page" v-model:page-size="size" :total="total"
        :page-sizes="[20,50,100]" layout="total,sizes,prev,pager,next"
        @change="loadData" style="margin-top:16px;justify-content:flex-end"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { getDeviceLogs, getDevices } from '@/api/endpoints'

const route = useRoute()
const logs = ref([])
const devices = ref([])
const loading = ref(false)
const page = ref(1)
const size = ref(20)
const total = ref(0)
const logDeviceId = ref(route.query.device_id ? Number(route.query.device_id) : null)
const logType = ref('')
let refreshTimer = null
let requestInFlight = false

async function loadData(silent = false) {
  if (!logDeviceId.value || requestInFlight) {
    if (!logDeviceId.value) {
      logs.value = []
      total.value = 0
    }
    return
  }
  requestInFlight = true
  if (silent !== true) loading.value = true
  try {
    const res = await getDeviceLogs(logDeviceId.value, {
      page: page.value,
      size: size.value,
      log_type: logType.value || undefined,
    })
    logs.value = res.data
    total.value = res.total
  } finally {
    if (silent !== true) loading.value = false
    requestInFlight = false
  }
}

function handleDeviceChange() {
  page.value = 1
  loadData(false)
}

function handleFilterChange() {
  page.value = 1
  loadData(false)
}

function formatDate(d) { return d ? new Date(d).toLocaleString('zh-CN') : '-' }

onMounted(async () => {
  try { devices.value = (await getDevices({ page: 1, size: 200 })).data || [] } catch {}
  if (logDeviceId.value) loadData()
  refreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible' && logDeviceId.value) loadData(true)
  }, 3000)
})

onBeforeUnmount(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
.log-message {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.55;
}
</style>
