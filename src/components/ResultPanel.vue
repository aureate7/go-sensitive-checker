<template>
  <el-card class="result-card" shadow="hover">
    <template #header>
      <div class="card-header">
        <span>检测结果</span>
        <span class="status-tag" v-if="result">
          <el-tag v-if="result.has_sensitive" type="danger">
            已发现敏感内容
          </el-tag>
          <el-tag v-else type="success">
            内容安全
          </el-tag>
        </span>
      </div>
    </template>

    <!-- 1. 加载中 -->
    <div v-if="loading" class="loading-wrap">
      <el-skeleton :rows="4" animated />
      <div class="loading-text">正在检测文本内容，请稍候…</div>
    </div>

    <!-- 2. 还没检测 -->
    <div v-else-if="!result" class="empty-wrap">
      <el-empty description="输入文本并点击“开始检测”" :image-size="120" />
    </div>

    <!-- 3. 有检测结果（不管是否有敏感词） -->
    <div v-else class="result-wrap">
      <!-- 3.1 总体提示 -->
      <div class="result-summary">
        <el-alert
          v-if="result.has_sensitive"
          type="error"
          show-icon
          :closable="false"
        >
          检测到
          <strong>{{ totalSensitiveCount }}</strong>
          个敏感词，
          风险等级：
          <el-tag size="small" type="danger">
            {{ riskLevelText }}
          </el-tag>
        </el-alert>

        <el-alert v-else type="success" show-icon :closable="false">
          恭喜，未发现敏感词，文本整体安全。
        </el-alert>
      </div>

      <div v-if="llmAssistVisible" class="llm-assist-block">
        <h4>大模型辅助鉴别</h4>
        <el-alert
          v-if="llmAssistError"
          type="warning"
          show-icon
          :closable="false"
          :description="`辅助鉴别调用失败：${llmAssistError}`"
        />
        <el-alert
          v-else
          :type="llmAssistAlertType"
          show-icon
          :closable="false"
        >
          结论：
          <el-tag size="small" :type="llmAssistTagType">
            {{ llmAssistRiskText }}
          </el-tag>
          ，{{ llmAssistReviewText }}
          <template v-if="llmAssistConfidenceText">
            ，置信度 {{ llmAssistConfidenceText }}
          </template>
          <template v-if="llmAssistModel">
            ，模型 {{ llmAssistModel }}
          </template>
          <template v-if="llmAssistReason">
            。说明：{{ llmAssistReason }}
          </template>
        </el-alert>

        <div v-if="llmAssistTerms.length" class="llm-assist-terms">
          <span class="llm-assist-terms__label">可疑词建议：</span>
          <el-tag
            v-for="term in llmAssistTerms"
            :key="term"
            size="small"
            class="llm-assist-terms__tag"
          >
            {{ term }}
          </el-tag>
        </div>
      </div>

      <div v-if="textTotalCount > 0" class="rate-summary">
        <div class="rate-title-row">
          <h4>敏感词率</h4>
          <el-tooltip
            effect="light"
            placement="top-start"
            popper-class="rate-formula-popper"
          >
            <template #content>
              <div class="rate-formula-pop">
                <div class="rate-formula-pop__item rate-formula-pop__item--total">
                  <span>敏感词总字符数 / 文本总字符数 = 敏感词率</span>
                  <strong>{{ sensitiveCharCount }}/{{ textTotalCount }} = {{ sensitiveRate }}</strong>
                </div>
                <div
                  v-for="item in riskRateItems"
                  :key="item.label"
                  class="rate-formula-pop__item"
                >
                  <span>{{ item.label }}</span>
                  <strong>{{ item.count }}/{{ textTotalCount }} = {{ item.rate }}</strong>
                </div>
              </div>
            </template>
            <span class="rate-help-trigger">
              <el-icon><QuestionFilled /></el-icon>
            </span>
          </el-tooltip>
        </div>

        <div class="rate-dashboard">
          <div class="rate-ring-card">
            <div
              class="rate-ring"
              :style="rateRingStyle"
            >
              <div class="rate-ring__inner">
                <div class="rate-ring__value">{{ ringCenterRate }}</div>
                <div class="rate-ring__label">{{ ringCenterLabel }}</div>
              </div>
            </div>

            <div class="rate-ring-legend">
              <div :class="['legend-item', 'legend-item--high', { 'legend-item--active': selectedRateKey === 'high' }]">
                <span class="legend-dot"></span>
                <span>高风险 {{ riskRateItems[0].rate }}</span>
              </div>
              <div :class="['legend-item', 'legend-item--medium', { 'legend-item--active': selectedRateKey === 'medium' }]">
                <span class="legend-dot"></span>
                <span>中风险 {{ riskRateItems[1].rate }}</span>
              </div>
              <div :class="['legend-item', 'legend-item--low', { 'legend-item--active': selectedRateKey === 'low' }]">
                <span class="legend-dot"></span>
                <span>低风险 {{ riskRateItems[2].rate }}</span>
              </div>
              <div :class="['legend-item', 'legend-item--safe', { 'legend-item--active': selectedRateKey === 'safe' }]">
                <span class="legend-dot"></span>
                <span>非敏感 {{ safeRate }}</span>
              </div>
            </div>
          </div>

          <div class="rate-metric-panel">
            <div
              v-for="item in rateVisualItems"
              :key="item.key"
              :class="['rate-metric', `rate-metric--${item.key}`, { 'rate-metric--active': selectedRateKey === item.key }]"
              @click="toggleRateFocus(item.key)"
            >
              <div class="rate-metric__head">
                <span>{{ item.title }}</span>
                <strong>{{ item.rate }}</strong>
              </div>
              <div class="rate-metric__bar">
                <span :style="{ width: `${item.percent}%` }"></span>
              </div>
              <div class="rate-metric__foot">
                {{ item.count }}/{{ textTotalCount }}
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 3.2 分类统计（有敏感词时） -->
      <div
        v-if="result.has_sensitive && Object.keys(displayCategorySummary).length"
        class="category-summary"
      >
        <h4>类别统计</h4>
        <el-row :gutter="8">
          <el-col
            v-for="(stats, cat) in displayCategorySummary"
            :key="cat"
            :xs="24"
            :sm="12"
          >
            <div class="cat-item">
              <div class="cat-name">{{ categoryMap[cat] || cat }}</div> <!-- 显示中文名称 -->
              <div class="cat-badges">
                <el-tag size="small">总计 {{ stats.total }}</el-tag>
                <el-tag v-if="stats.high > 0" size="small" type="danger">高 {{ stats.high }}</el-tag>
                <el-tag v-if="stats.medium > 0" size="small" type="warning">中 {{ stats.medium }}</el-tag>
                <el-tag v-if="stats.low > 0" size="small" type="info">低 {{ stats.low }}</el-tag>
              </div>
            </div>
          </el-col>
        </el-row>
      </div>

      <!-- 3.3 具体敏感词列表 -->
      <div
        v-if="result.has_sensitive && (Object.keys(displayCategories).length || result.detected_words)"
        class="words-block"
      >
        <div class="section-title-row">
          <h4>命中敏感词</h4>
          <el-tooltip
            effect="light"
            placement="top"
            popper-class="section-help-popper"
          >
            <template #content>
              <div class="section-help-pop">
                <div>1. 点击词条会自动滚动到下方“敏感词高亮”区域，并定位到原文对应位置。</div>
                <div>2. 同一词在原文出现多次时，连续点击会按出现顺序循环跳转。</div>
                <div>3. 若词条由映射规则命中，也会按原文实际命中片段进行定位。</div>
              </div>
            </template>
            <span class="rate-help-trigger">
              <el-icon><QuestionFilled /></el-icon>
            </span>
          </el-tooltip>
        </div>

        <!-- 优先按 categories 分组展示（卡片 + 标签网格） -->
        <template v-if="Object.keys(displayCategories).length">
          <el-row :gutter="12">
            <el-col
              v-for="(data, cat) in displayCategories"
              :key="cat"
              :xs="24"
              :sm="12"
              :md="8"
              class="word-col"
            >
              <el-card shadow="never" class="word-card">
                <template #header>
                  <div class="word-card__header">
                    <span class="word-card__title">{{ categoryMap[cat] || cat }}</span>
                    <el-tag size="small" type="info">
                      {{ data.words ? data.words.length : data.count }} 个
                    </el-tag>
                  </div>
                </template>

                <div class="word-grid">
                  <el-tooltip
                    v-for="w in normalizeWords(data.words || [])"
                    :key="w.word + (w.level || '')"
                    effect="light"
                    placement="top"
                    popper-class="word-hit-detail-popper"
                  >
                    <template #content>
                      <div class="word-hit-detail-pop">
                        <div class="word-hit-detail-pop__title">原文命中：{{ w.word }}</div>
                        <div class="word-hit-detail-pop__line">
                          词库命中：{{ getSensitiveWordHint(cat, w) }}
                        </div>
                      </div>
                    </template>
                    <el-tag
                      class="word-tag word-tag--jump"
                      :type="w.level === 'high' ? 'danger' : (w.level === 'medium' ? 'warning' : 'info')"
                      effect="light"
                      disable-transitions
                      @click="handleWordTagJump(cat, w.word)"
                    >
                      <span class="word-tag__text">{{ w.word }}</span>
                      <span class="word-tag__count">×{{ w.count_raw || 1 }}</span>
                    </el-tag>
                  </el-tooltip>
                </div>
              </el-card>
            </el-col>
          </el-row>
        </template>

        <!-- 如果没有 categories 字段，就直接平铺 detected_words -->
        <template v-else-if="result.detected_words && result.detected_words.length">
          <el-space direction="vertical" fill>
            <div
              v-for="w in result.detected_words"
              :key="w.word + w.category"
              class="word-item"
            >
              <el-tooltip
                effect="light"
                placement="top"
                popper-class="word-hit-detail-popper"
              >
                <template #content>
                  <div class="word-hit-detail-pop">
                    <div class="word-hit-detail-pop__title">原文命中：{{ w.word }}</div>
                    <div class="word-hit-detail-pop__line">
                      词库命中：{{ getSensitiveWordHint(w.category, w) }}
                    </div>
                  </div>
                </template>
                <span
                  class="word-text word-text--jump"
                  @click="handleWordTagJump(w.category, w.word)"
                >
                  {{ w.word }}
                </span>
              </el-tooltip>
              <span class="word-meta">
                <el-tag size="small">{{ categoryMap[w.category] || w.category }}</el-tag> <!-- 显示中文名称 -->
                <span class="word-count">
                  出现 {{ w.count_raw || 1 }} 次
                </span>
              </span>
            </div>
          </el-space>
        </template>

        <div v-if="maskCandidateList.length" class="mask-actions">
          <div class="section-title-row section-title-row--actions">
            <div class="section-title-left">
              <h4>勾选后打码</h4>
              <el-tooltip
                effect="light"
                placement="top"
                popper-class="section-help-popper"
              >
                <template #content>
                  <div class="section-help-pop">
                    <div>1. 下拉框显示“原文命中词”（同词已合并），勾选后会处理该词的所有命中位置。</div>
                    <div>2. 点击“对勾选词打码”仅处理当前已勾选词，不会影响未勾选词。</div>
                    <div>3. “清空勾选”只清空选择项，不会清空已经生成的打码结果。</div>
                  </div>
                </template>
                <span class="rate-help-trigger">
                  <el-icon><QuestionFilled /></el-icon>
                </span>
              </el-tooltip>
            </div>
            <el-space class="mask-title-actions" wrap>
              <el-button
                type="primary"
                size="small"
                :disabled="!selectedMaskTexts.length || !originalText"
                @click="handleMaskSelected"
              >
                对勾选词打码（{{ selectedMaskTexts.length }}）
              </el-button>
              <el-button
                size="small"
                @click="clearMaskSelection"
                :disabled="!selectedMaskTexts.length"
              >
                清空勾选
              </el-button>
            </el-space>
          </div>
          <el-select
            v-model="selectedMaskTexts"
            multiple
            collapse-tags
            collapse-tags-tooltip
            filterable
            clearable
            class="mask-select"
            placeholder="选择原文命中词（同词已自动合并）"
          >
            <el-option
              v-for="item in maskCandidateList"
              :key="item.matchedText"
              :label="item.optionLabel"
              :value="item.matchedText"
            />
          </el-select>

          <div class="quick-mask-row">
            <div class="quick-mask-title">
              <span class="quick-mask-row__label">一键打码</span>
              <el-tooltip
                effect="light"
                placement="top"
                popper-class="section-help-popper"
              >
                <template #content>
                  <div class="section-help-pop">
                    <div>1. 按风险级别批量处理全部命中词，默认高/中/低风险全选。</div>
                    <div>2. 可先取消某些风险级别，再点击“一键打码”只处理保留级别。</div>
                    <div>3. 打码后可在下方“打码文本”中点击高亮片段，单独切换打码/取消打码。</div>
                  </div>
                </template>
                <span class="rate-help-trigger">
                  <el-icon><QuestionFilled /></el-icon>
                </span>
              </el-tooltip>
            </div>
            <span class="quick-mask-row__sub-label">风险级别</span>
            <el-checkbox-group
              v-model="quickMaskLevels"
              size="small"
            >
              <el-checkbox label="high">高风险</el-checkbox>
              <el-checkbox label="medium">中风险</el-checkbox>
              <el-checkbox label="low">低风险</el-checkbox>
            </el-checkbox-group>
            <el-button
              type="warning"
              size="small"
              :disabled="!originalText || !quickMaskLevels.length"
              @click="handleMaskAllByRisk"
            >
              一键打码
            </el-button>
          </div>

          <el-alert
            v-if="maskError"
            type="error"
            show-icon
            :closable="false"
            :description="maskError"
            style="margin-top: 10px"
          />

          <el-alert
            v-if="maskNotice"
            type="warning"
            show-icon
            :closable="false"
            :description="maskNotice"
            style="margin-top: 10px"
          />
        </div>
      </div>

      <div
        v-if="hasMaskPreview"
        class="masked-block"
      >
        <div class="masked-head">
          <h4>打码文本</h4>
          <el-tooltip :content="copySuccess ? '已复制' : '复制'" placement="top">
            <el-button
              text
              circle
              class="copy-icon-btn"
              @click="handleCopyMaskedText"
            >
              <el-icon>
                <component :is="copySuccess ? Check : CopyDocument" />
              </el-icon>
            </el-button>
          </el-tooltip>
        </div>
        <div class="preview-scroll">
          <div class="masked-text">
            <template
              v-for="seg in maskedPreviewSegments"
              :key="seg.key"
            >
              <span v-if="!seg.rangeKey">{{ seg.text }}</span>
              <span
                v-else
                :class="['masked-frag', { 'masked-frag--off': !seg.isMasked }]"
                @click="toggleMaskedRange(seg.rangeKey)"
              >
                {{ seg.text }}
              </span>
            </template>
          </div>
        </div>
      </div>

      <!-- 3.4 相似敏感词（在 has_sensitive=false 情况下也可能存在） -->
      <div
        v-if="!result.has_sensitive && result.similar_sensitive && result.similar_words && result.similar_words.length"
        class="similar-block"
      >
        <h4>检测到相似敏感词</h4>
        <el-alert
          type="warning"
          :closable="false"
          show-icon
          description="这些内容未命中严格敏感词，但与敏感词十分相似，建议人工复核。"
        />
        <el-space
          direction="vertical"
          fill
          style="margin-top: 8px"
        >
          <div
            v-for="s in result.similar_words"
            :key="s.sensitive_word + s.matched_text"
            class="word-item"
          >
            <span class="word-text">{{ s.sensitive_word }}</span>
            <span class="word-meta">
              <el-tag size="small" type="info">
                {{ s.category || '相似词' }}
              </el-tag>
              <span class="word-count">
                命中片段：
                <span class="highlight">{{ s.matched_text }}</span>
              </span>
            </span>
          </div>
        </el-space>
      </div>
      <!-- 3.4 敏感词高亮（单独区域 + 滚动条） -->
      <div
        v-if="result.has_sensitive && originalText && (highlightRanges.length || detectedWordList.length)"
        class="highlight-block"
        ref="highlightBlockRef"
      >
        <h4>敏感词高亮</h4>
        <div class="preview-scroll" ref="highlightScrollRef">
          <HighlightText
            :text="originalText"
            :words="detectedWordList"
            :ranges="highlightRanges"
            :active-range-key="activeJumpRangeKey"
          />
        </div>
      </div>
    </div>
  </el-card>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, CopyDocument, QuestionFilled } from '@element-plus/icons-vue'
import HighlightText from '@/components/HighlightText.vue'   // 新增这一行

const props = defineProps({
  loading: {
    type: Boolean,
    default: false,
  },
  result: {
    type: Object,
    default: null,
  },
  originalText: {
    type: String,
    default: '',
  },
})

const categoryMap = {
  political_high: '政治高敏感',
  political_low: '政治低敏感',
  political_person: '政治敏感人物',
  political_banned_books: '禁书',
  political_prohibited: '政治违禁词',
  violent_high: '暴恐高敏感',
  violent_low: '暴恐低敏感',
  violent_chemical: '化学药剂',
  pornographic_high: '涉黄高敏感',
  pornographic_low: '涉黄低敏感',
  abusive_high: '辱骂高敏感',
  abusive_low: '辱骂低敏感',
  advertising_high: '广告高敏感',
  advertising_low: '广告低敏感',
}

/**
 * 收集所有需要高亮的敏感词
 * 兼容两种结构：
 * 1) result.detected_words: [{ word, ... }, ...]
 * 2) result.categories: { cat: { words: [...] }, ... }
 */

// 词条排序：level(高>中>低) + count_raw(降序)
const normalizeWords = (words = []) => {
  const rank = { high: 3, medium: 2, low: 1 }
  return [...words].sort((a, b) => {
    const ra = rank[a?.level] || 0
    const rb = rank[b?.level] || 0
    if (ra !== rb) return rb - ra

    const ca = Number(a?.count_raw || 1)
    const cb = Number(b?.count_raw || 1)
    if (ca !== cb) return cb - ca

    // 最后按词本身排序，保证稳定输出
    return String(a?.word || '').localeCompare(String(b?.word || ''), 'zh-Hans-CN')
  })
}

const riskLevelText = computed(() => {
  if (!props.result) return '--'
  const mp = {
    high: '高风险',
    medium: '中风险',
    low: '低风险',
    safe: '安全',
  }
  return mp[props.result.risk_level] || props.result.risk_level || '--'
})

const llmAssist = computed(() => props.result?.llm_assist || null)

const llmAssistVisible = computed(() => {
  const enabledByOptions = !!props.result?.applied_options?.enable_llm_assist
  return enabledByOptions || !!llmAssist.value
})

const llmAssistError = computed(() => {
  const errorText = String(llmAssist.value?.error || '').trim()
  if (errorText) return errorText
  if (!llmAssist.value && props.result?.applied_options?.enable_llm_assist) {
    return '后端未返回辅助鉴别结果'
  }
  return ''
})

const llmAssistRisk = computed(() =>
  String(llmAssist.value?.risk_level || '').toLowerCase(),
)

const llmAssistRiskText = computed(() => {
  const mp = {
    safe: '安全',
    low: '低风险',
    medium: '中风险',
    high: '高风险',
  }
  return mp[llmAssistRisk.value] || '未知'
})

const llmAssistAlertType = computed(() => {
  if (llmAssistRisk.value === 'high') return 'error'
  if (llmAssistRisk.value === 'medium') return 'warning'
  if (llmAssistRisk.value === 'low') return 'info'
  if (llmAssistRisk.value === 'safe') return 'success'
  return 'info'
})

const llmAssistTagType = computed(() => {
  if (llmAssistRisk.value === 'high') return 'danger'
  if (llmAssistRisk.value === 'medium') return 'warning'
  if (llmAssistRisk.value === 'low') return 'info'
  if (llmAssistRisk.value === 'safe') return 'success'
  return 'info'
})

const llmAssistReviewText = computed(() =>
  llmAssist.value?.should_review ? '建议人工复核' : '可直接放行',
)

const llmAssistReason = computed(() =>
  String(llmAssist.value?.reason || '').trim(),
)

const llmAssistModel = computed(() =>
  String(llmAssist.value?.model || '').trim(),
)

const llmAssistConfidenceText = computed(() => {
  const value = Number(llmAssist.value?.confidence)
  if (!Number.isFinite(value)) return ''
  return `${(Math.max(0, Math.min(1, value)) * 100).toFixed(1)}%`
})

const llmAssistTerms = computed(() => {
  const list = llmAssist.value?.suspected_terms
  if (!Array.isArray(list) || !list.length) return []
  return Array.from(new Set(list.map((item) => String(item || '').trim()).filter(Boolean)))
})

/**
 * 收集所有需要高亮的敏感词
 * 兼容两种结构：
 * 1) result.detected_words: [{ word, ... }, ...]
 * 2) result.categories: { cat: { words: [...] }, ... }
 */

// 过滤掉HTML标签
// const cleanText = (text) => {
//   return text.replace(/<\/?[^>]+(>|$)/g, "")
// }

const toCount = (value) => {
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : 0
}

const selectedMaskTexts = ref([])
const quickMaskLevels = ref(['high', 'medium', 'low'])
const selectedRateKey = ref('')
const activeJumpRangeKey = ref('')
const jumpCursorMap = ref({})
const highlightBlockRef = ref(null)
const highlightScrollRef = ref(null)
const maskSourceText = ref('')
const maskRanges = ref([])
const revealedRangeKeys = ref([])
const copySuccess = ref(false)
const maskError = ref('')
const maskNotice = ref('')
let copyTimer = null

const riskRank = { high: 3, medium: 2, low: 1 }

const riskLevelName = (level) => {
  const key = String(level || '').toLowerCase()
  if (key === 'high') return '高风险'
  if (key === 'medium') return '中风险'
  return '低风险'
}

const normalizeRiskLevel = (level, category = '') => {
  const lv = String(level || '').toLowerCase()
  if (lv === 'high' || lv === 'medium' || lv === 'low') return lv
  const cat = String(category || '').toLowerCase()
  if (
    cat.includes('high') ||
    cat.includes('banned_books') ||
    cat.includes('prohibited') ||
    cat.includes('person')
  ) {
    return 'high'
  }
  return 'low'
}

const normalizeMatchedText = (text) => {
  const value = String(text || '').trim()
  return value
}

const toRuneLength = (text) => Array.from(String(text || '')).length

const safeSliceByRune = (text, start, end) => {
  const runes = Array.from(String(text || ''))
  const s = Number.isFinite(start) ? Math.max(0, start) : 0
  const e = Number.isFinite(end) ? Math.min(runes.length, end) : runes.length
  if (s >= e || s >= runes.length) return ''
  return runes.slice(s, e).join('')
}

const detectedWordList = computed(() => {
  const r = props.result
  if (!r) return []

  if (Array.isArray(r.detected_words) && r.detected_words.length) {
    return r.detected_words
  }

  if (r.categories) {
    const all = []
    Object.values(r.categories).forEach((cat) => {
      if (Array.isArray(cat.words)) {
        all.push(...cat.words)
      }
    })
    return all
  }
  return []
})

const evidenceHitList = computed(() => {
  const evidences = props.result?.hit_evidences
  if (!Array.isArray(evidences) || !evidences.length) return []

  const source = String(props.originalText || '')
  const out = []
  evidences.forEach((ev) => {
    const word = String(ev?.word || '').trim()
    const category = String(ev?.category || '').trim()
    const start = Number(ev?.start)
    const end = Number(ev?.end)
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return
    const matchedText =
      String(ev?.matched_text || '') || safeSliceByRune(source, start, end)
    out.push({
      word,
      category,
      start,
      end,
      matchedText,
      riskLevel: normalizeRiskLevel(ev?.risk_level, category),
    })
  })
  return out
})

const mergedRangeHitList = computed(() => {
  if (!evidenceHitList.value.length) return []

  const source = String(props.originalText || '')
  const merged = new Map()
  evidenceHitList.value.forEach((ev) => {
    const key = `${ev.start}:${ev.end}`
    const current = merged.get(key) || {
      start: ev.start,
      end: ev.end,
      matchedText: safeSliceByRune(source, ev.start, ev.end) || ev.matchedText,
      riskLevel: ev.riskLevel,
      categories: new Set(),
      sensitiveWords: new Set(),
    }
    current.categories.add(ev.category)
    if (ev.word) current.sensitiveWords.add(ev.word)
    if ((riskRank[ev.riskLevel] || 0) > (riskRank[current.riskLevel] || 0)) {
      current.riskLevel = ev.riskLevel
    }
    if (!current.matchedText) {
      current.matchedText = safeSliceByRune(source, ev.start, ev.end) || ev.matchedText
    }
    merged.set(key, current)
  })

  return Array.from(merged.values()).sort((a, b) => {
    if (a.start !== b.start) return a.start - b.start
    return a.end - b.end
  })
})

const categoryMergedHitListMap = computed(() => {
  if (!evidenceHitList.value.length) return {}

  const source = String(props.originalText || '')
  const out = {}
  evidenceHitList.value.forEach((ev) => {
    const cat = ev.category || 'unknown'
    const matchedText = safeSliceByRune(source, ev.start, ev.end) || ev.matchedText
    const textKey = normalizeMatchedText(matchedText) || `${ev.start}:${ev.end}`
    if (!out[cat]) out[cat] = new Map()

    const current = out[cat].get(textKey) || {
      start: ev.start,
      end: ev.end,
      matchedText,
      riskLevel: ev.riskLevel,
      sensitiveWords: new Set(),
      occurrenceCount: 0,
    }
    current.occurrenceCount += 1
    if (ev.word) current.sensitiveWords.add(ev.word)
    if ((riskRank[ev.riskLevel] || 0) > (riskRank[current.riskLevel] || 0)) {
      current.riskLevel = ev.riskLevel
    }
    out[cat].set(textKey, current)
  })

  const normalized = {}
  Object.entries(out).forEach(([cat, hitMap]) => {
    normalized[cat] = Array.from(hitMap.values()).sort((a, b) => {
      if (a.start !== b.start) return a.start - b.start
      return a.end - b.end
    })
  })
  return normalized
})

const displayCategories = computed(() => {
  const merged = categoryMergedHitListMap.value
  if (Object.keys(merged).length) {
    const out = {}
    Object.entries(merged).forEach(([cat, hits]) => {
      const stats = { high: 0, medium: 0, low: 0 }
      const words = hits.map((hit) => {
        const level = normalizeRiskLevel(hit.riskLevel, cat)
        stats[level] = (stats[level] || 0) + 1
        return {
          word: hit.matchedText,
          category: cat,
          level,
          count_raw: Number(hit.occurrenceCount || 1),
          occurrence_count: Number(hit.occurrenceCount || 1),
          sensitive_words: Array.from(hit.sensitiveWords || []),
        }
      })

      out[cat] = {
        count: words.length,
        words,
        stats,
      }
    })
    return out
  }

  const fallback = props.result?.categories
  if (fallback && Object.keys(fallback).length) return fallback
  return {}
})

const displayCategorySummary = computed(() => {
  const summary = {}
  Object.entries(displayCategories.value || {}).forEach(([cat, data]) => {
    const stats = data?.stats || {}
    summary[cat] = {
      total: Number(data?.count || data?.words?.length || 0),
      high: Number(stats.high || 0),
      medium: Number(stats.medium || 0),
      low: Number(stats.low || 0),
    }
  })
  return summary
})

const highlightRanges = computed(() =>
  mergedRangeHitList.value.map((hit) => ({
    key: `${hit.start}:${hit.end}`,
    start: hit.start,
    end: hit.end,
  })),
)

const replaceCharMap = {
  '@': 'a',
  '4': 'a',
  '0': 'o',
  '1': 'i',
  '!': 'i',
  '3': 'e',
  '5': 's',
  '$': 's',
  '7': 't',
  '+': 't',
  '8': 'b',
}

const canonicalizeForMaskMatch = (text) => {
  const raw = Array.from(String(text || ''))
  const out = []
  raw.forEach((ch) => {
    let c = ch
    const code = c.charCodeAt(0)
    if (code === 12288) c = ' '
    if (code >= 65281 && code <= 65374) c = String.fromCharCode(code - 65248)
    c = c.toLowerCase()

    if (replaceCharMap[c]) {
      c = replaceCharMap[c]
    }

    if (/\s/.test(c)) return
    if (/-|_|\\|\/|\.|,|，|。|!|！|\?|？|:|：|;|；|'|"|`|~|·|\(|\)|（|）|\[|\]|\{|\}|<|>|《|》|=|%|\^|&|#|\$/.test(c)) {
      return
    }
    out.push(c)
  })
  return out.join('')
}

const sortSensitiveWords = (words = []) => {
  const uniq = Array.from(new Set((Array.isArray(words) ? words : []).map((w) => normalizeMatchedText(w)).filter(Boolean)))
  return uniq.sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
}

const resolveSensitiveWordList = (category, wordHit) => {
  const fromPayload = sortSensitiveWords(
    wordHit?.sensitive_words || wordHit?.sensitiveWords || [],
  )
  if (fromPayload.length) return fromPayload

  const sourceText = normalizeMatchedText(wordHit?.word || wordHit)
  if (!sourceText) return []

  const source = String(props.originalText || '')
  const canon = canonicalizeForMaskMatch(sourceText)
  const matched = new Set()

  evidenceHitList.value.forEach((ev) => {
    if (category && String(ev.category || '') !== String(category || '')) return
    const matchedText = normalizeMatchedText(ev.matchedText || safeSliceByRune(source, ev.start, ev.end))
    const sensitiveWord = normalizeMatchedText(ev.word)
    if (!sensitiveWord) return

    const hitByText = matchedText === sourceText || sensitiveWord === sourceText
    const hitByCanon =
      !!canon &&
      (canonicalizeForMaskMatch(matchedText) === canon || canonicalizeForMaskMatch(sensitiveWord) === canon)

    if (!hitByText && !hitByCanon) return
    matched.add(sensitiveWord)
  })

  return sortSensitiveWords(Array.from(matched))
}

const getSensitiveWordHint = (category, wordHit) => {
  const words = resolveSensitiveWordList(category, wordHit)
  if (!words.length) return '暂无词库命中详情'
  const limit = 12
  const clipped = words.slice(0, limit)
  return `${clipped.join(' / ')}${words.length > limit ? ` 等${words.length}个` : ''}`
}

const collectJumpRanges = (category, wordText) => {
  const text = normalizeMatchedText(wordText)
  if (!text) return []

  const source = String(props.originalText || '')
  const canon = canonicalizeForMaskMatch(text)
  const uniq = new Map()

  evidenceHitList.value.forEach((ev) => {
    if (category && String(ev.category || '') !== String(category || '')) return

    const matched = normalizeMatchedText(ev.matchedText || safeSliceByRune(source, ev.start, ev.end))
    const sensitive = normalizeMatchedText(ev.word)
    if (!matched && !sensitive) return

    const hitByText = matched === text || sensitive === text
    const hitByCanon =
      !!canon &&
      (canonicalizeForMaskMatch(matched) === canon || canonicalizeForMaskMatch(sensitive) === canon)

    if (!hitByText && !hitByCanon) return
    const key = `${ev.start}:${ev.end}`
    if (uniq.has(key)) return
    uniq.set(key, {
      key,
      start: ev.start,
      end: ev.end,
    })
  })

  return Array.from(uniq.values()).sort((a, b) => a.start - b.start)
}

const scrollToHighlightRange = async (rangeKey) => {
  if (!rangeKey) return
  const block = highlightBlockRef.value
  if (block?.scrollIntoView) {
    block.scrollIntoView({
      behavior: 'smooth',
      block: 'center',
    })
  }

  await nextTick()
  const scrollBox = highlightScrollRef.value
  if (!scrollBox) return
  const target = scrollBox.querySelector(`[data-range-key="${rangeKey}"]`)
  if (!target) return

  const boxRect = scrollBox.getBoundingClientRect()
  const targetRect = target.getBoundingClientRect()
  const offset = targetRect.top - boxRect.top - scrollBox.clientHeight / 2 + targetRect.height / 2
  scrollBox.scrollTo({
    top: scrollBox.scrollTop + offset,
    behavior: 'smooth',
  })
}

const handleWordTagJump = async (category, wordText) => {
  const ranges = collectJumpRanges(category, wordText)
  if (!ranges.length) {
    ElMessage.warning('未找到原文中的对应位置')
    return
  }

  const cursorKey = `${category || ''}@@${normalizeMatchedText(wordText)}`
  const currentCursor = Number(jumpCursorMap.value[cursorKey] ?? -1)
  const nextCursor = (currentCursor + 1) % ranges.length
  jumpCursorMap.value = {
    ...jumpCursorMap.value,
    [cursorKey]: nextCursor,
  }

  const target = ranges[nextCursor]
  activeJumpRangeKey.value = target.key
  await scrollToHighlightRange(target.key)
}

const maskCandidateList = computed(() => {
  const candidates = new Map()
  const addCandidate = (matchedText, sensitiveWord, category, riskLevel) => {
    const text = normalizeMatchedText(matchedText)
    const word = normalizeMatchedText(sensitiveWord)
    if (!text || !word) return

    const lv = normalizeRiskLevel(riskLevel, category)
    const key = text
    const current = candidates.get(key) || {
      matchedText: text,
      riskLevels: new Set(),
      sensitiveWords: new Set(),
      categories: new Set(),
    }
    current.riskLevels.add(lv)
    current.sensitiveWords.add(word)
    if (category) current.categories.add(String(category))
    candidates.set(key, current)
  }

  if (mergedRangeHitList.value.length) {
    mergedRangeHitList.value.forEach((item) => {
      const words = Array.from(item.sensitiveWords || [])
      if (!words.length) {
        addCandidate(item.matchedText, item.matchedText, '', item.riskLevel)
        return
      }
      words.forEach((word) => {
        addCandidate(item.matchedText, word, '', item.riskLevel)
      })
    })
  } else if (Array.isArray(props.result?.hit_evidences) && props.result.hit_evidences.length) {
    props.result.hit_evidences.forEach((ev) => {
      addCandidate(ev?.matched_text, ev?.word, ev?.category, ev?.risk_level)
    })
  } else {
    const suggestions = props.result?.mask_suggestions
    if (!Array.isArray(suggestions) || !suggestions.length) return []
    suggestions.forEach((item) => {
      const sensitiveWord = item?.sensitive_word || item?.word
      const matchedTexts = Array.isArray(item?.matched_texts) ? item.matched_texts : []
      matchedTexts.forEach((rawText) => {
        addCandidate(rawText, sensitiveWord, item?.category, item?.risk_level)
      })
    })
  }

  return Array.from(candidates.values())
    .map((item) => {
      const riskLevels = Array.from(item.riskLevels)
      const sensitiveWords = Array.from(item.sensitiveWords).sort((a, b) =>
        a.localeCompare(b, 'zh-Hans-CN'),
      )
      const categories = Array.from(item.categories)
      const highestRisk =
        riskLevels.sort((a, b) => (riskRank[b] || 0) - (riskRank[a] || 0))[0] || 'low'

      const wordsHint =
        sensitiveWords.length > 2
          ? `${sensitiveWords.slice(0, 2).join(' / ')} 等${sensitiveWords.length}个`
          : sensitiveWords.join(' / ')

      return {
        matchedText: item.matchedText,
        riskLevels,
        highestRisk,
        sensitiveWords,
        categories,
        optionLabel: `${item.matchedText} · ${riskLevelName(highestRisk)} · ${wordsHint}`,
      }
    })
    .sort((a, b) => {
      const ra = riskRank[a.highestRisk] || 0
      const rb = riskRank[b.highestRisk] || 0
      if (ra !== rb) return rb - ra
      const lenDiff = Array.from(b.matchedText).length - Array.from(a.matchedText).length
      if (lenDiff !== 0) return lenDiff
      return a.matchedText.localeCompare(b.matchedText, 'zh-Hans-CN')
    })
})

const textTotalCount = computed(() => {
  const r = props.result || {}
  const n = toCount(r.text_char_count ?? r.text_total_chars)
  if (n > 0) return n
  return toRuneLength(props.originalText)
})

const wordOccurrenceCount = (wordHit) =>
  toCount(
    wordHit?.occurrence_count ??
      wordHit?.count_raw ??
      wordHit?.total_count ??
      wordHit?.count ??
      1,
  )

const mergedRangeUnionLength = (ranges) => {
  if (!ranges.length) return 0
  const sorted = ranges
    .map((r) => ({
      start: Number(r.start),
      end: Number(r.end),
    }))
    .filter((r) => Number.isFinite(r.start) && Number.isFinite(r.end) && r.end > r.start)
    .sort((a, b) => a.start - b.start)

  if (!sorted.length) return 0
  let total = 0
  let curStart = sorted[0].start
  let curEnd = sorted[0].end

  for (let i = 1; i < sorted.length; i += 1) {
    const range = sorted[i]
    if (range.start > curEnd) {
      total += curEnd - curStart
      curStart = range.start
      curEnd = range.end
    } else if (range.end > curEnd) {
      curEnd = range.end
    }
  }
  total += curEnd - curStart
  return Math.max(0, total)
}

const sensitiveCharCount = computed(() =>
  mergedRangeUnionLength(mergedRangeHitList.value),
)

const riskCharCounts = computed(() => {
  const counts = { high: 0, medium: 0, low: 0 }
  if (!mergedRangeHitList.value.length) return counts

  mergedRangeHitList.value.forEach((hit) => {
    const lv = normalizeRiskLevel(hit?.riskLevel, '')
    const length = Math.max(0, Number(hit?.end) - Number(hit?.start))
    counts[lv] += Number.isFinite(length) ? length : 0
  })
  return counts
})

const totalSensitiveCount = computed(() => {
  if (mergedRangeHitList.value.length) return mergedRangeHitList.value.length
  if (!detectedWordList.value.length) return 0
  return detectedWordList.value.reduce(
    (sum, w) => sum + wordOccurrenceCount(w),
    0,
  )
})

const formatRate = (count, total) => {
  if (!total) return '0.00%'
  return `${((count / total) * 100).toFixed(2)}%`
}

const percentNumber = (count, total) => {
  if (!total) return 0
  const raw = (Number(count || 0) / Number(total || 0)) * 100
  if (!Number.isFinite(raw)) return 0
  return Math.max(0, Math.min(100, raw))
}

const riskRateItems = computed(() => [
  {
    label: '高风险敏感字符数 / 文本总字符数 = 高风险敏感词率',
    count: riskCharCounts.value.high,
    rate: formatRate(riskCharCounts.value.high, textTotalCount.value),
  },
  {
    label: '中风险敏感字符数 / 文本总字符数 = 中风险敏感词率',
    count: riskCharCounts.value.medium,
    rate: formatRate(riskCharCounts.value.medium, textTotalCount.value),
  },
  {
    label: '低风险敏感字符数 / 文本总字符数 = 低风险敏感词率',
    count: riskCharCounts.value.low,
    rate: formatRate(riskCharCounts.value.low, textTotalCount.value),
  },
])

const sensitiveRate = computed(() =>
  formatRate(sensitiveCharCount.value, textTotalCount.value),
)

const safeCharCount = computed(() =>
  Math.max(0, textTotalCount.value - sensitiveCharCount.value),
)

const safeRate = computed(() =>
  formatRate(safeCharCount.value, textTotalCount.value),
)

const rateVisualItems = computed(() => [
  {
    key: 'high',
    title: '高风险敏感字符',
    count: riskCharCounts.value.high,
    rate: formatRate(riskCharCounts.value.high, textTotalCount.value),
    percent: percentNumber(riskCharCounts.value.high, textTotalCount.value),
  },
  {
    key: 'medium',
    title: '中风险敏感字符',
    count: riskCharCounts.value.medium,
    rate: formatRate(riskCharCounts.value.medium, textTotalCount.value),
    percent: percentNumber(riskCharCounts.value.medium, textTotalCount.value),
  },
  {
    key: 'low',
    title: '低风险敏感字符',
    count: riskCharCounts.value.low,
    rate: formatRate(riskCharCounts.value.low, textTotalCount.value),
    percent: percentNumber(riskCharCounts.value.low, textTotalCount.value),
  },
  {
    key: 'safe',
    title: '非敏感字符',
    count: safeCharCount.value,
    rate: safeRate.value,
    percent: percentNumber(safeCharCount.value, textTotalCount.value),
  },
])

const rateRingStyle = computed(() => {
  const focused = selectedRateKey.value
  if (focused) {
    const item = rateVisualItems.value.find((it) => it.key === focused)
    const percent = item?.percent || 0
    const colorMap = {
      high: 'var(--ring-high)',
      medium: 'var(--ring-medium)',
      low: 'var(--ring-low)',
      safe: 'var(--ring-safe-active)',
    }
    const color = colorMap[focused] || 'var(--ring-high)'
    return {
      background: `conic-gradient(
        ${color} 0% ${percent}%,
        var(--ring-track) ${percent}% 100%
      )`,
    }
  }

  const total = textTotalCount.value || 1
  const high = percentNumber(riskCharCounts.value.high, total)
  const medium = percentNumber(riskCharCounts.value.medium, total)
  const low = percentNumber(riskCharCounts.value.low, total)
  const p1 = high
  const p2 = Math.min(100, p1 + medium)
  const p3 = Math.min(100, p2 + low)
  return {
    background: `conic-gradient(
      var(--ring-high) 0% ${p1}%,
      var(--ring-medium) ${p1}% ${p2}%,
      var(--ring-low) ${p2}% ${p3}%,
      var(--ring-safe-active) ${p3}% 100%
    )`,
  }
})

const ringCenterRate = computed(() => {
  if (!selectedRateKey.value) return sensitiveRate.value
  const item = rateVisualItems.value.find((it) => it.key === selectedRateKey.value)
  return item?.rate || sensitiveRate.value
})

const ringCenterLabel = computed(() => {
  const mp = {
    high: '高风险占比',
    medium: '中风险占比',
    low: '低风险占比',
    safe: '非敏感占比',
  }
  return mp[selectedRateKey.value] || '敏感词率'
})

const toggleRateFocus = (key) => {
  selectedRateKey.value = selectedRateKey.value === key ? '' : key
}

const clearMaskSelection = () => {
  selectedMaskTexts.value = []
}

const clearCopyState = () => {
  copySuccess.value = false
  if (copyTimer) {
    clearTimeout(copyTimer)
    copyTimer = null
  }
}

const resetMaskState = () => {
  selectedMaskTexts.value = []
  quickMaskLevels.value = ['high', 'medium', 'low']
  selectedRateKey.value = ''
  activeJumpRangeKey.value = ''
  jumpCursorMap.value = {}
  maskSourceText.value = ''
  maskRanges.value = []
  revealedRangeKeys.value = []
  maskError.value = ''
  maskNotice.value = ''
  clearCopyState()
}

watch(
  () => props.result,
  () => {
    resetMaskState()
  },
)

watch(
  () => props.originalText,
  () => {
    selectedRateKey.value = ''
    activeJumpRangeKey.value = ''
    jumpCursorMap.value = {}
    maskSourceText.value = ''
    maskRanges.value = []
    revealedRangeKeys.value = []
    maskError.value = ''
    maskNotice.value = ''
    clearCopyState()
  },
)

const escapeRegExp = (str) => String(str).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

const normalizeWordList = (words = []) => {
  const uniq = Array.from(
    new Set(
      words
        .map((w) => normalizeMatchedText(w))
        .filter(Boolean),
    ),
  )
  uniq.sort((a, b) => {
    const lenDiff = Array.from(b).length - Array.from(a).length
    if (lenDiff !== 0) return lenDiff
    return a.localeCompare(b, 'zh-Hans-CN')
  })
  return uniq
}

const buildMaskRangesFromHitRanges = (source, ranges = []) => {
  const rawText = String(source || '')
  if (!rawText || !ranges.length) return []

  const uniq = new Map()
  ranges.forEach((item) => {
    const start = Number(item?.start)
    const end = Number(item?.end)
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return
    uniq.set(`${start}:${end}`, { start, end })
  })

  const sorted = Array.from(uniq.values()).sort((a, b) => {
    if (a.start !== b.start) return a.start - b.start
    return a.end - b.end
  })
  if (!sorted.length) return []

  const merged = [sorted[0]]
  for (let i = 1; i < sorted.length; i += 1) {
    const cur = sorted[i]
    const last = merged[merged.length - 1]
    if (cur.start > last.end) {
      merged.push(cur)
      continue
    }
    if (cur.end > last.end) {
      last.end = cur.end
    }
  }

  return merged.map((range) => {
    const raw = safeSliceByRune(rawText, range.start, range.end)
    return {
      key: `${range.start}:${range.end}`,
      start: range.start,
      end: range.end,
      rawText: raw,
      maskedText: '*'.repeat(Math.max(1, toRuneLength(raw))),
    }
  })
}

const buildMaskRangesFromSelectedTexts = (source, words = []) => {
  if (!mergedRangeHitList.value.length) return []

  const directSet = new Set(
    words
      .map((w) => normalizeMatchedText(w))
      .filter(Boolean),
  )
  const canonicalSet = new Set(
    Array.from(directSet)
      .map((w) => canonicalizeForMaskMatch(w))
      .filter(Boolean),
  )

  const ranges = mergedRangeHitList.value
    .filter((hit) => {
      const text = normalizeMatchedText(hit.matchedText || safeSliceByRune(source, hit.start, hit.end))
      if (!text) return false
      if (directSet.has(text)) return true
      return canonicalSet.has(canonicalizeForMaskMatch(text))
    })
    .map((hit) => ({ start: hit.start, end: hit.end }))

  return buildMaskRangesFromHitRanges(source, ranges)
}

const buildMaskRangesByRiskLevels = (source, levels = []) => {
  if (!mergedRangeHitList.value.length) return []
  const levelSet = new Set(levels.map((lv) => String(lv || '').toLowerCase()))
  if (!levelSet.size) return []

  const ranges = mergedRangeHitList.value
    .filter((hit) => levelSet.has(normalizeRiskLevel(hit.riskLevel, '')))
    .map((hit) => ({ start: hit.start, end: hit.end }))

  return buildMaskRangesFromHitRanges(source, ranges)
}

const buildMaskRanges = (text, words) => {
  const rawText = String(text || '')
  if (!rawText) return []
  const uniqWords = normalizeWordList(words)
  if (!uniqWords.length) return []

  const pattern = uniqWords.map(escapeRegExp).join('|')
  if (!pattern) return []

  const re = new RegExp(pattern, 'g')
  const ranges = []
  for (const match of rawText.matchAll(re)) {
    const start = match.index ?? -1
    const matched = match[0]
    if (start < 0 || !matched) continue
    const end = start + matched.length
    ranges.push({
      key: `${start}:${end}`,
      start,
      end,
      rawText: matched,
      maskedText: '*'.repeat(Array.from(matched).length),
    })
  }
  return ranges
}

const applyMaskRanges = (source, ranges, noticePrefix) => {
  if (!ranges.length) return false
  maskSourceText.value = source
  maskRanges.value = ranges
  revealedRangeKeys.value = []
  maskNotice.value = `${noticePrefix}，共命中 ${ranges.length} 处`
  clearCopyState()
  return true
}

const applyMaskWords = (words, noticePrefix) => {
  const source = String(props.originalText || '')
  if (!source) return false

  const byHitRanges = buildMaskRangesFromSelectedTexts(source, words)
  if (byHitRanges.length) {
    return applyMaskRanges(source, byHitRanges, noticePrefix)
  }

  const ranges = buildMaskRanges(source, words) // 兼容无区间数据时的兜底逻辑
  if (!ranges.length) {
    maskError.value = '选中的词在原文中未命中'
    return false
  }

  return applyMaskRanges(source, ranges, noticePrefix)
}

const hasMaskPreview = computed(() =>
  Boolean(maskSourceText.value && maskRanges.value.length),
)

const revealedRangeSet = computed(() => new Set(revealedRangeKeys.value))

const maskedPreviewSegments = computed(() => {
  if (!hasMaskPreview.value) return []
  const text = maskSourceText.value
  const segments = []
  let cursor = 0

  maskRanges.value.forEach((range, index) => {
    if (range.start > cursor) {
      segments.push({
        key: `plain-${cursor}-${range.start}`,
        text: text.slice(cursor, range.start),
        rangeKey: '',
        isMasked: false,
      })
    }

    const isMasked = !revealedRangeSet.value.has(range.key)
    segments.push({
      key: `mask-${range.key}-${index}`,
      text: isMasked ? range.maskedText : range.rawText,
      rangeKey: range.key,
      isMasked,
    })
    cursor = range.end
  })

  if (cursor < text.length) {
    segments.push({
      key: `plain-${cursor}-end`,
      text: text.slice(cursor),
      rangeKey: '',
      isMasked: false,
    })
  }
  return segments
})

const maskedPreviewText = computed(() =>
  maskedPreviewSegments.value.map((seg) => seg.text).join(''),
)

const toggleMaskedRange = (rangeKey) => {
  if (!rangeKey) return
  const set = new Set(revealedRangeKeys.value)
  if (set.has(rangeKey)) {
    set.delete(rangeKey)
  } else {
    set.add(rangeKey)
  }
  revealedRangeKeys.value = Array.from(set)
}

const getQuickMaskTexts = () => {
  const selectedLevels = new Set(quickMaskLevels.value)
  if (!selectedLevels.size) return []

  return maskCandidateList.value
    .filter((item) => item.riskLevels.some((lv) => selectedLevels.has(lv)))
    .map((item) => item.matchedText)
}

const handleMaskSelected = () => {
  maskError.value = ''
  maskNotice.value = ''
  if (!props.originalText) return
  if (!selectedMaskTexts.value.length) {
    maskError.value = '请先选择要打码的原文命中词'
    return
  }

  applyMaskWords(selectedMaskTexts.value, `已完成勾选打码（${selectedMaskTexts.value.length} 个词）`)
}

const handleMaskAllByRisk = () => {
  maskError.value = ''
  maskNotice.value = ''
  if (!props.originalText) return
  if (!quickMaskLevels.value.length) {
    maskError.value = '请至少选择一个风险级别'
    return
  }

  const source = String(props.originalText || '')
  const rangesByRisk = buildMaskRangesByRiskLevels(source, quickMaskLevels.value)
  if (rangesByRisk.length) {
    const words = getQuickMaskTexts()
    selectedMaskTexts.value = words
    applyMaskRanges(source, rangesByRisk, `已按风险级别一键打码（${words.length} 个词）`)
    return
  }

  const words = getQuickMaskTexts()
  if (!words.length) {
    maskError.value = '当前风险级别下没有可打码的命中词'
    return
  }

  selectedMaskTexts.value = words
  applyMaskWords(words, `已按风险级别一键打码（${words.length} 个词）`)
}

const fallbackCopy = (text) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  document.execCommand('copy')
  document.body.removeChild(textarea)
}

const handleCopyMaskedText = async () => {
  if (!maskedPreviewText.value) return
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(maskedPreviewText.value)
    } else {
      fallbackCopy(maskedPreviewText.value)
    }
    clearCopyState()
    copySuccess.value = true
    copyTimer = setTimeout(() => {
      copySuccess.value = false
      copyTimer = null
    }, 1800)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}

onBeforeUnmount(() => {
  clearCopyState()
})

</script>

<style scoped>
.result-card {
  height: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
  color: var(--text-sub);
}

.status-tag {
  font-size: 12px;
}

.loading-wrap {
  padding: 8px 0;
}

.loading-text {
  margin-top: 8px;
  text-align: center;
  color: var(--text-sub);      /* 原来是 #999 */
}

.empty-wrap {
  padding: 24px 0;
  color: var(--text-sub);
}

.result-wrap > * + * {
  margin-top: 16px;
}

.rate-summary h4,
.llm-assist-block h4,
.category-summary h4,
.words-block h4,
.masked-block h4,
.similar-block h4,
.preview-block h4 {
  margin: 0 0 8px;
  font-size: 14px;
  color: var(--text-sub);     /* 标题用主文字色 */
}

.llm-assist-terms {
  margin-top: 10px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.llm-assist-terms__label {
  font-size: 12px;
  color: var(--text-sub);
}

.rate-summary {
  --ring-high: #ef4444;
  --ring-medium: #f59e0b;
  --ring-low: #3b82f6;
  --ring-track: #e2e8f0;
  --ring-safe-active: #10b981;
}

.rate-title-row {
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.rate-title-row h4 {
  margin: 0;
}

.section-title-row {
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-title-row h4 {
  margin: 0;
}

.section-title-row--actions {
  justify-content: flex-start;
  gap: 10px;
  flex-wrap: wrap;
}

.section-title-left {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.mask-title-actions {
  margin-left: 0;
  flex-wrap: wrap;
  justify-content: flex-start;
}

.section-help-pop,
:deep(.section-help-popper .section-help-pop) {
  max-width: 420px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  line-height: 1.55;
  color: #334155;
}

.word-hit-detail-pop,
:deep(.word-hit-detail-popper .word-hit-detail-pop) {
  max-width: 360px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: #334155;
}

.word-hit-detail-pop__title,
:deep(.word-hit-detail-popper .word-hit-detail-pop__title) {
  font-weight: 700;
  color: #1f2937;
}

.word-hit-detail-pop__line,
:deep(.word-hit-detail-popper .word-hit-detail-pop__line) {
  color: #475569;
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

.rate-formula-pop,
:deep(.rate-formula-popper .rate-formula-pop) {
  max-width: 440px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.rate-formula-pop__item,
:deep(.rate-formula-popper .rate-formula-pop__item) {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
  line-height: 1.45;
  color: #334155;
}

.rate-formula-pop__item strong,
:deep(.rate-formula-popper .rate-formula-pop__item strong) {
  color: #0f172a;
  font-weight: 700;
  white-space: nowrap;
}

.rate-formula-pop__item--total strong,
:deep(.rate-formula-popper .rate-formula-pop__item--total strong) {
  color: #e11d48;
}

.rate-dashboard {
  display: grid;
  grid-template-columns: minmax(205px, 230px) minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}

.rate-ring-card {
  border: 1px solid rgba(148, 163, 184, 0.28);
  border-radius: 12px;
  background: linear-gradient(145deg, rgba(59, 130, 246, 0.07), rgba(16, 185, 129, 0.05));
  padding: 10px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-start;
  gap: 9px;
  align-self: start;
}

.rate-ring {
  width: 126px;
  height: 126px;
  border-radius: 999px;
  padding: 8px;
  display: grid;
  place-items: center;
}

.rate-ring__inner {
  width: 100%;
  height: 100%;
  border-radius: inherit;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  background: #fff;
  border: 1px solid rgba(148, 163, 184, 0.2);
}

.rate-ring__value {
  font-size: 20px;
  font-weight: 700;
  line-height: 1;
  background: linear-gradient(135deg, #ef4444, #3b82f6);
  -webkit-background-clip: text;
  color: transparent;
}

.rate-ring__label {
  margin-top: 3px;
  font-size: 11px;
  color: var(--text-sub);
}

.rate-ring-legend {
  width: 100%;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
  align-content: start;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--text-sub);
  opacity: 0.86;
  padding: 3px 5px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.55);
  white-space: nowrap;
}

.legend-item--active {
  font-weight: 700;
  opacity: 1;
  background: rgba(59, 130, 246, 0.1);
}

.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
}

.legend-item--high .legend-dot {
  background: var(--ring-high);
}

.legend-item--medium .legend-dot {
  background: var(--ring-medium);
}

.legend-item--low .legend-dot {
  background: var(--ring-low);
}

.legend-item--safe .legend-dot {
  background: var(--ring-safe-active);
}

.rate-metric-panel {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  grid-auto-rows: minmax(98px, auto);
  gap: 8px;
  align-content: start;
  align-self: start;
}

.rate-metric {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 10px;
  padding: 8px 10px;
  background: rgba(255, 255, 255, 0.95);
  cursor: pointer;
  transition: 0.16s ease;
  display: grid;
  grid-template-rows: auto auto auto;
  gap: 6px;
}

.rate-metric:hover {
  border-color: rgba(59, 130, 246, 0.45);
  box-shadow: 0 2px 8px rgba(59, 130, 246, 0.12);
}

.rate-metric--active {
  border-color: rgba(59, 130, 246, 0.62);
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.12) inset;
}

.rate-metric__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--text-sub);
}

.rate-metric__head strong {
  font-size: 14px;
  color: var(--text-main);
}

.rate-metric__bar {
  height: 6px;
  border-radius: 999px;
  background: #edf2f7;
  overflow: hidden;
}

.rate-metric__bar > span {
  display: block;
  height: 100%;
  border-radius: inherit;
  transition: width 0.25s ease;
}

.rate-metric__foot {
  font-size: 11px;
  color: var(--text-sub);
}

.rate-metric--high .rate-metric__bar > span {
  background: linear-gradient(90deg, #fb7185, #ef4444);
}

.rate-metric--medium .rate-metric__bar > span {
  background: linear-gradient(90deg, #fbbf24, #f59e0b);
}

.rate-metric--low .rate-metric__bar > span {
  background: linear-gradient(90deg, #60a5fa, #2563eb);
}

.rate-metric--safe .rate-metric__bar > span {
  background: linear-gradient(90deg, #34d399, #10b981);
}

@media (max-width: 900px) {
  .rate-dashboard {
    grid-template-columns: 1fr;
  }
  .rate-metric-panel {
    grid-template-columns: 1fr;
  }
  .rate-ring {
    margin: 0 auto;
  }
}

.mask-actions {
  margin-top: 12px;
}

.mask-select {
  width: 100%;
}

.quick-mask-row {
  margin-top: 10px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}

.quick-mask-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.quick-mask-row__label {
  font-weight: 600;
  font-size: 12px;
  color: var(--text-sub);
}

.quick-mask-row__sub-label {
  font-size: 12px;
  color: var(--text-sub);
}

.masked-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.masked-head h4 {
  margin: 0;
}

.copy-icon-btn {
  width: 28px;
  height: 28px;
  border: 1px solid var(--border-subtle);
}

.masked-text {
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-sub);
}

.masked-frag {
  cursor: pointer;
  background: #fde68a;
  border-radius: 4px;
  padding: 0 2px;
  color: #92400e;
  transition: all 0.15s ease;
}

.masked-frag:hover {
  filter: brightness(0.97);
}

.masked-frag--off {
  background: rgba(59, 130, 246, 0.12);
  color: #1d4ed8;
}

.highlight-block h4 {
    color: var(--text-sub);
}

.preview-scroll {
  max-height: 240px;       /* 关键：限制高度 */
  overflow-y: auto;        /* 关键：出现滚动条 */
  padding: 10px;
  border-radius: 6px;
  border: 1px solid #eee;
  background: #fafafa;
  font-size: 13px;
  line-height: 1.6;
}

.cat-item {
  padding: 6px 8px;
  border-radius: 6px;
  background: #f7f7fb;
}

.cat-name {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--text-sub);
}

.cat-badges > * + * {
  margin-left: 4px;
}

.word-group + .word-group {
  margin-top: 12px;
}

.group-title {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--text-sub);
}

.group-count {
  margin-left: 6px;
  color: var(--text-sub);
  font-size: 12px;
}

.word-count {
  color: var(--text-sub);
}

.word-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid #eee;
  background: #fff;
}

.word-item + .word-item {
  margin-top: 6px;
}

.word-text {
  font-weight: 600;
  color: var(--text-sub);
}

.word-text--jump {
  cursor: pointer;
  transition: color 0.15s ease;
}

.word-text--jump:hover {
  color: #2563eb;
}

.word-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}

.word-count .highlight {
  background: #ffe58f;
  padding: 0 3px;
  border-radius: 3px;
}

/* 预览区域背景用浅 / 深都比较舒服的颜色 */
.preview-scroll,
.preview-box {
  max-height: 240px;
  overflow-y: auto;
  padding: 10px;
  border-radius: 6px;
  border: 1px solid var(--border-soft);
  background: rgba(248, 250, 252, 0.96);
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-sub);
}

/* ===== 命中敏感词：卡片 + 标签网格（更整齐） ===== */
.word-col {
  display: flex;
}

.word-card {
  width: 100%;
  height: 100%;
  border-radius: 12px;
  border: 1px solid rgba(148, 163, 184, 0.25);
  background: rgba(255, 255, 255, 0.92);
}

/* 让卡片内部 padding 更紧凑，整体更像“统计面板” */
.word-card :deep(.el-card__header) {
  padding: 12px 14px;
}

.word-card :deep(.el-card__body) {
  padding: 12px 14px 14px;
}

.word-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.word-card__title {
  font-weight: 600;
  color: var(--text-sub);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 标签改为网格布局：每行对齐、宽度一致 */
.word-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(92px, 1fr));
  gap: 10px;

  /* 类别词多时保持卡片高度更一致 */
  max-height: 170px;
  overflow: auto;
  padding-right: 2px;
}

.word-tag {
  width: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;

  box-sizing: border-box;
  border-radius: 8px;
}

.word-tag--jump {
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}

.word-tag--jump:hover {
  transform: translateY(-1px);
  box-shadow: 0 3px 8px rgba(59, 130, 246, 0.2);
}

.word-tag__text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.word-tag__count {
  flex: none;
  opacity: 0.75;
  font-size: 12px;
}

/* 深色模式兼容：如果你全局用 CSS 变量做暗色，这里尽量不写死颜色 */
@media (prefers-color-scheme: dark) {
  .word-card {
    background: rgba(15, 23, 42, 0.55);
    border-color: rgba(148, 163, 184, 0.25);
  }
}

.word-grid::-webkit-scrollbar {
  width: 6px;
}
.word-grid::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.35);
  border-radius: 6px;
}
.word-grid::-webkit-scrollbar-track {
  background: transparent;
}
</style>
