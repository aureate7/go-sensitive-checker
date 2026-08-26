<template>
  <div class="evaluation-page">
    <header class="evaluation-header"><div><span class="hero-badge">质量评测</span><h1>检测效果评估</h1><p>使用标注样本计算 Precision、Recall 和 F1，定位误报与漏报。</p></div><div class="token-box"><el-input v-model="token" type="password" show-password placeholder="管理令牌"/><el-button type="primary" @click="connect">连接</el-button></div></header>
    <el-alert v-if="error" type="error" :closable="false" :description="error"/>
    <el-card v-if="connected"><template #header>评测输入</template><div class="form"><el-select v-model="policyId"><el-option v-for="policy in policies" :key="policy.id" :label="`${policy.name} v${policy.version}`" :value="policy.id"/></el-select><el-input v-model="sampleJSON" type="textarea" :rows="14"/><div class="actions"><span>JSON 数组，每项包含 text 和 expected_sensitive</span><el-button type="primary" @click="run">运行评测</el-button></div></div></el-card>
    <template v-if="report"><section class="metrics"><el-card v-for="item in metrics" :key="item.label"><strong>{{ item.value }}</strong><span>{{ item.label }}</span></el-card></section><el-card><template #header>失败样本（{{ report.failures.length }}）</template><div v-for="failure in report.failures" :key="failure.index" class="failure"><el-tag :type="failure.reason==='误报'?'danger':'warning'">{{ failure.reason }}</el-tag><span>样本 #{{ failure.index + 1 }}</span><span>期望 {{ failure.expected }}，实际 {{ failure.actual }}</span></div><el-empty v-if="!report.failures.length" description="全部样本符合预期"/></el-card></template>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { describeAPIError, fetchPlatformPolicies, runPolicyEvaluation } from '@/services/api'
defineOptions({name:'QualityEvaluations'})
const token=ref(sessionStorage.getItem('sensitive_admin_token')||''),connected=ref(false),error=ref(''),policies=ref([]),policyId=ref('default'),report=ref(null)
const sampleJSON=ref(JSON.stringify([{text:'普通文本',expected_sensitive:false},{text:'示例敏感表达',expected_sensitive:true}],null,2))
const connect=async()=>{sessionStorage.setItem('sensitive_admin_token',token.value);try{policies.value=(await fetchPlatformPolicies(token.value)).items||[];connected.value=true;error.value=''}catch(err){error.value=describeAPIError(err).message}}
const run=async()=>{try{const samples=JSON.parse(sampleJSON.value);report.value=await runPolicyEvaluation(token.value,{policy_id:policyId.value,samples});error.value=''}catch(err){error.value=err instanceof SyntaxError?'样本 JSON 格式错误':describeAPIError(err).message}}
const percent=(value)=>`${(Number(value||0)*100).toFixed(2)}%`
const metrics=computed(()=>report.value?[{label:'Precision',value:percent(report.value.precision)},{label:'Recall',value:percent(report.value.recall)},{label:'F1',value:percent(report.value.f1)},{label:'TP / FP / FN',value:`${report.value.tp} / ${report.value.fp} / ${report.value.fn}`}]:[])
</script>

<style scoped>
.evaluation-page{display:flex;flex-direction:column;gap:18px}.evaluation-header,.actions{display:flex;justify-content:space-between;align-items:center;gap:12px}.evaluation-header h1{margin:8px 0}.evaluation-header p,.actions{color:var(--text-sub)}.token-box{display:flex;gap:8px;width:min(420px,100%)}.form{display:flex;flex-direction:column;gap:12px}.metrics{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}.metrics .el-card :deep(.el-card__body){display:flex;flex-direction:column;gap:8px;text-align:center}.metrics strong{font-size:24px}.failure{display:flex;align-items:center;gap:12px;padding:9px;border-bottom:1px solid var(--border-subtle)}@media(max-width:760px){.evaluation-header{flex-direction:column;align-items:flex-start}.metrics{grid-template-columns:1fr 1fr}}
</style>
