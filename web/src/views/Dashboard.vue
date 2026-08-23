<template>
  <div class="dashboard">
    <div class="page-header">
      <div>
        <h2>云控工作台</h2>
        <p>集中查看设备在线状态、任务执行情况和常用操作</p>
      </div>
      <div class="page-actions">
        <el-button @click="router.push('/device/list')">
          <el-icon><Monitor /></el-icon>
          设备管理
        </el-button>
        <el-button type="primary" @click="router.push('/screen-wall')">
          <el-icon><Monitor /></el-icon>
          屏幕墙
        </el-button>
        <el-button plain @click="router.push('/dashboard/fullscreen')">
          <el-icon><DataBoard /></el-icon>
          全屏大屏
        </el-button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <el-row :gutter="16" class="stats-row">
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-content">
            <div class="stat-icon device-icon"><el-icon :size="28"><Monitor /></el-icon></div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.device_count || 0 }}</div>
              <div class="stat-label">设备总数</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-content">
            <div class="stat-icon online-icon"><el-icon :size="28"><Connection /></el-icon></div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.online_count || 0 }}</div>
              <div class="stat-label">在线设备</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-content">
            <div class="stat-icon task-icon"><el-icon :size="28"><List /></el-icon></div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.task_count || 0 }}</div>
              <div class="stat-label">任务总数</div>
            </div>
          </div>
        </el-card>
      </el-col>
      <el-col :xs="12" :sm="6">
        <el-card shadow="hover" class="stat-card">
          <div class="stat-content">
            <div class="stat-icon running-icon"><el-icon :size="28"><VideoPlay /></el-icon></div>
            <div class="stat-info">
              <div class="stat-value">{{ stats.running_count || 0 }}</div>
              <div class="stat-label">运行中任务</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="quick-actions">
      <template #header>
        <div class="section-title">
          <span>常用操作</span>
          <span class="section-hint">高频入口集中到这里</span>
        </div>
      </template>
      <div class="action-grid">
        <button class="action-item" type="button" @click="router.push('/device/list')">
          <span class="action-icon device-action"><el-icon><Monitor /></el-icon></span>
          <span><strong>设备列表</strong><small>查看在线设备并批量控制</small></span>
        </button>
        <button class="action-item" type="button" @click="router.push('/task/list')">
          <span class="action-icon task-action"><el-icon><List /></el-icon></span>
          <span><strong>任务管理</strong><small>创建、启动和查看任务</small></span>
        </button>
        <button class="action-item" type="button" @click="router.push('/script/list')">
          <span class="action-icon script-action"><el-icon><Document /></el-icon></span>
          <span><strong>脚本管理</strong><small>维护手机端执行脚本</small></span>
        </button>
        <button class="action-item" type="button" @click="router.push('/resource/list')">
          <span class="action-icon resource-action"><el-icon><FolderOpened /></el-icon></span>
          <span><strong>资源管理</strong><small>上传和分发业务资源</small></span>
        </button>
      </div>
    </el-card>

    <!-- 在线设备和最近任务 -->
    <el-row :gutter="16" class="dashboard-tables">
      <el-col :xs="24" :lg="12">
        <el-card header="在线设备">
          <el-table :data="realtime.online_devices || []" size="small" max-height="300">
            <el-table-column prop="name" label="设备名称" />
            <el-table-column prop="device_id" label="设备ID" />
            <el-table-column prop="ip" label="IP地址" />
            <el-table-column prop="model" label="型号" />
          </el-table>
        </el-card>
      </el-col>
      <el-col :xs="24" :lg="12">
        <el-card header="最近任务">
          <el-table :data="realtime.recent_tasks || []" size="small" max-height="300">
            <el-table-column prop="name" label="任务名称" />
            <el-table-column prop="status" label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
                  {{ row.status === 1 ? '运行中' : '已停止' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="created_at" label="创建时间" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getDashboardStats, getDashboardRealtime } from '@/api/endpoints'

const router = useRouter()
const stats = ref({})
const realtime = ref({})

async function loadData() {
  try {
    const [statsRes, realtimeRes] = await Promise.all([
      getDashboardStats(),
      getDashboardRealtime(),
    ])
    stats.value = statsRes.data
    realtime.value = realtimeRes.data
  } catch (e) {
    console.error('加载仪表盘数据失败', e)
  }
}

function formatDate(dateStr) {
  if (!dateStr) return ''
  return new Date(dateStr).toLocaleString('zh-CN')
}

onMounted(loadData)
</script>

<style scoped>
.stats-row {
  margin-bottom: 16px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
}

.page-header h2 {
  font-size: 22px;
  line-height: 1.3;
}

.page-header p {
  color: #8e8e93;
  margin-top: 5px;
  font-size: 13px;
}

.page-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.quick-actions {
  margin-bottom: 16px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 600;
}

.section-hint {
  font-size: 12px;
  font-weight: 400;
  color: #8e8e93;
}

.action-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.action-item {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  border: 1px solid #e5e5ea;
  border-radius: 10px;
  background: #fff;
  padding: 14px;
  text-align: left;
  cursor: pointer;
  transition: border-color 180ms ease, box-shadow 180ms ease, transform 180ms ease;
}

.action-item:hover {
  border-color: #99ccff;
  box-shadow: 0 4px 14px rgba(0, 102, 255, .1);
  transform: translateY(-1px);
}

.action-item > span:last-child {
  min-width: 0;
}

.action-item strong,
.action-item small {
  display: block;
}

.action-item strong {
  color: #1a1a2e;
  font-size: 14px;
}

.action-item small {
  overflow: hidden;
  margin-top: 4px;
  color: #8e8e93;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.action-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 36px;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  color: #fff;
  font-size: 18px;
}

.device-action { background: linear-gradient(135deg, #1890ff, #36cfc9); }
.task-action { background: linear-gradient(135deg, #722ed1, #b37feb); }
.script-action { background: linear-gradient(135deg, #fa8c16, #ffc069); }
.resource-action { background: linear-gradient(135deg, #eb2f96, #ff85c0); }

.dashboard-tables {
  margin-top: 0;
}

.stat-card {
  cursor: pointer;
  transition: transform 0.2s;
}

.stat-card:hover {
  transform: translateY(-2px);
}

.stat-content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
}

.device-icon { background: linear-gradient(135deg, #1890ff, #36cfc9); }
.online-icon { background: linear-gradient(135deg, #52c41a, #73d13d); }
.task-icon { background: linear-gradient(135deg, #722ed1, #b37feb); }
.running-icon { background: linear-gradient(135deg, #13c2c2, #36cfc9); }
.script-icon { background: linear-gradient(135deg, #fa8c16, #ffc069); }
.res-icon { background: linear-gradient(135deg, #eb2f96, #ff85c0); }
.user-icon { background: linear-gradient(135deg, #2f54eb, #85a5ff); }
.today-icon { background: linear-gradient(135deg, #fa541c, #ff9c6e); }

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: #262626;
}

.stat-label {
  font-size: 14px;
  color: #8c8c8c;
  margin-top: 4px;
}

@media (max-width: 900px) {
  .action-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 600px) {
  .page-header {
    flex-direction: column;
  }

  .page-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .action-grid {
    grid-template-columns: 1fr;
  }
}
</style>
