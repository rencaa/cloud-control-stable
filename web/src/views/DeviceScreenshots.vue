<template>
  <div class="screenshot-page">
    <el-card>
      <template #header>
        <el-row justify="space-between" align="middle">
          <span>📸 设备截图 - {{ deviceName || deviceId }}</span>
          <el-button @click="refresh" :loading="loading">刷新</el-button>
        </el-row>
      </template>

      <div v-if="list.length === 0" style="text-align:center;padding:60px;color:#999">
        <div style="font-size:60px">📷</div>
        <div style="margin-top:16px">暂无截图</div>
        <div style="font-size:12px;margin-top:8px">请先在设备列表点「截图」按钮</div>
      </div>

      <el-row :gutter="12">
        <el-col v-for="item in list" :key="item.filename" :span="8" style="margin-bottom:12px">
          <el-card shadow="hover" :body-style="{ padding: '8px' }">
            <el-image
              :src="item.url"
              fit="cover"
              style="width:100%;height:200px;cursor:pointer"
              :preview-src-list="[item.url]"
              :initial-index="0"
            />
            <div style="padding:4px 8px;font-size:12px;color:#666">
              {{ formatTime(item.time) }}
              <span style="float:right">{{ formatSize(item.size) }}</span>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import http from '@/api/index'

const route = useRoute()
const deviceId = ref(route.query.device_id || '')
const deviceName = ref(route.query.device_name || '')
const list = ref([])
const loading = ref(false)

async function refresh() {
  loading.value = true
  try {
    const res = await http.get('/ws/screenshots', { params: { device_id: deviceId.value } })
    const token = encodeURIComponent(localStorage.getItem('token') || '')
    list.value = (res.data || []).map(item => ({ ...item, url: item.url + '&access_token=' + token })).sort((a, b) => b.time - a.time)
  } catch (e) {
    list.value = []
  }
  loading.value = false
}

function formatTime(ts) {
  if (!ts) return ''
  return new Date(ts * 1000).toLocaleString('zh-CN')
}

function formatSize(bytes) {
  if (!bytes) return ''
  if (bytes < 1024) return bytes + 'B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + 'KB'
  return (bytes / 1048576).toFixed(1) + 'MB'
}

onMounted(refresh)
</script>
