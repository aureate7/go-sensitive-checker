package main

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func registerAdminRoutes(r *gin.Engine, manager *adminManager, token string) {
	admin := r.Group("/api/admin")
	admin.Use(func(c *gin.Context) {
		if subtleTokenMismatch(c.GetHeader("Authorization"), token) {
			writeAPIError(c, http.StatusUnauthorized, "UNAUTHORIZED", "管理令牌无效", nil)
			c.Abort()
			return
		}
		c.Next()
	})

	admin.GET("/words", func(c *gin.Context) {
		result, err := manager.listWords(strings.TrimSpace(c.Query("category")), c.Query("q"), queryInt(c, "page", 1), queryInt(c, "page_size", 50))
		if err != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_CATEGORY", "检测类别无效", nil)
			return
		}
		c.JSON(http.StatusOK, result)
	})

	admin.POST("/words", func(c *gin.Context) {
		var req wordMutationRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "请求体不是有效 JSON", nil)
			return
		}
		req.Word = strings.TrimSpace(req.Word)
		if req.Word == "" || len([]rune(req.Word)) > 256 {
			writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_WORD", "词条为空或超过 256 个字符", nil)
			return
		}
		if _, exists := manager.service.detector().sensitiveWords[req.Category][req.Word]; exists {
			writeAPIError(c, http.StatusConflict, "WORD_EXISTS", "词条已经存在", nil)
			return
		}
		status, version, err := manager.mutate(req.Category, []string{req.Word}, "")
		entry := auditFromContext(c, "word.create", req.Category, req.Word, req.Reason, version, err)
		manager.audit(entry)
		if err != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "WORD_CREATE_FAILED", "新增词条失败", nil)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"created": true, "version": version, "wordlist": status})
	})

	admin.DELETE("/words", func(c *gin.Context) {
		var req wordMutationRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "请求体不是有效 JSON", nil)
			return
		}
		req.Word = strings.TrimSpace(req.Word)
		status, version, err := manager.mutate(req.Category, nil, req.Word)
		manager.audit(auditFromContext(c, "word.delete", req.Category, req.Word, req.Reason, version, err))
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, http.StatusNotFound, "WORD_NOT_FOUND", "词条不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "WORD_DELETE_FAILED", "删除词条失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true, "version": version, "wordlist": status})
	})

	admin.POST("/words/import/preview", func(c *gin.Context) {
		var req importRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "请求体不是有效 JSON", nil)
			return
		}
		detector := manager.service.detector()
		preview, err := parseImport(req.Category, req.Content, detector.sensitiveWords[req.Category])
		if err != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "IMPORT_INVALID", "导入内容无效", nil)
			return
		}
		c.JSON(http.StatusOK, preview)
	})

	admin.POST("/words/import/apply", func(c *gin.Context) {
		var req importRequest
		if c.ShouldBindJSON(&req) != nil {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "请求体不是有效 JSON", nil)
			return
		}
		detector := manager.service.detector()
		preview, err := parseImport(req.Category, req.Content, detector.sensitiveWords[req.Category])
		if err != nil || len(preview.Valid) == 0 {
			writeAPIError(c, http.StatusUnprocessableEntity, "IMPORT_EMPTY", "没有可导入的新词条", preview)
			return
		}
		status, version, mutationErr := manager.mutate(req.Category, preview.Valid, "")
		manager.audit(auditFromContext(c, "word.import", req.Category, "", req.Reason, version, mutationErr))
		if mutationErr != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "IMPORT_FAILED", "导入发布失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"imported": len(preview.Valid), "duplicates": len(preview.Duplicates), "version": version, "wordlist": status})
	})

	admin.GET("/wordlists/versions", func(c *gin.Context) {
		versions, err := manager.versions()
		if err != nil {
			writeAPIError(c, http.StatusInternalServerError, "VERSIONS_READ_FAILED", "读取版本失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": versions})
	})

	admin.POST("/wordlists/rollback/:version", func(c *gin.Context) {
		version := c.Param("version")
		status, err := manager.rollback(version)
		manager.audit(auditFromContext(c, "wordlist.rollback", "", "", "", version, err))
		if errors.Is(err, os.ErrNotExist) {
			writeAPIError(c, http.StatusNotFound, "VERSION_NOT_FOUND", "词库版本不存在", nil)
			return
		}
		if err != nil {
			writeAPIError(c, http.StatusUnprocessableEntity, "ROLLBACK_FAILED", "回滚失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"rolled_back": true, "version": version, "wordlist": status})
	})

	admin.GET("/audit", func(c *gin.Context) {
		entries, err := manager.readAudit(queryInt(c, "limit", 100))
		if err != nil {
			writeAPIError(c, http.StatusInternalServerError, "AUDIT_READ_FAILED", "读取审计日志失败", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": entries})
	})
}

func auditFromContext(c *gin.Context, action, category, word, reason, version string, err error) auditEntry {
	entry := auditEntry{Action: action, Category: category, Word: word, Reason: strings.TrimSpace(reason), Version: version, RequestID: c.GetString("request_id"), RemoteIP: c.ClientIP(), Success: err == nil}
	if err != nil {
		entry.Error = err.Error()
	}
	return entry
}
