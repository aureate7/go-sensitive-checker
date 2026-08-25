import assert from 'node:assert/strict'
import test from 'node:test'

import { describeAPIError } from '../src/services/api.js'

test('describeAPIError preserves structured API details', () => {
  const result = describeAPIError({
    response: {
      data: { error: { code: 'INVALID_CATEGORY', message: '类别无效', request_id: 'req-1' } },
      headers: {},
    },
  })
  assert.deepEqual(result, { message: '类别无效', code: 'INVALID_CATEGORY', requestId: 'req-1' })
})

test('describeAPIError maps timeout to actionable message', () => {
  const result = describeAPIError({ code: 'ECONNABORTED' })
  assert.equal(result.code, 'ECONNABORTED')
  assert.match(result.message, /超时/)
})
