<template>
  <el-card class="category-card" shadow="hover">
    <template #header>
      <div class="card-header">
        <span>文本检测</span>
      </div>
    </template>

    <el-form label-position="top">
      <!-- 文本输入 -->
      <el-form-item label="输入要检测的文本" :for="inputId">
        <el-input
          v-model="text"
          type="textarea"
          :id="inputId"
          :rows="8"
          class="dark-textarea"
          placeholder="请输入要检测的文本内容..."
          :maxlength="props.limits?.max_text_runes || undefined"
          show-word-limit
        />
      </el-form-item>

      <!-- 选择检测类别 + 按钮 -->
      <el-form-item>
        <div class="category-label-row" style="margin-bottom: 8px">
          <span class="category-label-main">选择检测类别</span>

          <div class="category-actions">
            <el-button class="btn-clear" size="small" @click="clearCategories">
              清空类别
            </el-button>

            <el-button class="btn-select-all" size="small" @click="selectAll"> 全选 </el-button>
          </div>
        </div>

        <el-checkbox-group v-model="selectedKeys">
          <el-row :gutter="12">
            <el-col v-for="(label, key) in categories" :key="key" :xs="12" :sm="8">
              <el-checkbox :label="key">
                {{ label }}
              </el-checkbox>
            </el-col>
          </el-row>
        </el-checkbox-group>
      </el-form-item>

      <el-form-item>
        <div class="mapping-box">
          <div class="mapping-head">
            <div class="mapping-title-row">
              <span class="category-label-main">词组映射</span>
              <el-tooltip effect="light" placement="top" popper-class="mapping-help-popper">
                <template #content>
                  <div class="mapping-help-pop">
                    <div>
                      1. 开启后，系统会先将原文中的隐晦写法映射为规范词，再进入敏感词检测流程。
                    </div>
                    <div>
                      2. 映射文件支持格式：`源词=>目标词`（也兼容
                      `->`、`=`、英文逗号`,`、制表符），每行一条。
                    </div>
                    <div>
                      3. `增量映射`：系统内置映射 +
                      你导入的映射共同生效；`覆盖映射`：只使用你导入的映射。
                    </div>
                    <div>4. 相同映射项会自动去重，注释行（`#` 或 `//` 开头）会被忽略。</div>
                  </div>
                </template>
                <span class="rate-help-trigger">
                  <el-icon><QuestionFilled /></el-icon>
                </span>
              </el-tooltip>
            </div>
            <el-switch
              v-model="enableTermMapping"
              inline-prompt
              active-text="开"
              inactive-text="关"
            />
          </div>

          <p class="mapping-help">
            支持格式：源词=>目标词（也支持 ->、=、英文逗号(,)、制表符），每行一条，# 开头为注释。
          </p>

          <div class="mapping-actions">
            <el-select
              v-model="mappingMode"
              size="small"
              class="mapping-mode-select"
              :disabled="!enableTermMapping"
            >
              <el-option label="增量映射（系统 + 用户）" value="incremental" />
              <el-option label="覆盖映射（仅用户）" value="override" />
            </el-select>

            <input
              ref="mappingFileInput"
              type="file"
              accept=".txt,.csv,.tsv,.map"
              class="mapping-file-input"
              @change="handleMappingFileChange"
            />

            <el-button size="small" :disabled="!enableTermMapping" @click="triggerMappingImport">
              导入映射文件
            </el-button>

            <el-button size="small" @click="clearMappings" :disabled="!customMappings.length">
              清空导入
            </el-button>
          </div>

          <div class="mapping-meta">
            <el-tag size="small" type="info"> 已导入 {{ customMappings.length }} 条 </el-tag>
            <span v-if="mappingFileName" class="mapping-file-name">
              {{ mappingFileName }}
            </span>
          </div>

          <el-alert
            v-if="mappingParseError"
            type="error"
            :closable="false"
            show-icon
            :description="mappingParseError"
          />
        </div>
      </el-form-item>

      <el-form-item>
        <div class="llm-box">
          <div class="llm-head">
            <div class="mapping-title-row">
              <span class="category-label-main">大模型辅助鉴别</span>
              <el-tooltip effect="light" placement="top" popper-class="mapping-help-popper">
                <template #content>
                  <div class="mapping-help-pop">
                    <div>
                      1. 开启后，将在规则检测完成后把部分原文发送到管理员配置的模型服务。
                    </div>
                    <div>2. 辅助结论不会替代规则引擎结果，可用于二次复核与边界文本判断。</div>
                    <div>3. 请仅在已完成隐私告知并获得授权时开启。</div>
                  </div>
                </template>
                <span class="rate-help-trigger">
                  <el-icon><QuestionFilled /></el-icon>
                </span>
              </el-tooltip>
            </div>
            <el-switch
              v-model="enableLLMAssist"
              inline-prompt
              active-text="开"
              inactive-text="关"
              :disabled="!props.llmEnabled"
            />
          </div>
          <p class="mapping-help">
            {{ props.llmEnabled ? '会向配置的第三方模型发送部分原文，仅建议在获得授权后开启。' : '服务端未启用大模型辅助鉴别。' }}
          </p>
        </div>
      </el-form-item>

      <!-- 底部按钮 -->
      <el-form-item>
        <el-space>
          <el-button @click="handleClear">清空文本</el-button>
          <el-button
            type="primary"
            :loading="props.loading"
            :disabled="!props.serviceReady || !text.trim() || selectedKeys.length === 0"
            @click="handleSubmit"
          >
            {{ props.loading ? '检测中...' : '开始检测' }}
          </el-button>
          <el-button v-if="props.loading" type="danger" plain @click="emit('cancel')">取消</el-button>
        </el-space>
      </el-form-item>
      <el-alert
        v-if="!props.serviceReady"
        type="warning"
        :closable="false"
        description="检测服务或词库尚未就绪，请检查后端状态。"
      />
    </el-form>
  </el-card>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { fetchCategories } from '@/services/api'
import { QuestionFilled } from '@element-plus/icons-vue'

const props = defineProps({
  loading: {
    type: Boolean,
    default: false,
  },
  serviceReady: { type: Boolean, default: false },
  limits: { type: Object, default: () => ({}) },
  llmEnabled: { type: Boolean, default: false },
})

const emit = defineEmits(['detect', 'cancel'])

const text = ref('')
const categories = ref({})
const selectedKeys = ref([])
const enableTermMapping = ref(true)
const enableLLMAssist = ref(false)
const mappingMode = ref('incremental')
const customMappings = ref([])
const mappingFileName = ref('')
const mappingParseError = ref('')
const mappingFileInput = ref(null)

// 动态生成 inputId，确保唯一性
const inputId = `el-id-${Math.random().toString(36).substr(2, 9)}`

// 加载类别（初始状态：不选任何一个）
onMounted(async () => {
  try {
    categories.value = (await fetchCategories()) || {}
    selectedKeys.value = [] // 初始不选
  } catch {
    mappingParseError.value = '检测类别加载失败，请刷新页面重试。'
  }
})

// 点击“开始检测”
const handleSubmit = () => {
  if (!text.value.trim()) {
    console.warn('文本为空，不发请求')
    return
  }
  if (selectedKeys.value.length === 0) {
    console.warn('未选择任何检测类别，不发请求')
    return
  }

  const options = {
    exact_match: true,
    normalize_match: true,
    fuzzy_match: true,
    pinyin_match: true,
    enable_term_mapping: enableTermMapping.value,
    enable_llm_assist: enableLLMAssist.value,
    mapping_mode: mappingMode.value,
    custom_mappings: customMappings.value,
  }

  emit('detect', {
    text: text.value,
    categories: selectedKeys.value,
    options,
  })
}

// 清空文本
const handleClear = () => {
  text.value = ''
}

// 全选所有类别
const selectAll = () => {
  selectedKeys.value = Object.keys(categories.value)
}

// 清空类别
const clearCategories = () => {
  selectedKeys.value = []
}

const parseMappingLine = (line) => {
  const m = line.match(/^(.*?)\s*(=>|->|=|,|\t)\s*(.*?)$/)
  if (!m) return null
  const from = m[1]?.trim()
  const to = m[3]?.trim()
  if (!from || !to) return null
  return { from, to }
}

const parseMappingText = (content) => {
  const lines = String(content || '').split(/\r?\n/)
  const pairs = []
  const seen = new Set()
  let invalidCount = 0

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#') || line.startsWith('//')) {
      continue
    }
    const pair = parseMappingLine(line)
    if (!pair) {
      invalidCount += 1
      continue
    }
    const key = `${pair.from}=>${pair.to}`
    if (seen.has(key)) {
      continue
    }
    seen.add(key)
    pairs.push(pair)
  }

  return { pairs, invalidCount }
}

const triggerMappingImport = () => {
  if (!mappingFileInput.value) return
  mappingFileInput.value.click()
}

const handleMappingFileChange = async (event) => {
  const file = event?.target?.files?.[0]
  if (!file) return

  mappingParseError.value = ''
  try {
    const content = await file.text()
    const { pairs, invalidCount } = parseMappingText(content)
    const maxMappings = Number(props.limits?.max_custom_mappings || 500)
    if (pairs.length > maxMappings) {
      mappingParseError.value = `映射数量为 ${pairs.length}，超过服务端上限 ${maxMappings}。`
      customMappings.value = []
      mappingFileName.value = file.name
      return
    }
    if (!pairs.length) {
      mappingParseError.value = '未解析到有效映射，请检查文件格式。'
      customMappings.value = []
      mappingFileName.value = file.name
      return
    }

    customMappings.value = pairs
    mappingFileName.value = file.name
    if (invalidCount > 0) {
      mappingParseError.value = `已导入 ${pairs.length} 条，忽略 ${invalidCount} 条无效行。`
    }
  } catch (err) {
    mappingParseError.value = err?.message || '读取映射文件失败'
  } finally {
    if (event?.target) {
      event.target.value = ''
    }
  }
}

const clearMappings = () => {
  customMappings.value = []
  mappingFileName.value = ''
  mappingParseError.value = ''
}
</script>

<style scoped>
.category-card {
  background: var(--bg-card-soft);
  border: 1px solid var(--border-subtle);
  color: var(--text-main);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
  color: var(--text-sub);
}

/* 文本输入区域 */
.dark-textarea .el-textarea__inner {
  background: var(--input-bg);
  border: 1px solid var(--input-border);
  color: var(--text-main);
  font-size: 14px;
  border-radius: 12px;
  padding: 12px;
  resize: none;
  min-height: 150px;
}

.dark-textarea .el-textarea__inner::placeholder {
  color: rgba(148, 163, 184, 0.7);
}

.app-shell--dark .dark-textarea .el-textarea__inner::placeholder {
  color: rgba(148, 163, 184, 0.9);
}

/* 顶部 label + 按钮在同一行 */
.category-label-row {
  display: flex;
  align-items: center;
}

.category-label-main {
  font-weight: 500;
  color: var(--text-sub);
}

/* 右侧按钮组：贴右侧，小胶囊排布 */
.category-actions {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

/* 统一压一压 Element 默认按钮高度 */
.category-actions .el-button {
  padding: 4px 14px;
  font-size: 12px;
  border-radius: 999px;
  line-height: 1.2;
}

/* —— 全选：主色渐变小胶囊 —— */
.btn-select-all {
  background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
  color: #fff;

  border: 1px solid rgba(255, 255, 255, 0.18);
  box-shadow: 0 2px 5px rgba(79, 70, 229, 0.28);

  cursor: pointer;
  transition: 0.18s ease;
}

.btn-select-all:hover {
  opacity: 0.97;
  transform: translateY(-1px);
  box-shadow: 0 4px 10px rgba(79, 70, 229, 0.38);
}

/* —— 清空类别：浅色线框按钮 —— */
.btn-clear {
  background: transparent;
  border: 1px solid var(--border-subtle);
  color: var(--text-sub);
  cursor: pointer;
  transition: 0.18s ease;
}

/* 浅色主题 hover */
.app-shell:not(.app-shell--dark) .btn-clear:hover {
  background: rgba(0, 0, 0, 0.04);
  color: var(--text-main);
}

/* 深色主题 hover */
.app-shell--dark .btn-clear:hover {
  background: rgba(255, 255, 255, 0.08);
  color: var(--text-main);
}

/* label 颜色走主题变量 */
:deep(.el-form-item__label) {
  color: var(--text-sub);
}

.mapping-box {
  width: 100%;
  border: 1px dashed var(--border-subtle);
  border-radius: 10px;
  padding: 10px;
}

.llm-box {
  width: 100%;
  border: 1px dashed var(--border-subtle);
  border-radius: 10px;
  padding: 10px;
}

.mapping-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.llm-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.mapping-title-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.rate-help-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #64748b;
  cursor: help;
  line-height: 0;
  transition: color 0.15s ease;
}

.rate-help-trigger :deep(svg) {
  width: 17px;
  height: 17px;
}

.rate-help-trigger:hover {
  color: #2563eb;
}

.mapping-help-pop,
:deep(.mapping-help-popper .mapping-help-pop) {
  max-width: 420px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.55;
  color: #334155;
}

.mapping-help {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--text-sub);
  line-height: 1.5;
}

.mapping-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.mapping-mode-select {
  width: 210px;
}

.mapping-file-input {
  display: none;
}

.mapping-meta {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.mapping-file-name {
  font-size: 12px;
  color: var(--text-sub);
}
</style>
