<template>
  <div class="screen-wall">
    <div class="sw-header">
      <h1>📱 屏幕墙</h1>
      <div class="sw-controls">
        <el-select v-model="filterGroup" placeholder="全部分组" clearable size="small" style="width:140px" @change="loadDevices">
          <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
        </el-select>
        <el-radio-group v-model="mode" size="small">
          <el-radio-button value="shot">📷 截图</el-radio-button>
          <el-radio-button value="stream">📺 推流</el-radio-button>
        </el-radio-group>
        <el-button v-if="mode==='shot'" type="primary" @click="captureAll" :loading="capturing">
          {{ capturing ? '截图中...' : '截图分组设备' }}
        </el-button>
        <el-button v-else :type="streaming?'danger':'success'" @click="toggleStream">
          {{ streaming ? '停止推流' : '开始推流' }}
        </el-button>
        <span class="sw-info" v-if="lastTime">上次: {{ lastTime }}</span>
      </div>
    </div>

    <div v-if="!devicesWithShots.length && !capturing && !streaming" class="sw-placeholder">
      <div class="sw-placeholder-icon">{{ mode==='shot' ? '📷' : '📺' }}</div>
      <div>选择分组后点击{{ mode==='shot' ? '截图' : '推流' }}</div>
    </div>

    <div v-else class="sw-grid">
      <div v-for="d in devicesWithShots" :key="d.device_id" class="sw-device" @click="selected = d">
        <div class="sw-device-header">
          <span class="sw-device-name">{{ d.name }}</span>
          <el-tag size="small" :type="d.online ? 'success' : 'info'">{{ d.online ? '在线' : '离线' }}</el-tag>
        </div>
        <div class="sw-frame">
          <img v-if="d.image" :src="d.image" class="sw-img" />
          <el-icon v-else :size="32" class="sw-loading"><Loading /></el-icon>
        </div>
      </div>
    </div>

    <el-dialog v-model="showDetail" :title="selected?.name" fullscreen>
      <img v-if="selected?.image" :src="selected.image" style="max-width:100%;max-height:85vh;margin:auto;display:block" />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import http from '@/api/index'

const devicesWithShots = ref([])
const groups = ref([])
const filterGroup = ref(null)
const mode = ref('shot')
const capturing = ref(false)
const streaming = ref(false)
const showDetail = ref(false)
const selected = ref(null)
const lastTime = ref('')
let timer = null

async function loadDevices() {
  try {
    const params = { page: 1, size: 200 }
    if (filterGroup.value) params.group_id = filterGroup.value
    const res = await http.get('/devices', { params })
    const list = (res.data || []).filter(d => d.status > 0)
    devicesWithShots.value = list.map(d => ({ ...d, image: null, online: d.status > 0 }))
  } catch (e) {}
}
async function loadGroups() { try { const res = await http.get('/device-groups'); groups.value = res.data || []; } catch (e) {} }

async function pushBatchCommand(command) {
  const deviceIds = devicesWithShots.value.filter(d => d.status > 0).map(d => d.device_id)
  if (!deviceIds.length) return
  await http.post('/ws/push-command-batch', { device_ids: deviceIds, command })
}

async function captureAll() {
  capturing.value = true
  try { await pushBatchCommand('screenshot') } catch (e) {}
  await new Promise(r => setTimeout(r, 2500))
  try {
    const res = await http.get('/ws/screenshots', { params: { page: 1, size: 500 } })
    const shots = res.data || []
    for (const d of devicesWithShots.value) {
      const match = shots.find(s => s.device_id === d.device_id || s.filename?.startsWith(d.device_id))
      if (match) d.image = match.url + '&access_token=' + encodeURIComponent(localStorage.getItem('token') || '')
    }
  } catch (e) {}
  const now = new Date(); lastTime.value = String(now.getHours()).padStart(2,'0')+':'+String(now.getMinutes()).padStart(2,'0')+':'+String(now.getSeconds()).padStart(2,'0')
  capturing.value = false
}

function startStream() {
  streaming.value = true
  refreshFrames()
  timer = setInterval(refreshFrames, 1500)
}
function stopStream() { streaming.value = false; if (timer) { clearInterval(timer); timer = null } }
async function refreshFrames() {
  try {
    const deviceIds = devicesWithShots.value.map(d => d.device_id).join(',')
    const res = await http.get('/ws/screen-frames', { params: { device_ids: deviceIds } })
    const frames = res.data || {}
    for (const d of devicesWithShots.value) { if (frames[d.device_id]) d.image = frames[d.device_id] }
  } catch (e) {}
}

async function toggleStream() {
  if (streaming.value) {
    stopStream()
    try { await pushBatchCommand('screen_stream_stop') } catch (e) {}
  } else {
    try { await pushBatchCommand('screen_stream_start') } catch (e) {}
    startStream()
  }
}

onMounted(() => { loadDevices(); loadGroups(); })
onUnmounted(() => { stopStream() })
</script>

<style scoped>
.screen-wall { min-height: 100vh; background: #0a0e1a; padding: 16px 20px; color: #fff; }
.sw-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.sw-header h1 { font-size: 22px; margin: 0; }
.sw-controls { display: flex; gap: 10px; align-items: center; }
.sw-info { font-size: 12px; color: #8890b0; }
.sw-placeholder { text-align: center; padding: 120px 0; color: #667; }
.sw-placeholder-icon { font-size: 64px; margin-bottom: 16px; }
.sw-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; }
.sw-device { background: rgba(255,255,255,.05); border-radius: 10px; overflow: hidden; cursor: pointer; transition: transform .2s; }
.sw-device:hover { transform: scale(1.02); }
.sw-device-header { display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; background: rgba(0,0,0,.3); }
.sw-device-name { font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sw-frame { aspect-ratio: 9/19; background: #111; display: flex; align-items: center; justify-content: center; overflow: hidden; }
.sw-img { width: 100%; height: 100%; object-fit: contain; }
.sw-loading { color: #444; animation: spin 1s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
</style>
