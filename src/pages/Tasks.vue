<template>
  <div class="tasks-page">
    <header class="tasks-header"><div><span class="hero-badge">批量检测</span><h1>任务中心</h1><p>每行作为一条独立文本检测，任务可取消并导出 CSV 或 JSONL。</p></div><div class="token-box"><el-input v-model="token" type="password" show-password placeholder="管理令牌"/><el-button type="primary" @click="connect">连接</el-button></div></header>
    <el-alert v-if="error" type="error" :closable="false" :description="error" />
    <el-card v-if="connected"><template #header>检测策略</template><div class="policy-grid"><el-input v-model="policyForm.id" placeholder="策略 ID（小写字母/数字）"/><el-input v-model="policyForm.name" placeholder="策略名称"/><el-select v-model="policyForm.categories" multiple collapse-tags placeholder="检测类别"><el-option v-for="(label,key) in categories" :key="key" :label="label" :value="key"/></el-select><el-input v-model.number="policyForm.max_text_runes" type="number" placeholder="单行字符上限"/><el-input v-model="whitelistText" placeholder="白名单短语，逗号分隔"/><el-input v-model="rulesText" placeholder='组合规则 JSON 数组，例如 [{"id":"r1","terms":["加我","微信"],"risk_level":"high"}]'/><el-button type="primary" @click="savePolicy">保存策略</el-button></div></el-card>
    <el-card v-if="connected"><template #header>创建任务</template><div class="create-grid"><el-select v-model="policyId" placeholder="选择检测策略"><el-option v-for="policy in policies" :key="policy.id" :label="`${policy.name}（${policy.id}）`" :value="policy.id" /></el-select><el-input v-model="content" type="textarea" :rows="8" placeholder="每行一条待检测文本" show-word-limit/><div class="create-meta"><span>有效行数：{{ lines.length }}</span><el-button type="primary" :disabled="!policyId || !lines.length" @click="submit">创建后台任务</el-button></div></div></el-card>
    <el-card v-if="connected"><template #header><div class="section-title"><span>任务列表 · 存储 {{ formatBytes(storage.used_bytes) }} / {{ formatBytes(storage.max_bytes) }}</span><div class="actions"><el-button @click="cleanup">清理过期</el-button><el-button @click="loadTasks">刷新</el-button></div></div></template><div class="task-list"><article v-for="task in tasks" :key="task.id" class="task-row"><div><strong>{{ task.id }}</strong><p>策略 {{ task.policy_id }} · {{ task.processed }}/{{ task.total }} · 命中 {{ task.sensitive }} · 失败 {{ task.failed }} · 到期 {{ formatTime(task.expires_at) }}</p></div><el-tag :type="statusType(task.status)">{{ statusText(task.status) }}</el-tag><div class="actions"><el-button v-if="['queued','running'].includes(task.status)" size="small" type="danger" plain @click="cancel(task.id)">取消</el-button><el-button v-if="task.status==='completed'" size="small" @click="download(task.id,'csv')">CSV</el-button><el-button v-if="task.status==='completed'" size="small" @click="download(task.id,'jsonl')">JSONL</el-button><el-button v-if="!['queued','running'].includes(task.status)" size="small" @click="retry(task.id)">重试</el-button><el-button v-if="!['queued','running'].includes(task.status)" size="small" type="danger" plain @click="remove(task.id)">删除</el-button></div></article><el-empty v-if="!tasks.length" description="暂无批量任务" /></div></el-card>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { cancelBatchTask, cleanupBatchTasks, createBatchTask, deleteBatchTask, describeAPIError, downloadBatchResults, fetchBatchTasks, fetchCategories, fetchPlatformPolicies, fetchTaskStorage, retryBatchTask, savePlatformPolicy } from '@/services/api'
defineOptions({ name: 'BatchTaskCenter' })
const token=ref(sessionStorage.getItem('sensitive_admin_token')||''),connected=ref(false),error=ref(''),policies=ref([]),categories=ref({}),policyId=ref('default'),content=ref(''),tasks=ref([])
const storage=ref({used_bytes:0,max_bytes:0})
const policyForm=ref({id:'',name:'',categories:[],max_text_runes:20000,enabled:true,options:{exact_match:true,normalize_match:true,fuzzy_match:true,pinyin_match:true,enable_term_mapping:true,enable_llm_assist:false,mapping_mode:'incremental'}})
const whitelistText=ref(''),rulesText=ref('[]')
let timer=null
const lines=computed(()=>content.value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean))
const report=(err)=>{error.value=describeAPIError(err).message}
const loadTasks=async()=>{try{const [taskData,storageData]=await Promise.all([fetchBatchTasks(token.value),fetchTaskStorage(token.value)]);tasks.value=taskData.items||[];storage.value=storageData;error.value=''}catch(err){report(err);connected.value=false}}
const loadPolicies=async()=>{policies.value=(await fetchPlatformPolicies(token.value)).items||[]}
const connect=async()=>{sessionStorage.setItem('sensitive_admin_token',token.value);try{categories.value=await fetchCategories();await loadPolicies();connected.value=true;await loadTasks();clearInterval(timer);timer=setInterval(loadTasks,2000)}catch(err){report(err)}}
const savePolicy=async()=>{try{const payload={...policyForm.value,whitelist:whitelistText.value.split(/[,，]/).map(v=>v.trim()).filter(Boolean),rules:JSON.parse(rulesText.value||'[]')};await savePlatformPolicy(token.value,payload);policyForm.value={...policyForm.value,id:'',name:'',categories:[]};whitelistText.value='';rulesText.value='[]';await loadPolicies()}catch(err){error.value=err instanceof SyntaxError?'组合规则 JSON 格式错误':describeAPIError(err).message}}
const submit=async()=>{try{await createBatchTask(token.value,{policy_id:policyId.value,lines:lines.value});content.value='';await loadTasks()}catch(err){report(err)}}
const cancel=async(id)=>{try{await cancelBatchTask(token.value,id);await loadTasks()}catch(err){report(err)}}
const retry=async(id)=>{try{await retryBatchTask(token.value,id);await loadTasks()}catch(err){report(err)}}
const remove=async(id)=>{if(!window.confirm('删除任务及全部输入、结果文件？'))return;try{await deleteBatchTask(token.value,id);await loadTasks()}catch(err){report(err)}}
const cleanup=async()=>{try{await cleanupBatchTasks(token.value);await loadTasks()}catch(err){report(err)}}
const download=async(id,format)=>{try{const blob=await downloadBatchResults(token.value,id,format);const url=URL.createObjectURL(blob);const link=document.createElement('a');link.href=url;link.download=`${id}.${format}`;link.click();URL.revokeObjectURL(url)}catch(err){report(err)}}
const statusText=(status)=>({queued:'排队中',running:'执行中',completed:'已完成',cancelled:'已取消',failed:'失败',interrupted:'启动中断'}[status]||status)
const statusType=(status)=>status==='completed'?'success':status==='failed'?'danger':status==='running'?'warning':'info'
const formatBytes=(value)=>{const n=Number(value||0);if(n<1024)return `${n} B`;if(n<1024*1024)return `${(n/1024).toFixed(1)} KB`;return `${(n/1024/1024).toFixed(1)} MB`}
const formatTime=(value)=>value?new Date(value).toLocaleString():'--'
onMounted(()=>{if(token.value)connect()})
onBeforeUnmount(()=>clearInterval(timer))
</script>

<style scoped>
.tasks-page{display:flex;flex-direction:column;gap:18px}.tasks-header,.section-title,.create-meta,.task-row,.actions{display:flex;justify-content:space-between;align-items:center;gap:12px}.tasks-header h1{margin:8px 0}.tasks-header p,.task-row p{color:var(--text-sub)}.token-box{display:flex;gap:8px;width:min(420px,100%)}.create-grid{display:flex;flex-direction:column;gap:12px}.policy-grid{display:grid;grid-template-columns:1fr 1fr 2fr 1fr auto;gap:10px}.task-row{display:grid;grid-template-columns:1fr 100px auto;padding:12px 4px;border-bottom:1px solid var(--border-subtle)}.task-row p{margin:6px 0 0}.task-row strong{font-family:monospace}@media(max-width:900px){.policy-grid{grid-template-columns:1fr 1fr}}@media(max-width:760px){.tasks-header{flex-direction:column;align-items:flex-start}.task-row{grid-template-columns:1fr}.actions{justify-content:flex-start}}
</style>
