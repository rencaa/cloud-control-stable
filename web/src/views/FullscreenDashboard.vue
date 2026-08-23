<template>
  <div class="big-screen">
    <div class="bs-header">
      <h1>📊 云控数据大屏</h1>
      <span class="bs-time">{{ now }}</span>
    </div>

    <el-row :gutter="16" class="bs-stats">
      <el-col :span="3" v-for="s in statsCards" :key="s.key">
        <div class="bs-stat-card" :style="{ borderTopColor: s.color }">
          <div class="bs-stat-num">{{ s.value }}</div>
          <div class="bs-stat-label">{{ s.label }}</div>
        </div>
      </el-col>
    </el-row>

    <el-row :gutter="16" style="margin-top:16px">
      <el-col :span="12">
        <div class="bs-chart-box">
          <div class="bs-chart-title">📈 任务执行统计</div>
          <v-chart :option="taskChartOption" style="height:300px" autoresize />
        </div>
      </el-col>
      <el-col :span="12">
        <div class="bs-chart-box">
          <div class="bs-chart-title">📋 在线设备列表</div>
          <el-table :data="onlineDevices" size="small" max-height="300">
            <el-table-column prop="name" label="名称" />
            <el-table-column prop="device_id" label="设备ID" width="200" />
            <el-table-column prop="ip" label="IP" width="140" />
            <el-table-column label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 3 ? 'danger' : row.status === 2 ? 'warning' : 'success'" size="small">
                  {{ row.status === 3 ? '执行中' : row.status === 2 ? '忙碌' : '在线' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-col>
    </el-row>

    <el-row :gutter="16" style="margin-top:16px">
      <el-col :span="16">
        <div class="bs-chart-box">
          <div class="bs-chart-title">📍 设备地域分布</div>
          <v-chart :option="mapOption" style="height:350px" autoresize />
        </div>
      </el-col>
      <el-col :span="8">
        <div class="bs-chart-box">
          <div class="bs-chart-title">📜 实时日志</div>
          <div class="bs-log-list">
            <div v-for="(log, i) in recentLogs" :key="i" class="bs-log-item">
              <span class="bs-log-time">{{ log.time }}</span>
              <span :class="'bs-log-tag ' + log.type">{{ log.type }}</span>
              <span class="bs-log-msg">{{ log.msg }}</span>
            </div>
          </div>
        </div>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import VChart from 'vue-echarts'
import 'echarts'
import http from '@/api/index'

const stats = ref({ device_count: 0, online_count: 0, task_count: 0, running_count: 0, script_count: 0, resource_count: 0, user_count: 0, today_task_count: 0 })
const onlineDevices = ref([])
const recentLogs = ref([])
const now = ref('')
let timer = null

const statsCards = computed(() => [
  { key: 'device', value: stats.value.device_count, label: '设备总数', color: '#0066FF' },
  { key: 'online', value: stats.value.online_count, label: '在线设备', color: '#00C853' },
  { key: 'task', value: stats.value.task_count, label: '任务总数', color: '#FF9500' },
  { key: 'running', value: stats.value.running_count, label: '执行中', color: '#FF3B30' },
  { key: 'script', value: stats.value.script_count, label: '脚本数', color: '#5856D6' },
  { key: 'resource', value: stats.value.resource_count, label: '资源数', color: '#AF52DE' },
  { key: 'user', value: stats.value.user_count, label: '用户数', color: '#30D158' },
  { key: 'today', value: stats.value.today_task_count, label: '今日任务', color: '#0A84FF' },
])

const taskChartOption = computed(() => ({
  tooltip: { trigger: 'item' },
  legend: { bottom: 0 },
  series: [{
    type: 'pie',
    radius: ['35%', '65%'],
    center: ['50%', '45%'],
    data: [
      { value: stats.value.task_count - stats.value.running_count, name: '已完成/停止', itemStyle: { color: '#0066FF' } },
      { value: stats.value.running_count, name: '执行中', itemStyle: { color: '#FF3B30' } },
    ],
    label: { fontSize: 13 },
  }],
}))

const mapOption = ref({})

async function load() {
  try {
    const [s, r, logsRes] = await Promise.all([
      http.get('/dashboard/stats'),
      http.get('/dashboard/realtime'),
      http.get('/system/logs?page=1&size=10').catch(() => ({})),
    ])
    stats.value = s.data || s
    onlineDevices.value = (r.data?.online_devices || r.data?.devices || []).slice(0, 10)
    // 设备日志
    const sysLogs = (logsRes?.data?.list || logsRes?.data || []).slice(0, 10)
    sysLogs.forEach(l => {
      addLog('info', (l.detail || l.action || l.message || '').substring(0, 100))
    })
  } catch (e) {}
}

function formatNow() {
  const d = new Date()
  now.value = d.getFullYear() + '-' +
    String(d.getMonth() + 1).padStart(2, '0') + '-' +
    String(d.getDate()).padStart(2, '0') + ' ' +
    String(d.getHours()).padStart(2, '0') + ':' +
    String(d.getMinutes()).padStart(2, '0') + ':' +
    String(d.getSeconds()).padStart(2, '0')
}

// 设备地域分布
function buildMapData() {
  mapOption.value = {
    tooltip: { trigger: 'axis' },
    grid: { left: 40, right: 20, top: 20, bottom: 30 },
    xAxis: { type: 'category', data: ['福建', '浙江', '广东', '江苏'], axisLabel: { color: '#b0b8d0' } },
    yAxis: { type: 'value', axisLabel: { color: '#b0b8d0' } },
    series: [{ type: 'bar', data: [3, 2, 2, 1], itemStyle: { color: '#0066FF', borderRadius: [4,4,0,0] } }],
  }
}

function addLog(type, msg) {
  const d = new Date()
  recentLogs.value.unshift({
    time: String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0') + ':' + String(d.getSeconds()).padStart(2, '0'),
    type, msg,
  })
  if (recentLogs.value.length > 30) recentLogs.value.pop()
}

onMounted(() => {
  load()
  formatNow()
  buildMapData()
  timer = setInterval(() => { load(); formatNow() }, 10000)
  addLog('info', '数据大屏已启动')
})

onUnmounted(() => clearInterval(timer))
</script>

<style scoped>
.big-screen {
  min-height: 100vh;
  background: linear-gradient(180deg, #0A0E27 0%, #121840 100%);
  padding: 20px 24px;
  color: #fff;
}
.bs-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.bs-header h1 { font-size: 26px; font-weight: 700; margin: 0; }
.bs-time { font-size: 18px; color: #8890b0; font-variant-numeric: tabular-nums; }
.bs-stats { margin: 0 -8px; }
.bs-stats .el-col { padding: 0 8px; }
.bs-stat-card {
  background: rgba(255,255,255,.06); border-radius: 10px; padding: 16px 12px; text-align: center;
  border-top: 3px solid #0066FF; backdrop-filter: blur(10px);
}
.bs-stat-num { font-size: 28px; font-weight: 700; }
.bs-stat-label { font-size: 12px; color: #8890b0; margin-top: 4px; }
.bs-chart-box { background: rgba(255,255,255,.06); border-radius: 10px; padding: 16px; backdrop-filter: blur(10px); }
.bs-chart-title { font-size: 14px; font-weight: 600; margin-bottom: 12px; color: #b0b8d0; }
.bs-log-list { max-height: 320px; overflow-y: auto; font-size: 12px; }
.bs-log-item { padding: 4px 0; display: flex; gap: 8px; align-items: center; border-bottom: 1px solid rgba(255,255,255,.05); }
.bs-log-time { color: #667; flex-shrink: 0; }
.bs-log-tag { padding: 0 4px; border-radius: 3px; font-size: 10px; flex-shrink: 0; }
.bs-log-tag.info { background: #0066FF33; color: #0066FF; }
.bs-log-tag.warn { background: #FF950033; color: #FF9500; }
.bs-log-tag.error { background: #FF3B3033; color: #FF3B30; }
.bs-log-msg { color: #b0b8d0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
