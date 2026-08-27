<template>
  <div class="admin-page">
    <header class="admin-header">
      <div><span class="hero-badge">管理控制台</span><h1>词库治理</h1><p>词条变更会自动创建快照、重建索引并记录审计日志。</p></div>
      <div class="token-box"><el-input v-model="token" type="password" show-password placeholder="管理令牌" /><el-button type="primary" @click="connect">连接</el-button></div>
    </header>
    <el-alert v-if="error" type="error" :closable="false" :description="error" />
    <template v-if="connected">
      <section class="admin-grid">
        <el-card><template #header>新增词条</template><el-select v-model="form.category" placeholder="类别"><el-option v-for="(label,key) in categories" :key="key" :label="label" :value="key" /></el-select><el-input v-model="form.word" placeholder="敏感词" /><el-input v-model="form.reason" placeholder="变更原因" /><el-button type="primary" @click="addWord">新增并发布</el-button></el-card>
        <el-card><template #header>批量导入</template><el-select v-model="importForm.category" placeholder="类别"><el-option v-for="(label,key) in categories" :key="key" :label="label" :value="key" /></el-select><el-input v-model="importForm.content" type="textarea" :rows="5" placeholder="每行一个词条" /><div class="actions"><el-button @click="previewImport">预检</el-button><el-button type="primary" :disabled="!preview?.valid?.length" @click="applyImport">导入 {{ preview?.valid?.length || 0 }} 条</el-button></div><p v-if="preview" class="hint">重复 {{ preview.duplicates.length }} 条，无效 {{ preview.invalid_count }} 条。</p></el-card>
      </section>
      <el-card class="section-card"><template #header><div class="section-title"><span>词条列表（{{ words.total }}）</span><div class="actions"><el-select v-model="filters.category" clearable placeholder="全部类别"><el-option v-for="(label,key) in categories" :key="key" :label="label" :value="key" /></el-select><el-input v-model="filters.q" clearable placeholder="搜索词条" /><el-button @click="loadWords">查询</el-button></div></div></template><div class="word-table"><div v-for="item in words.items" :key="`${item.category}:${item.word}`" class="word-row"><span>{{ item.word }}</span><el-tag>{{ item.category_name }}</el-tag><el-button type="danger" plain size="small" @click="removeWord(item)">删除</el-button></div><el-empty v-if="!words.items.length" description="没有匹配词条" /></div></el-card>
      <section class="admin-grid">
        <el-card><template #header>可回滚版本</template><div v-for="item in versions" :key="item.version" class="version-row"><code>{{ item.version }}</code><el-button size="small" @click="rollback(item.version)">回滚</el-button></div><el-empty v-if="!versions.length" description="暂无快照" /></el-card>
        <el-card><template #header>最近审计</template><div v-for="item in audits" :key="`${item.time}:${item.request_id}`" class="audit-row"><strong>{{ item.action }}</strong><span>{{ item.category }} {{ item.word }}</span><small>{{ formatTime(item.time) }} · {{ item.success ? '成功' : '失败' }}</small></div><el-empty v-if="!audits.length" description="暂无记录" /></el-card>
      </section>
      <el-card class="section-card"><template #header><div class="section-title"><span>白名单管理（误报豁免，{{ whitelistCount }} 条）</span><div class="actions"><el-select v-model="whitelistForm.categories" multiple clearable placeholder="全部类别（可多选限定）" style="min-width:260px"><el-option v-for="(label,key) in categories" :key="key" :label="label" :value="key" /></el-select><el-input v-model="whitelistForm.word" clearable placeholder="豁免词条" /><el-button type="primary" @click="addWhitelist">添加</el-button></div></div></template><div class="word-table"><div v-for="entry in whitelistEntries" :key="entry.key" class="word-row"><span>{{ entry.word }}</span><el-tag>{{ entry.scope }}</el-tag><el-button type="danger" plain size="small" @click="removeWhitelist(entry)">移除</el-button></div><el-empty v-if="!whitelistEntries.length" description="白名单为空" /></div></el-card>
    </template>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { applyAdminImport, createAdminWhitelist, createAdminWord, deleteAdminWhitelist, deleteAdminWord, describeAPIError, fetchAdminWhitelist, fetchAdminWords, fetchAuditEntries, fetchCategories, fetchWordlistVersions, previewAdminImport, rollbackWordlist } from '@/services/api'

defineOptions({ name: 'WordlistAdmin' })
const token = ref(sessionStorage.getItem('sensitive_admin_token') || '')
const connected = ref(false), error = ref(''), categories = ref({}), preview = ref(null), versions = ref([]), audits = ref([])
const words = reactive({ items: [], total: 0 }), filters = reactive({ category: '', q: '' })
const form = reactive({ category: '', word: '', reason: '' }), importForm = reactive({ category: '', content: '', reason: '' })
const whitelistEntries = ref([]), whitelistForm = reactive({ word: '', categories: [] })
const whitelistCount = ref(0)
const report = (err) => { error.value = describeAPIError(err).message }
const loadWords = async () => { try { Object.assign(words, await fetchAdminWords(token.value, filters)); error.value = '' } catch (err) { report(err); connected.value = false } }
const refreshSidebars = async () => { const [v,a] = await Promise.all([fetchWordlistVersions(token.value), fetchAuditEntries(token.value)]); versions.value=v.items||[]; audits.value=a.items||[] }
const loadWhitelist = async () => {
  const data = await fetchAdminWhitelist(token.value)
  const list = []
  for (const word of data.global || []) list.push({ key: `g:${word}`, word, scope: '全部类别' })
  for (const [category, words] of Object.entries(data.by_category || {})) {
    for (const word of words || []) list.push({ key: `c:${category}:${word}`, word, scope: categories.value[category] || category })
  }
  whitelistEntries.value = list
  whitelistCount.value = list.length
}
const addWhitelist = async () => {
  if (!whitelistForm.word.trim()) return
  try {
    await createAdminWhitelist(token.value, { word: whitelistForm.word, categories: whitelistForm.categories, reason: '控制台添加' })
    whitelistForm.word = ''; whitelistForm.categories = []
    await loadWhitelist()
  } catch(err){ report(err) }
}
const removeWhitelist = async (entry) => {
  if(!window.confirm(`确认移除白名单“${entry.word}”（${entry.scope}）？`)) return
  try { await deleteAdminWhitelist(token.value, { word: entry.word, reason: '控制台移除' }); await loadWhitelist() } catch(err){ report(err) }
}
const connect = async () => { sessionStorage.setItem('sensitive_admin_token', token.value); connected.value=true; try { await Promise.all([loadWords(), refreshSidebars(), loadWhitelist()]) } catch (err) { report(err); connected.value=false } }
const addWord = async () => { try { await createAdminWord(token.value, form); form.word=''; await Promise.all([loadWords(), refreshSidebars()]) } catch(err){ report(err) } }
const removeWord = async (item) => { if(!window.confirm(`确认删除“${item.word}”？`)) return; try { await deleteAdminWord(token.value,{category:item.category,word:item.word,reason:'控制台删除'}); await Promise.all([loadWords(),refreshSidebars()]) } catch(err){report(err)} }
const previewImport = async () => { try { preview.value=await previewAdminImport(token.value,importForm) } catch(err){report(err)} }
const applyImport = async () => { try { await applyAdminImport(token.value,importForm); preview.value=null; importForm.content=''; await Promise.all([loadWords(),refreshSidebars()]) } catch(err){report(err)} }
const rollback = async (version) => { if(!window.confirm(`确认回滚到 ${version}？`)) return; try { await rollbackWordlist(token.value,version); await Promise.all([loadWords(),refreshSidebars()]) } catch(err){report(err)} }
const formatTime = (value) => new Date(value).toLocaleString()
onMounted(async()=>{ try { categories.value=await fetchCategories() } catch(err){report(err)}; if(token.value) await connect() })
</script>

<style scoped>
.admin-page{display:flex;flex-direction:column;gap:18px}.admin-header,.section-title,.actions,.version-row{display:flex;justify-content:space-between;align-items:center;gap:12px}.admin-header h1{margin:8px 0}.admin-header p,.hint{color:var(--text-sub)}.token-box{display:flex;gap:8px;width:min(420px,100%)}.admin-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.admin-grid .el-card :deep(.el-card__body){display:flex;flex-direction:column;gap:10px}.section-card{width:100%}.section-title .actions{flex-wrap:wrap}.word-table{max-height:520px;overflow:auto}.word-row{display:grid;grid-template-columns:minmax(180px,1fr) 180px 80px;align-items:center;gap:12px;padding:9px;border-bottom:1px solid var(--border-subtle)}.version-row,.audit-row{padding:8px 0;border-bottom:1px solid var(--border-subtle)}.audit-row{display:grid;grid-template-columns:140px 1fr auto;gap:10px}.audit-row small{color:var(--text-sub)}@media(max-width:800px){.admin-header{align-items:flex-start;flex-direction:column}.admin-grid{grid-template-columns:1fr}.word-row{grid-template-columns:1fr auto}.word-row .el-tag{display:none}.audit-row{grid-template-columns:1fr}}
</style>
