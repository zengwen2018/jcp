//go:build web

package webapi

import (
	"encoding/json"

	"github.com/run-bigpig/jcp/internal/models"
)

// maskAPIKey 对 API Key 做脱敏处理。
//
// Web 模式下浏览器只需要知道"是否已配置"，不需要拿到明文。
// 因此读取配置时统一把 Key 替换为掩码，
// 保存时若收到的值等于掩码，则保留原有明文（避免被覆盖清空）。
const maskToken = "********"

// maskSecret 掩码一个密钥字段。
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return maskToken
}

// MaskConfig 返回脱敏后的配置副本。
//
// 不修改原始配置对象——它仍需保留明文供后端调用大模型。
func MaskConfig(cfg *models.AppConfig) *models.AppConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	// 复制切片，避免污染原配置。
	clone.AIConfigs = make([]models.AIConfig, len(cfg.AIConfigs))
	copy(clone.AIConfigs, cfg.AIConfigs)
	for i := range clone.AIConfigs {
		clone.AIConfigs[i].APIKey = maskSecret(clone.AIConfigs[i].APIKey)
		clone.AIConfigs[i].CredentialsJSON = maskSecret(clone.AIConfigs[i].CredentialsJSON)
	}
	return &clone
}

// RestoreSecrets 将脱敏后的配置合并回原始配置。
//
// 规则：
//   - 若传入的密钥为掩码或空，则沿用原配置中的明文；
//   - 否则视为用户新填写的密钥，覆盖原值。
//
// 这样前端保存其它设置时不会误清空已配置的 Key。
func RestoreSecrets(original *models.AppConfig, incoming *models.AppConfig) {
	if original == nil || incoming == nil {
		return
	}
	for i := range incoming.AIConfigs {
		if i >= len(original.AIConfigs) {
			break
		}
		orig := &original.AIConfigs[i]
		inc := &incoming.AIConfigs[i]

		if inc.APIKey == maskToken || inc.APIKey == "" {
			inc.APIKey = orig.APIKey
		}
		if inc.CredentialsJSON == maskToken || inc.CredentialsJSON == "" {
			inc.CredentialsJSON = orig.CredentialsJSON
		}
	}
}

// RequestSanitizer 返回请求体还原函数。
//
// getOriginal 用于取回当前配置中的明文密钥。
//
// 请求体有两种可能形态：
//   - 裸配置对象：{ "aiConfigs": [...], ... }
//   - 带包裹层：{ "data": { "aiConfigs": [...], ... } }
//
// 两种都要能识别，否则前端保存配置时会写入空结构。
func RequestSanitizer(getOriginal func() *models.AppConfig) func(body []byte) []byte {
	return func(body []byte) []byte {
		if len(body) == 0 {
			return body
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(body, &probe); err != nil {
			return body
		}

		// 情形一：带 data 包裹层。
		if raw, ok := probe["data"]; ok {
			var inner map[string]json.RawMessage
			if err := json.Unmarshal(raw, &inner); err != nil {
				return body
			}
			if _, hasAI := inner["aiConfigs"]; !hasAI {
				return body
			}
			// 路由器会把返回的 body 直接反序列化进目标类型，
			// 因此这里必须返回内层对象，而不是重新包一层。
			restored, ok := restoreConfigJSON(raw, getOriginal())
			if !ok {
				return body
			}
			return restored
		}

		// 情形二：裸配置对象。
		if _, hasAI := probe["aiConfigs"]; !hasAI {
			return body
		}
		restored, ok := restoreConfigJSON(body, getOriginal())
		if !ok {
			return body
		}
		return restored
	}
}

// restoreConfigJSON 还原配置 JSON 中的掩码密钥。
func restoreConfigJSON(raw []byte, original *models.AppConfig) ([]byte, bool) {
	if original == nil {
		return nil, false
	}
	var incoming models.AppConfig
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, false
	}
	if len(incoming.AIConfigs) == 0 {
		return nil, false
	}
	RestoreSecrets(original, &incoming)
	restored, err := json.Marshal(&incoming)
	if err != nil {
		return nil, false
	}
	return restored, true
}
